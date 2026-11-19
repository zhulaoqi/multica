package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
	redactAgentResponseForActor(&resp, actorType)
	writeJSON(w, http.StatusOK, resp)
}

// ensurePrimaryTwinInWorkspace gives a member their digital twin in a newly
// joined project. If they already own an active agent there, the oldest one is
// promoted; otherwise the most recent primary twin from another workspace is
// cloned. The function is best-effort: callers log failures and still complete
// the join, because binding is a separate step the member can finish manually.
func (h *Handler) ensurePrimaryTwinInWorkspace(
	ctx context.Context,
	workspaceID, userID, orgUnitID pgtype.UUID,
) error {
	if !workspaceID.Valid || !userID.Valid {
		return nil
	}
	_, err := h.Queries.GetPrimaryAgentForOwner(ctx, db.GetPrimaryAgentForOwnerParams{
		WorkspaceID: workspaceID,
		OwnerID:     userID,
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	if _, err := h.Queries.EnsurePrimaryAgentForOwner(ctx, db.EnsurePrimaryAgentForOwnerParams{
		WorkspaceID: workspaceID,
		OwnerID:     userID,
	}); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	sources, err := h.Queries.GetPrimaryTwinForUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if source.WorkspaceID == workspaceID {
			continue
		}
		_, err := h.Queries.CloneAgentAsTwin(ctx, db.CloneAgentAsTwinParams{
			WorkspaceID: workspaceID,
			OwnerID:     userID,
			OrgUnitID:   orgUnitID,
			SourceID:    source.ID,
		})
		return err
	}
	return nil
}
