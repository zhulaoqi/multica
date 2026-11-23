package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dispatchToProject delivers a dispatch into the project workspace itself: the
// root issue is created there, assigned to an agent that runs in that
// workspace, and the first run is enqueued as the dispatch's leader task. The
// origin row stays in the organization workspace so the requester can follow
// the work from the C-end and Feishu.
func (s *OrgDispatchService) dispatchToProject(
	ctx context.Context,
	p OrgDispatchParams,
	units []db.ListOrgUnitsRow,
	unit db.ListOrgUnitsRow,
	project db.Workspace,
) (OrgDispatchResult, error) {
	executor, err := s.pickProjectExecutor(ctx, project.ID, unit.ID, units)
	if err != nil {
		return OrgDispatchResult{}, err
	}
	// Uploads land in the organization workspace the request was made from.
	// Move the requester's own unbound files to the project first so the create
	// there can bind them.
	if err := s.rehomeDispatchAttachments(ctx, p, project.ID); err != nil {
		return OrgDispatchResult{}, err
	}

	created, err := s.Issues.Create(ctx, IssueCreateParams{
		WorkspaceID:   project.ID,
		Title:         p.Title,
		Description:   pgtype.Text{String: p.Description, Valid: strings.TrimSpace(p.Description) != ""},
		Status:        issuestatus.Todo,
		Priority:      "none",
		AssigneeType:  pgtype.Text{String: "agent", Valid: true},
		AssigneeID:    executor,
		CreatorType:   "member",
		CreatorID:     p.ActorID,
		AttachmentIDs: p.AttachmentIDs,
	}, IssueCreateOpts{Platform: p.Platform, SkipAssignedTaskEnqueue: true})
	if err != nil {
		return OrgDispatchResult{}, err
	}
	if len(created.Attachments) < len(p.AttachmentIDs) {
		slog.Warn("project dispatch: some attachments did not bind to the issue",
			"issue_id", util.UUIDToString(created.Issue.ID),
			"project_id", util.UUIDToString(project.ID),
			"requested", len(p.AttachmentIDs),
			"linked", len(created.Attachments))
	}

	if err := s.recordOrigin(ctx, created.Issue, unit.ID, pgtype.UUID{}, false, p); err != nil {
		return OrgDispatchResult{}, err
	}
	if s.Tasks != nil {
		if _, err := s.Tasks.EnqueueTaskForDispatchExecutor(ctx, created.Issue, executor, p.ActorID); err != nil {
			return OrgDispatchResult{}, fmt.Errorf("wake the project executor: %w", err)
		}
	}
	return OrgDispatchResult{Issue: created.Issue, Unit: unit, Project: &project}, nil
}

// pickProjectExecutor chooses the agent in the project workspace that runs the
// dispatch. Candidates are online user agents; an owner who leads the owning
// department goes before an ancestor's lead (the direct lead knows the project
// best), then the primary twin, then the oldest agent. No ready candidate is an
// explicit refusal: the work belongs to the project, so it must not be pushed
// back onto the department.
func (s *OrgDispatchService) pickProjectExecutor(ctx context.Context, projectID, owningUnitID pgtype.UUID, units []db.ListOrgUnitsRow) (pgtype.UUID, error) {
	agents, err := s.Queries.ListProjectDispatchAgents(ctx, projectID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("list project agents: %w", err)
	}
	if len(agents) == 0 {
		return pgtype.UUID{}, ErrProjectAgentOffline
	}
	ordered := make([]db.ListProjectDispatchAgentsRow, len(agents))
	copy(ordered, agents)
	sort.SliceStable(ordered, func(i, j int) bool {
		leftDepth, leftLead := orgscope.LeadDepth(units, ordered[i].OwnerID, owningUnitID)
		rightDepth, rightLead := orgscope.LeadDepth(units, ordered[j].OwnerID, owningUnitID)
		if leftLead != rightLead {
			return leftLead
		}
		return leftLead && leftDepth < rightDepth
	})
	for _, candidate := range ordered {
		agent, err := s.Queries.GetAgent(ctx, candidate.ID)
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("load project agent: %w", err)
		}
		verdict, err := AgentReadiness(ctx, RuntimeLookup{
			Queries: s.Queries,
			Source:  metrics.RuntimeLookupSourceOther,
		}, agent)
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("check project agent runtime: %w", err)
		}
		if verdict.Ready() {
			return candidate.ID, nil
		}
	}
	return pgtype.UUID{}, ErrProjectAgentOffline
}

// rehomeDispatchAttachments moves the requester's unbound uploads from the
// organization workspace into the project so the create there can bind them.
// Only the caller's own unattached files qualify; a missing one fails the whole
// dispatch instead of dropping the file silently.
func (s *OrgDispatchService) rehomeDispatchAttachments(ctx context.Context, p OrgDispatchParams, projectID pgtype.UUID) error {
	if len(p.AttachmentIDs) == 0 {
		return nil
	}
	moved, err := s.Queries.RehomeDispatchAttachments(ctx, db.RehomeDispatchAttachmentsParams{
		ToWorkspaceID:   projectID,
		FromWorkspaceID: p.WorkspaceID,
		UploaderID:      p.ActorID,
		AttachmentIds:   p.AttachmentIDs,
	})
	if err != nil {
		return fmt.Errorf("move attachments to the project: %w", err)
	}
	if int(moved) != len(p.AttachmentIDs) {
		return ErrDispatchAttachmentNotFound
	}
	return nil
}
