package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var intakeShanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// IntakeProgressInterval is 30 minutes from 08:00 through 21:59 Shanghai time,
// and 2 hours overnight.
func IntakeProgressInterval(now time.Time) time.Duration {
	hour := now.In(intakeShanghai).Hour()
	if hour >= 8 && hour < 22 {
		return 30 * time.Minute
	}
	return 2 * time.Hour
}

// IntakeProgressAnchor is when the cadence starts. A future start_at has not
// started, so no notice is due. After that, the clock starts at the later of
// the agreed start and the previous notice.
func IntakeProgressAnchor(created, start, lastNotice, now time.Time) (time.Time, bool) {
	if !start.IsZero() && now.Before(start) {
		return time.Time{}, false
	}
	anchor := created
	if !start.IsZero() && start.After(anchor) {
		anchor = start
	}
	if !lastNotice.IsZero() && lastNotice.After(anchor) {
		anchor = lastNotice
	}
	return anchor, true
}

// IntakeProgressDue reports whether a notice should go out. anchor is the
// later of the start time and the previous successful notice.
func IntakeProgressDue(now, anchor time.Time) bool {
	if anchor.IsZero() || now.Before(anchor) {
		return false
	}
	return now.Sub(anchor) >= IntakeProgressInterval(now)
}

// IntakeProgressStopped is true once the requester should no longer receive
// "做到这里" notices.
func IntakeProgressStopped(status string, openBlocker, stalled bool) bool {
	switch status {
	case "in_review", "done", "cancelled", "blocked":
		return true
	default:
		return openBlocker || stalled
	}
}

// DeliverIntakeCard sends one personal card. Nil skips Feishu. A non-nil error
// rolls a progress claim back so the next sweep retries. title is the card header.
var DeliverIntakeCard func(ctx context.Context, workspaceID, userID, issueID pgtype.UUID, title, text string) error

