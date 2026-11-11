package service

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IntakeMachineAgent is one agent that has a scope sentence and shares a machine.
type IntakeMachineAgent struct {
	ID          string
	Name        string
	OwnerID     string
	RuntimeID   string
	WorkspaceID string
	LeadIDs     []string
}

// IntakeAssignedRequest is an unfinished intake issue on one of those agents.
type IntakeAssignedRequest struct {
	IssueID     string
	RequesterID string
	SourceBody  string
	AssigneeID  string
	WorkspaceID string
	Status      string
}

// IntakePersonNotice is one personal card. Requester notices never name the agent or the machine.
type IntakePersonNotice struct {
	WorkspaceID string
	UserID      string
	IssueID     string
	Title       string
	Body        string
}

// PlanIntakeOfflineNotices fires once for an online-to-offline transition.
// Each owner gets one card per machine. A project lead gets one card only for
// agents they do not already own. Each affected requester gets 「还没开始」.
func PlanIntakeOfflineNotices(agents []IntakeMachineAgent, requests []IntakeAssignedRequest) []IntakePersonNotice {
	return planIntakeMachineNotices(agents, requests, false)
}

// PlanIntakeRecoveryNotices tells owners and leads the machine is back, and
// tells requesters whose work is still waiting that they can continue. It does
// not enqueue anything.
func PlanIntakeRecoveryNotices(agents []IntakeMachineAgent, requests []IntakeAssignedRequest) []IntakePersonNotice {
	return planIntakeMachineNotices(agents, requests, true)
}

func planIntakeMachineNotices(agents []IntakeMachineAgent, requests []IntakeAssignedRequest, recovered bool) []IntakePersonNotice {
	type machineKey struct{ runtime, workspace string }
	order := []machineKey{}
	grouped := map[machineKey][]IntakeMachineAgent{}
	for _, agent := range agents {
		if agent.RuntimeID == "" || agent.WorkspaceID == "" || agent.OwnerID == "" || strings.TrimSpace(agent.Name) == "" {
			continue
		}
		key := machineKey{agent.RuntimeID, agent.WorkspaceID}
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], agent)
	}
	var out []IntakePersonNotice
	for _, key := range order {
		group := grouped[key]
		title, tail := "Agent 不在线", "请恢复这台机器。"
		if recovered {
			title, tail = "已恢复", "机器已恢复。"
		}
		owners := map[string][]string{}
		ownerOrder := []string{}
		for _, agent := range group {
			if _, ok := owners[agent.OwnerID]; !ok {
				ownerOrder = append(ownerOrder, agent.OwnerID)
			}
			owners[agent.OwnerID] = appendName(owners[agent.OwnerID], agent.Name)
		}
		for _, owner := range ownerOrder {
			out = append(out, IntakePersonNotice{
				WorkspaceID: key.workspace, UserID: owner, Title: title,
				Body: strings.Join(owners[owner], "、") + "\n" + tail,
			})
		}
		leads := map[string][]string{}
		leadOrder := []string{}
		for _, agent := range group {
			for _, lead := range agent.LeadIDs {
				if lead == "" || lead == agent.OwnerID {
					continue
				}
				if _, ok := leads[lead]; !ok {
					leadOrder = append(leadOrder, lead)
				}
				leads[lead] = appendName(leads[lead], agent.Name)
			}
		}
		for _, lead := range leadOrder {
			out = append(out, IntakePersonNotice{
				WorkspaceID: key.workspace, UserID: lead, Title: title,
				Body: strings.Join(leads[lead], "、") + "\n" + tail,
			})
		}
		for _, req := range requests {
			if req.WorkspaceID != key.workspace || req.RequesterID == "" {
				continue
			}
			if !agentInGroup(group, req.AssigneeID) {
				continue
			}
			if recovered {
				if req.Status != "todo" && req.Status != "backlog" {
					continue
				}
				out = append(out, IntakePersonNotice{
					WorkspaceID: req.WorkspaceID, UserID: req.RequesterID, IssueID: req.IssueID, Title: "已恢复",
					Body: IntakeLabeled("需求", IntakeDemandLine(req.SourceBody), "状态", "可以继续"),
				})
				continue
			}
			if !intakeUnfinished(req.Status) {
				continue
			}
			out = append(out, IntakePersonNotice{
				WorkspaceID: req.WorkspaceID, UserID: req.RequesterID, IssueID: req.IssueID, Title: "还没开始",
				Body: IntakeLabeled("需求", IntakeDemandLine(req.SourceBody), "状态", "还没开始，恢复后可以继续"),
			})
		}
	}
	return out
}

