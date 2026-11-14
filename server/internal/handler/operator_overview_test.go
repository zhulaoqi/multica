package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestOperatorOverviewReportsWorkspaceCounts(t *testing.T) {
	if testHandler == nil || dbfx == nil {
		t.Skip("handler not available")
	}

	workspaceID := dbfx.Workspace(t, "Operator Overview WS", "operator-overview-test")
	ownerID := dbfx.User(t, "Overview Owner", "operator-overview-owner@example.com")
	dbfx.Member(t, workspaceID, ownerID, "owner")
	adminID := dbfx.User(t, "Overview Admin", "operator-overview-admin@example.com")
	dbfx.Member(t, workspaceID, adminID, "admin")
	agentID := dbfx.Agent(t, "Overview Agent", "", testutil.Cols{"workspace_id": workspaceID})

	dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      workspaceID,
		"agent_id":          agentID,
		"channel_type":      "feishu",
		"status":            "active",
		"installer_user_id": testUserID,
	})
	dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      workspaceID,
		"agent_id":          agentID,
		"channel_type":      "slack",
		"status":            "revoked",
		"installer_user_id": testUserID,
	})

	usageRow := func(hoursAgo int, taskCount, costTicks int64) {
		dbfx.Insert(t, "task_usage_hourly", testutil.Cols{
			"bucket_hour":        testutil.Raw(fmt.Sprintf("date_trunc('hour', now()) - interval '%d hour'", hoursAgo)),
			"workspace_id":       workspaceID,
			"runtime_id":         testRuntimeID,
			"agent_id":           agentID,
			"provider":           "anthropic",
			"model":              "claude-test",
			"input_tokens":       100,
			"output_tokens":      50,
			"cache_read_tokens":  10,
			"cache_write_tokens": 5,
			"task_count":         taskCount,
			"cost_usd_ticks":     costTicks,
		})
	}
	usageRow(1, 3, 100)
	usageRow(2, 2, 250)

	req := httptest.NewRequest(http.MethodGet, "/api/operator/overview", nil)
	req.Header.Set(auth.OperatorHeader, "1")
	var out operatorOverviewResponse
	testutil.Call(t, testHandler.OperatorOverview, req).Want(http.StatusOK).JSON(&out)

	var row *operatorWorkspaceOverview
	for i := range out.Workspaces {
		if out.Workspaces[i].ID == workspaceID {
			row = &out.Workspaces[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("workspace %s missing from overview (%d rows)", workspaceID, len(out.Workspaces))
	}
	if row.Slug != "operator-overview-test" {
		t.Errorf("slug = %q", row.Slug)
	}
	if len(row.OwnerEmails) != 1 || row.OwnerEmails[0] != "operator-overview-owner@example.com" {
		t.Errorf("owner emails = %v", row.OwnerEmails)
	}
	if row.OwnerCount != 1 || row.AdminCount != 1 || row.MemberCount != 0 {
		t.Errorf("role counts = owner %d admin %d member %d", row.OwnerCount, row.AdminCount, row.MemberCount)
	}
	if row.AgentCount != 1 {
		t.Errorf("agent count = %d, want 1", row.AgentCount)
	}
	channels := map[string]operatorChannelCount{}
	for _, c := range row.Channels {
		channels[c.Type] = c
	}
	if c := channels["feishu"]; c.Active != 1 || c.Revoked != 0 {
		t.Errorf("feishu channels = %+v", c)
	}
	if c := channels["slack"]; c.Active != 0 || c.Revoked != 1 {
		t.Errorf("slack channels = %+v", c)
	}
	if row.Usage7d.TaskCount != 5 {
		t.Errorf("usage task count = %d, want 5", row.Usage7d.TaskCount)
	}
	if row.Usage7d.InputTokens != 200 || row.Usage7d.OutputTokens != 100 {
		t.Errorf("usage tokens = %+v", row.Usage7d)
	}
	if row.Usage7d.CostUSDTicks != 350 {
		t.Errorf("usage cost ticks = %d, want 350", row.Usage7d.CostUSDTicks)
	}
}

func TestGetMeReportsOperatorFlag(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}

	get := func(operator bool) UserResponse {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("X-User-ID", testUserID)
		if operator {
			req.Header.Set(auth.OperatorHeader, "1")
		}
		var out UserResponse
		testutil.Call(t, testHandler.GetMe, req).Want(http.StatusOK).JSON(&out)
		return out
	}

	if got := get(true); !got.IsOperator {
		t.Error("is_operator = false for operator session")
	}
	if got := get(false); got.IsOperator {
		t.Error("is_operator = true for a regular session")
	}
}
