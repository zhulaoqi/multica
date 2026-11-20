package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SetPrimaryAgent promotes one of the caller's agents to their digital twin.
// Switching the primary clears the previous one in the same transaction so the
// partial unique index never sees two. A department lead or workspace manager
// may also promote an agent they are allowed to configure (for example, a
// direct report's twin), matching the intake editor's permissions.
func (h *Handler) SetPrimaryAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "agent id")
	if !ok {
		return
	}

	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID: agentID, WorkspaceID: wsUUID,
	})
	if err != nil || agent.ArchivedAt.Valid || !agent.OwnerID.Valid {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if rejectProjectionWrite(w, agent) {
		return
	}
	if !requestIsOperator(r) && !h.mayConfigureIntakeAgentOwnedBy(r, wsUUID, member, agent.OwnerID) {
		writeError(w, http.StatusForbidden, "cannot change this agent's primary status")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set primary agent")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.ClearPrimaryAgentsForOwner(r.Context(), db.ClearPrimaryAgentsForOwnerParams{
		WorkspaceID: wsUUID,
		OwnerID:     agent.OwnerID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set primary agent")
		return
	}
	updated, err := qtx.SetPrimaryAgent(r.Context(), db.SetPrimaryAgentParams{
		ID:          agentID,
		WorkspaceID: wsUUID,
		OwnerID:     agent.OwnerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to set primary agent")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set primary agent")
		return
	}

	wsID := uuidToString(wsUUID)
	slog.Info("primary agent set", append(logger.RequestAttrs(r), "agent_id", uuidToString(agentID), "owner_id", uuidToString(agent.OwnerID), "workspace_id", wsID)...)
	resp := h.agentToResponse(updated)
	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventAgentUpdated, wsID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	h.projectPrimaryTwin(r.Context(), updated)
	redactAgentResponseForActor(&resp, actorType)
	writeJSON(w, http.StatusOK, resp)
}
