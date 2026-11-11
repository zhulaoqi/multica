package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IntakeFireEnqueues reports whether a due wakeup may create a run.
// Ordinary wakeups always enqueue. An intake wakeup enqueues only while the
// agent already chosen for it is online. The fire does not switch agents.
func IntakeFireEnqueues(intake, online bool) bool {
	if !intake {
		return true
	}
	return online
}

// holdOfflineIntakeWakeup records 「需要维护」 when a scheduled intake wakeup
// fires and the chosen machine is offline. The caller must close the wakeup
// without creating a task. notify runs after the transaction commits.
func holdOfflineIntakeWakeup(ctx context.Context, q *db.Queries, issue db.Issue, runtimeID pgtype.UUID, now time.Time) (bool, func(), error) {
	req, err := q.GetIntakeRequestByIssue(ctx, db.GetIntakeRequestByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if IntakeFireEnqueues(true, intakeRuntimeOnline(ctx, q, runtimeID, now)) {
		return false, nil, nil
	}
	if err := q.SetIntakeProgress(ctx, intakeProgress(req.ID, "还没开始", "需要维护")); err != nil {
		return false, nil, err
	}
	notify := func() {
		if DeliverIntakeCard == nil {
			return
		}
		_ = DeliverIntakeCard(ctx, issue.WorkspaceID, req.RequesterID, issue.ID, "还没开始", IntakeLabeled("需求", IntakeDemandLine(req.SourceBody), "状态", "需要维护"))
	}
	return true, notify, nil
}

func intakeRuntimeOnline(ctx context.Context, q *db.Queries, runtimeID pgtype.UUID, now time.Time) bool {
	if q == nil || !runtimeID.Valid {
		return false
	}
	runtime, err := q.GetAgentRuntime(ctx, runtimeID)
	if err != nil || runtime.Status != "online" {
		return false
	}
	seen := runtime.UpdatedAt.Time
	if runtime.LastSeenAt.Valid {
		seen = runtime.LastSeenAt.Time
	}
	return now.Sub(seen) <= time.Duration(RuntimeClaimFreshnessSeconds)*time.Second
}
