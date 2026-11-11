package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type intakeBlockerBody struct {
	TaskID      string `json:"task_id"`
	Done        string `json:"done"`
	Missing     string `json:"missing"`
	Consequence string `json:"consequence"`
}

type intakeContinueBody struct {
	Body string `json:"body"`
}

func (h *Handler) CreateIntakeBlocker(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" {
		writeError(w, http.StatusForbidden, "only the executing agent can report a blocker")
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue id")
	if !ok {
		return
	}
	issue, err := h.Queries.GetIssue(r.Context(), issueID)
	if err != nil || uuidToString(issue.WorkspaceID) != workspaceID || uuidToString(issue.AssigneeID) != actorID {
		writeError(w, http.StatusForbidden, "this agent is not the assignee")
		return
	}
	var body intakeBlockerBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	done, missing, consequence := strings.TrimSpace(body.Done), strings.TrimSpace(body.Missing), strings.TrimSpace(body.Consequence)
	if done == "" || missing == "" || consequence == "" {
		writeError(w, http.StatusBadRequest, "done, missing, and consequence are required")
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, body.TaskID, "task_id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || uuidToString(task.IssueID) != uuidToString(issueID) || uuidToString(task.AgentID) != actorID {
		writeError(w, http.StatusBadRequest, "task does not belong to this agent")
		return
	}
	continued, _ := h.Queries.CountContinuedIntakeBlockers(r.Context(), issueID)
	blockerID, err := h.Queries.InsertIntakeBlocker(r.Context(), db.InsertIntakeBlockerParams{
		WorkspaceID: parseUUID(workspaceID), IssueID: issueID, TaskID: taskID, AgentID: parseUUID(actorID),
		DoneText: done, MissingText: missing, ConsequenceText: consequence,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErrorCode(w, http.StatusConflict, "intake_blocker_open", "a blocker is already open")
			return
		}
		writeError(w, http.StatusBadRequest, "could not record blocker")
		return
	}
	if continued > 0 && h.switchIntakeToBackup(r, issue, actorID) {
		_ = h.Queries.CloseIntakeBlocker(r.Context(), db.CloseIntakeBlockerParams{
			ID: blockerID, State: "continued", DecidedBy: issue.CreatorID,
		})
		writeJSON(w, http.StatusCreated, map[string]string{"state": "switched"})
		return
	}
	_ = h.Queries.SetIntakeIssueStatus(r.Context(), db.SetIntakeIssueStatusParams{ID: issueID, WorkspaceID: parseUUID(workspaceID), Status: "blocked"})
	if reqRow, err := h.Queries.GetIntakeRequestByIssue(r.Context(), db.GetIntakeRequestByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID}); err == nil && service.DeliverIntakeCard != nil {
		body := service.IntakeLabeled("需求", service.IntakeDemandLine(reqRow.SourceBody), "已经做了", done, "缺什么", missing, "不补会怎样", consequence)
		hour := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Hour()
		if hour >= 22 || hour < 8 {
			body += "\n提示：15 分钟内未答复将按已有信息继续"
		}
		_ = service.DeliverIntakeCard(r.Context(), issue.WorkspaceID, reqRow.RequesterID, issue.ID, "等你补充", body)
	}
	_, _, _ = inbox.CreateIfEligible(r.Context(), h.Queries, db.CreateInboxItemParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, RecipientType: "member", RecipientID: issue.CreatorID,
		Type: "intake_blocker", Severity: "action", IssueID: issue.ID, Title: "等你补充",
		Body:      pgtype.Text{String: done + "\n" + missing + "\n" + consequence, Valid: true},
		ActorType: pgtype.Text{String: "agent", Valid: true}, ActorID: parseUUID(actorID),
	})
	writeJSON(w, http.StatusCreated, map[string]string{"state": "open"})
}

