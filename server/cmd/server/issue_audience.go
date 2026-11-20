package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// issueAudienceQueries is what organization issue routing reads.
type issueAudienceQueries interface {
	GetOrganizationWorkspace(ctx context.Context) (db.Workspace, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	ListIssueAudience(ctx context.Context, issueID pgtype.UUID) ([]pgtype.UUID, error)
}

const (
	organizationCacheTTL = time.Minute
	issueAudienceTimeout = 2 * time.Second
)

// issueAudience routes organization-workspace events about a department issue
// to the members who may read it, instead of the whole company. Issues are
// scoped by department there (workspaceaccess.IssueScope); every other event,
// and every project, keeps workspace fanout.
type issueAudience struct {
	queries issueAudienceQueries

	mu        sync.Mutex
	orgID     string
	checkedAt time.Time
}

func newIssueAudience(queries issueAudienceQueries) *issueAudience {
	return &issueAudience{queries: queries}
}

// organizationID returns the cached organization workspace id, "" when none.
func (a *issueAudience) organizationID(ctx context.Context) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.checkedAt.IsZero() && time.Since(a.checkedAt) < organizationCacheTTL {
		return a.orgID
	}
	ws, err := a.queries.GetOrganizationWorkspace(ctx)
	if err != nil {
		a.orgID = ""
	} else {
		a.orgID = util.UUIDToString(ws.ID)
	}
	a.checkedAt = time.Now()
	return a.orgID
}

// issueRefs are the payload fields that name the issue an event is about.
type issueRefs struct {
	IssueID string `json:"issue_id"`
	TaskID  string `json:"task_id"`
	Issue   struct {
		ID string `json:"id"`
	} `json:"issue"`
	Comment struct {
		IssueID string `json:"issue_id"`
	} `json:"comment"`
}

// recipients returns the user ids that may receive e and ok=true when e must be
// delivered per user. ok=false keeps workspace fanout: projects, events not
// about an issue, and issues without a department.
//
// An issue that no longer resolves (issue:deleted) keeps fanout; the event
// carries only its id.
func (a *issueAudience) recipients(e events.Event, data []byte) ([]string, bool) {
	if a == nil || e.WorkspaceID == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), issueAudienceTimeout)
	defer cancel()
	if a.organizationID(ctx) != e.WorkspaceID {
		return nil, false
	}
	issueID, ok := a.issueID(ctx, e, data)
	if !ok {
		return nil, false
	}
	issue, err := a.queries.GetIssue(ctx, issueID)
	if err != nil || util.UUIDToString(issue.WorkspaceID) != e.WorkspaceID || !issue.OrgUnitID.Valid {
		return nil, false
	}
	users, err := a.queries.ListIssueAudience(ctx, issue.ID)
	if err != nil {
		// Never widen a department issue to the whole company on error.
		slog.Warn("realtime: issue audience lookup failed; dropping event",
			"event_type", e.Type, "issue_id", util.UUIDToString(issue.ID), "error", err)
		return []string{}, true
	}
	out := make([]string, 0, len(users))
	for _, user := range users {
		out = append(out, util.UUIDToString(user))
	}
	return out, true
}

func (a *issueAudience) issueID(ctx context.Context, e events.Event, data []byte) (pgtype.UUID, bool) {
	var frame struct {
		Payload issueRefs `json:"payload"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return pgtype.UUID{}, false
	}
	refs := frame.Payload
	for _, raw := range []string{refs.IssueID, refs.Issue.ID, refs.Comment.IssueID} {
		if id, err := util.ParseUUID(raw); err == nil {
			return id, true
		}
	}
	taskID := refs.TaskID
	if taskID == "" {
		taskID = e.TaskID
	}
	id, err := util.ParseUUID(taskID)
	if err != nil {
		return pgtype.UUID{}, false
	}
	task, err := a.queries.GetAgentTask(ctx, id)
	if err != nil || !task.IssueID.Valid {
		return pgtype.UUID{}, false
	}
	return task.IssueID, true
}

// deliverToAudience sends data to each recipient's connections bound to the workspace.
func deliverToAudience(b realtime.Broadcaster, workspaceID string, users []string, data []byte) {
	for _, user := range users {
		b.BroadcastToScope(realtime.ScopeWorkspaceMember, realtime.WorkspaceMemberScopeID(workspaceID, user), data)
	}
}
