package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The chat control surface lets a requester manage a dispatch from the same
// conversation that created it: status, follow-ups, cancellation, acceptance
// and reassignment. Every action is requester-scoped — the person who asked
// for the work is the one who can change its course.

var (
	ErrChatDispatchNotFound   = errors.New("dispatch not found")
	ErrChatDispatchAmbiguous  = errors.New("dispatch reference is ambiguous")
	ErrChatDispatchNotOwner   = errors.New("only the requester can control this dispatch")
	ErrChatDispatchClosed     = errors.New("dispatch is already closed")
	ErrChatDispatchNoTransfer = errors.New("dispatch has no squad leader to hand work to")
)

// ChatDispatch is one open dispatch as a chat conversation sees it.
type ChatDispatch struct {
	IssueID        pgtype.UUID
	WorkspaceID    pgtype.UUID
	Identifier     string
	Number         int32
	Title          string
	Status         string
	StatusCategory string
	LeaderState    string
	UnitName       string
	WorkspaceName  string
	WorkspaceSlug  string
	Personal       bool
	ChatID         pgtype.Text
	ThreadID       pgtype.Text
	CreatedAt      pgtype.Timestamptz
}

// ChatDispatchReply is what a follow-up that wakes the leader reports back.
type ChatDispatchReply struct {
	Issue     db.Issue
	Comment   db.Comment
	Delivered bool
}

// ChatDispatchStartNotice announces that the department began working.
type ChatDispatchStartNotice struct {
	Origin     db.DispatchOrigin
	Issue      db.Issue
	UnitName   string
	Identifier string
}

