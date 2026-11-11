package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// IntakeRedispatchInstruction marks a wakeup that looks at the module roster
// again instead of rerunning the agent stored on the wakeup.
const IntakeRedispatchInstruction = "intake-redispatch"

// IntakeRetryPlan decides the next attempt after a failed run on the same module.
// The first two failures stay on the current unit, after 1 minute and then 5 minutes.
// The next failure moves to the backup when the current unit is the primary and
// the backup is online. Otherwise the roster is tried again after 15 minutes.
// A second exhausted pass stops automatic retries.
type IntakeRetryPlan struct {
	Wait           time.Duration
	SwitchToBackup bool
	Redispatch     bool
	Stop           bool
}

// IntakeRetryProgress is the requester-facing line while a retry stays inside
// the same module. The status remains 正在做. The sentences do not name an agent.
func IntakeRetryProgress(plan IntakeRetryPlan) (done, remain string, ok bool) {
	switch {
	case plan.SwitchToBackup:
		return "正在换注册单位", "备责接着同一份任务", true
	case plan.Redispatch:
		return "这一轮没有选出单位", "十五分钟后再分一次", true
	case plan.Wait > 0:
		return "这一轮没有新输出，正在同一台重试", "重试之后继续", true
	default:
		return "", "", false
	}
}

func PlanIntakeRetry(passes int, currentIsPrimary, backupOnline, alreadyRedispatched bool) IntakeRetryPlan {
	switch {
	case passes <= 0:
		return IntakeRetryPlan{}
	case passes == 1:
		return IntakeRetryPlan{Wait: time.Minute}
	case passes == 2:
		return IntakeRetryPlan{Wait: 5 * time.Minute}
	case currentIsPrimary && backupOnline:
		return IntakeRetryPlan{SwitchToBackup: true}
	case !alreadyRedispatched:
		return IntakeRetryPlan{Redispatch: true, Wait: 15 * time.Minute}
	default:
		return IntakeRetryPlan{Stop: true}
	}
}

func intakeRetryReason(reason string) bool {
	switch reason {
	case "idle_watchdog", "timeout", "runtime_offline", "queued_expired", "provider_network", "provider_5xx":
		return true
	default:
		return false
	}
}

// intakeOwnsRetry reports that this failure belongs to an intake request, so
// the generic task retry must not create a child. The intake plan waits one
// minute, then five, then switches unit. A generic child would consume the
// failure first and leave that plan with nothing to do.
func (s *TaskService) intakeOwnsRetry(ctx context.Context, task db.AgentTaskQueue, reason string) bool {
	if s == nil || s.Queries == nil || !task.IssueID.Valid || !intakeRetryReason(reason) {
		return false
	}
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return false
	}
	_, err = s.Queries.GetIntakeRequestByIssue(ctx, db.GetIntakeRequestByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	return err == nil
}

// scheduleIntakeRetry applies the intake retry plan when the generic task
// retry did not already create a child. A wait becomes a one-shot wakeup.
// Switching to the backup enqueues that agent immediately.
func (s *TaskService) scheduleIntakeRetry(ctx context.Context, task db.AgentTaskQueue, failureReason string, alreadyRetried bool) {
	if s == nil || s.Queries == nil || alreadyRetried || !task.IssueID.Valid || !intakeRetryReason(failureReason) {
		return
	}
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	req, err := s.Queries.GetIntakeRequestByIssue(ctx, db.GetIntakeRequestByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		return
	}
	module, err := s.Queries.GetIntakeModuleByID(ctx, db.GetIntakeModuleByIDParams{WorkspaceID: issue.WorkspaceID, ID: req.ModuleID})
	if err != nil {
		return
	}
	backupOnline := module.BackupAgentID.Valid && s.intakeAgentOnline(ctx, module.BackupAgentID)
	passes, err := s.Queries.AddIntakeRetryPass(ctx, req.ID)
	if err != nil {
		return
	}
	currentIsPrimary := task.AgentID.Valid && module.AgentID.Valid && task.AgentID.Bytes == module.AgentID.Bytes
	plan := PlanIntakeRetry(int(passes), currentIsPrimary, backupOnline, req.RedispatchUsed)
	switch {
	case plan.SwitchToBackup:
		if done, remain, ok := IntakeRetryProgress(plan); ok {
			_ = s.Queries.SetIntakeProgress(ctx, intakeProgress(req.ID, done, remain))
		}
		unitName := "备责"
		if agent, err := s.Queries.GetAgent(ctx, module.BackupAgentID); err == nil && agent.Name != "" {
			unitName = agent.Name
		}
		_ = s.Queries.AddIntakeUnitSwitch(ctx, db.AddIntakeUnitSwitchParams{ID: req.ID, UnitLog: unitName})
		_ = s.Queries.SetIntakeStartNotice(ctx, db.SetIntakeStartNoticeParams{ID: req.ID, StartNoticeAgentID: module.BackupAgentID})
		_ = s.Queries.SetIntakeAssignee(ctx, db.SetIntakeAssigneeParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, AssigneeID: module.BackupAgentID})
		updated, err := s.Queries.GetIssue(ctx, issue.ID)
		if err != nil {
			return
		}
		if _, err := s.EnqueueIntakeTask(ctx, updated, true); err != nil {
			slog.Warn("intake backup enqueue failed", "issue_id", issue.ID, "error", err)
		}
		_ = s.Queries.ResetIntakeRetryPass(ctx, req.ID)
	case plan.Redispatch:
		if done, remain, ok := IntakeRetryProgress(plan); ok {
			_ = s.Queries.SetIntakeProgress(ctx, intakeProgress(req.ID, done, remain))
		}
		_ = s.Queries.MarkIntakeRedispatch(ctx, req.ID)
		when := time.Now().Add(plan.Wait)
		_, err := s.Queries.CreateIssueWakeup(ctx, db.CreateIssueWakeupParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
			AgentID: module.AgentID, CreatedBy: req.RequesterID, Instruction: IntakeRedispatchInstruction,
			Kind: "at", Mode: "once", EventTypes: []string{}, Timezone: "UTC",
			NextFireAt: pgtype.Timestamptz{Time: when, Valid: true},
		})
		if err != nil {
			slog.Warn("intake redispatch wakeup failed", "issue_id", issue.ID, "error", err)
		}
	case plan.Wait > 0:
		if done, remain, ok := IntakeRetryProgress(plan); ok {
			_ = s.Queries.SetIntakeProgress(ctx, intakeProgress(req.ID, done, remain))
		}
		when := time.Now().Add(plan.Wait)
		_, err := s.Queries.CreateIssueWakeup(ctx, db.CreateIssueWakeupParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
			AgentID: task.AgentID, CreatedBy: req.RequesterID, Instruction: req.SourceBody,
			Kind: "at", Mode: "once", EventTypes: []string{}, Timezone: "UTC",
			NextFireAt: pgtype.Timestamptz{Time: when, Valid: true},
		})
		if err != nil {
			slog.Warn("intake retry wakeup failed", "issue_id", issue.ID, "error", err)
		}
	case plan.Stop:
		_ = s.Queries.SetIntakeIssueStatus(ctx, db.SetIntakeIssueStatusParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: "todo"})
		_ = s.Queries.SetIntakeProgress(ctx, intakeProgress(req.ID, "还没做完", "执行没有进展"))
		if DeliverIntakeCard != nil {
			_ = DeliverIntakeCard(ctx, issue.WorkspaceID, req.RequesterID, issue.ID, "还没做完", IntakeLabeled("需求", IntakeDemandLine(req.SourceBody), "说明", "已经试过可以接手的单位，没有做完", "下一步", "打开页面可以让原来的单位继续"))
		}
	}
}

