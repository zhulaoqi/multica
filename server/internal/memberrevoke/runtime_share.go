package memberrevoke

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// RuntimeAccessRevocationPlan is the owner-visible snapshot of work that an
// access restriction must revoke. It intentionally excludes the runtime
// owner's agents. Hidden Builder agents owned by teammates also represent work
// borrowing the owner's machine and must lose access.
type RuntimeAccessRevocationPlan struct {
	Agents []db.Agent
	Tasks  []db.AgentTaskQueue
}

// HasDependencies reports whether revoking the access would disrupt running
// work, which the HTTP callers turn into a confirmation response.
func (p RuntimeAccessRevocationPlan) HasDependencies() bool {
	return len(p.Agents) > 0 || len(p.Tasks) > 0
}

// LockRuntimeAccessRevocationPlan snapshots and locks the work that loses
// access when the runtime narrows. Both explicit access changes and workspace
// departure use the same lock order and cancellation settlement, inside their
// caller's transaction.
func LockRuntimeAccessRevocationPlan(ctx context.Context, qtx *db.Queries, next db.AgentRuntime) (RuntimeAccessRevocationPlan, error) {
	agents, err := qtx.ListNonOwnerAgentsByRuntimeForUpdate(ctx, db.ListNonOwnerAgentsByRuntimeForUpdateParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
	})
	if err != nil {
		return RuntimeAccessRevocationPlan{}, err
	}
	if _, err := qtx.LockRuntimeRevocationChatSessions(ctx, db.LockRuntimeRevocationChatSessionsParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
	}); err != nil {
		return RuntimeAccessRevocationPlan{}, err
	}
	tasks, err := qtx.ListNonOwnerActiveTasksByRuntimeForUpdate(ctx, db.ListNonOwnerActiveTasksByRuntimeForUpdateParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
	})
	return RuntimeAccessRevocationPlan{Agents: agents, Tasks: tasks}, err
}

// ApplyRuntimeAccessRevocation rewrites the runtime's sharing and tears down
// the work that no longer has access. The caller owns the surrounding
// transaction.
func ApplyRuntimeAccessRevocation(ctx context.Context, qtx *db.Queries, next db.AgentRuntime) (db.AgentRuntime, service.RuntimeTeardownResult, []pgtype.UUID, error) {
	var result service.RuntimeTeardownResult
	updated, err := qtx.UpdateAgentRuntimeVisibility(ctx, db.UpdateAgentRuntimeVisibilityParams{
		ID: next.ID, Visibility: next.Visibility, SharedWith: next.SharedWith,
	})
	if err != nil {
		return updated, result, nil, err
	}
	result.CancelledTasks, err = qtx.CancelNonOwnerTasksByRuntime(ctx, db.CancelNonOwnerTasksByRuntimeParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
		FailureReason: string(taskfailure.ReasonRuntimeAccessRevoked),
	})
	if err != nil {
		return updated, result, nil, err
	}
	readyChats, err := service.PrepareRuntimeAccessCancellation(ctx, qtx, result.CancelledTasks)
	if err != nil {
		return updated, result, nil, err
	}
	result.UnboundAgents, err = qtx.UnbindNonOwnerAgentsFromRuntime(ctx, db.UnbindNonOwnerAgentsFromRuntimeParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
	})
	if err != nil {
		return updated, result, nil, err
	}
	ids := make([]pgtype.UUID, len(result.UnboundAgents))
	for i, a := range result.UnboundAgents {
		ids[i] = a.ID
	}
	result.PausedAutopilots, err = qtx.PauseAutopilotsByUnboundAgents(ctx, ids)
	return updated, result, readyChats, err
}
