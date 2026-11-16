package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// placeAgentOwnerUnderLead puts the agent owner into a child department of a
// department led by leadID, so the lead reaches the owner through an ancestor.
func placeAgentOwnerUnderLead(t *testing.T, leadID, ownerID string) {
	t.Helper()
	ctx := context.Background()
	parentID := teamRuntimeUnit(t, "agent-lead-parent", leadID)
	var childID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO org_unit (workspace_id, name, parent_id, position)
		 VALUES ($1, 'agent-lead-child', $2, 0) RETURNING id`, testWorkspaceID, parentID,
	).Scan(&childID); err != nil {
		t.Fatalf("create child unit: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM org_unit WHERE id = $1`, childID)
	})
	if _, err := testPool.Exec(ctx,
		`UPDATE member SET org_unit_id = $3 WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, ownerID, childID,
	); err != nil {
		t.Fatalf("place agent owner: %v", err)
	}
}

func TestDepartmentLeadCanViewAndInvokeTeamPrivateAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerID, leadID := privateAgentTestFixture(t)
	agent, err := testHandler.Queries.GetAgent(context.Background(), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	if testHandler.canInvokeAgent(context.Background(), agent, "member", leadID, leadID, testWorkspaceID) {
		t.Fatal("member who leads nothing must not invoke another member's private agent")
	}

	placeAgentOwnerUnderLead(t, leadID, ownerID)

	if !testHandler.canInvokeAgent(context.Background(), agent, "member", leadID, leadID, testWorkspaceID) {
		t.Fatal("lead must invoke a private agent owned by a member of a descendant department")
	}
	// An agent run started by the lead carries the lead as originator.
	if !testHandler.canInvokeAgent(context.Background(), agent, "agent", "some-agent", leadID, testWorkspaceID) {
		t.Fatal("A2A chain originated by the lead must inherit the lead's reach")
	}
	if testHandler.canInvokeAgent(context.Background(), agent, "system", "", "", testWorkspaceID) {
		t.Fatal("unattributed system trigger must not reach a private agent")
	}

	w := httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequestAs(leadID, http.MethodGet, "/api/agents/"+agentID, nil), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent as lead: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if !detail.ViewerLeadsOwner {
		t.Fatal("detail must report viewer_leads_owner for the lead")
	}

	w = httptest.NewRecorder()
	testHandler.ListAgents(w, newRequestAs(leadID, http.MethodGet, "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents as lead: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, item := range list {
		if item.ID == agentID {
			found = true
			if !item.ViewerLeadsOwner {
				t.Fatal("list entry must report viewer_leads_owner for the lead")
			}
		}
	}
	if !found {
		t.Fatalf("ListAgents as lead must include team private agent %s", agentID)
	}
}

// The owner's own view never claims a lead relation, even if they lead a unit.
func TestAgentOwnerIsNotReportedAsOwnLead(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerID, _ := privateAgentTestFixture(t)
	placeAgentOwnerUnderLead(t, ownerID, ownerID)

	w := httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequestAs(ownerID, http.MethodGet, "/api/agents/"+agentID, nil), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent as owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.ViewerLeadsOwner {
		t.Fatal("owner must not be reported as their own lead")
	}
}