func (h *Handler) switchIntakeToBackup(r *http.Request, issue db.Issue, currentAgentID string) bool {
	reqRow, err := h.Queries.GetIntakeRequestByIssue(r.Context(), db.GetIntakeRequestByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		return false
	}
	module, err := h.Queries.GetIntakeModuleByID(r.Context(), db.GetIntakeModuleByIDParams{WorkspaceID: issue.WorkspaceID, ID: reqRow.ModuleID})
	if err != nil || !module.BackupAgentID.Valid || uuidToString(module.AgentID) != currentAgentID {
		return false
	}
	if !h.intakeAgentOnline(r.Context(), module.BackupAgentID, time.Now()) {
		return false
	}
	_, _ = h.DB.Exec(r.Context(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $3, status = 'todo', updated_at = now() WHERE id = $1 AND workspace_id = $2`,
		issue.ID, issue.WorkspaceID, module.BackupAgentID)
	_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "还在继续", "接着做"))
	unitName := "备责"
	if agent, err := h.Queries.GetAgent(r.Context(), module.BackupAgentID); err == nil && agent.Name != "" {
		unitName = agent.Name
	}
	_ = h.Queries.AddIntakeUnitSwitch(r.Context(), db.AddIntakeUnitSwitchParams{ID: reqRow.ID, UnitLog: unitName})
	_ = h.Queries.SetIntakeStartNotice(r.Context(), db.SetIntakeStartNoticeParams{ID: reqRow.ID, StartNoticeAgentID: module.BackupAgentID})
	_ = h.Queries.TouchIntakeNotice(r.Context(), reqRow.ID)
	if service.DeliverIntakeCard != nil {
		_ = service.DeliverIntakeCard(r.Context(), issue.WorkspaceID, reqRow.RequesterID, issue.ID, "还在继续", service.IntakeLabeled("需求", service.IntakeDemandLine(reqRow.SourceBody), "状态", "还在继续"))
	}
	updated, err := h.Queries.GetIssue(r.Context(), issue.ID)
	if err != nil || h.TaskService == nil {
		return true
	}
	_, _ = h.TaskService.EnqueueIntakeTask(r.Context(), updated, true)
	return true
}

func (h *Handler) ContinueIntakeRequest(w http.ResponseWriter, r *http.Request) {
	h.decideIntake(w, r, "continued")
}

func (h *Handler) StopIntakeRequest(w http.ResponseWriter, r *http.Request) {
	h.decideIntake(w, r, "stopped")
}

func (h *Handler) decideIntake(w http.ResponseWriter, r *http.Request, state string) {
	reqRow, issue, member, userID, ok := h.loadIntakeDecision(w, r)
	if !ok {
		return
	}
	if !intakeCanDecide(member, reqRow, userID) {
		writeError(w, http.StatusForbidden, "cannot decide this request")
		return
	}
	var body intakeContinueBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	text := strings.TrimSpace(body.Body)
	if state == "continued" && text == "" && reqRow.ProgressRemain.String == "执行没有进展" {
		h.enqueueIntakeContinuation(w, r, reqRow, issue)
		return
	}
	blocker, err := h.Queries.GetOpenIntakeBlocker(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "no open blocker")
		return
	}
	if state == "continued" && text == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	if err := h.Queries.CloseIntakeBlocker(r.Context(), db.CloseIntakeBlockerParams{
		ID: blocker.ID, State: state, DecidedBy: parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not close the blocker")
		return
	}
	if text != "" {
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "补充已收到", text))
	}
	_ = h.Queries.TouchIntakeNotice(r.Context(), reqRow.ID)
	if state == "stopped" {
		if h.TaskService != nil {
			_ = h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
		}
		remain := text
		if remain == "" {
			remain = "提出人要求停止"
		}
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "已经停住", remain))
		_ = h.Queries.SetIntakeIssueStatus(r.Context(), db.SetIntakeIssueStatusParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: "blocked"})
		writeJSON(w, http.StatusOK, map[string]string{"state": "stopped"})
		return
	}
	active, err := h.Queries.GetActiveIntakeTask(r.Context(), db.GetActiveIntakeTaskParams{IssueID: issue.ID, AgentID: issue.AssigneeID})
	if err == nil && active.Status == "running" {
		_, supplementErr := h.Queries.CreateTaskSupplement(r.Context(), db.CreateTaskSupplementParams{
			TaskID: active.ID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
			AuthorID: parseUUID(userID), Content: text, ClientRequestID: parseUUID(uuid.NewString()),
		})
		if supplementErr == nil {
			_ = h.Queries.SetIntakeIssueStatus(r.Context(), db.SetIntakeIssueStatusParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: "todo"})
			writeJSON(w, http.StatusOK, map[string]string{"state": "continued"})
			return
		}
	}
	h.enqueueIntakeContinuation(w, r, reqRow, issue)
}

func (h *Handler) enqueueIntakeContinuation(w http.ResponseWriter, r *http.Request, reqRow db.GetIntakeRequestByIssueRow, issue db.Issue) {
	_ = h.Queries.SetIntakeIssueStatus(r.Context(), db.SetIntakeIssueStatusParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: "todo"})
	_ = h.Queries.TouchIntakeNotice(r.Context(), reqRow.ID)
	updated, err := h.Queries.GetIssue(r.Context(), issue.ID)
	if err != nil || h.TaskService == nil {
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "还没排上", "入队没有成功"))
		writeJSON(w, http.StatusOK, map[string]string{"state": "continued", "enqueue_error": "enqueue_failed"})
		return
	}
	if _, err := h.TaskService.EnqueueTaskForIssue(r.Context(), updated); err != nil {
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "还没排上", "入队没有成功"))
		writeJSON(w, http.StatusOK, map[string]string{"state": "continued", "enqueue_error": "enqueue_failed"})
		return
	}
	if reqRow.ProgressRemain.String == "执行没有进展" {
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "正在匹配", "匹配完就开始"))
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "continued"})
}

func (h *Handler) AcceptIntakeRequest(w http.ResponseWriter, r *http.Request) {
	reqRow, issue, member, userID, ok := h.loadIntakeDecision(w, r)
	if !ok {
		return
	}
	if !intakeCanDecide(member, reqRow, userID) {
		writeError(w, http.StatusForbidden, "cannot accept this request")
		return
	}
	if _, err := h.Queries.GetOpenIntakeBlocker(r.Context(), issue.ID); err == nil {
		writeError(w, http.StatusConflict, "a blocker is still open")
		return
	}
	if issue.Status != "in_review" {
		writeError(w, http.StatusConflict, "request is not ready to accept")
		return
	}
	_ = h.Queries.SetIntakeIssueStatus(r.Context(), db.SetIntakeIssueStatusParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: "done"})
	writeJSON(w, http.StatusOK, map[string]string{"state": "done"})
}

func (h *Handler) CancelIntakeRequest(w http.ResponseWriter, r *http.Request) {
	reqRow, issue, member, userID, ok := h.loadIntakeDecision(w, r)
	if !ok {
		return
	}
	if !intakeCanDecide(member, reqRow, userID) {
		writeError(w, http.StatusForbidden, "cannot cancel this request")
		return
	}
	if _, err := h.Queries.GetActiveIntakeTask(r.Context(), db.GetActiveIntakeTaskParams{IssueID: issue.ID, AgentID: issue.AssigneeID}); err == nil {
		writeError(w, http.StatusConflict, "request already started")
		return
	}
	_ = h.Queries.DisableIntakeWakeups(r.Context(), db.DisableIntakeWakeupsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	_ = h.Queries.SetIntakeIssueStatus(r.Context(), db.SetIntakeIssueStatusParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: "cancelled"})
	writeJSON(w, http.StatusOK, map[string]string{"state": "cancelled"})
}

func (h *Handler) MatchIntakeRequest(w http.ResponseWriter, r *http.Request) {
	h.redispatchIntake(w, r, false)
}

func (h *Handler) ResumeIntakeRequest(w http.ResponseWriter, r *http.Request) {
	h.redispatchIntake(w, r, true)
}

func (h *Handler) redispatchIntake(w http.ResponseWriter, r *http.Request, allowLead bool) {
	reqRow, issue, member, userID, ok := h.loadIntakeDecision(w, r)
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: reqRow.ProjectID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !(intakeCanDecide(member, reqRow, userID) || (allowLead && intakeCanMaintain(member, project, userID))) {
		writeError(w, http.StatusForbidden, "cannot resume this request")
		return
	}
	module, err := h.Queries.GetIntakeModuleByID(r.Context(), db.GetIntakeModuleByIDParams{WorkspaceID: issue.WorkspaceID, ID: reqRow.ModuleID})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "module is gone")
		return
	}
	now := time.Now()
	backupID := ""
	if module.BackupAgentID.Valid {
		backupID = uuidToString(module.BackupAgentID)
	}
	plan := service.DispatchIntake(uuidToString(module.AgentID), backupID,
		h.intakeAgentOnline(r.Context(), module.AgentID, now),
		module.BackupAgentID.Valid && h.intakeAgentOnline(r.Context(), module.BackupAgentID, now),
		false)
	_, _ = h.DB.Exec(r.Context(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $3, status = 'todo', updated_at = now() WHERE id = $1 AND workspace_id = $2`,
		issue.ID, issue.WorkspaceID, parseUUID(plan.AssigneeID))
	if !plan.Enqueue {
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "还没开始", "需要维护"))
		service.NotifyIntakeMaintenance(r.Context(), h.Queries, issue.WorkspaceID, reqRow.ProjectID, parseUUID(plan.AssigneeID), reqRow.RequesterID, issue.ID, reqRow.SourceBody)
		writeJSON(w, http.StatusOK, map[string]string{"state": "waiting"})
		return
	}
	updated, err := h.Queries.GetIssue(r.Context(), issue.ID)
	if err != nil || h.TaskService == nil {
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "还没排上", "入队没有成功"))
		writeJSON(w, http.StatusOK, map[string]string{"state": "waiting", "enqueue_error": "enqueue_failed"})
		return
	}
	if _, err := h.TaskService.EnqueueIntakeTask(r.Context(), updated, false); err != nil {
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(reqRow.ID, "还没排上", "入队没有成功"))
		writeJSON(w, http.StatusOK, map[string]string{"state": "waiting", "enqueue_error": "enqueue_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "queued"})
}

func (h *Handler) loadIntakeDecision(w http.ResponseWriter, r *http.Request) (db.GetIntakeRequestByIssueRow, db.Issue, db.Member, string, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.GetIntakeRequestByIssueRow{}, db.Issue{}, db.Member{}, "", false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.GetIntakeRequestByIssueRow{}, db.Issue{}, db.Member{}, "", false
	}
	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue id")
	if !ok {
		return db.GetIntakeRequestByIssueRow{}, db.Issue{}, db.Member{}, "", false
	}
	issue, err := h.Queries.GetIssue(r.Context(), issueID)
	if err != nil || uuidToString(issue.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "request not found")
		return db.GetIntakeRequestByIssueRow{}, db.Issue{}, db.Member{}, "", false
	}
	reqRow, err := h.Queries.GetIntakeRequestByIssue(r.Context(), db.GetIntakeRequestByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		writeError(w, http.StatusNotFound, "request not found")
		return db.GetIntakeRequestByIssueRow{}, db.Issue{}, db.Member{}, "", false
	}
	return reqRow, issue, member, userID, true
}

func intakeCanDecide(member db.Member, reqRow db.GetIntakeRequestByIssueRow, userID string) bool {
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	return uuidToString(reqRow.RequesterID) == userID
}
