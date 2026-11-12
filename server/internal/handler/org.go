package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type orgUnitResponse struct {
	ID          string `json:"id"`
	ParentID    string `json:"parent_id"`
	Name        string `json:"name"`
	LeadUserID  string `json:"lead_user_id"`
	Description string `json:"description"`
	Position    int32  `json:"position"`
}

type orgAgentResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	UnitID string `json:"unit_id"`
	Can    string `json:"can"`
	Cannot string `json:"cannot"`
}

type orgTreeResponse struct {
	Units  []orgUnitResponse  `json:"units"`
	Agents []orgAgentResponse `json:"agents"`
}

type orgImportBody struct {
	Rows []service.OrgImportRow `json:"rows"`
}

type orgImportResponse struct {
	CreatedUnits   int      `json:"created_units"`
	ExistingUnits  int      `json:"existing_units"`
	AgentsAssigned int      `json:"agents_assigned"`
	UnknownAgents  []string `json:"unknown_agents"`
	UnknownLeads   []string `json:"unknown_leads"`
}

// ListOrgUnits returns the flat organisation tree plus every agent that has a
// unit. Clients assemble the tree; the payload stays stable when units move.
func (h *Handler) ListOrgUnits(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	units, err := h.Queries.ListOrgUnits(r.Context(), wsUUID)
	if err != nil {
		slog.Error("list org units failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list organisation units")
		return
	}
	agents, err := h.Queries.ListAgentsInOrgUnits(r.Context(), wsUUID)
	if err != nil {
		slog.Error("list org agents failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list organisation agents")
		return
	}
	resp := orgTreeResponse{
		Units:  make([]orgUnitResponse, 0, len(units)),
		Agents: make([]orgAgentResponse, 0, len(agents)),
	}
	for _, unit := range units {
		resp.Units = append(resp.Units, orgUnitResponse{
			ID:          uuidToString(unit.ID),
			ParentID:    uuidToString(unit.ParentID),
			Name:        unit.Name,
			LeadUserID:  uuidToString(unit.LeadUserID),
			Description: unit.Description,
			Position:    unit.Position,
		})
	}
	for _, agent := range agents {
		resp.Agents = append(resp.Agents, orgAgentResponse{
			ID:     uuidToString(agent.ID),
			Name:   agent.Name,
			UnitID: uuidToString(agent.OrgUnitID),
			Can:    textValue(agent.IntakeCan),
			Cannot: textValue(agent.IntakeCannot),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ImportOrgUnits upserts units from row paths, then assigns leads and agents.
// The whole import is one transaction: a partially applied tree would leave
// agents pointing at units the operator did not ask for.
func (h *Handler) ImportOrgUnits(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var body orgImportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	units, agents, err := service.PlanOrgImport(body.Rows)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start organisation import")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	result := orgImportResponse{UnknownAgents: []string{}, UnknownLeads: []string{}}
	resolved := map[string]pgtype.UUID{}
	for _, unit := range units {
		unitID, ok := h.upsertOrgUnitPath(r, w, qtx, wsUUID, unit.Path, resolved, &result)
		if !ok {
			return
		}
		if unit.LeadEmail == "" {
			continue
		}
		leadID, err := qtx.FindWorkspaceMemberByEmail(r.Context(), db.FindWorkspaceMemberByEmailParams{
			WorkspaceID: wsUUID,
			Lower:       strings.ToLower(unit.LeadEmail),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			result.UnknownLeads = append(result.UnknownLeads, unit.LeadEmail)
			continue
		}
		if err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		if err := qtx.SetOrgUnitLead(r.Context(), db.SetOrgUnitLeadParams{
			ID:          unitID,
			WorkspaceID: wsUUID,
			LeadUserID:  leadID,
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
	}
	for name, path := range agents {
		agentID, err := qtx.FindAgentByName(r.Context(), db.FindAgentByNameParams{
			WorkspaceID: wsUUID,
			Name:        name,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			result.UnknownAgents = append(result.UnknownAgents, name)
			continue
		}
		if err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		if err := qtx.SetAgentOrgUnit(r.Context(), db.SetAgentOrgUnitParams{
			ID:          agentID,
			WorkspaceID: wsUUID,
			OrgUnitID:   resolved[orgPathKey(path)],
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		result.AgentsAssigned++
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// upsertOrgUnitPath ensures every segment of path exists, returning the leaf.
// resolved carries ids across rows so a shared parent is looked up once.
func (h *Handler) upsertOrgUnitPath(
	r *http.Request,
	w http.ResponseWriter,
	qtx *db.Queries,
	wsUUID pgtype.UUID,
	path []string,
	resolved map[string]pgtype.UUID,
	result *orgImportResponse,
) (pgtype.UUID, bool) {
	parent := pgtype.UUID{}
	var leafID pgtype.UUID
	for depth := range path {
		key := orgPathKey(path[:depth+1])
		if id, ok := resolved[key]; ok {
			parent = id
			leafID = id
			continue
		}
		id, err := qtx.GetOrgUnitByParentAndName(r.Context(), db.GetOrgUnitByParentAndNameParams{
			WorkspaceID: wsUUID,
			ParentID:    parent,
			Name:        path[depth],
		})
		switch {
		case err == nil:
			result.ExistingUnits++
		case errors.Is(err, pgx.ErrNoRows):
			id, err = qtx.CreateOrgUnit(r.Context(), db.CreateOrgUnitParams{
				WorkspaceID: wsUUID,
				ParentID:    parent,
				Name:        path[depth],
			})
			if err != nil {
				h.importOrgFailed(w, r, err)
				return pgtype.UUID{}, false
			}
			result.CreatedUnits++
		default:
			h.importOrgFailed(w, r, err)
			return pgtype.UUID{}, false
		}
		resolved[key] = id
		parent = id
		leafID = id
	}
	return leafID, true
}

func (h *Handler) importOrgFailed(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("import org units failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "could not import organisation units")
}

func orgPathKey(path []string) string {
	return strings.Join(path, "\x00")
}

func textValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}
