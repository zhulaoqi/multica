package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// A department squad leader may decide that a request routed to its department
// really belongs to one of the projects the requester may hand work. The
// transfer delivers it there the way a project dispatch would have: a new
// issue in the project workspace, run by a project agent, with the origin row
// following it so the requester keeps tracking the same request.

var (
	// ErrDispatchTransferForbidden means the caller is not the leader of the
	// department squad that holds the dispatch on its current run.
	ErrDispatchTransferForbidden = errors.New("only the department squad leader can transfer this dispatch")
	// ErrDispatchTransferUnsupported means the issue is not an open,
	// untransferred department dispatch: a personal or project dispatch, a
	// closed one, or one already handed to a project.
	ErrDispatchTransferUnsupported = errors.New("this dispatch cannot be transferred to a project")
	// ErrDispatchTransferAgentUnavailable means the named agent is not an
	// online agent of the target project.
	ErrDispatchTransferAgentUnavailable = errors.New("the chosen agent cannot run work in this project")
)

type DispatchTransferParams struct {
	// WorkspaceID is the workspace the request came from; it must be the
	// dispatch's organization workspace.
	WorkspaceID pgtype.UUID
	// AgentID and TaskID identify the leader run that asks for the transfer.
	AgentID pgtype.UUID
	TaskID  pgtype.UUID
	IssueID pgtype.UUID
	// ProjectID is the project workspace the work moves to.
	ProjectID pgtype.UUID
	// PreferredAgentID optionally names the project agent to run it.
	PreferredAgentID pgtype.UUID
	Note             string
}

type DispatchTransferResult struct {
	Issue    db.Issue
	Previous db.Issue
	Project  db.Workspace
	Executor pgtype.UUID
}

// TransferCandidate is one project a department leader may hand a dispatch to.
type TransferCandidate struct {
	ProjectID   pgtype.UUID
	Name        string
	Description string
	Context     string
	Agents      []db.ListProjectAgentDutiesRow
}