// SweepIntakeProgress claims each due intake request and stores the progress
// on that request. It does not write an issue comment.
func SweepIntakeProgress(ctx context.Context, q *db.Queries, tasks *TaskService, now time.Time) (int, error) {
	if q == nil {
		return 0, nil
	}
	rows, err := q.ListIntakeProgressCandidates(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, row := range rows {
		if IntakeProgressStopped(row.Status, false, row.ProgressRemain.String == "执行没有进展") || !row.CreatedAt.Valid {
			continue
		}
		var start time.Time
		if row.StartAt.Valid {
			start = row.StartAt.Time
		}
		var last time.Time
		if row.LastUserNoticeAt.Valid {
			last = row.LastUserNoticeAt.Time
		}
		anchor, ready := IntakeProgressAnchor(row.CreatedAt.Time, start, last, now)
		if !ready || !IntakeProgressDue(now, anchor) {
			continue
		}
		claimed, err := q.ClaimIntakeNotice(ctx, db.ClaimIntakeNoticeParams{
			ID: row.ID, PreviousNoticeAt: row.LastUserNoticeAt, NextNoticeAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil || claimed == 0 {
			continue
		}
		done, remain := IntakeNoticeProgress(row.RunStatus, row.ProgressDone.String, row.ProgressRemain.String, row.LatestLine.String)
		if err := q.SetIntakeProgress(ctx, intakeProgress(row.ID, done, remain)); err != nil {
			_, _ = q.ClaimIntakeNotice(ctx, db.ClaimIntakeNoticeParams{
				ID: row.ID, PreviousNoticeAt: pgtype.Timestamptz{Time: now, Valid: true}, NextNoticeAt: row.LastUserNoticeAt,
			})
			continue
		}
		if DeliverIntakeCard != nil {
			body := IntakeLabeled("需求", IntakeDemandLine(row.SourceBody), "做到", done, "还剩", remain)
			if err := DeliverIntakeCard(ctx, row.WorkspaceID, row.RequesterID, row.IssueID, "做到这里", body); err != nil {
				_, _ = q.ClaimIntakeNotice(ctx, db.ClaimIntakeNoticeParams{
					ID: row.ID, PreviousNoticeAt: pgtype.Timestamptz{Time: now, Valid: true}, NextNoticeAt: row.LastUserNoticeAt,
				})
				continue
			}
		}
		sent++
	}
	started, err := noticeIntakeStarts(ctx, q)
	if err != nil {
		slog.Warn("intake start notice failed", "error", err)
		started = 0
	}
	reviewed, err := noticeIntakeReviews(ctx, q)
	if err != nil {
		return sent, err
	}
	continued, err := autoContinueIntakeBlockers(ctx, q, tasks)
	if err != nil {
		return sent, err
	}
	return sent + started + reviewed + continued, nil
}

func noticeIntakeStarts(ctx context.Context, q *db.Queries) (int, error) {
	rows, err := q.ListIntakeStartNotices(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		if DeliverIntakeCard != nil {
			started := ""
			if row.StartedAt.Valid {
				started = row.StartedAt.Time.Format(time.RFC3339)
			}
			agreed := ""
			if row.StartAt.Valid {
				agreed = row.StartAt.Time.Format(time.RFC3339)
			}
			body := IntakeLabeled("需求", IntakeDemandLine(row.SourceBody), "约定时刻", agreed, "实际开始", started)
			if err := DeliverIntakeCard(ctx, row.WorkspaceID, row.RequesterID, row.IssueID, "开始执行", body); err != nil {
				continue
			}
		}
		if err := q.SetIntakeStartNotice(ctx, db.SetIntakeStartNoticeParams{ID: row.ID, StartNoticeAgentID: row.AssigneeID}); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

// IntakeNoticeProgress chooses the two lines for one progress card.
// Agent lines that start with 做到 and 还剩 win. A retry or maintenance line
// already stored on the request is sent again instead of being replaced.
func IntakeNoticeProgress(runStatus, done, remain, latest string) (string, string) {
	if agentDone, agentRemain, ok := IntakeProgressFromAgent(latest); ok {
		return agentDone, agentRemain
	}
	switch remain {
	case "重试之后继续", "需要维护", "还没有测试环境":
		return done, remain
	}
	if done == "正在换注册单位" || strings.HasPrefix(remain, "测试环境：") {
		return done, remain
	}
	switch runStatus {
	case "queued":
		return "还在排队", "排到后开始，之后按同样间隔汇报"
	case "running", "dispatched":
		line := "仍在执行"
		if comment := truncateIntakeRunes(strings.TrimSpace(latest), 80); comment != "" {
			line += "。" + comment
		}
		return line, "还没有新的划分，会继续做"
	default:
		return "正在匹配", "匹配完就开始"
	}
}

// IntakeProgressFromAgent reads the two labeled lines an agent wrote.
func IntakeProgressFromAgent(text string) (done, remain string, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if rest, found := cutIntakeLabel(line, "做到"); found {
			done = rest
		}
		if rest, found := cutIntakeLabel(line, "还剩"); found {
			remain = rest
		}
	}
	if done == "" || remain == "" {
		return "", "", false
	}
	return done, remain, true
}

func cutIntakeLabel(line, label string) (string, bool) {
	for _, sep := range []string{"：", ":"} {
		if rest, found := strings.CutPrefix(line, label+sep); found {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

func truncateIntakeRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// IntakeDemandLine is the requester-facing first line, capped at 80 runes.
func IntakeDemandLine(text string) string {
	return truncateIntakeRunes(intakeFirstLine(text), 80)
}

// IntakeLabeled joins fields as 名称：内容, one field per line.
func IntakeLabeled(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(pairs[i])
		b.WriteString("：")
		b.WriteString(pairs[i+1])
	}
	return b.String()
}

// IntakeReviewEnvironment is the line a requester sees when work is ready to accept.
func IntakeReviewEnvironment(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return "还没有测试环境"
	}
	return "测试环境：" + url
}

// IntakeReviewNotice is the result, the environment line, and the card body.
// The card names neither an agent nor a machine.
func IntakeReviewNotice(source, result, environment string) (done, remain, card string) {
	result = strings.TrimSpace(result)
	if result == "" {
		result = "做完了"
	}
	card = IntakeLabeled("需求", IntakeDemandLine(source), "结果", result, "测试环境", environment)
	return result, environment, card
}

func noticeIntakeReviews(ctx context.Context, q *db.Queries) (int, error) {
	rows, err := q.ListIntakeReadyForReview(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		environment := IntakeReviewEnvironment(intakeProjectTestURL(ctx, q, row.WorkspaceID, row.ProjectID))
		done, remain, card := IntakeReviewNotice(row.SourceBody, row.ProgressDone.String, environment)
		if DeliverIntakeCard != nil {
			if err := DeliverIntakeCard(ctx, row.WorkspaceID, row.RequesterID, row.IssueID, "待验收", card); err != nil {
				continue
			}
		}
		if err := q.SetIntakeProgress(ctx, intakeProgress(row.ID, done, remain)); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

func intakeProjectTestURL(ctx context.Context, q *db.Queries, workspaceID, projectID pgtype.UUID) string {
	resources, err := q.ListProjectResourcesInWorkspace(ctx, db.ListProjectResourcesInWorkspaceParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return ""
	}
	for _, resource := range resources {
		if resource.ResourceType != "test_environment" {
			continue
		}
		var ref struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(resource.ResourceRef, &ref) == nil {
			return ref.URL
		}
	}
	return ""
}

// IntakeAutoContinueText is written when nobody answers a blocker in 15 minutes.
const IntakeAutoContinueText = "提出人尚未答复。按已有信息继续，不确定处写进结果。"

func autoContinueIntakeBlockers(ctx context.Context, q *db.Queries, tasks *TaskService) (int, error) {
	rows, err := q.ListStaleOpenIntakeBlockers(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		if err := q.CloseIntakeBlocker(ctx, db.CloseIntakeBlockerParams{
			ID: row.BlockerID, State: "continued", DecidedBy: row.RequesterID,
		}); err != nil {
			continue
		}
		_ = q.SetIntakeIssueStatus(ctx, db.SetIntakeIssueStatusParams{ID: row.IssueID, WorkspaceID: row.WorkspaceID, Status: "todo"})
		_ = q.SetIntakeProgress(ctx, intakeProgress(row.RequestID, "按已有信息继续", IntakeAutoContinueText))
		_ = q.TouchIntakeNotice(ctx, row.RequestID)
		if DeliverIntakeCard != nil {
			body := IntakeLabeled("需求", IntakeDemandLine(row.SourceBody), "说明", IntakeAutoContinueText)
			_ = DeliverIntakeCard(ctx, row.WorkspaceID, row.RequesterID, row.IssueID, "按已有信息继续", body)
		}
		if tasks != nil && row.AssigneeID.Valid {
			issue, err := q.GetIssue(ctx, row.IssueID)
			if err == nil {
				_, _ = tasks.EnqueueTaskForIssue(ctx, issue)
			}
		}
		n++
	}
	return n, nil
}
