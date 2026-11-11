package service

import "strings"
import "testing"

func TestPlanIntakeOfflineNotices_OncePerOwnerAndRequester(t *testing.T) {
	agents := []IntakeMachineAgent{{
		ID: "agent-1", Name: "结算助手", OwnerID: "owner-1", RuntimeID: "machine-1", WorkspaceID: "ws-1",
		LeadIDs: []string{"owner-1", "lead-1"},
	}, {
		ID: "agent-2", Name: "对账助手", OwnerID: "owner-1", RuntimeID: "machine-1", WorkspaceID: "ws-1",
		LeadIDs: []string{"lead-1"},
	}}
	requests := []IntakeAssignedRequest{{
		RequesterID: "person-1", SourceBody: "把结算页的导出补上", AssigneeID: "agent-1",
		WorkspaceID: "ws-1", Status: "todo",
	}}
	notices := PlanIntakeOfflineNotices(agents, requests)
	if len(notices) != 3 {
		t.Fatalf("notices = %+v", notices)
	}
	owner := notices[0]
	if owner.UserID != "owner-1" || owner.Title != "Agent 不在线" || !strings.Contains(owner.Body, "结算助手") || !strings.Contains(owner.Body, "对账助手") {
		t.Fatalf("owner = %+v", owner)
	}
	lead := notices[1]
	if lead.UserID != "lead-1" || !strings.Contains(lead.Body, "结算助手") {
		t.Fatalf("lead = %+v", lead)
	}
	requester := notices[2]
	if requester.Title != "还没开始" || strings.Contains(requester.Body, "结算助手") || strings.Contains(requester.Body, "机器") {
		t.Fatalf("requester = %+v", requester)
	}
}

func TestIntakeFireEnqueues_KeepsTheChosenMachine(t *testing.T) {
	if !IntakeFireEnqueues(false, false) {
		t.Fatal("ordinary wakeups still enqueue")
	}
	if IntakeFireEnqueues(true, false) {
		t.Fatal("an offline intake wakeup must not enqueue or switch")
	}
	if !IntakeFireEnqueues(true, true) {
		t.Fatal("an online intake wakeup enqueues")
	}
}

func TestPlanIntakeRecoveryNotices_DoesNotRestartWork(t *testing.T) {
	agents := []IntakeMachineAgent{{
		ID: "agent-1", Name: "结算助手", OwnerID: "owner-1", RuntimeID: "machine-1", WorkspaceID: "ws-1",
	}}
	waiting := []IntakeAssignedRequest{{
		RequesterID: "person-1", SourceBody: "补导出", AssigneeID: "agent-1", WorkspaceID: "ws-1", Status: "todo",
	}}
	running := []IntakeAssignedRequest{{
		RequesterID: "person-2", SourceBody: "正在做的", AssigneeID: "agent-1", WorkspaceID: "ws-1", Status: "in_progress",
	}}
	recovered := PlanIntakeRecoveryNotices(agents, append(waiting, running...))
	if len(recovered) != 2 {
		t.Fatalf("recovered = %+v", recovered)
	}
	if recovered[0].Title != "已恢复" || recovered[1].Title != "已恢复" || !strings.Contains(recovered[1].Body, "可以继续") {
		t.Fatalf("titles = %+v", recovered)
	}
	if strings.Contains(recovered[1].Body, "结算助手") {
		t.Fatalf("requester recovery names the agent: %s", recovered[1].Body)
	}
}