// TransferDispatchToProject moves a department dispatch into a project
// workspace on behalf of the department squad leader.
func (s *OrgDispatchService) TransferDispatchToProject(ctx context.Context, p DispatchTransferParams) (DispatchTransferResult, error) {
	origin, issue, squad, err := s.transferSource(ctx, p.IssueID)
	if err != nil {
		return DispatchTransferResult{}, err
	}
	if origin.WorkspaceID != p.WorkspaceID {
		return DispatchTransferResult{}, ErrDispatchNotFound
	}
	if squad.LeaderID != p.AgentID {
		return DispatchTransferResult{}, ErrDispatchTransferForbidden
	}
	if p.TaskID.Valid {
		task, err := s.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{
			ID:          p.TaskID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil || task.AgentID != p.AgentID || task.IssueID != issue.ID {
			return DispatchTransferResult{}, ErrDispatchTransferForbidden
		}
	}

	// The project must be one the requester could have asked directly: the
	// leader acts for the requester and cannot widen who reaches a project.
	requesterParams := OrgDispatchParams{
		WorkspaceID: origin.WorkspaceID,
		ActorID:     origin.RequesterID,
		ProjectID:   p.ProjectID,
	}
	access, err := workspaceaccess.Resolve(ctx, s.Queries, origin.WorkspaceID, origin.RequesterID, false)
	if err != nil || !access.Allowed() {
		return DispatchTransferResult{}, ErrOrgUnitNotFound
	}
	units, err := orgUnitsForUser(ctx, s.Queries, origin.WorkspaceID, access)
	if err != nil {
		return DispatchTransferResult{}, fmt.Errorf("list org units: %w", err)
	}
	unit, project, err := s.projectDispatchUnit(ctx, requesterParams, access, units)
	if err != nil {
		return DispatchTransferResult{}, err
	}

	executor, err := s.transferExecutor(ctx, *project, unit.ID, units, p.PreferredAgentID)
	if err != nil {
		return DispatchTransferResult{}, err
	}

	created, err := s.Issues.Create(ctx, IssueCreateParams{
		WorkspaceID:  project.ID,
		Title:        issue.Title,
		Description:  issue.Description,
		Status:       issuestatus.Todo,
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   executor,
		CreatorType:  "member",
		CreatorID:    origin.RequesterID,
	}, IssueCreateOpts{
		SkipAssignedTaskEnqueue: true,
		BroadcastPayload: func(created db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
			return map[string]any{"issue": IssueToMapResolved(ctx, s.Queries, created, project.IssuePrefix)}
		},
	})
	if err != nil {
		return DispatchTransferResult{}, err
	}

	// The origin is the transfer's commit point: once it follows the new issue,
	// the request is the project's. A concurrent transfer that got here first
	// leaves no row to update.
	if _, err := s.Queries.TransferDispatchOriginToProject(ctx, db.TransferDispatchOriginToProjectParams{
		ToIssueID:   created.Issue.ID,
		ProjectID:   project.ID,
		FromIssueID: issue.ID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrDispatchTransferUnsupported
		}
		s.discardTransferIssue(ctx, created.Issue)
		return DispatchTransferResult{}, err
	}

	if moved, err := s.Queries.MoveIssueAttachmentsToIssue(ctx, db.MoveIssueAttachmentsToIssueParams{
		ToWorkspaceID:   project.ID,
		ToIssueID:       created.Issue.ID,
		FromIssueID:     issue.ID,
		FromWorkspaceID: issue.WorkspaceID,
	}); err != nil {
		slog.Warn("dispatch transfer: move attachments failed",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
	} else if moved > 0 {
		slog.Info("dispatch transfer: moved attachments",
			"issue_id", util.UUIDToString(issue.ID), "count", moved)
	}

	previous := s.closeTransferredIssue(ctx, issue, created.Issue, *project, p)

	if s.Tasks != nil {
		note := s.projectDispatchRoutingNote(ctx, project.ID, executor)
		if _, err := s.Tasks.EnqueueTaskForDispatchExecutor(ctx, created.Issue, executor, origin.RequesterID, note); err != nil {
			return DispatchTransferResult{}, fmt.Errorf("wake the project executor: %w", err)
		}
	}
	return DispatchTransferResult{Issue: created.Issue, Previous: previous, Project: *project, Executor: executor}, nil
}

// DispatchTransferCandidates lists the projects a department leader may hand
// the dispatch on issue to, with what each does and who runs it. Anything that
// keeps the issue from being transferable returns no candidates.
func (s *OrgDispatchService) DispatchTransferCandidates(ctx context.Context, issueID pgtype.UUID) []TransferCandidate {
	origin, _, _, err := s.transferSource(ctx, issueID)
	if err != nil {
		return nil
	}
	access, err := workspaceaccess.Resolve(ctx, s.Queries, origin.WorkspaceID, origin.RequesterID, false)
	if err != nil || !access.Allowed() {
		return nil
	}
	units, err := orgUnitsForUser(ctx, s.Queries, origin.WorkspaceID, access)
	if err != nil {
		return nil
	}
	sources, err := intakeSources(ctx, s.Queries, origin.WorkspaceID)
	if err != nil {
		return nil
	}
	candidates, err := s.routeProjectCandidates(ctx, origin.WorkspaceID, access, units, sources, orgscope.Paths(units))
	if err != nil {
		slog.Warn("dispatch transfer: list project candidates failed",
			"issue_id", util.UUIDToString(issueID), "error", err)
		return nil
	}
	out := make([]TransferCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, TransferCandidate{
			ProjectID:   candidate.project.ID,
			Name:        candidate.project.Name,
			Description: candidate.project.Description,
			Context:     candidate.project.Context,
			Agents:      candidate.agents,
		})
	}
	return out
}

// transferSource loads an open department dispatch with the squad that holds
// it. Every other shape answers ErrDispatchTransferUnsupported.
func (s *OrgDispatchService) transferSource(ctx context.Context, issueID pgtype.UUID) (db.DispatchOrigin, db.Issue, db.Squad, error) {
	origin, err := s.Queries.GetDispatchOrigin(ctx, issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, ErrDispatchNotFound
	}
	if err != nil {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, fmt.Errorf("load dispatch origin: %w", err)
	}
	if !origin.TargetUnitID.Valid || origin.TargetProjectID.Valid {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, ErrDispatchTransferUnsupported
	}
	issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueID,
		WorkspaceID: origin.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, ErrDispatchNotFound
	}
	if err != nil {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, fmt.Errorf("load dispatched issue: %w", err)
	}
	category := issuestatus.Category(ctx, s.Queries, issue.WorkspaceID, issue.Status)
	if category == issuestatus.CategoryDone || category == issuestatus.CategoryClosed {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, ErrDispatchTransferUnsupported
	}
	if issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, ErrDispatchTransferUnsupported
	}
	squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, ErrDispatchTransferUnsupported
	}
	if err != nil {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, fmt.Errorf("load dispatch squad: %w", err)
	}
	if !squad.OrgManaged || squad.IsPrivate {
		return db.DispatchOrigin{}, db.Issue{}, db.Squad{}, ErrDispatchTransferUnsupported
	}
	return origin, issue, squad, nil
}

// transferExecutor honours the leader's choice when it names a ready agent of
// the project, and otherwise picks the way a project dispatch does.
func (s *OrgDispatchService) transferExecutor(ctx context.Context, project db.Workspace, owningUnitID pgtype.UUID, units []db.ListOrgUnitsRow, preferred pgtype.UUID) (pgtype.UUID, error) {
	if !preferred.Valid {
		return s.pickProjectExecutor(ctx, project.ID, owningUnitID, units)
	}
	agent, err := s.Queries.GetAgent(ctx, preferred)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (agent.WorkspaceID != project.ID || agent.Kind != "user")) {
		return pgtype.UUID{}, ErrDispatchTransferAgentUnavailable
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("load chosen project agent: %w", err)
	}
	verdict, err := AgentReadiness(ctx, RuntimeLookup{Queries: s.Queries, Source: metrics.RuntimeLookupSourceOther}, agent)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("check chosen project agent runtime: %w", err)
	}
	if !verdict.Ready() {
		return pgtype.UUID{}, ErrDispatchTransferAgentUnavailable
	}
	return agent.ID, nil
}

