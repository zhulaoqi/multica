// Package memberrevoke converges all server-side state that must follow a
// member leaving a workspace. It is shared by the HTTP remove/leave paths and
// the one-time project-member cleanup command, so the cleanup cannot leave the
// orphaned runtimes, agents and subscriptions that a bare member-row delete
// would.
package memberrevoke

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/seatcapacity"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TxStarter begins the transaction the revocation runs in. *pgxpool.Pool
// satisfies it.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service is the data layer the revocation runs against.
type Service struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// Params identifies the member row to remove and who triggered it.
type Params struct {
	WorkspaceID pgtype.UUID
	UserID      pgtype.UUID
	MemberID    pgtype.UUID
	// ArchivedBy is the actor recorded on archived agents. HTTP callers pass the
	// requester or the leaving member; the maintenance command passes a zero UUID.
	ArchivedBy pgtype.UUID
	// ReleaseCapacity mirrors the API server's seat-capacity configuration. When
	// true, a seat-release intent is enqueued in the same transaction.
	ReleaseCapacity bool
}

// Result captures everything the transaction touched so the caller can fan out
// events after commit. Publishing inside the transaction would let subscribers
// observe a state the tx might still roll back (see
// TaskService.BroadcastCancelledTasks docstring).
type Result struct {
	Runtimes           []db.AgentRuntime
	ArchivedAgents     []db.Agent
	CancelledTasks     []db.AgentTaskQueue
	OfflineRuntimeIDs  []db.ForceOfflineRuntimesByIDsRow
	RevokedTokenHashes []string
	SharedRuntimeCount int
	SharedTeardown     service.RuntimeTeardownResult
	ReadyChats         []pgtype.UUID
}

// IsEmpty reports that no runtime or shared-runtime state was revoked, so the
// caller can skip post-commit event fanout.
func (r Result) IsEmpty() bool {
	return len(r.Runtimes) == 0 && r.SharedRuntimeCount == 0
}

