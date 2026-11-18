package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/memberrevoke"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// revocationResult is what a member revocation touched, so the handler can fan
// out events after the transaction commits.
type revocationResult = memberrevoke.Result

// revokeAndRemoveMember converges all server-side state that should follow a
// member leaving a workspace: runtimes, agents, tasks, subscriptions and the
// member row itself. The transaction lives in memberrevoke so the one-time
// project-member cleanup command runs the same sequence.
func (h *Handler) revokeAndRemoveMember(ctx context.Context, workspaceID, userID, memberID, archivedBy pgtype.UUID) (revocationResult, error) {
	return memberrevoke.Service{
		Queries:   h.Queries,
		TxStarter: h.TxStarter,
	}.Revoke(ctx, memberrevoke.Params{
		WorkspaceID:     workspaceID,
		UserID:          userID,
		MemberID:        memberID,
		ArchivedBy:      archivedBy,
		ReleaseCapacity: h.seatCapacityEnabled(),
	})
}

// publishRevocation runs all post-commit side effects: invalidate daemon token
// cache, broadcast task:cancelled with per-agent reconciliation, broadcast
// agent:archived, and signal a runtime-list refresh. Safe to call on an empty
// result — it returns immediately.
func (h *Handler) publishRevocation(ctx context.Context, result revocationResult, workspaceIDStr, actorType, actorIDStr string) {
	if result.IsEmpty() {
		return
	}

	for _, hash := range result.RevokedTokenHashes {
		h.DaemonTokenCache.Invalidate(ctx, hash)
	}

	// Per-task cancellation: TaskService handles status reconciliation and
	// per-task event broadcast. Run this before the agent:archived burst so
	// subscribers see "task cancelled" before the parent agent disappears
	// from active lists, matching the order ArchiveAgent uses.
	if h.TaskService != nil && len(result.CancelledTasks) > 0 {
		// Revocation only archives agents, so a per-task lookup would still
		// resolve here; the workspace is passed for the same reason as
		// everywhere else — it is known, and it is the one being revoked.
		h.TaskService.BroadcastCancelledTasks(ctx, workspaceIDStr, result.CancelledTasks)
	}

	for _, agent := range result.ArchivedAgents {
		h.publish(protocol.EventAgentArchived, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"agent": h.agentToResponse(agent),
		})
	}

	h.PublishRuntimeTeardown(ctx, result.SharedTeardown, workspaceIDStr, actorType, actorIDStr, "revoke", false)
	if h.TaskService != nil {
		for _, taskID := range result.ReadyChats {
			h.TaskService.FinalizeDeferredCancelledChat(ctx, taskID)
		}
	}

	// Tell connected clients to refresh the runtime list. We piggyback on
	// EventDaemonRegister with a "revoke" action — same channel the runtime
	// delete handler uses — so the frontend invalidates its cached list
	// without us having to introduce a new event type the desktop app would
	// need a build to learn about.
	if len(result.OfflineRuntimeIDs) > 0 || result.SharedRuntimeCount > 0 {
		h.publish(protocol.EventDaemonRegister, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"action": "revoke",
		})
	}
}

// logRevocation emits a structured info line summarising the revocation. Kept
// separate from publish so the log is identical whether or not the bus is wired.
func logRevocation(result revocationResult, workspaceID, userID string, attrs ...any) {
	if result.IsEmpty() {
		return
	}

	base := []any{
		"workspace_id", workspaceID,
		"user_id", userID,
		"runtimes_revoked", len(result.Runtimes),
		"agents_archived", len(result.ArchivedAgents),
		"tasks_cancelled", len(result.CancelledTasks),
		"runtimes_taken_offline", len(result.OfflineRuntimeIDs),
		"daemon_tokens_revoked", len(result.RevokedTokenHashes),
	}
	slog.Info("member runtimes revoked", append(base, attrs...)...)
}