// closeTransferredIssue stops the department's copy: it records where the work
// went, cancels the other runs still queued on it and closes it. The caller's
// own run is left to finish its turn. Failures are logged; the request already
// lives in the project.
func (s *OrgDispatchService) closeTransferredIssue(ctx context.Context, issue, delivered db.Issue, project db.Workspace, p DispatchTransferParams) db.Issue {
	identifier := IssueIdentifier(project.IssuePrefix, delivered.Number)
	content := fmt.Sprintf("已转交到项目「%s」：%s", project.Name, identifier)
	if note := strings.TrimSpace(p.Note); note != "" {
		content += "\n\n" + note
	}
	if _, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    p.AgentID,
		Content:     content,
		Type:        "system",
	}); err != nil {
		slog.Warn("dispatch transfer: record handoff comment failed",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
	}
	if s.Tasks != nil {
		if err := s.Tasks.CancelTasksForIssueExcept(ctx, issue.ID, p.TaskID); err != nil {
			slog.Warn("dispatch transfer: cancel department runs failed",
				"issue_id", util.UUIDToString(issue.ID), "error", err)
		}
	}
	updated, err := s.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Status:      issuestatus.Cancelled,
	})
	if err != nil {
		slog.Warn("dispatch transfer: close department issue failed",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
		return issue
	}
	if s.Tasks != nil {
		s.Tasks.broadcastIssueUpdated(ctx, updated, issue.Status)
	}
	return updated
}

// discardTransferIssue removes the project issue a lost transfer created, so a
// retry or the winning transfer leaves exactly one copy in the project.
func (s *OrgDispatchService) discardTransferIssue(ctx context.Context, issue db.Issue) {
	if err := s.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID}); err != nil {
		slog.Warn("dispatch transfer: discard orphan project issue failed",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
	}
}

// dispatchTransferBriefingLimit bounds the projects one briefing lists.
const dispatchTransferBriefingLimit = 20

// DispatchTransferBriefing is the "## Project Handoff" section a department
// leader reads on a dispatch it holds: the projects the requester may hand
// work, what each does, who runs it, and the command that moves the request.
// An issue that cannot be transferred, or a requester with no requestable
// project, gets no section.
func (s *OrgDispatchService) DispatchTransferBriefing(ctx context.Context, issueID pgtype.UUID) string {
	candidates := s.DispatchTransferCandidates(ctx, issueID)
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) > dispatchTransferBriefingLimit {
		candidates = candidates[:dispatchTransferBriefingLimit]
	}
	issueRef := util.UUIDToString(issueID)
	var b strings.Builder
	b.WriteString("## Project Handoff\n\n")
	b.WriteString("This request was routed to your department. If it is really about one of the projects below — it names the project, or it asks for what the project or one of its agents is responsible for — do not do it or split it here. Transfer it to that project instead:\n\n")
	fmt.Fprintf(&b, "    multica squad transfer-to-project %s --project <project-id> [--agent <agent-id>] [--note \"<why>\"]\n\n", issueRef)
	b.WriteString("- The project gets a new issue with the same title, description and attachments, run by the agent you name (pick the one whose responsibilities match), or by the project's default agent when you name none. The requester keeps following the same request.\n")
	b.WriteString("- This issue is then closed as transferred. Record the decision with `multica squad activity` as usual and end the turn; do not post a separate reply.\n")
	b.WriteString("- When the request is department work, or no project clearly fits, ignore this section.\n\n")
	b.WriteString("Projects:\n")
	for _, candidate := range candidates {
		fmt.Fprintf(&b, "- %s — project id `%s`", candidate.Name, util.UUIDToString(candidate.ProjectID))
		if desc := routeCatalogText(candidate.Description, dispatchRouteDescLimit); desc != "" {
			b.WriteString(" — " + desc)
		}
		if background := routeCatalogText(candidate.Context, dispatchRouteContextLimit); background != "" {
			b.WriteString(" — context: " + background)
		}
		b.WriteString("\n")
		for i, agent := range candidate.Agents {
			if i == dispatchRouteDutyCount {
				break
			}
			duty := routeCatalogText(dutyRowText(agent), dispatchRouteDutyLimit)
			if duty == "" {
				duty = "(none declared)"
			}
			fmt.Fprintf(&b, "  - agent %s — agent id `%s` — responsibilities: %s\n", agent.Name, util.UUIDToString(agent.ID), duty)
		}
	}
	return b.String()
}