// Revoke removes the member row and converges every dependent resource in one
// transaction: every runtime the member owns becomes unusable, every agent
// pinned to one of those runtimes is archived, every in-flight task on those
// runtimes is cancelled (cancelled rather than failed so the daemon's per-task
// status poller interrupts the running agent gracefully), the member's durable
// subscriptions and daemon_token rows are deleted, and finally the member row
// itself is removed.
//
// All DB writes run inside a single transaction so a partial revocation never
// leaves the workspace half-converged — e.g. a member who is "gone" but whose
// runtime row is still active.
//
// Note on scope: this revokes every runtime whose owner_id matches UserID,
// regardless of how the daemon authenticates. Today most daemons fall back to
// PAT/JWT and `daemon_token` rows are unused in production; deleting them is
// a no-op for those daemons but takes effect once the mdt_ flow is live.
// Either way the agent-archive + task-cancel + force-offline writes are the
// actual production safety net: even if the daemon races back online with a
// still-valid PAT, it finds no agent it can run for, no queued task to claim,
// and the dispatcher (which gates on agent.archived_at IS NULL) won't hand it
// new work.
func (s Service) Revoke(ctx context.Context, p Params) (Result, error) {
	var empty Result

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	// Taken FIRST, before this tx touches member or issue_subscriber. The
	// delegated auto-subscribe rule takes the same (workspace, user) lock, so
	// a run that is mid-decomposition cannot slip a new subscriber row in
	// between this tx's membership delete and its subscription cleanup below.
	// First also means every holder acquires it in the same order, so these
	// paths cannot deadlock against each other (MUL-5483 review round 7).
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: p.WorkspaceID,
		UserID:      p.UserID,
	}); err != nil {
		return empty, err
	}

	if _, err := qtx.LockRuntimeShareMemberRemoval(ctx, db.LockRuntimeShareMemberRemovalParams{
		WorkspaceID: p.WorkspaceID, UserID: p.UserID,
	}); err != nil {
		return empty, err
	}

	runtimes, err := qtx.ListAgentRuntimesByOwner(ctx, db.ListAgentRuntimesByOwnerParams{
		WorkspaceID: p.WorkspaceID,
		OwnerID:     p.UserID,
	})
	if err != nil {
		return empty, err
	}

	result := Result{Runtimes: runtimes}

	if len(runtimes) > 0 {
		runtimeIDs := make([]pgtype.UUID, len(runtimes))
		daemonIDs := make([]string, 0, len(runtimes))
		for i, rt := range runtimes {
			runtimeIDs[i] = rt.ID
			if rt.DaemonID.Valid && rt.DaemonID.String != "" {
				daemonIDs = append(daemonIDs, rt.DaemonID.String)
			}
		}

		result.ArchivedAgents, err = qtx.ArchiveAgentsByRuntime(ctx, db.ArchiveAgentsByRuntimeParams{
			ArchivedBy: p.ArchivedBy,
			RuntimeIds: runtimeIDs,
		})
		if err != nil {
			return empty, err
		}

		// Cancel by runtime AND by archived agent. agent.runtime_id can be
		// reassigned via UpdateAgent without rewriting the runtime_id on
		// historical agent_task_queue rows, so an archived agent may still
		// have queued/running tasks pinned to a different runtime — and
		// ClaimAgentTask does not gate on agent.archived_at, so those tasks
		// would otherwise stay claimable after the agent is gone.
		archivedAgentIDs := make([]pgtype.UUID, len(result.ArchivedAgents))
		for i, a := range result.ArchivedAgents {
			archivedAgentIDs[i] = a.ID
		}
		result.CancelledTasks, err = qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
			RuntimeIds: runtimeIDs,
			AgentIds:   archivedAgentIDs,
		})
		if err != nil {
			return empty, err
		}
		if err = service.SettleTerminalTaskState(ctx, qtx, result.CancelledTasks...); err != nil {
			return empty, err
		}

		result.OfflineRuntimeIDs, err = qtx.ForceOfflineRuntimesByIDs(ctx, runtimeIDs)
		if err != nil {
			return empty, err
		}

		if len(daemonIDs) > 0 {
			result.RevokedTokenHashes, err = qtx.DeleteDaemonTokensByWorkspaceAndDaemons(ctx, db.DeleteDaemonTokensByWorkspaceAndDaemonsParams{
				WorkspaceID: p.WorkspaceID,
				DaemonIds:   daemonIDs,
			})
			if err != nil {
				return empty, err
			}
		}
	}

	// channel_user_binding used to carry a member FK with ON DELETE CASCADE, so
	// a removed member's IM bindings vanished automatically. MUL-3515 §4 dropped
	// every channel_* foreign key, moving that integrity rule to the application
	// layer: prune the bindings here, in the same tx as the member-row delete.
	// The inbound path also re-checks access (see the channel stores), but
	// pruning stops a stale binding from lingering across a remove/re-add.
	if err := qtx.DeleteChannelUserBindingsByWorkspaceMember(ctx, db.DeleteChannelUserBindingsByWorkspaceMemberParams{
		WorkspaceID:   p.WorkspaceID,
		MulticaUserID: p.UserID,
	}); err != nil {
		return empty, err
	}

	// agent_invocation_target carries member-target grants with NO database FK
	// (MUL-3963 keeps the new table FK-free, matching the MUL-3515 channel
	// generalization). Prune this leaving member's grants in the same tx as the
	// member-row delete so a re-invited user does not silently reclaim old
	// invocation permission on agents that had allow-listed them. SCOPED to
	// this workspace: the same user may belong to other workspaces, and
	// removing them here must not touch their grants on agents elsewhere.
	if err := qtx.DeleteAgentInvocationTargetsByMember(ctx, db.DeleteAgentInvocationTargetsByMemberParams{
		WorkspaceID: p.WorkspaceID,
		TargetID:    p.UserID,
	}); err != nil {
		return empty, err
	}

	// A private quick action is visible and runnable ONLY by its creator
	// (quick_action carries no FK, so nothing removes it implicitly). Once the
	// creator is gone the row is unreachable by every remaining member while
	// still consuming the workspace's active-action limit, so drop those in
	// the same tx. Public actions are workspace furniture and survive.
	if err := qtx.DeletePrivateQuickActionsByCreator(ctx, db.DeletePrivateQuickActionsByCreatorParams{
		WorkspaceID: p.WorkspaceID,
		CreatedByID: p.UserID,
	}); err != nil {
		return empty, err
	}

	// Saved views follow the private-quick-action rule above: a departed
	// member's PRIVATE views are invisible to everyone left yet still count
	// against quota, and a re-invite must not resurrect them. Shared views
	// stay — they are workspace furniture other members may rely on. The
	// per-user view-bar preferences are meaningless without the member.
	if err := qtx.DeletePrivateIssueViewsByOwner(ctx, db.DeletePrivateIssueViewsByOwnerParams{
		WorkspaceID: p.WorkspaceID,
		OwnerID:     p.UserID,
	}); err != nil {
		return empty, err
	}
	if err := qtx.DeleteIssueViewPreferencesByUser(ctx, db.DeleteIssueViewPreferencesByUserParams{
		WorkspaceID: p.WorkspaceID,
		UserID:      p.UserID,
	}); err != nil {
		return empty, err
	}

	// issue_subscriber carries no FK either (same MUL-3515 rule as the two
	// prunes above), and MUL-5483 gave agents a path that writes member
	// subscriber rows on their own initiative. Dropping them in this tx is what
	// stops a departed member from accruing inbox rows, and stops a re-invite
	// from silently restoring visibility of everything they used to watch.
	if err := qtx.DeleteSubscriptionsByMember(ctx, db.DeleteSubscriptionsByMemberParams{
		WorkspaceID: p.WorkspaceID,
		UserID:      p.UserID,
	}); err != nil {
		return empty, err
	}

	// autopilot_subscriber is another FK-free subscription template. Leaving
	// stale rows here made the detail API return a user the member picker could
	// no longer render; the next full-replace PATCH then failed membership
	// validation, blocking every edit to that autopilot. Prune only templates in
	// this workspace so the same user's subscriptions elsewhere survive.
	if err := qtx.DeleteAutopilotSubscribersByMember(ctx, db.DeleteAutopilotSubscribersByMemberParams{
		WorkspaceID: p.WorkspaceID,
		UserID:      p.UserID,
	}); err != nil {
		return empty, err
	}

	// Member row deletion lives inside the same tx so a successful revoke is
	// never followed by a failed member-delete (which would leave the user
	// still a member with a dead runtime), and a failed revoke never leaves
	// the user out of the workspace with a still-online runtime.
	if p.ReleaseCapacity {
		if err := enqueueMemberCapacityRelease(ctx, qtx, p.WorkspaceID, p.MemberID); err != nil {
			return empty, err
		}
	}
	sharedRuntimes, err := qtx.ListSharedAgentRuntimesForUpdate(ctx, db.ListSharedAgentRuntimesForUpdateParams{
		WorkspaceID: p.WorkspaceID, UserID: p.UserID,
	})
	if err != nil {
		return empty, err
	}
	result.SharedRuntimeCount = len(sharedRuntimes)
	for _, rt := range sharedRuntimes {
		rt.SharedWith = slices.DeleteFunc(rt.SharedWith, func(id pgtype.UUID) bool { return id == p.UserID })
		if rt.Visibility == "public" {
			if _, err := qtx.UpdateAgentRuntimeVisibility(ctx, db.UpdateAgentRuntimeVisibilityParams{
				ID: rt.ID, Visibility: rt.Visibility, SharedWith: rt.SharedWith,
			}); err != nil {
				return empty, err
			}
			continue
		}
		if _, err := LockRuntimeAccessRevocationPlan(ctx, qtx, rt); err != nil {
			return empty, err
		}
		_, teardown, readyChats, err := ApplyRuntimeAccessRevocation(ctx, qtx, rt)
		if err != nil {
			return empty, err
		}
		result.SharedTeardown.UnboundAgents = append(result.SharedTeardown.UnboundAgents, teardown.UnboundAgents...)
		result.SharedTeardown.CancelledTasks = append(result.SharedTeardown.CancelledTasks, teardown.CancelledTasks...)
		result.SharedTeardown.PausedAutopilots = append(result.SharedTeardown.PausedAutopilots, teardown.PausedAutopilots...)
		result.ReadyChats = append(result.ReadyChats, readyChats...)
	}

	if err := qtx.DeleteMember(ctx, p.MemberID); err != nil {
		return empty, err
	}

	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}

	return result, nil
}

// enqueueMemberCapacityRelease records the seat-release intent in the same
// transaction as the member-row delete. The outbox worker turns it into the
// provider-side release, so a crash between the two steps cannot leak a seat.
func enqueueMemberCapacityRelease(ctx context.Context, q *db.Queries, workspaceID, memberID pgtype.UUID) error {
	operationToken := uuid.New()
	if _, err := q.UpsertSeatCapacityIntent(ctx, db.UpsertSeatCapacityIntentParams{
		WorkspaceID:    workspaceID,
		OperationToken: pgtype.UUID{Bytes: operationToken, Valid: true},
		Action:         seatcapacity.ActionReleaseMember,
		NextAttemptAt:  pgtype.Timestamptz{Time: time.Now(), Valid: true},
		MemberID:       memberID,
	}); err != nil {
		return err
	}
	return nil
}
