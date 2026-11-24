package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Kinds of target a dispatch can be handed to.
const (
	DispatchHandoffProject    = "project"
	DispatchHandoffSquad      = "squad"
	DispatchHandoffDepartment = "department"
)

// DispatchHandoff names who a dispatch was handed to, for the receipts the
// requester reads. Name is the project, personal squad or department name.
type DispatchHandoff struct {
	Kind string
	Name string
}

// dispatchOriginParentDepth bounds the walk from a sub-issue up to the
// dispatched root. Leaders split work one or two levels deep.
const dispatchOriginParentDepth = 6

// DispatchOriginForIssue finds the dispatch an issue belongs to. Only the root
// issue has an origin row, so a sub-issue is resolved through its parents.
func (s *OrgDispatchService) DispatchOriginForIssue(ctx context.Context, issueID pgtype.UUID) (db.DispatchOrigin, bool) {
	for depth := 0; depth < dispatchOriginParentDepth && issueID.Valid; depth++ {
		origin, err := s.Queries.GetDispatchOrigin(ctx, issueID)
		if err == nil {
			return origin, true
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("dispatch handoff: load origin failed", "issue_id", util.UUIDToString(issueID), "error", err)
			return db.DispatchOrigin{}, false
		}
		issue, err := s.Queries.GetIssue(ctx, issueID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("dispatch handoff: load issue failed", "issue_id", util.UUIDToString(issueID), "error", err)
			}
			return db.DispatchOrigin{}, false
		}
		issueID = issue.ParentIssueID
	}
	return db.DispatchOrigin{}, false
}

// HandoffOf reads who the dispatch was handed to. A project dispatch also
// records the department that owns the project; the project is the answer.
func (s *OrgDispatchService) HandoffOf(ctx context.Context, origin db.DispatchOrigin) DispatchHandoff {
	switch {
	case origin.TargetProjectID.Valid:
		return DispatchHandoff{Kind: DispatchHandoffProject, Name: s.projectName(ctx, origin.TargetProjectID)}
	case origin.TargetSquadID.Valid:
		squad, err := s.Queries.GetSquad(ctx, origin.TargetSquadID)
		if err != nil {
			slog.Warn("dispatch handoff: load squad failed", "squad_id", util.UUIDToString(origin.TargetSquadID), "error", err)
			return DispatchHandoff{Kind: DispatchHandoffSquad}
		}
		return DispatchHandoff{Kind: DispatchHandoffSquad, Name: strings.TrimSpace(squad.Name)}
	case origin.TargetUnitID.Valid:
		return DispatchHandoff{Kind: DispatchHandoffDepartment, Name: s.unitName(ctx, origin.WorkspaceID, origin.TargetUnitID)}
	}
	return DispatchHandoff{}
}

// DispatchLeadAgentName names the agent running a dispatched root issue: the
// assigned agent, or the leader of the squad it is assigned to.
func (s *OrgDispatchService) DispatchLeadAgentName(ctx context.Context, issue db.Issue) string {
	if !issue.AssigneeID.Valid {
		return ""
	}
	var agentID pgtype.UUID
	switch issue.AssigneeType.String {
	case "agent":
		agentID = issue.AssigneeID
	case "squad":
		squad, err := s.Queries.GetSquad(ctx, issue.AssigneeID)
		if err != nil {
			return ""
		}
		agentID = squad.LeaderID
	}
	if !agentID.Valid {
		return ""
	}
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(agent.Name)
}
