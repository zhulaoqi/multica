package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// insertProjectAgent creates a user agent in projectID bound to an online
// runtime there, so a project dispatch has an executor to hand the work to.
func insertProjectAgent(t *testing.T, projectID, name, ownerID string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'test', 'online', 'project dispatch runtime', '{}'::jsonb, $3, now())
		RETURNING id
	`, projectID, "project-runtime-"+name, ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("create project runtime %s: %v", name, err)
	}
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, projectID, name, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create project agent %s: %v", name, err)
	}
	t.Cleanup(func() {
		// The agent still points at the runtime when cleanups run in reverse
		// registration order, so unbind before deleting the row.
		testPool.Exec(context.Background(), `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return agentID
}

type projectDispatchSetup struct {
	admin     string
	requester string
	unitID    string
	projectID string
	agentID   string
}

// setupProjectDispatch builds the C-end case: an organization with one
// department that owns one project, a requester its project list admits, and
// (when onlineAgent) an online agent in the project workspace.
func setupProjectDispatch(t *testing.T, prefix, projectName string, onlineAgent bool) projectDispatchSetup {
	t.Helper()
	startDispatchTest(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, prefix+"-admin@example.com")
	requester := createPermissionTestMember(t, prefix+"-requester@example.com")
	// Dispatch requires the requester's own twin in the organization; this
	// agent is not placed in a department, so it does not make the department
	// dispatchable.
	insertRunnableDutyAgent(t, prefix+"-requester-agent", requester)
	unitID := createOrgUnitAs(t, admin, prefix+"部", "")
	projectID := createOrganizationProject(t, projectName, unitID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_intake_source WHERE workspace_id = $1`, projectID)
		clearDispatchFixtures(t)
	})
	addWorkspaceIntakeSource(t, projectID, "user", requester)
	setup := projectDispatchSetup{
		admin:     admin,
		requester: requester,
		unitID:    unitID,
		projectID: projectID,
	}
	if onlineAgent {
		setup.agentID = insertProjectAgent(t, projectID, prefix+"-project-agent", admin)
	}
	return setup
}

func TestDispatchWork_ProjectTargetDeliversIntoTheProjectWorkspace(t *testing.T) {
	setup := setupProjectDispatch(t, "project-target", "数据中台", true)

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 整理数据中台的接入文档",
		"client_request_id": "66666666-6666-6666-6666-666666666001",
		"project_id":        setup.projectID,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	if res.Issue.WorkspaceID != setup.projectID {
		t.Fatalf("issue workspace = %s, want project %s", res.Issue.WorkspaceID, setup.projectID)
	}
	if res.Issue.AssigneeType == nil || *res.Issue.AssigneeType != "agent" {
		t.Fatalf("assignee type = %v, want agent", res.Issue.AssigneeType)
	}
	if res.Issue.AssigneeID == nil || *res.Issue.AssigneeID != setup.agentID {
		t.Fatalf("assignee = %v, want project agent %s", res.Issue.AssigneeID, setup.agentID)
	}
	if res.Dispatch.ProjectID != setup.projectID || res.Dispatch.ProjectName != "数据中台" {
		t.Fatalf("dispatch = %+v, want project %s", res.Dispatch, setup.projectID)
	}

	var requester, targetUnit, targetProject string
	var targetSquad pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `
		SELECT requester_id, target_unit_id, target_project_id, target_squad_id
		FROM dispatch_origin WHERE issue_id = $1
	`, res.Issue.ID).Scan(&requester, &targetUnit, &targetProject, &targetSquad); err != nil {
		t.Fatalf("load dispatch origin: %v", err)
	}
	if requester != setup.requester || targetUnit != setup.unitID || targetProject != setup.projectID {
		t.Fatalf("origin = %s/%s/%s, want requester/%s/%s", requester, targetUnit, targetProject, setup.unitID, setup.projectID)
	}
	if targetSquad.Valid {
		t.Fatalf("origin target squad = %v, want none for a project dispatch", targetSquad)
	}

	var leaderTask bool
	var squadID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `
		SELECT is_leader_task, squad_id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2
	`, res.Issue.ID, setup.agentID).Scan(&leaderTask, &squadID); err != nil {
		t.Fatalf("load project executor task: %v", err)
	}
	if !leaderTask || squadID.Valid {
		t.Fatalf("task leader/squad = %v/%v, want a leader task without a squad", leaderTask, squadID)
	}
}

// TestDispatchWork_FreeTextNamesProjectBeforeTheModel covers the original
// report: "AIHUB now serves which models?" names the project, so the project
// runs it even though the owning department has no organization agent, and the
// model is never asked to second-guess the name.
func TestDispatchWork_FreeTextNamesProjectBeforeTheModel(t *testing.T) {
	setup := setupProjectDispatch(t, "free-text-project", "AIHUB", true)

	llmCalls := 0
	previous := testHandler.OrgDispatchService.LLM
	testHandler.OrgDispatchService.LLM = fakeTextGenerator(func(string, string) (string, error) {
		llmCalls++
		// A department answer must not be able to pull the work back.
		return `{"target_type":"department","target_id":"` + setup.unitID + `"}`, nil
	})
	t.Cleanup(func() { testHandler.OrgDispatchService.LLM = previous })

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "AIHUB现在接了哪些模型？",
		"client_request_id": "66666666-6666-6666-6666-666666666002",
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	if llmCalls != 0 {
		t.Fatalf("model calls = %d, want 0: the project name is a deterministic match", llmCalls)
	}
	if res.Dispatch.ProjectID != setup.projectID || res.Dispatch.ProjectName != "AIHUB" {
		t.Fatalf("dispatch = %+v, want project %s", res.Dispatch, setup.projectID)
	}
	if res.Issue.WorkspaceID != setup.projectID {
		t.Fatalf("issue workspace = %s, want project %s", res.Issue.WorkspaceID, setup.projectID)
	}
	if res.Issue.AssigneeID == nil || *res.Issue.AssigneeID != setup.agentID {
		t.Fatalf("assignee = %v, want project agent %s", res.Issue.AssigneeID, setup.agentID)
	}
}

// TestDispatchWork_ProjectWinsTieWithItsDepartment keeps a project named like
// its department authoritative: a tie on the containment length goes to the
// project, which is the label the requester actually used.
func TestDispatchWork_ProjectWinsTieWithItsDepartment(t *testing.T) {
	setup := setupProjectDispatch(t, "project-tie", "结算中心", true)
	// Put an organization agent in the department too, so the department is a
	// real candidate and the tie is decided by the rule, not by absence.
	deptAgent := insertRunnableDutyAgent(t, "project-tie-dept-agent", setup.admin)
	placeOrgAgent(t, setup.admin, deptAgent, setup.unitID)

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 结算中心的对账口径",
		"client_request_id": "66666666-6666-6666-6666-666666666003",
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	if res.Dispatch.ProjectID != setup.projectID {
		t.Fatalf("dispatch = %+v, want the named project %s to win the tie", res.Dispatch, setup.projectID)
	}
	if res.Issue.WorkspaceID != setup.projectID {
		t.Fatalf("issue workspace = %s, want project %s", res.Issue.WorkspaceID, setup.projectID)
	}
}

func TestDispatchWork_ProjectWithoutOnlineAgentIsRefused(t *testing.T) {
	setup := setupProjectDispatch(t, "project-offline", "离线项目", false)

	status, _, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 离线项目的问题",
		"client_request_id": "66666666-6666-6666-6666-666666666004",
		"project_id":        setup.projectID,
	})
	if status != http.StatusConflict || !strings.Contains(body, "org_dispatch_project_agent_offline") {
		t.Fatalf("status = %d %s, want 409 org_dispatch_project_agent_offline", status, body)
	}
	if n := countDispatchOrigins(t); n != 0 {
		t.Fatalf("refused dispatch created %d origins, want 0", n)
	}
	if n := countRows(t, `SELECT count(*) FROM issue WHERE workspace_id = $1`, setup.projectID); n != 0 {
		t.Fatalf("refused dispatch created %d issues in the project, want 0", n)
	}
}

func TestDispatchWork_RehomesRequesterAttachmentsIntoTheProject(t *testing.T) {
	setup := setupProjectDispatch(t, "project-attach", "附件项目", true)

	var attachmentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'brief.pdf', 'https://files.example/brief.pdf', 'application/pdf', 2048)
		RETURNING id::text
	`, testWorkspaceID, setup.requester).Scan(&attachmentID); err != nil {
		t.Fatalf("seed requester attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
	})

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 按附件整理项目材料",
		"client_request_id": "66666666-6666-6666-6666-666666666005",
		"project_id":        setup.projectID,
		"attachment_ids":    []string{attachmentID},
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	var workspace, issueID, filename string
	if err := testPool.QueryRow(context.Background(), `
		SELECT workspace_id, issue_id, filename FROM attachment WHERE id = $1
	`, attachmentID).Scan(&workspace, &issueID, &filename); err != nil {
		t.Fatalf("load rehomed attachment: %v", err)
	}
	if workspace != setup.projectID || issueID != res.Issue.ID || filename != "brief.pdf" {
		t.Fatalf("attachment = %s/%s/%s, want project/issue/brief.pdf", workspace, issueID, filename)
	}

	// A file that is not the requester's own stays where it is and fails the
	// whole dispatch instead of being dropped silently.
	var foreignAttachmentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'foreign.pdf', 'https://files.example/foreign.pdf', 'application/pdf', 64)
		RETURNING id::text
	`, testWorkspaceID, setup.admin).Scan(&foreignAttachmentID); err != nil {
		t.Fatalf("seed foreign attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, foreignAttachmentID)
	})
	status, _, body = postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 用别人的附件",
		"client_request_id": "66666666-6666-6666-6666-666666666006",
		"project_id":        setup.projectID,
		"attachment_ids":    []string{foreignAttachmentID},
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "attachment_not_found") {
		t.Fatalf("foreign attachment = %d %s, want 400 attachment_not_found", status, body)
	}
	var leftInOrg string
	if err := testPool.QueryRow(context.Background(), `
		SELECT workspace_id FROM attachment WHERE id = $1
	`, foreignAttachmentID).Scan(&leftInOrg); err != nil {
		t.Fatalf("load foreign attachment: %v", err)
	}
	if leftInOrg != testWorkspaceID {
		t.Fatalf("foreign attachment moved to %s, want to stay in %s", leftInOrg, testWorkspaceID)
	}
}

func TestGetDispatchTrail_ReportsTheProjectDelivery(t *testing.T) {
	setup := setupProjectDispatch(t, "project-trail", "轨迹项目", true)

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 轨迹项目的交付",
		"client_request_id": "66666666-6666-6666-6666-666666666007",
		"project_id":        setup.projectID,
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}
	rootID := res.Issue.ID

	insertChild := func(title, childStatus string) string {
		var childID string
		if err := testPool.QueryRow(context.Background(), `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, parent_issue_id, number)
			VALUES ($1, $2, $3, 'none', 'member', $4, $5,
			        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
			RETURNING id::text
		`, setup.projectID, title, childStatus, setup.requester, rootID).Scan(&childID); err != nil {
			t.Fatalf("insert child %s: %v", title, err)
		}
		return childID
	}
	insertChild("dispatch-test 子任务一", "done")
	insertChild("dispatch-test 子任务二", "in_progress")

	// An internal member comment must not become the reported answer, while the
	// project agent's answer and the file it produced are what the requester
	// came to see.
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, author_type, author_id, content, type, workspace_id)
		VALUES ($1, 'member', $2, '内部备注：不要外发', 'comment', $3)
	`, rootID, setup.requester, setup.projectID); err != nil {
		t.Fatalf("insert member comment: %v", err)
	}
	var answerID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, author_type, author_id, content, type, workspace_id)
		VALUES ($1, 'agent', $2, 'AIHUB 当前接入了三个模型。', 'comment', $3)
		RETURNING id::text
	`, rootID, setup.agentID, setup.projectID).Scan(&answerID); err != nil {
		t.Fatalf("insert agent answer: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO attachment (
			workspace_id, issue_id, comment_id, uploader_type, uploader_id, filename, url, content_type, size_bytes
		)
		VALUES ($1, $2, $3, 'agent', $4, 'models.md', 'https://files.example/models.md', 'text/markdown', 128)
	`, setup.projectID, rootID, answerID, setup.agentID); err != nil {
		t.Fatalf("insert answer attachment: %v", err)
	}

	readTrail := func(userID string) (int, dispatchTrailResponse, string) {
		rec := httptest.NewRecorder()
		testHandler.GetDispatchTrail(rec, orgRequestWithParam(userID, "GET", "/api/org/dispatches/x/trail", "issueId", rootID, nil))
		var trail dispatchTrailResponse
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &trail); err != nil {
				t.Fatalf("decode trail: %v %s", err, rec.Body.String())
			}
		}
		return rec.Code, trail, rec.Body.String()
	}

	code, trail, raw := readTrail(setup.requester)
	if code != http.StatusOK {
		t.Fatalf("trail status = %d, want 200: %s", code, raw)
	}
	if trail.Issue.WorkspaceID != setup.projectID || trail.Issue.ID != rootID {
		t.Fatalf("trail issue = %s/%s, want project %s root %s", trail.Issue.WorkspaceID, trail.Issue.ID, setup.projectID, rootID)
	}
	if trail.AssigneeName != "project-trail-project-agent" {
		t.Fatalf("assignee name = %q, want the project agent", trail.AssigneeName)
	}
	if trail.Progress.Done != 1 || trail.Progress.Total != 2 {
		t.Fatalf("progress = %+v, want 1 of 2", trail.Progress)
	}
	if len(trail.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(trail.Children))
	}
	if trail.Result == nil {
		t.Fatal("trail result is missing")
	}
	if trail.Result.Content != "AIHUB 当前接入了三个模型。" {
		t.Fatalf("result content = %q, want the agent's answer", trail.Result.Content)
	}
	if len(trail.Result.Attachments) != 1 || trail.Result.Attachments[0].DownloadURL == "" {
		t.Fatalf("result attachments = %+v, want one downloadable file", trail.Result.Attachments)
	}
	if len(trail.Deliverables) != 1 || trail.Deliverables[0].Filename != "models.md" || trail.Deliverables[0].DownloadURL == "" {
		t.Fatalf("deliverables = %+v, want models.md with a download URL", trail.Deliverables)
	}

	// Anyone else in the organization cannot read the requester's dispatch.
	otherID := createPermissionTestMember(t, "project-trail-other@example.com")
	if code, _, raw := readTrail(otherID); code != http.StatusNotFound || !strings.Contains(raw, "dispatch_not_found") {
		t.Fatalf("foreign trail = %d %s, want 404 dispatch_not_found", code, raw)
	}

	// The history row is read from the project workspace, carries its prefix,
	// and counts the project's own sub-issues.
	status, items, raw := listDispatches(t, setup.requester, "")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", status, raw)
	}
	for _, item := range items {
		if item.Issue.ID != rootID {
			continue
		}
		if item.Issue.WorkspaceID != setup.projectID {
			t.Fatalf("history issue workspace = %s, want %s", item.Issue.WorkspaceID, setup.projectID)
		}
		if !strings.HasPrefix(item.Issue.Identifier, "ORG-") {
			t.Fatalf("history identifier = %q, want the project prefix", item.Issue.Identifier)
		}
		if item.Progress == nil || item.Progress.Done != 1 || item.Progress.Total != 2 {
			t.Fatalf("history progress = %+v, want 1 of 2", item.Progress)
		}
		return
	}
	t.Fatalf("dispatch %s missing from history: %s", rootID, raw)
}

