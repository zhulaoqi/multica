package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// inboxTypeSquadLeaderFailed tells the department lead that a squad leader
// exhausted its runs without delivering. Distinct from the generic issue
// notifications so the inbox can point at the department queue.
const inboxTypeSquadLeaderFailed = "squad_leader_failed"

// checkSquadChildGuard enforces the squad's dispatch limits when an agent
// creates a child issue under a squad-assigned parent. Humans are not gated:
// a person adding a sub-issue by hand is not the runaway loop these limits
// exist to stop. Returns 0 when the create may proceed.
func (h *Handler) checkSquadChildGuard(ctx context.Context, wsUUID pgtype.UUID, parentID pgtype.UUID) (int, string) {
	parent, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          parentID,
		WorkspaceID: wsUUID,
	})
	if err != nil || !parent.AssigneeType.Valid || parent.AssigneeType.String != "squad" || !parent.AssigneeID.Valid {
		return 0, ""
	}
	squad, err := h.Queries.GetSquadByAssignee(ctx, db.GetSquadByAssigneeParams{
		ID:          parent.AssigneeID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		// Routing to a missing squad already fails elsewhere; do not turn a
		// stale assignee into a create error the caller cannot act on.
		return 0, ""
	}
	terminal, err := h.terminalIssueStatusKeys(ctx, wsUUID)
	if err != nil {
		slog.Error("squad guard: resolve terminal statuses failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return http.StatusInternalServerError, "failed to resolve status categories"
	}
	counts, err := h.Queries.CountChildIssues(ctx, db.CountChildIssuesParams{
		TerminalStatusKeys: terminal,
		WorkspaceID:        wsUUID,
		ParentIssueID:      parentID,
	})
	if err != nil {
		slog.Error("squad guard: count child issues failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return http.StatusInternalServerError, "failed to count child issues"
	}
	if squad.MaxChildren > 0 && counts.Total >= squad.MaxChildren {
		return http.StatusConflict, fmt.Sprintf(
			"dispatch limit reached: this squad allows %d child issues total. Stop creating children; post a status comment on the parent and escalate to the human if more work is needed.",
			squad.MaxChildren,
		)
	}
	if squad.WipLimit > 0 && counts.Open >= squad.WipLimit {
		return http.StatusConflict, fmt.Sprintf(
			"open child limit reached: %d child issues are still open (limit %d). Do not retry; wait for running children to finish, then continue or escalate.",
			counts.Open, squad.WipLimit,
		)
	}
	return 0, ""
}

// notifySquadLeaderFailure tells the department lead when a squad leader has
// exhausted its attempts. The leader may have died without a comment, so the
// inbox is the only human-visible signal.
func (h *Handler) notifySquadLeaderFailure(ctx context.Context, task db.AgentTaskQueue, workspaceID string) {
	if !task.IsLeaderTask || !task.SquadID.Valid || !task.IssueID.Valid {
		return
	}
	if task.MaxAttempts > 0 && task.Attempt < task.MaxAttempts {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return
	}
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          task.SquadID,
		WorkspaceID: wsUUID,
	})
	if err != nil || !squad.OrgUnitID.Valid {
		return
	}
	units, err := h.Queries.ListOrgUnits(ctx, wsUUID)
	if err != nil {
		slog.Warn("squad failure: list units failed", "squad_id", uuidToString(squad.ID), "error", err)
		return
	}
	var recipient pgtype.UUID
	for _, unit := range units {
		if unit.ID == squad.OrgUnitID && unit.LeadUserID.Valid {
			recipient = unit.LeadUserID
			break
		}
	}
	if !recipient.Valid {
		recipient = squad.CreatorID
	}
	if !recipient.Valid {
		return
	}
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          task.IssueID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return
	}
	title := fmt.Sprintf("Squad leader failed on %s-%d", h.getIssuePrefix(ctx, wsUUID), issue.Number)
	body := "The squad leader exhausted its runs without completing the dispatch."
	if task.Error.Valid && task.Error.String != "" {
		body = fmt.Sprintf("%s Last error: %s", body, redact.Text(task.Error.String))
	}
	details, _ := json.Marshal(map[string]any{
		"task_id":     uuidToString(task.ID),
		"squad_id":    uuidToString(squad.ID),
		"unit_id":     uuidToString(squad.OrgUnitID),
		"issue_id":    uuidToString(task.IssueID),
		"agent_id":    uuidToString(task.AgentID),
		"error":       redact.Text(textValue(task.Error)),
		"attempt":     task.Attempt,
		"max_attempt": task.MaxAttempts,
	})
	item, inserted, err := inbox.CreateIfEligible(ctx, h.Queries, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   recipient,
		Type:          inboxTypeSquadLeaderFailed,
		Severity:      "action_required",
		IssueID:       task.IssueID,
		Title:         title,
		Body:          pgtype.Text{String: body, Valid: true},
		ActorType:     pgtype.Text{String: "agent", Valid: true},
		ActorID:       task.AgentID,
		Details:       details,
	})
	if err != nil {
		slog.Error("squad failure: inbox write failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	if !inserted {
		return
	}
	h.publish(protocol.EventInboxNew, workspaceID, "agent", uuidToString(task.AgentID), map[string]any{
		"item": map[string]any{
			"id":             uuidToString(item.ID),
			"workspace_id":   uuidToString(item.WorkspaceID),
			"recipient_type": item.RecipientType,
			"recipient_id":   uuidToString(item.RecipientID),
			"type":           item.Type,
			"severity":       item.Severity,
			"issue_id":       uuidToPtr(item.IssueID),
			"title":          item.Title,
			"body":           textPtr(item.Body),
			"read":           item.Read,
			"archived":       item.Archived,
			"created_at":     timestampToString(item.CreatedAt),
			"actor_type":     textToPtr(item.ActorType),
			"actor_id":       uuidToPtr(item.ActorID),
			"details":        json.RawMessage(item.Details),
			"issue_status":   issue.Status,
		},
	})
}
