package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type intakeRun struct {
	Status       string `json:"status"`
	StartedAt    string `json:"started_at,omitempty"`
	DispatchedAt string `json:"dispatched_at,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	AgentName    string `json:"agent_name,omitempty"`
}

type intakeProgress struct {
	Done   string `json:"done,omitempty"`
	Remain string `json:"remain,omitempty"`
}

type intakeBlockerView struct {
	Done        string `json:"done,omitempty"`
	Missing     string `json:"missing,omitempty"`
	Consequence string `json:"consequence,omitempty"`
	State       string `json:"state,omitempty"`
}

type intakeListItem struct {
	IssueID            string             `json:"issue_id"`
	Identifier         string             `json:"identifier,omitempty"`
	Body               string             `json:"body"`
	ProjectID          string             `json:"project_id,omitempty"`
	ProjectTitle       string             `json:"project_title,omitempty"`
	ModuleID           string             `json:"module_id,omitempty"`
	Product            string             `json:"product,omitempty"`
	Module             string             `json:"module,omitempty"`
	AgentID            string             `json:"agent_id,omitempty"`
	AgentName          string             `json:"agent_name,omitempty"`
	RuntimeOnline      *bool              `json:"runtime_online,omitempty"`
	Status             string             `json:"display_status"`
	StartAt            string             `json:"start_at,omitempty"`
	Progress           *intakeProgress    `json:"progress,omitempty"`
	Blocker            *intakeBlockerView `json:"blocker,omitempty"`
	ResultSummary      string             `json:"result_summary,omitempty"`
	UnitLog            string             `json:"unit_log,omitempty"`
	TestEnvironmentURL string             `json:"test_environment_url,omitempty"`
	Run                *intakeRun         `json:"run,omitempty"`
	CanContinue        bool               `json:"can_continue"`
	CanStop            bool               `json:"can_stop"`
	CanAccept          bool               `json:"can_accept"`
	CanCancel          bool               `json:"can_cancel"`
	CanResume          bool               `json:"can_resume"`
	CanMatch           bool               `json:"can_match"`
	UnitSwitches       int                `json:"unit_switches,omitempty"`
}

func (h *Handler) IntakeSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": uuidToString(user.ID), "name": user.Name, "can_maintain": h.intakeCallerMaintains(r, userID),
	})
}

func (h *Handler) ListMyScheduledIntake(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListMyScheduledIntake(r.Context(), db.ListMyScheduledIntakeParams{
		WorkspaceID: parseUUID(workspaceID), RequesterID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list scheduled requests")
		return
	}
	out := make([]intakeListItem, 0, len(rows))
	for _, row := range rows {
		item := intakeListItem{IssueID: uuidToString(row.IssueID), Body: row.SourceBody, Status: "等待处理"}
		if row.StartAt.Valid {
			item.StartAt = row.StartAt.Time.Format(time.RFC3339)
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) ListMyIntakeRequests(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("mine") != "1" {
		h.ListProjectIntakeRequests(w, r)
		return
	}
	projectFilter := strings.TrimSpace(r.URL.Query().Get("project_id"))
	rows, err := h.Queries.ListMyIntakeRequests(r.Context(), db.ListMyIntakeRequestsParams{
		WorkspaceID: parseUUID(workspaceID), RequesterID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list requests")
		return
	}
	now := time.Now()
	prefix := h.getIssuePrefix(r.Context(), parseUUID(workspaceID))
	out := make([]intakeListItem, 0, len(rows))
	for _, row := range rows {
		item := intakeListItem{IssueID: uuidToString(row.IssueID), Body: row.SourceBody}
		fillIntakeItem(&item, row.Status, row.ProgressDone.String, row.ProgressRemain.String, row.RunStatus, row.DoneText.Valid)
		placeIntake(&item, prefix, row.Number, row.ProjectID, row.ProjectTitle, row.ModuleID, row.Product, row.Module)
		item.ModuleID, item.Product, item.Module = "", "", ""
		item.Run = intakeRunFrom(row.RunStatus, row.RunStartedAt)
		intakeRunDispatched(item.Run, row.RunDispatchedAt)
		if row.StartAt.Valid {
			item.StartAt = row.StartAt.Time.Format(time.RFC3339)
		}
		if row.DoneText.Valid {
			item.Blocker = &intakeBlockerView{Done: row.DoneText.String, Missing: row.MissingText.String, Consequence: row.ConsequenceText.String, State: "open"}
		}
		applyIntakeActions(&item, true, true, row.StartAt, row.BlockerAt, now)
		if line, err := h.Queries.LatestIntakeTaskLine(r.Context(), row.IssueID); err == nil && (row.Status == "in_review" || row.Status == "done") {
			item.ResultSummary = truncateIntakeSummary(line.String, 200)
		}
		if projectFilter != "" && item.ProjectID != projectFilter {
			continue
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func intakeDisplayStatus(status string, openBlocker bool, runStatus, done, remain string) string {
	switch {
	case openBlocker:
		return "等你补充"
	case status == "blocked":
		return "已经停住"
	case status == "in_review":
		return "待验收"
	case status == "done":
		return "已完成"
	case remain == "重试之后继续" || remain == "十五分钟后再分一次" || done == "正在换注册单位":
		return "正在做"
	case runStatus == "queued":
		return "等待处理"
	case runStatus == "running" || runStatus == "dispatched" || status == "in_progress":
		return "正在做"
	default:
		return "等待处理"
	}
}

func intakeRunFrom(status string, started pgtype.Timestamptz) *intakeRun {
	if status == "" {
		return nil
	}
	run := &intakeRun{Status: status}
	if started.Valid {
		run.StartedAt = started.Time.Format(time.RFC3339)
	}
	return run
}

func intakeRunDispatched(run *intakeRun, dispatched pgtype.Timestamptz) {
	if run != nil && dispatched.Valid {
		run.DispatchedAt = dispatched.Time.Format(time.RFC3339)
	}
}

func placeIntake(item *intakeListItem, prefix string, number int32, projectID pgtype.UUID, projectTitle string, moduleID pgtype.UUID, product, module string) {
	item.Identifier = service.IssueIdentifier(prefix, number)
	item.ProjectID = uuidToString(projectID)
	item.ProjectTitle = projectTitle
	item.ModuleID = uuidToString(moduleID)
	item.Product = product
	item.Module = module
}

func intakeResultSummary(status, done, remain string) string {
	if status != "in_review" && status != "done" {
		return ""
	}
	summary := done
	if summary == "" {
		summary = remain
	}
	return truncateIntakeSummary(summary, 200)
}

func truncateIntakeSummary(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func fillIntakeItem(item *intakeListItem, status, done, remain, runStatus string, openBlocker bool) {
	item.Status = intakeDisplayStatus(status, openBlocker, runStatus, done, remain)
	if done != "" || remain != "" {
		item.Progress = &intakeProgress{Done: done, Remain: remain}
	}
	item.ResultSummary = intakeResultSummary(status, done, remain)
	if strings.HasPrefix(remain, "测试环境：") {
		item.TestEnvironmentURL = strings.TrimPrefix(remain, "测试环境：")
	}
}

func applyIntakeActions(item *intakeListItem, requester, canAcceptRole bool, startAt, blockerAt pgtype.Timestamptz, now time.Time) {
	open := item.Blocker != nil
	fresh := blockerAt.Valid && now.Sub(blockerAt.Time) < 15*time.Minute
	if requester && open && fresh {
		item.CanContinue = true
		item.CanStop = true
	}
	if canAcceptRole && item.Status == "待验收" {
		item.CanAccept = true
	}
	future := startAt.Valid && startAt.Time.After(now)
	if requester && future && item.Run == nil {
		item.CanCancel = true
	}
	queued := item.Run != nil && item.Run.Status == "queued"
	remain := ""
	if item.Progress != nil {
		remain = item.Progress.Remain
	}
	if requester && item.Status == "等待处理" && !future && !queued && remain == "入队没有成功" {
		item.CanMatch = true
	}
	if requester && item.Status == "等待处理" && !future && !queued && remain == "执行没有进展" {
		item.CanContinue = true
	}
	if requester && item.Status == "等待处理" && !future && !queued && remain != "重试之后继续" && remain != "入队没有成功" && remain != "十五分钟后再分一次" && remain != "执行没有进展" {
		item.CanResume = true
	}
}

type intakeProjectItem struct {
	intakeListItem
	ProjectID    string `json:"project_id"`
	ProjectTitle string `json:"project_title"`
	Mine         bool   `json:"mine"`
	CanDecide    bool   `json:"can_decide"`
	Elapsed      string `json:"elapsed,omitempty"`
	AssigneeName string `json:"assignee_name,omitempty"`
}

func (h *Handler) ListProjectIntakeRequests(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	projectFilter := strings.TrimSpace(r.URL.Query().Get("project_id"))
	rows, err := h.Queries.ListVisibleIntakeRequests(r.Context(), db.ListVisibleIntakeRequestsParams{
		WorkspaceID: parseUUID(workspaceID), LeadID: parseUUID(userID), Column3: requestIsOperator(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list project requests")
		return
	}
	now := time.Now()
	prefix := h.getIssuePrefix(r.Context(), parseUUID(workspaceID))
	out := make([]intakeProjectItem, 0, len(rows))
	for _, row := range rows {
		item := intakeProjectItem{
			intakeListItem: intakeListItem{IssueID: uuidToString(row.IssueID), Body: row.SourceBody},
			ProjectID:      uuidToString(row.ProjectID),
			ProjectTitle:   row.ProjectTitle,
			Mine:           uuidToString(row.RequesterID) == userID,
		}
		fillIntakeItem(&item.intakeListItem, row.Status, row.ProgressDone.String, row.ProgressRemain.String, row.RunStatus, row.DoneText.Valid)
		placeIntake(&item.intakeListItem, prefix, row.Number, row.ProjectID, row.ProjectTitle, row.ModuleID, row.Product, row.Module)
		item.ProjectID = item.intakeListItem.ProjectID
		item.ProjectTitle = item.intakeListItem.ProjectTitle
		item.Run = intakeRunFrom(row.RunStatus, row.RunStartedAt)
		intakeRunDispatched(item.Run, row.RunDispatchedAt)
		if row.StartAt.Valid {
			item.StartAt = row.StartAt.Time.Format(time.RFC3339)
		}
		if row.DoneText.Valid {
			item.Blocker = &intakeBlockerView{Done: row.DoneText.String, Missing: row.MissingText.String, Consequence: row.ConsequenceText.String, State: "open"}
			item.CanDecide = item.Mine && row.BlockerAt.Valid && now.Sub(row.BlockerAt.Time) < 15*time.Minute
		}
		canAccept := item.Mine || member.Role == "owner" || member.Role == "admin"
		applyIntakeActions(&item.intakeListItem, item.Mine, canAccept, row.StartAt, row.BlockerAt, now)
		maintains := member.Role == "owner" || member.Role == "admin" || uuidToString(row.LeadID) == userID
		if maintains && !item.CanResume && item.Status == "等待处理" && item.Run == nil && (row.StartAt.Valid == false || !row.StartAt.Time.After(now)) {
			remain := ""
			if item.Progress != nil {
				remain = item.Progress.Remain
			}
			if remain == "执行没有进展" {
				item.CanContinue = true
			} else if remain != "重试之后继续" && remain != "入队没有成功" && remain != "十五分钟后再分一次" {
				item.CanResume = true
			}
		}
		if row.RunStartedAt.Valid && item.Run != nil && item.Run.Status == "running" {
			item.Elapsed = intakeElapsed(now.Sub(row.RunStartedAt.Time))
		}
		if row.UnitSwitches > 0 {
			item.UnitSwitches = int(row.UnitSwitches)
		}
		if row.UnitLog != "" && (member.Role == "owner" || member.Role == "admin" || uuidToString(row.LeadID) == userID) {
			item.UnitLog = row.UnitLog
		}
		if line, err := h.Queries.LatestIntakeTaskLine(r.Context(), row.IssueID); err == nil && (row.Status == "in_review" || row.Status == "done") {
			item.ResultSummary = truncateIntakeSummary(line.String, 200)
		}
		if projectFilter != "" && item.ProjectID != projectFilter {
			continue
		}
		if row.AssigneeName.Valid && (member.Role == "owner" || member.Role == "admin" || uuidToString(row.LeadID) == userID) {
			item.AgentID = uuidToString(row.AgentID)
			item.AgentName = row.AssigneeName.String
			online := row.RuntimeOnline
			item.RuntimeOnline = &online
			if item.Run != nil {
				item.Run.AgentID = item.AgentID
				item.Run.AgentName = item.AgentName
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) intakeTranscriptAllowed(r *http.Request, workspaceID pgtype.UUID, task db.AgentTaskQueue) (bool, error) {
	req, err := h.Queries.GetIntakeRequestByIssue(r.Context(), db.GetIntakeRequestByIssueParams{WorkspaceID: workspaceID, IssueID: task.IssueID})
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	userID := requestUserID(r)
	if uuidToString(req.RequesterID) == userID {
		return true, nil
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID: parseUUID(userID), WorkspaceID: workspaceID,
	})
	if err == nil && (member.Role == "owner" || member.Role == "admin") {
		return true, nil
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err == nil && uuidToString(agent.OwnerID) == userID {
		return true, nil
	}
	return false, nil
}

func intakeElapsed(d time.Duration) string {
	if d < time.Minute {
		return "不到 1 分钟"
	}
	if d < time.Hour {
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if mins == 0 {
		return fmt.Sprintf("%d 小时", hours)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", hours, mins)
}