func TestCommentDispatch_WakesTheProjectExecutor(t *testing.T) {
	setup := setupProjectDispatch(t, "project-comment", "追问项目", true)

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 追问项目的初始需求",
		"client_request_id": "66666666-6666-6666-6666-666666666008",
		"project_id":        setup.projectID,
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}
	rootID := res.Issue.ID

	// Finish the first run first: a pending run already covers the issue, and
	// the follow-up is meant to wake the executor after it reported.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1
	`, rootID); err != nil {
		t.Fatalf("complete the first run: %v", err)
	}
	var before int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue WHERE issue_id = $1
	`, rootID).Scan(&before); err != nil {
		t.Fatalf("count tasks before follow-up: %v", err)
	}

	rec := httptest.NewRecorder()
	testHandler.CommentDispatch(rec, orgRequestWithParam(setup.requester, "POST", "/api/org/dispatches/x/messages", "issueId", rootID, map[string]any{
		"content": "再补一下模型的上下文窗口",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("follow-up status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var followUp struct {
		Delivered bool `json:"delivered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &followUp); err != nil {
		t.Fatalf("decode follow-up: %v %s", err, rec.Body.String())
	}
	if !followUp.Delivered {
		t.Fatal("follow-up did not wake the project executor")
	}
	// The comment is written in the issue's own workspace, not the origin's.
	var commentWorkspace, commentContent string
	if err := testPool.QueryRow(context.Background(), `
		SELECT workspace_id, content FROM comment
		WHERE issue_id = $1 AND author_type = 'member' AND author_id = $2
		ORDER BY created_at DESC LIMIT 1
	`, rootID, setup.requester).Scan(&commentWorkspace, &commentContent); err != nil {
		t.Fatalf("load follow-up comment: %v", err)
	}
	if commentWorkspace != setup.projectID || commentContent != "再补一下模型的上下文窗口" {
		t.Fatalf("comment = %s/%q, want project/%q", commentWorkspace, commentContent, "再补一下模型的上下文窗口")
	}
	var after int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2
	`, rootID, setup.agentID).Scan(&after); err != nil {
		t.Fatalf("count tasks after follow-up: %v", err)
	}
	if after <= before {
		t.Fatalf("project executor tasks = %d, want more than %d", after, before)
	}

	// A teammate cannot post into someone else's dispatch.
	otherID := createPermissionTestMember(t, "project-comment-other@example.com")
	rec = httptest.NewRecorder()
	testHandler.CommentDispatch(rec, orgRequestWithParam(otherID, "POST", "/api/org/dispatches/x/messages", "issueId", rootID, map[string]any{
		"content": "我是同事",
	}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign follow-up = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestSetDispatchStatus_AppliesTheRequesterVerdict(t *testing.T) {
	setup := setupProjectDispatch(t, "project-status", "验收项目", true)

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 验收项目交付",
		"client_request_id": "66666666-6666-6666-6666-666666666009",
		"project_id":        setup.projectID,
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}
	rootID := res.Issue.ID

	setVerdict := func(userID, verdict string) (int, string) {
		rec := httptest.NewRecorder()
		testHandler.SetDispatchStatus(rec, orgRequestWithParam(userID, "POST", "/api/org/dispatches/x/status", "issueId", rootID, map[string]any{
			"status": verdict,
		}))
		return rec.Code, rec.Body.String()
	}

	if code, raw := setVerdict(setup.requester, "invalid"); code != http.StatusBadRequest || !strings.Contains(raw, "dispatch_invalid_status") {
		t.Fatalf("invalid verdict = %d %s, want 400 dispatch_invalid_status", code, raw)
	}
	if code, raw := setVerdict(setup.requester, "done"); code != http.StatusOK {
		t.Fatalf("accept = %d, want 200: %s", code, raw)
	}
	var issueStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, rootID).Scan(&issueStatus); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if issueStatus != "done" {
		t.Fatalf("issue status = %q, want done", issueStatus)
	}
	if code, raw := setVerdict(setup.requester, "in_progress"); code != http.StatusOK {
		t.Fatalf("send back = %d, want 200: %s", code, raw)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, rootID).Scan(&issueStatus); err != nil {
		t.Fatalf("reload issue status: %v", err)
	}
	if issueStatus != "in_progress" {
		t.Fatalf("issue status = %q, want in_progress", issueStatus)
	}

	otherID := createPermissionTestMember(t, "project-status-other@example.com")
	if code, raw := setVerdict(otherID, "done"); code != http.StatusNotFound {
		t.Fatalf("foreign verdict = %d, want 404: %s", code, raw)
	}
}

func TestReassignDispatch_ProjectDeliveryIsUnsupported(t *testing.T) {
	setup := setupProjectDispatch(t, "project-reassign", "改派项目", true)
	otherUnit := createOrgUnitAs(t, setup.admin, "改派目的部", "")

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 改派项目的需求",
		"client_request_id": "66666666-6666-6666-6666-666666666010",
		"project_id":        setup.projectID,
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}

	rec := httptest.NewRecorder()
	testHandler.ReassignDispatch(rec, orgRequestWithParam(setup.requester, "POST", "/api/org/dispatches/x/reassign", "issueId", res.Issue.ID, map[string]any{
		"unit_id": otherUnit,
	}))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "dispatch_project_reassign_unsupported") {
		t.Fatalf("reassign = %d %s, want 409 dispatch_project_reassign_unsupported", rec.Code, rec.Body.String())
	}
}

// TestChatDispatch_ProjectDeliveryIsListedAndResumesInItsOwnWorkspace keeps
// the Feishu surface working after the move: the conversation lists the
// dispatch against the project workspace it lives in, an in-thread reply finds
// it there, and a follow-up wakes the project executor.
func TestChatDispatch_ProjectDeliveryIsListedAndResumesInItsOwnWorkspace(t *testing.T) {
	setup := setupProjectDispatch(t, "project-chat", "飞书项目", true)

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 飞书项目的需求",
		"client_request_id": "66666666-6666-6666-6666-666666666012",
		"project_id":        setup.projectID,
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}
	rootID := res.Issue.ID
	if _, err := testPool.Exec(context.Background(), `
		UPDATE dispatch_origin SET channel_type = 'feishu', chat_id = 'oc_project_chat', thread_id = 'omt_project_thread'
		WHERE issue_id = $1
	`, rootID); err != nil {
		t.Fatalf("mark origin thread: %v", err)
	}

	list, err := testHandler.OrgDispatchService.ListChatDispatches(context.Background(), parseUUID(setup.requester), 10)
	if err != nil {
		t.Fatalf("ListChatDispatches: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("open dispatches = %d, want 1", len(list))
	}
	row := list[0]
	if row.IssueID != parseUUID(rootID) {
		t.Fatalf("row issue = %v, want %s", row.IssueID, rootID)
	}
	// The row keeps answering the origin workspace for requester-scoped
	// actions, while the workspace name and slug now describe where the work
	// runs, so the chat's link opens the issue in the project.
	if row.WorkspaceID != parseUUID(testWorkspaceID) {
		t.Fatalf("row origin workspace = %v, want %s", row.WorkspaceID, testWorkspaceID)
	}
	if row.WorkspaceSlug != "飞书项目" || row.WorkspaceName != "飞书项目" {
		t.Fatalf("row workspace = %q/%q, want the project workspace", row.WorkspaceSlug, row.WorkspaceName)
	}
	if !strings.HasPrefix(row.Identifier, "ORG-") {
		t.Fatalf("row identifier = %q, want the project's prefix", row.Identifier)
	}
	if row.UnitName != "project-chat部" {
		t.Fatalf("row unit = %q, want the owning department", row.UnitName)
	}

	found, err := testHandler.OrgDispatchService.GetChatDispatchByThread(context.Background(), parseUUID(setup.requester), "oc_project_chat", "omt_project_thread")
	if err != nil || found.IssueID != parseUUID(rootID) || found.WorkspaceSlug != "飞书项目" {
		t.Fatalf("thread lookup = %+v %v, want the project dispatch", found, err)
	}

	// The executor's first run is still pending; a follow-up is meant to wake
	// it after that run reported.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1
	`, rootID); err != nil {
		t.Fatalf("complete the first run: %v", err)
	}
	reply, err := testHandler.OrgDispatchService.FollowUpChatDispatch(context.Background(), parseUUID(setup.requester), parseUUID(rootID), "补充一下模型列表")
	if err != nil {
		t.Fatalf("follow up: %v", err)
	}
	if !reply.Delivered {
		t.Fatal("follow-up did not wake the project executor")
	}
	var commentWorkspace string
	if err := testPool.QueryRow(context.Background(), `
		SELECT workspace_id FROM comment WHERE id = $1
	`, reply.Comment.ID).Scan(&commentWorkspace); err != nil {
		t.Fatalf("load follow-up comment: %v", err)
	}
	if commentWorkspace != setup.projectID {
		t.Fatalf("comment workspace = %s, want project %s", commentWorkspace, setup.projectID)
	}
}

func TestCompleteDispatch_ProjectInboxNoticeHasNoIssueLink(t *testing.T) {
	setup := setupProjectDispatch(t, "project-inbox", "通知项目", true)

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test 通知项目的需求",
		"client_request_id": "66666666-6666-6666-6666-666666666011",
		"project_id":        setup.projectID,
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(res.Issue.ID))
	if err != nil {
		t.Fatalf("load dispatched issue: %v", err)
	}
	if _, err := testHandler.OrgDispatchService.CompleteDispatch(context.Background(), issue); err != nil {
		t.Fatalf("complete dispatch: %v", err)
	}

	var linkedIssue pgtype.UUID
	var bodyText pgtype.Text
	if err := testPool.QueryRow(context.Background(), `
		SELECT issue_id, body FROM inbox_item
		WHERE workspace_id = $1 AND recipient_id = $2 AND type = 'dispatch_completed'
		ORDER BY created_at DESC LIMIT 1
	`, testWorkspaceID, setup.requester).Scan(&linkedIssue, &bodyText); err != nil {
		t.Fatalf("load dispatch inbox item: %v", err)
	}
	if linkedIssue.Valid {
		t.Fatalf("inbox issue link = %v, want none for a project delivery", linkedIssue)
	}
	if !strings.Contains(bodyText.String, `project "通知项目"`) {
		t.Fatalf("inbox body = %q, want the project name", bodyText.String)
	}
}
