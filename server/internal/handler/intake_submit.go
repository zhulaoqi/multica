package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type intakeSubmitBody struct {
	Body            string `json:"body"`
	ClientRequestID string `json:"client_request_id"`
	StartAt         string `json:"start_at"`
}

type intakeSubmitResponse struct {
	IssueID      string `json:"issue_id"`
	EnqueueError string `json:"enqueue_error,omitempty"`
}

func (h *Handler) CreateIntakeRequest(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var body intakeSubmitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	text := strings.TrimSpace(body.Body)
	if text == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	clientID, ok := parseUUIDOrBadRequest(w, body.ClientRequestID, "client_request_id")
	if !ok {
		return
	}
	wsUUID := parseUUID(workspaceID)
	userUUID := parseUUID(userID)
	existing, err := h.Queries.GetIntakeRequestByClient(r.Context(), db.GetIntakeRequestByClientParams{
		WorkspaceID: wsUUID, RequesterID: userUUID, ClientRequestID: clientID,
	})
	if err == nil {
		writeJSON(w, http.StatusOK, intakeSubmitResponse{IssueID: uuidToString(existing.IssueID)})
		return
	}
	if !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, "could not load request")
		return
	}

	modules, err := h.Queries.ListIntakeModulesForCaller(r.Context(), db.ListIntakeModulesForCallerParams{
		WorkspaceID: wsUUID, LeadID: userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load modules")
		return
	}
	if len(modules) == 0 {
		writeErrorCode(w, http.StatusUnprocessableEntity, "intake_project_lead_missing", "no project is available")
		return
	}
	choices := make([]service.IntakeModuleChoice, 0, len(modules))
	for _, module := range modules {
		choices = append(choices, service.IntakeModuleChoice{
			ID: uuidToString(module.ID), Product: module.Product, Module: module.Module, Scope: module.Scope,
		})
	}
	var gen service.TextGenerator
	if h.LLM != nil {
		gen = h.LLM
	}
	split, err := service.SplitIntakeUtterance(r.Context(), gen, choices, text)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "intake_split_empty", "could not place the request")
		return
	}
	var chosen *db.ListIntakeModulesForCallerRow
	for i := range modules {
		if uuidToString(modules[i].ID) == split.ModuleID {
			chosen = &modules[i]
			break
		}
	}
	if chosen == nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "intake_split_empty", "could not place the request")
		return
	}

	var startAt time.Time
	future := false
	if strings.TrimSpace(body.StartAt) != "" {
		parsed, err := time.Parse(time.RFC3339, body.StartAt)
		if err != nil || parsed.After(time.Now().AddDate(1, 0, 0)) {
			writeError(w, http.StatusBadRequest, "start_at must be an RFC3339 time within one year")
			return
		}
		startAt = parsed
		future = parsed.After(time.Now())
	}
	now := time.Now()
	primaryOnline := h.intakeAgentOnline(r.Context(), chosen.AgentID, now)
	backupOnline := false
	backupID := ""
	if chosen.BackupAgentID.Valid {
		backupID = uuidToString(chosen.BackupAgentID)
		backupOnline = h.intakeAgentOnline(r.Context(), chosen.BackupAgentID, now)
	}
	plan := service.DispatchIntake(uuidToString(chosen.AgentID), backupID, primaryOnline, backupOnline, future)
	description := chosen.Product + " / " + chosen.Module + "\n" + chosen.Scope + "\n" + service.IntakeTaskDescription(split.Tasks)
	description += h.intakeTestEnvironmentLine(r.Context(), wsUUID, chosen.ProjectID)
	if plan.SwitchedToBackup {
		description += "\n主责不在线，所以交给备责。"
	}
	assignee := parseUUID(plan.AssigneeID)
	status := "todo"
	if !plan.Enqueue {
		status = "backlog"
	}
	created, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID:  wsUUID,
		Title:        intakeTitle(split.Tasks[0]),
		Description:  pgtype.Text{String: description, Valid: true},
		Status:       status,
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   assignee,
		CreatorType:  "member",
		CreatorID:    userUUID,
		ProjectID:    chosen.ProjectID,
	}, service.IssueCreateOpts{ActorID: userID, AnalyticsAgentID: plan.AssigneeID})
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create request")
		return
	}
	start := pgtype.Timestamptz{}
	if future {
		start = pgtype.Timestamptz{Time: startAt, Valid: true}
	}
	stored, err := h.Queries.CreateIntakeRequest(r.Context(), db.CreateIntakeRequestParams{
		WorkspaceID: wsUUID, IssueID: created.Issue.ID, ProjectID: chosen.ProjectID, ModuleID: chosen.ID,
		RequesterID: userUUID, ClientRequestID: clientID, SourceBody: text, StartAt: start,
	})
	if err != nil {
		if isUniqueViolation(err) {
			again, loadErr := h.Queries.GetIntakeRequestByClient(r.Context(), db.GetIntakeRequestByClientParams{
				WorkspaceID: wsUUID, RequesterID: userUUID, ClientRequestID: clientID,
			})
			if loadErr == nil {
				writeJSON(w, http.StatusOK, intakeSubmitResponse{IssueID: uuidToString(again.IssueID)})
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "could not store request")
		return
	}
	if future {
		_, _ = h.Queries.CreateIssueWakeup(r.Context(), db.CreateIssueWakeupParams{
			ID: parseUUID(uuid.NewString()), WorkspaceID: wsUUID, IssueID: created.Issue.ID,
			AgentID: assignee, CreatedBy: userUUID, Instruction: text,
			Kind: "at", Mode: "once", Timezone: "UTC",
			NextFireAt: pgtype.Timestamptz{Time: startAt, Valid: true},
		})
	}
	if !plan.Enqueue {
		_ = h.Queries.SetIntakeIssueTodo(r.Context(), db.SetIntakeIssueTodoParams{
			ID: created.Issue.ID, WorkspaceID: wsUUID,
		})
		if !future {
			_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(stored.ID, "还没开始", "需要维护"))
			service.NotifyIntakeMaintenance(r.Context(), h.Queries, wsUUID, chosen.ProjectID, assignee, userUUID, created.Issue.ID, text)
		}
	}
	resp := intakeSubmitResponse{IssueID: uuidToString(created.Issue.ID)}
	if plan.Enqueue && !created.AssignedTaskID.Valid {
		resp.EnqueueError = "enqueue_failed"
		_ = h.Queries.SetIntakeProgress(r.Context(), setIntakeProgress(stored.ID, "还没排上", "入队没有成功"))
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) intakeAgentOnline(ctx context.Context, agentID pgtype.UUID, now time.Time) bool {
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || !agent.RuntimeID.Valid {
		return false
	}
	runtime, err := h.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil || runtime.Status != "online" {
		return false
	}
	seen := runtime.UpdatedAt.Time
	if runtime.LastSeenAt.Valid {
		seen = runtime.LastSeenAt.Time
	}
	return now.Sub(seen) <= time.Duration(service.RuntimeClaimFreshnessSeconds)*time.Second
}

func (h *Handler) intakeTestEnvironmentLine(ctx context.Context, workspaceID, projectID pgtype.UUID) string {
	resources, err := h.Queries.ListProjectResourcesInWorkspace(ctx, db.ListProjectResourcesInWorkspaceParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return "\n该项目还没有测试环境"
	}
	for _, resource := range resources {
		if resource.ResourceType != "test_environment" {
			continue
		}
		var ref struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(resource.ResourceRef, &ref) == nil && strings.TrimSpace(ref.URL) != "" {
			return "\n测试环境：" + strings.TrimSpace(ref.URL)
		}
	}
	return "\n该项目还没有测试环境"
}

func intakeTitle(task string) string {
	task = strings.Join(strings.Fields(task), " ")
	if utf8.RuneCountInString(task) <= 80 {
		return task
	}
	return string([]rune(task)[:80])
}
