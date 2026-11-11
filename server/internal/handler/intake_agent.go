package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type intakeAgentBody struct {
	Name        string `json:"name"`
	Can         string `json:"can"`
	Cannot      string `json:"cannot"`
	RuntimeMode string `json:"runtime_mode"`
}

type intakePlacement struct {
	Project string `json:"project"`
	Product string `json:"product"`
	Module  string `json:"module"`
	Role    string `json:"role"`
}

type intakeAgentResponse struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Can         string            `json:"can"`
	Cannot      string            `json:"cannot"`
	RuntimeMode string            `json:"runtime_mode"`
	Online      bool              `json:"online"`
	ActiveCount int               `json:"active_count"`
	Placements  []intakePlacement `json:"placements"`
	OwnerID     string            `json:"owner_id"`
}

func (h *Handler) CreateIntakeAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var body intakeAgentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	can := strings.TrimSpace(body.Can)
	if strings.TrimSpace(body.Name) == "" || can == "" {
		writeError(w, http.StatusBadRequest, "name and can are required")
		return
	}
	if body.RuntimeMode != "local" && body.RuntimeMode != "cloud" {
		writeError(w, http.StatusBadRequest, "runtime_mode must be local or cloud")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	ownerUUID, ok := parseUUIDOrBadRequest(w, ownerID, "user id")
	if !ok {
		return
	}
	cannot := strings.TrimSpace(body.Cannot)
	agent, err := h.Queries.CreateIntakeAgent(r.Context(), db.CreateIntakeAgentParams{
		WorkspaceID:  wsUUID,
		Name:         strings.TrimSpace(body.Name),
		RuntimeMode:  body.RuntimeMode,
		OwnerID:      ownerUUID,
		IntakeCan:    pgtype.Text{String: can, Valid: true},
		IntakeCannot: pgtype.Text{String: cannot, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not register agent")
		return
	}
	writeJSON(w, http.StatusCreated, intakeAgentFromRow(agent, false))
}

func (h *Handler) ListIntakeAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListIntakeAgentsForOwner(r.Context(), db.ListIntakeAgentsForOwnerParams{
		WorkspaceID:      parseUUID(workspaceID),
		OwnerID:          parseUUID(ownerID),
		RuntimeStaleSecs: service.RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list agents")
		return
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	places := map[string][]intakePlacement{}
	if len(ids) > 0 {
		found, err := h.Queries.ListIntakePlacements(r.Context(), db.ListIntakePlacementsParams{
			WorkspaceID: parseUUID(workspaceID), Column2: ids,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list agents")
			return
		}
		for _, place := range found {
			item := intakePlacement{Project: place.Title, Product: place.Product, Module: place.Module}
			if id := uuidToString(place.AgentID); intakeIDListed(ids, place.AgentID) {
				item.Role = "主责"
				places[id] = append(places[id], item)
			}
			if place.BackupAgentID.Valid && intakeIDListed(ids, place.BackupAgentID) {
				backup := item
				backup.Role = "备责"
				places[uuidToString(place.BackupAgentID)] = append(places[uuidToString(place.BackupAgentID)], backup)
			}
		}
	}
	out := make([]intakeAgentResponse, 0, len(rows))
	for _, row := range rows {
		id := uuidToString(row.ID)
		found := places[id]
		if found == nil {
			found = []intakePlacement{}
		}
		out = append(out, intakeAgentResponse{
			ID: id, Name: row.Name, Can: row.IntakeCan.String, Cannot: row.IntakeCannot.String,
			RuntimeMode: row.RuntimeMode, Online: row.Online.Bool, ActiveCount: int(row.ActiveCount),
			Placements: found, OwnerID: uuidToString(row.OwnerID),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func intakeIDListed(ids []pgtype.UUID, id pgtype.UUID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func (h *Handler) ListIntakeAssignableAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if !h.intakeCallerMaintains(r, userID) {
		writeError(w, http.StatusForbidden, "cannot list assignable agents")
		return
	}
	rows, err := h.Queries.ListIntakeAssignableAgents(r.Context(), db.ListIntakeAssignableAgentsParams{
		WorkspaceID: parseUUID(workspaceID), Column2: service.RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list agents")
		return
	}
	type item struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Can         string `json:"can"`
		Online      bool   `json:"online"`
		ActiveCount int    `json:"active_count"`
	}
	out := make([]item, 0, len(rows))
	for _, row := range rows {
		out = append(out, item{ID: uuidToString(row.ID), Name: row.Name, Can: row.IntakeCan.String, Online: row.Online.Bool, ActiveCount: int(row.ActiveCount)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) PatchIntakeAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agent id")
	if !ok {
		return
	}
	var body intakeAgentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	can := strings.TrimSpace(body.Can)
	if strings.TrimSpace(body.Name) == "" || can == "" {
		writeError(w, http.StatusBadRequest, "name and can are required")
		return
	}
	if body.RuntimeMode != "local" && body.RuntimeMode != "cloud" {
		writeError(w, http.StatusBadRequest, "runtime_mode must be local or cloud")
		return
	}
	existing, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID: agentID, WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	editorID := ownerID
	if uuidToString(existing.OwnerID) != ownerID {
		if member.Role != "owner" && member.Role != "admin" {
			writeError(w, http.StatusForbidden, "cannot edit this agent")
			return
		}
		editorID = uuidToString(existing.OwnerID)
	}
	updated, err := h.Queries.UpdateIntakeAgentIdentity(r.Context(), db.UpdateIntakeAgentIdentityParams{
		ID: agentID, WorkspaceID: parseUUID(workspaceID), Name: strings.TrimSpace(body.Name),
		RuntimeMode: body.RuntimeMode, IntakeCan: pgtype.Text{String: can, Valid: true},
		IntakeCannot: pgtype.Text{String: strings.TrimSpace(body.Cannot), Valid: true},
		OwnerID:      parseUUID(editorID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, intakeAgentFromRow(updated, false))
}

func intakeAgentFromRow(agent db.Agent, online bool) intakeAgentResponse {
	can, cannot := agent.IntakeCan.String, agent.IntakeCannot.String
	return intakeAgentResponse{
		ID:          uuidToString(agent.ID),
		Name:        agent.Name,
		Can:         can,
		Cannot:      cannot,
		RuntimeMode: agent.RuntimeMode,
		Online:      online,
		Placements:  []intakePlacement{},
		OwnerID:     uuidToString(agent.OwnerID),
	}
}