func appendName(names []string, name string) []string {
	for _, existing := range names {
		if existing == name {
			return names
		}
	}
	return append(names, name)
}

func agentInGroup(group []IntakeMachineAgent, id string) bool {
	for _, agent := range group {
		if agent.ID == id {
			return true
		}
	}
	return false
}

func intakeUnfinished(status string) bool {
	switch status {
	case "todo", "backlog", "in_progress", "blocked":
		return true
	default:
		return false
	}
}

func intakeFirstLine(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "这条需求"
	}
	if line, _, ok := strings.Cut(body, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return body
}

// NotifyIntakeMaintenance tells the requester the work has not started, and
// tells the agent owner and project lead which agent needs a machine.
// The requester card does not name the agent.
func NotifyIntakeMaintenance(ctx context.Context, q *db.Queries, workspaceID, projectID, agentID, requesterID, issueID pgtype.UUID, source string) {
	if DeliverIntakeCard == nil || q == nil {
		return
	}
	_ = DeliverIntakeCard(ctx, workspaceID, requesterID, issueID, "还没开始", IntakeLabeled("需求", IntakeDemandLine(source), "状态", "需要维护"))
	agent, err := q.GetAgent(ctx, agentID)
	if err != nil {
		return
	}
	staff := agent.Name + "\n请恢复这台机器。"
	if agent.OwnerID.Valid {
		_ = DeliverIntakeCard(ctx, workspaceID, agent.OwnerID, issueID, "需要维护", staff)
	}
	project, err := q.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil || project.LeadType.String != "member" || !project.LeadID.Valid {
		return
	}
	if agent.OwnerID.Valid && project.LeadID == agent.OwnerID {
		return
	}
	_ = DeliverIntakeCard(ctx, workspaceID, project.LeadID, issueID, "需要维护", staff)
}

// AnnounceIntakeRuntimes sends the offline or recovery cards for runtimes that
// just changed. The sweeper calls it only after a runtime has already missed
// heartbeats for the stale window, so one missed beat does not alert. A runtime
// that has never been online is not in that set. Recovery does not enqueue.
func AnnounceIntakeRuntimes(ctx context.Context, q *db.Queries, runtimeIDs []pgtype.UUID, recovered bool) {
	if q == nil || DeliverIntakeCard == nil || len(runtimeIDs) == 0 {
		return
	}
	rows, err := q.ListIntakeAgentsByRuntimes(ctx, runtimeIDs)
	if err != nil || len(rows) == 0 {
		return
	}
	agentIDs := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		agentIDs = append(agentIDs, row.ID)
	}
	leadRows, _ := q.ListIntakeLeadsForAgents(ctx, agentIDs)
	leads := map[string][]string{}
	for _, row := range leadRows {
		id := util.UUIDToString(row.AgentID)
		leads[id] = append(leads[id], util.UUIDToString(row.LeadID))
	}
	requestRows, _ := q.ListIntakeRequestsForAgents(ctx, agentIDs)
	agents := make([]IntakeMachineAgent, 0, len(rows))
	for _, row := range rows {
		id := util.UUIDToString(row.ID)
		agents = append(agents, IntakeMachineAgent{
			ID: id, Name: row.Name, OwnerID: util.UUIDToString(row.OwnerID),
			RuntimeID: util.UUIDToString(row.RuntimeID), WorkspaceID: util.UUIDToString(row.WorkspaceID),
			LeadIDs: leads[id],
		})
	}
	requests := make([]IntakeAssignedRequest, 0, len(requestRows))
	for _, row := range requestRows {
		requests = append(requests, IntakeAssignedRequest{
			IssueID: util.UUIDToString(row.IssueID), RequesterID: util.UUIDToString(row.RequesterID), SourceBody: row.SourceBody,
			AssigneeID: util.UUIDToString(row.AssigneeID), WorkspaceID: util.UUIDToString(row.WorkspaceID),
			Status: row.Status,
		})
	}
	notices := PlanIntakeOfflineNotices(agents, requests)
	if recovered {
		notices = PlanIntakeRecoveryNotices(agents, requests)
	}
	for _, notice := range notices {
		workspaceID, err := util.ParseUUID(notice.WorkspaceID)
		if err != nil {
			continue
		}
		userID, err := util.ParseUUID(notice.UserID)
		if err != nil {
			continue
		}
		var issueID pgtype.UUID
		if notice.IssueID != "" {
			issueID, _ = util.ParseUUID(notice.IssueID)
		}
		_ = DeliverIntakeCard(ctx, workspaceID, userID, issueID, notice.Title, notice.Body)
	}
}