// ListChatDispatches returns the requester's open dispatches across projects,
// newest first. The order is the numbering the chat shows.
func (s *OrgDispatchService) ListChatDispatches(ctx context.Context, requesterID pgtype.UUID, limit int32) ([]ChatDispatch, error) {
	if limit <= 0 || limit > 30 {
		limit = 10
	}
	rows, err := s.Queries.ListOpenChatDispatches(ctx, db.ListOpenChatDispatchesParams{
		RequesterID: requesterID,
		Limit:       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list open chat dispatches: %w", err)
	}
	dispatches := make([]ChatDispatch, 0, len(rows))
	for _, row := range rows {
		dispatches = append(dispatches, ChatDispatch{
			IssueID:        row.IssueID,
			WorkspaceID:    row.WorkspaceID,
			Identifier:     IssueIdentifier(row.IssuePrefix, row.Number),
			Number:         row.Number,
			Title:          row.Title,
			Status:         row.Status,
			StatusCategory: row.StatusCategory,
			LeaderState:    row.LeaderState,
			UnitName:       row.UnitName,
			WorkspaceName:  row.WorkspaceName,
			WorkspaceSlug:  row.Slug,
			Personal:       row.Personal,
			ChatID:         row.ChatID,
			ThreadID:       row.ThreadID,
			CreatedAt:      row.CreatedAt,
		})
	}
	return dispatches, nil
}

// FindChatDispatch resolves a "#2" position or a workspace-qualified
// identifier ("PAY-12") against the requester's open dispatches. When several
// entries match, the caller shows the list again instead of guessing.
func (s *OrgDispatchService) FindChatDispatch(ctx context.Context, requesterID pgtype.UUID, reference string) (ChatDispatch, error) {
	dispatches, err := s.ListChatDispatches(ctx, requesterID, 20)
	if err != nil {
		return ChatDispatch{}, err
	}
	if len(dispatches) == 0 {
		return ChatDispatch{}, ErrChatDispatchNotFound
	}
	reference = strings.TrimSpace(reference)
	if reference == "" {
		if len(dispatches) == 1 {
			return dispatches[0], nil
		}
		return ChatDispatch{}, ErrChatDispatchAmbiguous
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(reference, "#")); err == nil {
		if n < 1 || n > len(dispatches) {
			return ChatDispatch{}, ErrChatDispatchNotFound
		}
		return dispatches[n-1], nil
	}
	needle := strings.ToLower(reference)
	var matches []ChatDispatch
	for _, dispatch := range dispatches {
		switch {
		case strings.EqualFold(dispatch.Identifier, reference),
			strconv.Itoa(int(dispatch.Number)) == reference,
			strings.Contains(strings.ToLower(dispatch.Title), needle):
			matches = append(matches, dispatch)
		}
	}
	switch len(matches) {
	case 0:
		return ChatDispatch{}, ErrChatDispatchNotFound
	case 1:
		return matches[0], nil
	default:
		return ChatDispatch{}, ErrChatDispatchAmbiguous
	}
}

// GetChatDispatchByThread finds the dispatch an in-thread reply belongs to.
func (s *OrgDispatchService) GetChatDispatchByThread(ctx context.Context, requesterID pgtype.UUID, chatID, threadID string) (ChatDispatch, error) {
	if chatID == "" || threadID == "" {
		return ChatDispatch{}, ErrChatDispatchNotFound
	}
	row, err := s.Queries.GetOpenChatDispatchByThread(ctx, db.GetOpenChatDispatchByThreadParams{
		RequesterID: requesterID,
		ChatID:      pgtype.Text{String: chatID, Valid: true},
		ThreadID:    pgtype.Text{String: threadID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatDispatch{}, ErrChatDispatchNotFound
	}
	if err != nil {
		return ChatDispatch{}, fmt.Errorf("load dispatch by thread: %w", err)
	}
	return ChatDispatch{
		IssueID:        row.IssueID,
		WorkspaceID:    row.WorkspaceID,
		Identifier:     IssueIdentifier(row.IssuePrefix, row.Number),
		Number:         row.Number,
		Title:          row.Title,
		Status:         row.Status,
		StatusCategory: row.StatusCategory,
		LeaderState:    row.LeaderState,
		UnitName:       row.UnitName,
		WorkspaceName:  row.WorkspaceName,
		WorkspaceSlug:  row.Slug,
		Personal:       row.Personal,
		ChatID:         row.ChatID,
		ThreadID:       row.ThreadID,
		CreatedAt:      row.CreatedAt,
	}, nil
}

// CancelChatDispatch stops the work at the requester's request.
func (s *OrgDispatchService) CancelChatDispatch(ctx context.Context, actorID, issueID pgtype.UUID) (db.Issue, error) {
	_, issue, err := s.chatDispatchTarget(ctx, actorID, issueID)
	if err != nil {
		return db.Issue{}, err
	}
	if issue.Status == "cancelled" {
		return issue, ErrChatDispatchClosed
	}
	if issue.Status == "done" {
		return issue, ErrChatDispatchClosed
	}
	if s.Tasks != nil {
		if err := s.Tasks.CancelTasksForIssue(ctx, issue.ID); err != nil {
			return db.Issue{}, fmt.Errorf("cancel dispatch tasks: %w", err)
		}
	}
	return s.setChatDispatchStatus(ctx, issue, "cancelled", actorID)
}

// AcceptChatDispatch records structured acceptance: the requester closes the
// work the department delivered.
func (s *OrgDispatchService) AcceptChatDispatch(ctx context.Context, actorID, issueID pgtype.UUID) (db.Issue, error) {
	_, issue, err := s.chatDispatchTarget(ctx, actorID, issueID)
	if err != nil {
		return db.Issue{}, err
	}
	if issue.Status == "done" {
		return issue, ErrChatDispatchClosed
	}
	if issue.Status == "cancelled" {
		return db.Issue{}, ErrChatDispatchClosed
	}
	return s.setChatDispatchStatus(ctx, issue, "done", actorID)
}

// RejectChatDispatch returns the work to the department with the reasoning
// attached, and wakes the leader so the next run sees it.
func (s *OrgDispatchService) RejectChatDispatch(ctx context.Context, actorID, issueID pgtype.UUID, reason string) (ChatDispatchReply, error) {
	_, issue, err := s.chatDispatchTarget(ctx, actorID, issueID)
	if err != nil {
		return ChatDispatchReply{}, err
	}
	if issue.Status == "cancelled" {
		return ChatDispatchReply{}, ErrChatDispatchClosed
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "验收未通过，请按原要求返工。"
	}
	reply, err := s.postChatDispatchComment(ctx, actorID, issue, "验收未通过："+reason)
	if err != nil {
		return ChatDispatchReply{}, err
	}
	updated, err := s.setChatDispatchStatus(ctx, reply.Issue, "in_progress", actorID)
	if err != nil {
		return reply, err
	}
	reply.Issue = updated
	return reply, nil
}

// FollowUpChatDispatch adds the requester's message as a comment and wakes the
// squad leader so it can continue the conversation.
func (s *OrgDispatchService) FollowUpChatDispatch(ctx context.Context, actorID, issueID pgtype.UUID, text string) (ChatDispatchReply, error) {
	_, issue, err := s.chatDispatchTarget(ctx, actorID, issueID)
	if err != nil {
		return ChatDispatchReply{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ChatDispatchReply{}, errors.New("follow-up text is required")
	}
	return s.postChatDispatchComment(ctx, actorID, issue, text)
}

// StartDispatch reports a department's first run once, before the completion
// or failure notice. Non-feishu origins and repeated events return nil.
func (s *OrgDispatchService) StartDispatch(ctx context.Context, issue db.Issue) (*ChatDispatchStartNotice, error) {
	origin, err := s.Queries.ClaimDispatchStart(ctx, issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim dispatch start: %w", err)
	}
	return &ChatDispatchStartNotice{
		Origin:     origin,
		Issue:      issue,
		UnitName:   s.unitName(ctx, origin.WorkspaceID, origin.TargetUnitID),
		Identifier: s.issueIdentifier(ctx, issue),
	}, nil
}

// chatDispatchTarget loads the dispatch and checks that actorID is the person
// who requested it. Chat actions are not general issue edits: a teammate in the
// same conversation cannot redirect someone else's request.
func (s *OrgDispatchService) chatDispatchTarget(ctx context.Context, actorID, issueID pgtype.UUID) (db.DispatchOrigin, db.Issue, error) {
	origin, err := s.Queries.GetDispatchOrigin(ctx, issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.DispatchOrigin{}, db.Issue{}, ErrChatDispatchNotFound
	}
	if err != nil {
		return db.DispatchOrigin{}, db.Issue{}, fmt.Errorf("load dispatch origin: %w", err)
	}
	if origin.RequesterID != actorID {
		return db.DispatchOrigin{}, db.Issue{}, ErrChatDispatchNotOwner
	}
	issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueID,
		WorkspaceID: origin.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.DispatchOrigin{}, db.Issue{}, ErrChatDispatchNotFound
	}
	if err != nil {
		return db.DispatchOrigin{}, db.Issue{}, fmt.Errorf("load dispatched issue: %w", err)
	}
	return origin, issue, nil
}

// postChatDispatchComment stores the requester's message as a member comment,
// publishes it for the web app, and asks the squad leader to run again.
func (s *OrgDispatchService) postChatDispatchComment(ctx context.Context, actorID pgtype.UUID, issue db.Issue, content string) (ChatDispatchReply, error) {
	created, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    actorID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return ChatDispatchReply{}, fmt.Errorf("create dispatch comment: %w", err)
	}
	comment := created.Comment()
	if s.Bus != nil {
		fields := commentEventFields(comment)
		fields["revision"] = comment.Revision
		s.Bus.Publish(events.Event{
			Type:        protocol.EventCommentCreated,
			WorkspaceID: util.UUIDToString(issue.WorkspaceID),
			ActorType:   "member",
			ActorID:     util.UUIDToString(actorID),
			Payload: map[string]any{
				"comment":        fields,
				"issue_title":    issue.Title,
				"issue_status":   issue.Status,
				"issue_revision": created.IssueRevision,
			},
		})
	}
	delivered := s.wakeChatDispatchLeader(ctx, issue, actorID)
	return ChatDispatchReply{Issue: issue, Comment: comment, Delivered: delivered}, nil
}

// wakeChatDispatchLeader enqueues a leader run for a follow-up. A failure here
// does not undo the comment: the message is recorded and the leader sees it on
// its next run.
func (s *OrgDispatchService) wakeChatDispatchLeader(ctx context.Context, issue db.Issue, actorID pgtype.UUID) bool {
	if s.Tasks == nil || !issue.AssigneeID.Valid || issue.AssigneeType.String != "squad" {
		return false
	}
	squad, err := s.Queries.GetSquadByAssignee(ctx, db.GetSquadByAssigneeParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}
	allowed, err := s.Tasks.MemberMayInvokeAgent(ctx, squad.LeaderID, actorID)
	if err != nil || !allowed {
		return false
	}
	if _, err := s.Tasks.EnqueueTaskForSquadLeaderByActor(ctx, issue, squad.LeaderID, squad.ID, actorID); err != nil {
		slog.Warn("chat dispatch: wake leader failed",
			"issue_id", util.UUIDToString(issue.ID), "squad_id", util.UUIDToString(squad.ID), "error", err)
		return false
	}
	return true
}

// setChatDispatchStatus writes the status, records the activity and publishes
// the projection the web app reconciles from.
func (s *OrgDispatchService) setChatDispatchStatus(ctx context.Context, issue db.Issue, status string, actorID pgtype.UUID) (db.Issue, error) {
	updated, err := s.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Status:      status,
	})
	if err != nil {
		return db.Issue{}, fmt.Errorf("update dispatch status: %w", err)
	}
	details, _ := json.Marshal(map[string]any{
		"from": issue.Status, "to": status, "source": "feishu_org_bot",
	})
	activity, err := s.Queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:          dbid.NewV7(),
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     actorID,
		Action:      "status_changed",
		Details:     details,
	})
	if err != nil {
		slog.Warn("chat dispatch: record status activity failed",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
	} else if s.Bus != nil {
		actor := util.UUIDToString(actorID)
		s.Bus.Publish(events.Event{
			Type:        protocol.EventActivityCreated,
			WorkspaceID: util.UUIDToString(issue.WorkspaceID),
			ActorType:   "member",
			ActorID:     actor,
			Payload: map[string]any{
				"issue_id": util.UUIDToString(issue.ID),
				"entry": map[string]any{
					"type":       "activity",
					"id":         util.UUIDToString(activity.ID),
					"actor_type": "member",
					"actor_id":   actor,
					"action":     activity.Action,
					"details":    json.RawMessage(details),
					"created_at": util.TimestampToString(activity.CreatedAt),
				},
			},
		})
	}
	if s.Tasks != nil {
		s.Tasks.broadcastIssueUpdated(ctx, updated, issue.Status)
	}
	return updated, nil
}
