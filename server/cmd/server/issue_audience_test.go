package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	audienceOrgWS     = "00000000-0000-0000-0000-0000000000a1"
	audienceProjectWS = "00000000-0000-0000-0000-0000000000a2"
	audienceDeptIssue = "00000000-0000-0000-0000-0000000000b1"
	audienceFlatIssue = "00000000-0000-0000-0000-0000000000b2"
	audienceTask      = "00000000-0000-0000-0000-0000000000c1"
	audienceUnit      = "00000000-0000-0000-0000-0000000000d1"
	audienceUserA     = "00000000-0000-0000-0000-0000000000e1"
	audienceUserB     = "00000000-0000-0000-0000-0000000000e2"
)

type audienceQueriesFake struct {
	noOrganization bool
	audienceErr    error
	audienceCalls  int
}

func audienceUUID(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return id
}

func (f *audienceQueriesFake) GetOrganizationWorkspace(context.Context) (db.Workspace, error) {
	if f.noOrganization {
		return db.Workspace{}, pgx.ErrNoRows
	}
	return db.Workspace{ID: audienceUUID(audienceOrgWS), IsOrganization: true}, nil
}

func (f *audienceQueriesFake) GetIssue(_ context.Context, id pgtype.UUID) (db.Issue, error) {
	issue := db.Issue{ID: id, WorkspaceID: audienceUUID(audienceOrgWS)}
	switch util.UUIDToString(id) {
	case audienceDeptIssue:
		issue.OrgUnitID = audienceUUID(audienceUnit)
	case audienceFlatIssue:
	default:
		return db.Issue{}, pgx.ErrNoRows
	}
	return issue, nil
}

func (f *audienceQueriesFake) GetAgentTask(_ context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	if util.UUIDToString(id) != audienceTask {
		return db.AgentTaskQueue{}, pgx.ErrNoRows
	}
	return db.AgentTaskQueue{ID: id, IssueID: audienceUUID(audienceDeptIssue)}, nil
}

func (f *audienceQueriesFake) ListIssueAudience(context.Context, pgtype.UUID) ([]pgtype.UUID, error) {
	f.audienceCalls++
	if f.audienceErr != nil {
		return nil, f.audienceErr
	}
	return []pgtype.UUID{audienceUUID(audienceUserA), audienceUUID(audienceUserB)}, nil
}

func TestIssueAudienceRouting(t *testing.T) {
	deptAudience := []string{
		realtime.WorkspaceMemberScopeID(audienceOrgWS, audienceUserA),
		realtime.WorkspaceMemberScopeID(audienceOrgWS, audienceUserB),
	}
	cases := []struct {
		name          string
		queries       *audienceQueriesFake
		event         events.Event
		wantScopes    []string
		wantWorkspace bool
	}{
		{
			name:    "department issue update reaches only its audience",
			queries: &audienceQueriesFake{},
			event: events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: audienceOrgWS,
				Payload: map[string]any{"issue": map[string]any{"id": audienceDeptIssue}}},
			wantScopes: deptAudience,
		},
		{
			name:    "comment on a department issue reaches only its audience",
			queries: &audienceQueriesFake{},
			event: events.Event{Type: protocol.EventCommentCreated, WorkspaceID: audienceOrgWS,
				Payload: map[string]any{"comment": map[string]any{"issue_id": audienceDeptIssue}}},
			wantScopes: deptAudience,
		},
		{
			name:    "task progress resolves its issue through the task",
			queries: &audienceQueriesFake{},
			event: events.Event{Type: protocol.EventTaskProgress, WorkspaceID: audienceOrgWS, TaskID: audienceTask,
				Payload: map[string]any{"summary": "working"}},
			wantScopes: deptAudience,
		},
		{
			name:    "audience lookup failure drops instead of widening",
			queries: &audienceQueriesFake{audienceErr: errors.New("db down")},
			event: events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: audienceOrgWS,
				Payload: map[string]any{"issue_id": audienceDeptIssue}},
		},
		{
			name:    "issue without a department keeps workspace fanout",
			queries: &audienceQueriesFake{},
			event: events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: audienceOrgWS,
				Payload: map[string]any{"issue_id": audienceFlatIssue}},
			wantWorkspace: true,
		},
		{
			name:    "deleted issue keeps workspace fanout",
			queries: &audienceQueriesFake{},
			event: events.Event{Type: protocol.EventIssueDeleted, WorkspaceID: audienceOrgWS,
				Payload: map[string]any{"issue_id": "00000000-0000-0000-0000-0000000000ff"}},
			wantWorkspace: true,
		},
		{
			name:    "project events keep workspace fanout",
			queries: &audienceQueriesFake{},
			event: events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: audienceProjectWS,
				Payload: map[string]any{"issue_id": audienceDeptIssue}},
			wantWorkspace: true,
		},
		{
			name:    "no organization workspace keeps workspace fanout",
			queries: &audienceQueriesFake{noOrganization: true},
			event: events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: audienceOrgWS,
				Payload: map[string]any{"issue_id": audienceDeptIssue}},
			wantWorkspace: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListenersWithAudience(bus, fb, newIssueAudience(tc.queries))
			bus.Publish(tc.event)

			fb.mu.Lock()
			defer fb.mu.Unlock()
			if got := len(fb.workspaceCalls) == 1; got != tc.wantWorkspace || len(fb.workspaceCalls) > 1 {
				t.Fatalf("workspace fanout = %+v, want workspace=%v", fb.workspaceCalls, tc.wantWorkspace)
			}
			if len(fb.userCalls) != 0 || fb.broadcastCalled != 0 {
				t.Fatalf("unexpected user=%+v broadcast=%d", fb.userCalls, fb.broadcastCalled)
			}
			var scopes []string
			for _, call := range fb.scopeCalls {
				if call.scopeType != realtime.ScopeWorkspaceMember {
					t.Fatalf("scope type = %q", call.scopeType)
				}
				scopes = append(scopes, call.scopeID)
			}
			if len(scopes) != len(tc.wantScopes) {
				t.Fatalf("scopes = %v, want %v", scopes, tc.wantScopes)
			}
			for i := range scopes {
				if scopes[i] != tc.wantScopes[i] {
					t.Fatalf("scopes = %v, want %v", scopes, tc.wantScopes)
				}
			}
		})
	}
}
