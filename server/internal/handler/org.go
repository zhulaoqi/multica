package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
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
	Units     []orgUnitResponse  `json:"units"`
	Agents    []orgAgentResponse `json:"agents"`
	CanManage bool               `json:"can_manage"`
}

type orgAgentUnitBody struct {
	UnitID string `json:"unit_id"`
}

type orgUnitBody struct {
	Name       string  `json:"name"`
	ParentID   string  `json:"parent_id"`
	LeadUserID *string `json:"lead_user_id"`
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
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
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
		Units:     make([]orgUnitResponse, 0, len(units)),
		Agents:    make([]orgAgentResponse, 0, len(agents)),
		CanManage: roleAllowed(member.Role, "owner", "admin"),
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

// CreateOrgUnit adds one unit, optionally under a parent and with a lead.
func (h *Handler) CreateOrgUnit(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.orgManageWorkspace(w, r)
	if !ok {
		return
	}
	var body orgUnitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, ok := h.orgUnitName(w, body.Name)
	if !ok {
		return
	}
	parent := pgtype.UUID{}
	if body.ParentID != "" {
		parent, ok = h.orgUnitRef(w, r, wsUUID, body.ParentID)
		if !ok {
			return
		}
	}
	lead := pgtype.UUID{}
	if body.LeadUserID != nil && *body.LeadUserID != "" {
		lead, ok = h.orgLeadRef(w, r, wsUUID, *body.LeadUserID)
		if !ok {
			return
		}
	}
	id, err := h.Queries.CreateOrgUnit(r.Context(), db.CreateOrgUnitParams{
		WorkspaceID: wsUUID,
		ParentID:    parent,
		Name:        name,
		LeadUserID:  lead,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "同级的部门已有同名")
			return
		}
		h.importOrgFailed(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, http.StatusCreated, map[string]string{"id": uuidToString(id)})
}

// UpdateOrgUnit renames a unit and/or sets its lead; an empty lead clears it.
func (h *Handler) UpdateOrgUnit(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.orgManageWorkspace(w, r)
	if !ok {
		return
	}
	unitID, ok := h.orgUnitRef(w, r, wsUUID, chi.URLParam(r, "unitId"))
	if !ok {
		return
	}
	var body orgUnitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Name) == "" && body.LeadUserID == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}
	if strings.TrimSpace(body.Name) != "" {
		name, ok := h.orgUnitName(w, body.Name)
		if !ok {
			return
		}
		if err := h.Queries.RenameOrgUnit(r.Context(), db.RenameOrgUnitParams{
			ID:          unitID,
			WorkspaceID: wsUUID,
			Name:        name,
		}); err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "同级的部门已有同名")
				return
			}
			h.importOrgFailed(w, r, err)
			return
		}
	}
	if body.LeadUserID != nil {
		lead := pgtype.UUID{}
		if *body.LeadUserID != "" {
			lead, ok = h.orgLeadRef(w, r, wsUUID, *body.LeadUserID)
			if !ok {
				return
			}
		}
		if err := h.Queries.SetOrgUnitLead(r.Context(), db.SetOrgUnitLeadParams{
			ID:          unitID,
			WorkspaceID: wsUUID,
			LeadUserID:  lead,
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteOrgUnit removes an empty-leaved unit: agents in it become unassigned,
// while units below it must be moved or deleted first.
func (h *Handler) DeleteOrgUnit(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.orgManageWorkspace(w, r)
	if !ok {
		return
	}
	unitID, ok := h.orgUnitRef(w, r, wsUUID, chi.URLParam(r, "unitId"))
	if !ok {
		return
	}
	children, err := h.Queries.CountOrgUnitChildren(r.Context(), db.CountOrgUnitChildrenParams{
		WorkspaceID: wsUUID,
		ParentID:    unitID,
	})
	if err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	if children > 0 {
		writeError(w, http.StatusConflict, "先移走或删除下级部门")
		return
	}
	if err := h.Queries.ClearOrgUnitAgents(r.Context(), db.ClearOrgUnitAgentsParams{
		WorkspaceID: wsUUID,
		OrgUnitID:   unitID,
	}); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	if err := h.Queries.DeleteOrgUnit(r.Context(), db.DeleteOrgUnitParams{
		ID:          unitID,
		WorkspaceID: wsUUID,
	}); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// orgManageWorkspace gates unit editing to workspace owners and admins.
func (h *Handler) orgManageWorkspace(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return pgtype.UUID{}, false
	}
	return parseUUIDOrBadRequest(w, workspaceID, "workspace id")
}

func (h *Handler) orgUnitName(w http.ResponseWriter, raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return "", false
	}
	if len([]rune(name)) > service.OrgUnitNameMaxRunes {
		writeError(w, http.StatusBadRequest, "name is too long")
		return "", false
	}
	return name, true
}

func (h *Handler) orgUnitRef(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, raw string) (pgtype.UUID, bool) {
	id, ok := parseUUIDOrBadRequest(w, raw, "unit id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetOrgUnit(r.Context(), db.GetOrgUnitParams{ID: id, WorkspaceID: wsUUID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "organisation unit not found")
			return pgtype.UUID{}, false
		}
		h.importOrgFailed(w, r, err)
		return pgtype.UUID{}, false
	}
	return id, true
}

func (h *Handler) orgLeadRef(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, raw string) (pgtype.UUID, bool) {
	userID, ok := parseUUIDOrBadRequest(w, raw, "lead user id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "lead is not a member of this workspace")
			return pgtype.UUID{}, false
		}
		h.importOrgFailed(w, r, err)
		return pgtype.UUID{}, false
	}
	return userID, true
}

// SetOrgAgentUnit places an agent in a unit, or clears its unit when unit_id
// is empty. Agents stay in the workspace either way.
func (h *Handler) SetOrgAgentUnit(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var body orgAgentUnitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agent id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentUUID)
	if err != nil || agent.WorkspaceID != wsUUID {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	unitUUID := pgtype.UUID{}
	if body.UnitID != "" {
		unitUUID, ok = parseUUIDOrBadRequest(w, body.UnitID, "unit id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetOrgUnit(r.Context(), db.GetOrgUnitParams{
			ID:          unitUUID,
			WorkspaceID: wsUUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "organisation unit not found")
				return
			}
			h.importOrgFailed(w, r, err)
			return
		}
	}
	if err := h.Queries.SetAgentOrgUnit(r.Context(), db.SetAgentOrgUnitParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
		OrgUnitID:   unitUUID,
	}); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