// RedispatchIntake runs the roster again after the 15 minute wait.
// An online primary is used first. An online backup is used otherwise.
// Nobody online ends the second pass.
func (s *TaskService) RedispatchIntake(ctx context.Context, issue db.Issue) {
	if s == nil || s.Queries == nil {
		return
	}
	req, err := s.Queries.GetIntakeRequestByIssue(ctx, db.GetIntakeRequestByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		return
	}
	module, err := s.Queries.GetIntakeModuleByID(ctx, db.GetIntakeModuleByIDParams{WorkspaceID: issue.WorkspaceID, ID: req.ModuleID})
	if err != nil {
		return
	}
	backupID := ""
	backupOnline := false
	if module.BackupAgentID.Valid {
		backupID = util.UUIDToString(module.BackupAgentID)
		backupOnline = s.intakeAgentOnline(ctx, module.BackupAgentID)
	}
	plan := DispatchIntake(util.UUIDToString(module.AgentID), backupID, s.intakeAgentOnline(ctx, module.AgentID), backupOnline, false)
	if !plan.Enqueue {
		_ = s.Queries.SetIntakeIssueStatus(ctx, db.SetIntakeIssueStatusParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: "todo"})
		_ = s.Queries.SetIntakeProgress(ctx, intakeProgress(req.ID, "还没做完", "执行没有进展"))
		if DeliverIntakeCard != nil {
			_ = DeliverIntakeCard(ctx, issue.WorkspaceID, req.RequesterID, issue.ID, "还没做完", IntakeLabeled("需求", IntakeDemandLine(req.SourceBody), "说明", "已经试过可以接手的单位，没有做完", "下一步", "打开页面可以让原来的单位继续"))
		}
		return
	}
	assignee := module.AgentID
	fresh := false
	if plan.SwitchedToBackup {
		assignee = module.BackupAgentID
		fresh = true
		_ = s.Queries.AddIntakeUnitSwitch(ctx, db.AddIntakeUnitSwitchParams{ID: req.ID, UnitLog: "备责"})
		_ = s.Queries.SetIntakeStartNotice(ctx, db.SetIntakeStartNoticeParams{ID: req.ID, StartNoticeAgentID: module.BackupAgentID})
	}
	_ = s.Queries.SetIntakeAssignee(ctx, db.SetIntakeAssigneeParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, AssigneeID: assignee})
	_ = s.Queries.ResetIntakeRetryPass(ctx, req.ID)
	_ = s.Queries.SetIntakeProgress(ctx, intakeProgress(req.ID, "正在匹配", "匹配完就开始"))
	updated, err := s.Queries.GetIssue(ctx, issue.ID)
	if err != nil {
		return
	}
	if _, err := s.EnqueueIntakeTask(ctx, updated, fresh); err != nil {
		slog.Warn("intake redispatch enqueue failed", "issue_id", issue.ID, "error", err)
	}
}

func (s *TaskService) intakeAgentOnline(ctx context.Context, agentID pgtype.UUID) bool {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil || !agent.RuntimeID.Valid {
		return false
	}
	runtime, err := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil || runtime.Status != "online" {
		return false
	}
	seen := runtime.UpdatedAt.Time
	if runtime.LastSeenAt.Valid {
		seen = runtime.LastSeenAt.Time
	}
	return time.Since(seen) <= time.Duration(RuntimeClaimFreshnessSeconds)*time.Second
}
