package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// primaryTwinTestFixture is privateAgentTestFixture with the private agent
// promoted to its owner's primary twin.
func primaryTwinTestFixture(t *testing.T) (agentID, ownerID, memberID string) {
	t.Helper()
	agentID, ownerID, memberID = privateAgentTestFixture(t)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET is_primary = true WHERE id = $1`, agentID); err != nil {
		t.Fatalf("promote primary twin: %v", err)
	}
	return agentID, ownerID, memberID
}

// TestPrimaryTwin_OpenToCollaboration covers the collaboration paths a
// member's private primary twin is open on: assignment, @-mention and comment
// routing, plus the visibility the pickers need. The non-primary counterparts
// are the *_PrivateAgentForbidsPlainMember tests in agent_access_test.go.
func TestPrimaryTwin_OpenToCollaboration(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, _, memberID := primaryTwinTestFixture(t)

	t.Run("plain member may assign", func(t *testing.T) {
		title := "assign-to-primary-twin test"
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
		})
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, newRequestAs(memberID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":         title,
			"status":        "todo",
			"assignee_type": "agent",
			"assignee_id":   agentID,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue as plain member: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("workspace member may mention", func(t *testing.T) {
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		})
		issueID := createMentionFixtureIssue(t, ctx, "mention-primary-twin test")
		if reason := mentionAgentReasonCode(t, issueID, agentID); reason != "" {
			t.Fatalf("mention of primary twin blocked: %s", reason)
		}
	})

	t.Run("comment routes to the assigned primary twin", func(t *testing.T) {
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id,
			                   assignee_type, assignee_id, number)
			VALUES ($1, 'on_comment primary-twin test', 'todo', 'medium', 'member', $2,
			        'agent', $3,
			        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
			RETURNING id
		`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
			t.Fatalf("create issue: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })
		issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
		if err != nil {
			t.Fatalf("load issue: %v", err)
		}
		if !testHandler.shouldEnqueueAssigneeFallback(ctx, issue, "member", memberID, commentTriggerComputeOptions{}) {
			t.Fatal("plain member comment did not route to the primary twin")
		}
		// An agent actor with no human originator gets no primary-twin pass.
		if testHandler.shouldEnqueueAssigneeFallback(ctx, issue, "agent", agentID, commentTriggerComputeOptions{}) {
			t.Fatal("agent actor without originator routed to a private primary twin")
		}
	})

	t.Run("plain member may view", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetAgent(w, withURLParam(newRequestAs(memberID, "GET", "/api/agents/"+agentID, nil), "id", agentID))
		if w.Code != http.StatusOK {
			t.Fatalf("GetAgent as plain member: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		testHandler.ListAgents(w, newRequestAs(memberID, "GET", "/api/agents", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListAgents as plain member: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if !listContainsAgent(t, w.Body.Bytes(), agentID) {
			t.Fatal("ListAgents as plain member did not include the primary twin")
		}
	})

	t.Run("private chat keeps the access setting", func(t *testing.T) {
		memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      util.MustParseUUID(memberID),
			WorkspaceID: util.MustParseUUID(testWorkspaceID),
		})
		if err != nil {
			t.Fatalf("load plain member row: %v", err)
		}
		w := httptest.NewRecorder()
		req := newRequestAs(memberID, "POST", "/api/chat/sessions", map[string]any{
			"agent_id": agentID,
			"title":    "should be denied",
		})
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
		testHandler.CreateChatSession(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateChatSession as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestCanCollaborateWithAgent_EffectiveUser(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, _, memberID := primaryTwinTestFixture(t)
	agent, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	cases := []struct {
		name       string
		actorType  string
		actorID    string
		originator string
		want       bool
	}{
		{"member", "member", memberID, "", true},
		{"agent with member originator", "agent", agentID, memberID, true},
		{"agent without originator", "agent", agentID, "", false},
		{"system without originator", "system", "", "", false},
		{"system with member originator", "system", "", memberID, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := testHandler.canCollaborateWithAgent(ctx, agent, tc.actorType, tc.actorID, tc.originator, testWorkspaceID)
			if got != tc.want {
				t.Fatalf("canCollaborateWithAgent = %v, want %v", got, tc.want)
			}
		})
	}

	// A non-primary private agent keeps its access setting on the same paths.
	if _, err := testPool.Exec(ctx, `UPDATE agent SET is_primary = false WHERE id = $1`, agentID); err != nil {
		t.Fatalf("demote agent: %v", err)
	}
	agent.IsPrimary = false
	if testHandler.canCollaborateWithAgent(ctx, agent, "member", memberID, "", testWorkspaceID) {
		t.Fatal("plain member may collaborate with a private non-primary agent")
	}
}
