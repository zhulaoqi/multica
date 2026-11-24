package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/orgsquad"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Dispatch channels decide how the requester is told the department delivered:
// in-app requesters get an inbox item, chat requesters get a reply in the
// conversation that asked.
const (
	DispatchChannelApp    = "app"
	DispatchChannelFeishu = "feishu"

	inboxTypeDispatchCompleted = "dispatch_completed"
	inboxTypeDispatchFailed    = "dispatch_failed"

	// dispatchMatchLimit bounds the candidate list a chat reply can render.
	dispatchMatchLimit = 5
)

var (
	ErrOrgUnitNotFound           = errors.New("org unit not found")
	ErrOrgUnitNoAccess           = errors.New("no access to this org unit")
	ErrOrgUnitNoAgents           = errors.New("org unit has no agent to lead the dispatch")
	ErrDispatchLeaderUnavailable = errors.New("squad leader is not available to this requester")
	// ErrDispatchLeaderOffline means the department has agents, but none of them
	// can take work now: their runtimes are missing or offline. Dispatch refuses
	// instead of creating an issue no leader task will ever pick up.
	ErrDispatchLeaderOffline = errors.New("no agent in this department can run work right now")
	// ErrProjectAgentOffline means the target project has no agent that can run
	// work right now. The work belongs to the project, so it is refused rather
	// than silently flattened into the owning department.
	ErrProjectAgentOffline = errors.New("no agent in this project can run work right now")
	// ErrDispatchProjectReassignUnsupported means the caller tried to move a
	// dispatch that was delivered into a project workspace. That work belongs
	// to the project and is not moved between departments.
	ErrDispatchProjectReassignUnsupported = errors.New("a dispatch delivered to a project cannot be reassigned")
	// ErrDispatchAttachmentNotFound means some requested attachments did not
	// belong to the requester as unbound uploads, so they cannot follow the
	// work into the project workspace.
	ErrDispatchAttachmentNotFound = errors.New("dispatch attachment not found")
	ErrDispatchRequesterNotInTeam = errors.New("requester is not a member of this workspace")
	ErrDispatchRequesterUnbound   = errors.New("requester has not bound an agent in this workspace")
	ErrPersonalSquadNotFound      = errors.New("personal squad not found")
	ErrDispatchNotFound           = errors.New("dispatch not found")
	ErrDispatchReassignForbidden  = errors.New("only the requester can reassign this dispatch")
	// ErrDispatchOrganizationOnly means the caller dispatched inside a project
	// while an organization workspace exists. Departments and their agents
	// work in the organization workspace only.
	ErrDispatchOrganizationOnly = errors.New("dispatch work from the organization workspace")
)

// DispatchOriginInfo carries the transport coordinates a dispatch came from.
// App dispatches leave them empty; a chat request keeps them so the completion
// notice can return to the same conversation.
type DispatchOriginInfo struct {
	ChannelType    string
	InstallationID pgtype.UUID
	ChatID         string
	ThreadID       string
	// MessageID is the chat message that asked for the work. Notices reply
	// inside its topic so the conversation stays in one place.
	MessageID string
}

type OrgDispatchParams struct {
	WorkspaceID pgtype.UUID
	ActorID     pgtype.UUID
	UnitID      pgtype.UUID
	// ProjectID targets a project the caller may request work for. The work is
	// created and run in the project workspace; the owning department only
	// gates who may ask and stays on the origin row.
	ProjectID pgtype.UUID
	// Personal targets the requester's own private squad instead of a
	// department. SquadID selects one of the caller's private squads; leaving
	// it empty with Personal set reuses (or creates) their personal squad.
	Personal    bool
	SquadID     pgtype.UUID
	Title       string
	Description string
	// ClientRequestID deduplicates in-app retries; chat callers leave it empty.
	ClientRequestID pgtype.Text
	// AttachmentIDs are files the requester uploaded with the request. They are
	// bound to the root issue in the same create, so the squad leader can open
	// the deck or screenshot that motivated the work.
	AttachmentIDs []pgtype.UUID
	Origin        DispatchOriginInfo
	// Platform tags the IssueCreated analytics event with the caller's surface.
	Platform string
}

type OrgDispatchResult struct {
	Issue db.Issue
	Squad db.Squad
	Unit  db.ListOrgUnitsRow
	// Project is the target project when the dispatch was aimed at one; the
	// work still landed on its owning department.
	Project *db.Workspace
	// Personal marks a dispatch to the requester's own squad; Unit is empty
	// in that case.
	Personal bool
}

// OrgDispatchNotice is everything a chat transport needs to report completion:
// the original conversation plus the delivered issue. App dispatches consume
// their notice as an inbox item and never return one.
type OrgDispatchNotice struct {
	Origin     db.DispatchOrigin
	Issue      db.Issue
	UnitName   string
	Identifier string
}

// OrgDispatchFailureNotice is what a chat transport needs to report a
// dispatch that the department could not run: the original conversation plus
// the failed root issue and why the leader run stopped.
type OrgDispatchFailureNotice struct {
	Origin     db.DispatchOrigin
	Issue      db.Issue
	UnitName   string
	Identifier string
	Reason     string
}

// UnitMatch is one department a chat command name resolved to.
type UnitMatch struct {
	Unit db.ListOrgUnitsRow
	Path string
}

// OrgDispatchService is the single dispatch entry shared by the web app and
// chat bots: it enforces department visibility, makes sure the department has
// a squad to lead the work, creates the root issue assigned to that squad, and
// remembers who asked so completion can be reported back.
type OrgDispatchService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Issues    *IssueService
	Tasks     *TaskService
	Bus       *events.Bus
	// LLM routes natural-language workbench requests between departments.
	// Nil or disabled falls back to deterministic matching.
	LLM TextGenerator
}

// Dispatch creates the root issue for one department. The squad leader is the
// only agent woken: it decomposes the request and assigns the children.
func (s *OrgDispatchService) Dispatch(ctx context.Context, p OrgDispatchParams) (OrgDispatchResult, error) {
	if err := s.requireOrganizationWorkspace(ctx, p.WorkspaceID); err != nil {
		return OrgDispatchResult{}, err
	}
	access, err := workspaceaccess.Resolve(ctx, s.Queries, p.WorkspaceID, p.ActorID, false)
	if err != nil || !access.Allowed() {
		return OrgDispatchResult{}, ErrDispatchRequesterNotInTeam
	}
	// Unbound members may browse and review, but not hand work to a department:
	// their requests would have no digital twin to answer for the follow-up.
	twin, err := s.Queries.GetPrimaryAgentForOwner(ctx, db.GetPrimaryAgentForOwnerParams{
		WorkspaceID: p.WorkspaceID,
		OwnerID:     p.ActorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OrgDispatchResult{}, ErrDispatchRequesterUnbound
		}
		return OrgDispatchResult{}, fmt.Errorf("check requester twin: %w", err)
	}

	var squad db.Squad
	var unit db.ListOrgUnitsRow
	var project *db.Workspace
	personal := p.Personal || p.SquadID.Valid
	if personal {
		squad, err = s.resolvePersonalSquad(ctx, p, twin)
		if err != nil {
			return OrgDispatchResult{}, err
		}
		// The requester's own twin leads a personal squad. If its runtime is
		// offline the leader task would queue under a machine that is not there,
		// so say so now instead of leaving the work silent.
		state, err := s.leaderPickState(ctx, squad.LeaderID, p.ActorID, p.WorkspaceID)
		if err != nil {
			return OrgDispatchResult{}, err
		}
		if state != pickReady {
			return OrgDispatchResult{}, ErrDispatchLeaderOffline
		}
	} else {
		units, err := orgUnitsForUser(ctx, s.Queries, p.WorkspaceID, access)
		if err != nil {
			return OrgDispatchResult{}, fmt.Errorf("list org units: %w", err)
		}
		if p.ProjectID.Valid {
			unit, project, err = s.projectDispatchUnit(ctx, p, access, units)
			if err != nil {
				return OrgDispatchResult{}, err
			}
			// Project work is delivered into the project workspace itself,
			// assigned to an agent that runs there. The owning department keeps
			// the origin row for reporting but is not asked to run the work.
			return s.dispatchToProject(ctx, p, units, unit, *project)
		} else {
			found, ok := findUnit(units, p.UnitID)
			if !ok {
				return OrgDispatchResult{}, ErrOrgUnitNotFound
			}
			sources, err := intakeSources(ctx, s.Queries, p.WorkspaceID)
			if err != nil {
				return OrgDispatchResult{}, err
			}
			if !canRequestToUnit(access, units, sources, found.ID) {
				return OrgDispatchResult{}, ErrOrgUnitNoAccess
			}
			unit = found
		}

		agents, err := s.Queries.ListAgentsInOrgUnits(ctx, p.WorkspaceID)
		if err != nil {
			return OrgDispatchResult{}, fmt.Errorf("list org agents: %w", err)
		}
		squad, err = s.ensureSquad(ctx, p.WorkspaceID, p.ActorID, unit, units, agents)
		if err != nil {
			return OrgDispatchResult{}, err
		}
		// An existing squad may still point at a leader whose runtime went away.
		// Move the squad to an available agent so this dispatch can actually run.
		squad, err = s.ensureRunnableLeader(ctx, p.WorkspaceID, p.ActorID, unit, units, agents, squad)
		if err != nil {
			return OrgDispatchResult{}, err
		}
	}
	if s.Tasks != nil {
		allowed, err := s.Tasks.MemberMayCollaborateWithAgent(ctx, squad.LeaderID, p.ActorID)
		if err != nil {
			return OrgDispatchResult{}, fmt.Errorf("check leader invocation: %w", err)
		}
		if !allowed {
			return OrgDispatchResult{}, ErrDispatchLeaderUnavailable
		}
	}

	created, err := s.Issues.Create(ctx, IssueCreateParams{
		WorkspaceID:   p.WorkspaceID,
		Title:         p.Title,
		Description:   pgtype.Text{String: p.Description, Valid: strings.TrimSpace(p.Description) != ""},
		Status:        issuestatus.Todo,
		Priority:      "none",
		AssigneeType:  pgtype.Text{String: "squad", Valid: true},
		AssigneeID:    squad.ID,
		CreatorType:   "member",
		CreatorID:     p.ActorID,
		AttachmentIDs: p.AttachmentIDs,
		OrgUnitID:     unit.ID,
	}, IssueCreateOpts{
		Platform: p.Platform,
		// Squad and department issues are created here, not in dispatchToProject.
		// The subscriber listener reads the creator from this payload; without
		// it nobody is subscribed, so comments and status changes never become
		// inbox items and the Feishu bot has nothing to relay.
		BroadcastPayload: func(issue db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
			prefix := ""
			ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
			if err != nil {
				slog.Warn("dispatch: load workspace for created event failed",
					"issue_id", util.UUIDToString(issue.ID), "error", err)
			} else {
				prefix = ws.IssuePrefix
			}
			return map[string]any{"issue": IssueToMapResolved(ctx, s.Queries, issue, prefix)}
		},
	})
	if err != nil {
		return OrgDispatchResult{}, err
	}

	if err := s.recordOrigin(ctx, created.Issue, unit.ID, squad.ID, personal, p); err != nil {
		return OrgDispatchResult{}, err
	}
	return OrgDispatchResult{Issue: created.Issue, Squad: squad, Unit: unit, Project: project, Personal: personal}, nil
}

type OrgDispatchReassignParams struct {
	WorkspaceID pgtype.UUID
	ActorID     pgtype.UUID
	IssueID     pgtype.UUID
	UnitID      pgtype.UUID
}

// Reassign moves a dispatch the caller created to another department. The
// requester is the only one who can redirect their own request; the target
// department goes through the same leader-availability and invocation checks
// as a fresh dispatch, so a reassignment cannot park the issue behind an
// agent nobody can run.
func (s *OrgDispatchService) Reassign(ctx context.Context, p OrgDispatchReassignParams) (OrgDispatchResult, error) {
	if err := s.requireOrganizationWorkspace(ctx, p.WorkspaceID); err != nil {
		return OrgDispatchResult{}, err
	}
	access, err := workspaceaccess.Resolve(ctx, s.Queries, p.WorkspaceID, p.ActorID, false)
	if err != nil || !access.Allowed() {
		return OrgDispatchResult{}, ErrDispatchRequesterNotInTeam
	}
	origin, err := s.Queries.GetDispatchOrigin(ctx, p.IssueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgDispatchResult{}, ErrDispatchNotFound
	}
	if err != nil {
		return OrgDispatchResult{}, fmt.Errorf("load dispatch origin: %w", err)
	}
	if origin.WorkspaceID != p.WorkspaceID {
		return OrgDispatchResult{}, ErrDispatchNotFound
	}
	if origin.RequesterID != p.ActorID {
		return OrgDispatchResult{}, ErrDispatchReassignForbidden
	}
	// Work delivered into a project workspace lives on the project's board and
	// is run by its agents; moving it to a department would cross workspaces.
	if origin.TargetProjectID.Valid {
		return OrgDispatchResult{}, ErrDispatchProjectReassignUnsupported
	}
	issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          p.IssueID,
		WorkspaceID: p.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgDispatchResult{}, ErrDispatchNotFound
	}
	if err != nil {
		return OrgDispatchResult{}, fmt.Errorf("load dispatched issue: %w", err)
	}

	units, err := orgUnitsForUser(ctx, s.Queries, p.WorkspaceID, access)
	if err != nil {
		return OrgDispatchResult{}, fmt.Errorf("list org units: %w", err)
	}
	unit, ok := findUnit(units, p.UnitID)
	if !ok {
		return OrgDispatchResult{}, ErrOrgUnitNotFound
	}
	sources, err := intakeSources(ctx, s.Queries, p.WorkspaceID)
	if err != nil {
		return OrgDispatchResult{}, err
	}
	if !canRequestToUnit(access, units, sources, unit.ID) {
		return OrgDispatchResult{}, ErrOrgUnitNoAccess
	}
	agents, err := s.Queries.ListAgentsInOrgUnits(ctx, p.WorkspaceID)
	if err != nil {
		return OrgDispatchResult{}, fmt.Errorf("list org agents: %w", err)
	}
	squad, err := s.ensureSquad(ctx, p.WorkspaceID, p.ActorID, unit, units, agents)
	if err != nil {
		return OrgDispatchResult{}, err
	}
	squad, err = s.ensureRunnableLeader(ctx, p.WorkspaceID, p.ActorID, unit, units, agents, squad)
	if err != nil {
		return OrgDispatchResult{}, err
	}
	if s.Tasks != nil {
		allowed, err := s.Tasks.MemberMayCollaborateWithAgent(ctx, squad.LeaderID, p.ActorID)
		if err != nil {
			return OrgDispatchResult{}, fmt.Errorf("check leader invocation: %w", err)
		}
		if !allowed {
			return OrgDispatchResult{}, ErrDispatchLeaderUnavailable
		}
	}

	updated, err := s.Queries.UpdateIssueAssignee(ctx, db.UpdateIssueAssigneeParams{
		ID:          issue.ID,
		AssigneeID:  squad.ID,
		WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return OrgDispatchResult{}, fmt.Errorf("move issue to the new squad: %w", err)
	}
	if _, err := s.Queries.ReassignDispatchOrigin(ctx, db.ReassignDispatchOriginParams{
		IssueID:      issue.ID,
		TargetUnitID: unit.ID,
	}); err != nil {
		return OrgDispatchResult{}, fmt.Errorf("retarget dispatch origin: %w", err)
	}
	if err := s.Queries.SetIssueTreeOrgUnit(ctx, db.SetIssueTreeOrgUnitParams{
		IssueID:     issue.ID,
		WorkspaceID: p.WorkspaceID,
		OrgUnitID:   unit.ID,
	}); err != nil {
		return OrgDispatchResult{}, fmt.Errorf("move dispatch to the new department: %w", err)
	}
	updated.OrgUnitID = unit.ID
	if s.Tasks != nil {
		if _, err := s.Tasks.EnqueueTaskForSquadLeaderByActor(ctx, updated, squad.LeaderID, squad.ID, p.ActorID); err != nil {
			return OrgDispatchResult{}, fmt.Errorf("wake the new squad leader: %w", err)
		}
	}
	s.publishAssigneeChanged(ctx, updated, p.ActorID)
	return OrgDispatchResult{Issue: updated, Squad: squad, Unit: unit}, nil
}

// requireOrganizationWorkspace refuses dispatch outside the organization
// workspace once one exists. Before the organization is set up, each project
// keeps dispatching to its own departments.
func (s *OrgDispatchService) requireOrganizationWorkspace(ctx context.Context, workspaceID pgtype.UUID) error {
	org, err := s.Queries.GetOrganizationWorkspace(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load organization workspace: %w", err)
	}
	if org.ID != workspaceID {
		return ErrDispatchOrganizationOnly
	}
	return nil
}

func (s *OrgDispatchService) publishAssigneeChanged(ctx context.Context, issue db.Issue, actorID pgtype.UUID) {
	if s.Bus == nil {
		return
	}
	workspace, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		slog.Warn("dispatch reassign: load workspace failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "member",
		ActorID:     util.UUIDToString(actorID),
		Payload: map[string]any{
			"issue":            IssueToMapResolved(ctx, s.Queries, issue, workspace.IssuePrefix),
			"assignee_changed": true,
			"status_changed":   false,
			"project_changed":  false,
		},
	})
}

// resolvePersonalSquad returns the caller's own squad: the one they named, or
// the single personal squad created on their first personal dispatch.
func (s *OrgDispatchService) resolvePersonalSquad(ctx context.Context, p OrgDispatchParams, twin db.Agent) (db.Squad, error) {
	if !p.SquadID.Valid {
		return s.ensurePersonalSquad(ctx, p.WorkspaceID, p.ActorID, twin)
	}
	squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          p.SquadID,
		WorkspaceID: p.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Squad{}, ErrPersonalSquadNotFound
	}
	if err != nil {
		return db.Squad{}, fmt.Errorf("load personal squad: %w", err)
	}
	// Someone else's squad, a shared squad, or an archived one all answer the
	// same way: a personal target only ever resolves the caller's own.
	if !squad.IsPrivate || squad.ArchivedAt.Valid || util.UUIDToString(squad.CreatorID) != util.UUIDToString(p.ActorID) {
		return db.Squad{}, ErrPersonalSquadNotFound
	}
	return squad, nil
}

// ensurePersonalSquad returns the caller's personal squad, creating it on the
// first personal dispatch. The caller's digital twin leads it, so the leader
// engine decomposes personal work exactly as it does department work.
func (s *OrgDispatchService) ensurePersonalSquad(ctx context.Context, wsUUID, actorID pgtype.UUID, twin db.Agent) (db.Squad, error) {
	squad, err := s.Queries.GetPersonalSquadByCreator(ctx, db.GetPersonalSquadByCreatorParams{
		WorkspaceID: wsUUID,
		CreatorID:   actorID,
	})
	if err == nil {
		return squad, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Squad{}, fmt.Errorf("load personal squad: %w", err)
	}

	name := strings.TrimSpace(twin.Name)
	if name == "" {
		if user, err := s.Queries.GetUser(ctx, actorID); err == nil {
			name = strings.TrimSpace(user.Name)
		}
	}
	if name == "" {
		name = "Personal squad"
	}
	squad, err = s.Queries.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: wsUUID,
		Name:        name,
		LeaderID:    twin.ID,
		CreatorID:   actorID,
		IsPrivate:   true,
	})
	if err != nil {
		// A concurrent first dispatch won the one-personal-squad index; use
		// the row it created.
		if isUniqueViolation(err) {
			existing, loadErr := s.Queries.GetPersonalSquadByCreator(ctx, db.GetPersonalSquadByCreatorParams{
				WorkspaceID: wsUUID,
				CreatorID:   actorID,
			})
			if loadErr == nil {
				return existing, nil
			}
		}
		return db.Squad{}, fmt.Errorf("create personal squad: %w", err)
	}
	if _, err := s.Queries.AddSquadMember(ctx, db.AddSquadMemberParams{
		SquadID:    squad.ID,
		MemberType: "agent",
		MemberID:   twin.ID,
		Role:       "leader",
	}); err != nil {
		return db.Squad{}, fmt.Errorf("add personal squad leader: %w", err)
	}
	return squad, nil
}

func (s *OrgDispatchService) recordOrigin(ctx context.Context, issue db.Issue, unitID, squadID pgtype.UUID, personal bool, p OrgDispatchParams) error {
	channel := p.Origin.ChannelType
	if channel == "" {
		channel = DispatchChannelApp
	}
	targetUnit := unitID
	targetSquad := pgtype.UUID{}
	if personal {
		targetUnit = pgtype.UUID{}
		targetSquad = squadID
	}
	_, err := s.Queries.CreateDispatchOrigin(ctx, db.CreateDispatchOriginParams{
		IssueID:         issue.ID,
		WorkspaceID:     p.WorkspaceID,
		RequesterID:     p.ActorID,
		TargetUnitID:    targetUnit,
		TargetProjectID: p.ProjectID,
		ChannelType:     channel,
		InstallationID:  p.Origin.InstallationID,
		ChatID:          pgtype.Text{String: p.Origin.ChatID, Valid: p.Origin.ChatID != ""},
		ThreadID:        pgtype.Text{String: p.Origin.ThreadID, Valid: p.Origin.ThreadID != ""},
		OriginMessageID: pgtype.Text{String: p.Origin.MessageID, Valid: p.Origin.MessageID != ""},
		ClientRequestID: p.ClientRequestID,
		TargetSquadID:   targetSquad,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("record dispatch origin: %w", err)
	}
	return nil
}

// ensureSquad returns the department's squad, creating the mirror squad on
// first dispatch. The department lead's own agent leads when it has one;
// otherwise the first agent placed in the subtree does.
func (s *OrgDispatchService) ensureSquad(
	ctx context.Context,
	wsUUID, actorID pgtype.UUID,
	unit db.ListOrgUnitsRow,
	units []db.ListOrgUnitsRow,
	agents []db.ListAgentsInOrgUnitsRow,
) (db.Squad, error) {
	squad, err := s.Queries.GetSquadByOrgUnit(ctx, db.GetSquadByOrgUnitParams{
		WorkspaceID: wsUUID,
		OrgUnitID:   unit.ID,
	})
	if err == nil {
		return squad, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Squad{}, fmt.Errorf("load department squad: %w", err)
	}
	leaderID, err := s.pickRunnableLeader(ctx, wsUUID, agents, units, unit, actorID)
	if err != nil {
		return db.Squad{}, err
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Squad{}, fmt.Errorf("begin squad tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	// Another dispatch may have created the squad between the read and this
	// transaction. The re-read keeps one implicit squad per department.
	if existing, err := qtx.GetSquadByOrgUnit(ctx, db.GetSquadByOrgUnitParams{
		WorkspaceID: wsUUID,
		OrgUnitID:   unit.ID,
	}); err == nil {
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Squad{}, fmt.Errorf("reload department squad: %w", err)
	}

	squad, err = qtx.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: wsUUID,
		Name:        unit.Name,
		LeaderID:    leaderID,
		CreatorID:   actorID,
	})
	if err != nil {
		return db.Squad{}, fmt.Errorf("create department squad: %w", err)
	}
	if err := qtx.SetSquadOrgUnit(ctx, db.SetSquadOrgUnitParams{
		ID:          squad.ID,
		WorkspaceID: wsUUID,
		OrgUnitID:   unit.ID,
	}); err != nil {
		return db.Squad{}, fmt.Errorf("bind department squad: %w", err)
	}
	if err := qtx.MarkSquadOrgManaged(ctx, db.MarkSquadOrgManagedParams{
		ID:          squad.ID,
		WorkspaceID: wsUUID,
	}); err != nil {
		return db.Squad{}, fmt.Errorf("mark department squad: %w", err)
	}
	squad.OrgUnitID = unit.ID
	squad.OrgManaged = true
	if _, err := orgsquad.SyncMembers(ctx, qtx, units, squad, unit.ID, pgtype.UUID{}); err != nil {
		return db.Squad{}, fmt.Errorf("sync department squad: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Squad{}, fmt.Errorf("commit department squad: %w", err)
	}
	return squad, nil
}

// ResolveUnits matches a chat command's department argument against the units
// the requester may dispatch to. Exact paths win; otherwise unique substring
// matches are returned so the caller can ask for a fuller name.
func (s *OrgDispatchService) ResolveUnits(ctx context.Context, wsUUID, actorID pgtype.UUID, query string) ([]UnitMatch, error) {
	access, err := workspaceaccess.Resolve(ctx, s.Queries, wsUUID, actorID, false)
	if err != nil || !access.Allowed() {
		return nil, ErrDispatchRequesterNotInTeam
	}
	units, err := orgUnitsForUser(ctx, s.Queries, wsUUID, access)
	if err != nil {
		return nil, fmt.Errorf("list org units: %w", err)
	}
	sources, err := intakeSources(ctx, s.Queries, wsUUID)
	if err != nil {
		return nil, err
	}
	visible := requestableUnits(access, units, sources)
	paths := orgscope.Paths(units)

	var exact, partial []UnitMatch
	needle := strings.ToLower(strings.TrimSpace(query))
	for _, unit := range units {
		if visible != nil && !visible[unit.ID] {
			continue
		}
		match := UnitMatch{Unit: unit, Path: paths[unit.ID]}
		switch {
		case needle != "" && (strings.EqualFold(match.Path, needle) || strings.EqualFold(unit.Name, needle)):
			exact = append(exact, match)
		case needle != "" && (strings.Contains(strings.ToLower(match.Path), needle) || strings.Contains(strings.ToLower(unit.Name), needle)):
			partial = append(partial, match)
		}
	}
	if len(exact) > 0 {
		return exact, nil
	}
	if len(partial) > dispatchMatchLimit {
		partial = partial[:dispatchMatchLimit]
	}
	return partial, nil
}

// CompleteDispatch reports a delivered dispatch exactly once. App requesters
// get an inbox item and no notice; chat requesters get the origin back so the
// transport can reply in the original conversation. Duplicate status events
// lose the claim race and return no notice.
func (s *OrgDispatchService) CompleteDispatch(ctx context.Context, issue db.Issue) (*OrgDispatchNotice, error) {
	origin, err := s.Queries.ClaimDispatchCompletion(ctx, issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim dispatch completion: %w", err)
	}
	unitName := s.unitName(ctx, origin.WorkspaceID, origin.TargetUnitID)
	identifier := s.issueIdentifier(ctx, issue)
	notice := &OrgDispatchNotice{Origin: origin, Issue: issue, UnitName: unitName, Identifier: identifier}
	if origin.ChannelType == DispatchChannelFeishu {
		return notice, nil
	}
	if err := s.notifyInbox(ctx, notice); err != nil {
		return nil, err
	}
	return nil, nil
}

// FailDispatch tells the requester once that the dispatch's squad-leader run
// failed for good. The claim is keyed to the leader task itself, so a child
// task failing on its own does not report the whole dispatch as failed, and a
// repeated failure event finds no row.
func (s *OrgDispatchService) FailDispatch(ctx context.Context, issue db.Issue, taskID pgtype.UUID, reason string) (*OrgDispatchFailureNotice, error) {
	origin, err := s.Queries.ClaimDispatchFailure(ctx, db.ClaimDispatchFailureParams{
		IssueID: issue.ID,
		ID:      taskID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim dispatch failure: %w", err)
	}
	notice := &OrgDispatchFailureNotice{
		Origin:     origin,
		Issue:      issue,
		UnitName:   s.unitName(ctx, origin.WorkspaceID, origin.TargetUnitID),
		Identifier: s.issueIdentifier(ctx, issue),
		Reason:     reason,
	}
	if origin.ChannelType == DispatchChannelFeishu {
		return notice, nil
	}
	if err := s.notifyFailureInbox(ctx, notice); err != nil {
		return nil, err
	}
	return nil, nil
}

// IsDispatchCompleted reports whether a status means the department delivered:
// review, done or closed. In review counts because a human decision is the next
// step and the requester should see the result now.
func IsDispatchCompleted(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, status string) bool {
	if status == issuestatus.InReview {
		return true
	}
	category, err := issuestatus.CategoryWithError(ctx, q, workspaceID, status)
	if err != nil {
		return false
	}
	return category == issuestatus.CategoryDone || category == issuestatus.CategoryClosed
}

func (s *OrgDispatchService) notifyInbox(ctx context.Context, notice *OrgDispatchNotice) error {
	detailsMap := map[string]any{
		"dispatch_unit_id":   util.UUIDToString(notice.Origin.TargetUnitID),
		"dispatch_unit_name": notice.UnitName,
		"identifier":         notice.Identifier,
		"status":             notice.Issue.Status,
	}
	// A project dispatch's issue lives in the project workspace. The inbox item
	// belongs to the requester in the organization, where opening that issue
	// would 404, so the notice names the project and carries no issue link.
	issueID := notice.Issue.ID
	body := fmt.Sprintf("%s delivered your dispatch to %s.", notice.Identifier, notice.UnitName)
	if notice.UnitName == "" {
		body = fmt.Sprintf("%s delivered your dispatch.", notice.Identifier)
	}
	if notice.Issue.WorkspaceID != notice.Origin.WorkspaceID {
		projectName := s.projectName(ctx, notice.Origin.TargetProjectID)
		detailsMap["dispatch_project_id"] = util.UUIDToString(notice.Origin.TargetProjectID)
		detailsMap["dispatch_project_name"] = projectName
		issueID = pgtype.UUID{}
		if projectName != "" {
			body = fmt.Sprintf("%s delivered your dispatch to project %q.", notice.Identifier, projectName)
		} else {
			body = fmt.Sprintf("%s delivered your dispatch to its project.", notice.Identifier)
		}
	}
	details, _ := json.Marshal(detailsMap)
	item, inserted, err := inbox.CreateIfEligible(ctx, s.Queries, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   notice.Origin.WorkspaceID,
		RecipientType: "member",
		RecipientID:   notice.Origin.RequesterID,
		Type:          inboxTypeDispatchCompleted,
		Severity:      "info",
		IssueID:       issueID,
		Title:         notice.Issue.Title,
		Body:          pgtype.Text{String: body, Valid: true},
		ActorType:     pgtype.Text{String: "system", Valid: true},
		Details:       details,
	})
	if err != nil {
		return fmt.Errorf("write dispatch inbox item: %w", err)
	}
	if !inserted {
		return nil
	}
	s.publishInboxNew(item, map[string]any{"issue_status": notice.Issue.Status})
	return nil
}

func (s *OrgDispatchService) notifyFailureInbox(ctx context.Context, notice *OrgDispatchFailureNotice) error {
	detailsMap := map[string]any{
		"dispatch_unit_id":   util.UUIDToString(notice.Origin.TargetUnitID),
		"dispatch_unit_name": notice.UnitName,
		"identifier":         notice.Identifier,
		"status":             notice.Issue.Status,
		"failure_reason":     notice.Reason,
	}
	issueID := notice.Issue.ID
	body := fmt.Sprintf("%s failed in %s.", notice.Identifier, notice.UnitName)
	if notice.UnitName == "" {
		body = fmt.Sprintf("%s failed.", notice.Identifier)
	}
	if notice.Issue.WorkspaceID != notice.Origin.WorkspaceID {
		projectName := s.projectName(ctx, notice.Origin.TargetProjectID)
		detailsMap["dispatch_project_id"] = util.UUIDToString(notice.Origin.TargetProjectID)
		detailsMap["dispatch_project_name"] = projectName
		issueID = pgtype.UUID{}
		if projectName != "" {
			body = fmt.Sprintf("%s failed in project %q.", notice.Identifier, projectName)
		} else {
			body = fmt.Sprintf("%s failed in its project.", notice.Identifier)
		}
	}
	if notice.Reason != "" {
		body += " " + notice.Reason
	}
	details, _ := json.Marshal(detailsMap)
	item, inserted, err := inbox.CreateIfEligible(ctx, s.Queries, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   notice.Origin.WorkspaceID,
		RecipientType: "member",
		RecipientID:   notice.Origin.RequesterID,
		Type:          inboxTypeDispatchFailed,
		Severity:      "action_required",
		IssueID:       issueID,
		Title:         notice.Issue.Title,
		Body:          pgtype.Text{String: body, Valid: true},
		ActorType:     pgtype.Text{String: "system", Valid: true},
		Details:       details,
	})
	if err != nil {
		return fmt.Errorf("write dispatch failure inbox item: %w", err)
	}
	if !inserted {
		return nil
	}
	s.publishInboxNew(item, map[string]any{"issue_status": notice.Issue.Status})
	return nil
}

func (s *OrgDispatchService) publishInboxNew(item db.InboxItem, extra map[string]any) {
	if s.Bus == nil {
		return
	}
	payload := map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
	}
	for key, value := range extra {
		payload[key] = value
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: util.UUIDToString(item.WorkspaceID),
		ActorType:   "system",
		Payload:     map[string]any{"item": payload},
	})
}

func (s *OrgDispatchService) unitName(ctx context.Context, wsUUID, unitID pgtype.UUID) string {
	units, err := s.Queries.ListOrgUnits(ctx, wsUUID)
	if err != nil {
		slog.Warn("dispatch completion: list units failed", "workspace_id", util.UUIDToString(wsUUID), "error", err)
		return ""
	}
	if unit, ok := findUnit(units, unitID); ok {
		return unit.Name
	}
	return ""
}

func (s *OrgDispatchService) projectName(ctx context.Context, projectID pgtype.UUID) string {
	if !projectID.Valid {
		return ""
	}
	project, err := s.Queries.GetWorkspace(ctx, projectID)
	if err != nil {
		slog.Warn("dispatch notice: load project failed", "project_id", util.UUIDToString(projectID), "error", err)
		return ""
	}
	return project.Name
}

func (s *OrgDispatchService) issueIdentifier(ctx context.Context, issue db.Issue) string {
	ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return IssueIdentifier("", issue.Number)
	}
	return IssueIdentifier(ws.IssuePrefix, issue.Number)
}

// projectDispatchUnit resolves a project target to the department that owns
// it and applies the project's intake list. The work still lands on that
// department; the project only decides who may ask. A project the caller may
// not request is reported as a missing unit, so a caller cannot probe which
// projects exist.
func (s *OrgDispatchService) projectDispatchUnit(ctx context.Context, p OrgDispatchParams, access workspaceaccess.Access, units []db.ListOrgUnitsRow) (db.ListOrgUnitsRow, *db.Workspace, error) {
	project, err := s.Queries.GetWorkspace(ctx, p.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ListOrgUnitsRow{}, nil, ErrOrgUnitNotFound
	}
	if err != nil {
		return db.ListOrgUnitsRow{}, nil, fmt.Errorf("load project: %w", err)
	}
	if project.IsOrganization || !project.OrgUnitID.Valid {
		return db.ListOrgUnitsRow{}, nil, ErrOrgUnitNotFound
	}
	found, ok := findUnit(units, project.OrgUnitID)
	if !ok {
		return db.ListOrgUnitsRow{}, nil, ErrOrgUnitNotFound
	}
	sources, err := s.Queries.ListWorkspaceIntakeSources(ctx, p.ProjectID)
	if err != nil {
		return db.ListOrgUnitsRow{}, nil, fmt.Errorf("list project intake sources: %w", err)
	}
	var departmentSources []db.ListOrgUnitIntakeSourcesRow
	if len(sources) == 0 {
		departmentSources, err = intakeSources(ctx, s.Queries, p.WorkspaceID)
		if err != nil {
			return db.ListOrgUnitsRow{}, nil, err
		}
	}
	_, memberErr := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      p.ActorID,
		WorkspaceID: p.ProjectID,
	})
	if memberErr != nil && !errors.Is(memberErr, pgx.ErrNoRows) {
		return db.ListOrgUnitsRow{}, nil, fmt.Errorf("check project membership: %w", memberErr)
	}
	if !ProjectRequestable(access, units, departmentSources, project, sources, memberErr == nil) {
		return db.ListOrgUnitsRow{}, nil, ErrOrgUnitNoAccess
	}
	return found, &project, nil
}

func findUnit(units []db.ListOrgUnitsRow, unitID pgtype.UUID) (db.ListOrgUnitsRow, bool) {
	for _, unit := range units {
		if unit.ID == unitID {
			return unit, true
		}
	}
	return db.ListOrgUnitsRow{}, false
}

// orgUnitsForUser scopes the org tree to what a project member may see: the
// owning department's subtree for a department project, the whole tree for a
// legacy flat workspace.
func orgUnitsForUser(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, access workspaceaccess.Access) ([]db.ListOrgUnitsRow, error) {
	if access.RootUnitID.Valid {
		return workspaceaccess.SubtreeUnits(ctx, q, workspaceID)
	}
	return q.ListOrgUnits(ctx, workspaceID)
}

// intakeSources loads who each department accepts work from.
func intakeSources(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) ([]db.ListOrgUnitIntakeSourcesRow, error) {
	sources, err := q.ListOrgUnitIntakeSources(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list intake sources: %w", err)
	}
	return sources, nil
}

// requestableUnits answers which departments accept the caller's work.
// Platform managers and the leads of a department or its ancestors always may.
// Everyone else needs a configured intake source: a person, or a department
// whose subtree covers them. A department with no sources accepts nobody but
// its leads, so "who may hand me work" is a decision each lead makes.
// A nil result means every unit.
func requestableUnits(access workspaceaccess.Access, units []db.ListOrgUnitsRow, sources []db.ListOrgUnitIntakeSourcesRow) map[pgtype.UUID]bool {
	if access.IsAdmin() || access.Implicit() {
		return nil
	}
	allowed := make(map[pgtype.UUID]bool, len(sources)+1)
	for _, unit := range units {
		if orgscope.Leads(units, access.UserID, unit.ID) {
			allowed[unit.ID] = true
		}
	}
	subtreeCache := map[pgtype.UUID]map[pgtype.UUID]bool{}
	for _, source := range sources {
		if !source.SourceID.Valid {
			continue
		}
		switch source.SourceType {
		case "user":
			if source.SourceID == access.UserID {
				allowed[source.OrgUnitID] = true
			}
		case "org_unit":
			covered := subtreeCache[source.SourceID]
			if covered == nil {
				covered = orgscope.Subtree(units, source.SourceID)
				subtreeCache[source.SourceID] = covered
			}
			if access.MemberUnit.Valid && covered[access.MemberUnit] ||
				orgscope.Leads(units, access.UserID, source.SourceID) {
				allowed[source.OrgUnitID] = true
			}
		}
	}
	return allowed
}

func canRequestToUnit(access workspaceaccess.Access, units []db.ListOrgUnitsRow, sources []db.ListOrgUnitIntakeSourcesRow, unitID pgtype.UUID) bool {
	allowed := requestableUnits(access, units, sources)
	return allowed == nil || allowed[unitID]
}

func isWorkspaceAdmin(role string) bool {
	return role == "owner" || role == "admin"
}

// ensureRunnableLeader keeps the department squad pointed at an agent that can
// take work right now. The leader's runtime is what picks up the root issue, so
// a leader whose machine is gone would leave every dispatch queued with nobody
// to answer. When the current leader cannot run, the squad moves to the first
// available agent in the department and the old leader stays a squad member.
func (s *OrgDispatchService) ensureRunnableLeader(
	ctx context.Context,
	wsUUID, actorID pgtype.UUID,
	unit db.ListOrgUnitsRow,
	units []db.ListOrgUnitsRow,
	agents []db.ListAgentsInOrgUnitsRow,
	squad db.Squad,
) (db.Squad, error) {
	state, err := s.leaderPickState(ctx, squad.LeaderID, actorID, wsUUID)
	if err != nil {
		return db.Squad{}, err
	}
	if state == pickReady {
		return squad, nil
	}

	replacement, err := s.pickRunnableLeader(ctx, wsUUID, agents, units, unit, actorID)
	if err != nil {
		return db.Squad{}, err
	}
	if replacement == squad.LeaderID {
		return squad, nil
	}
	previous := squad.LeaderID
	updated, err := s.Queries.UpdateSquad(ctx, db.UpdateSquadParams{
		ID:       squad.ID,
		LeaderID: replacement,
	})
	if err != nil {
		return db.Squad{}, fmt.Errorf("switch department squad leader: %w", err)
	}
	// The previous leader keeps working as an ordinary member; the roster sync
	// drops the reserved leader role and adds the new leader.
	if _, err := orgsquad.SyncMembers(ctx, s.Queries, units, updated, unit.ID, previous); err != nil {
		return db.Squad{}, fmt.Errorf("sync department squad after leader switch: %w", err)
	}
	slog.Info("dispatch moved the squad to an available leader",
		"workspace_id", util.UUIDToString(wsUUID),
		"unit_id", util.UUIDToString(unit.ID),
		"squad_id", util.UUIDToString(squad.ID),
		"previous_leader_id", util.UUIDToString(previous),
		"leader_id", util.UUIDToString(replacement))
	return updated, nil
}

// leaderPickState is why an agent can or cannot lead the requester's dispatch.
// Not-invokable is kept apart from not-ready so the caller can answer "share the
// agent with me" instead of "nothing is online".
type leaderPickState int

const (
	pickReady leaderPickState = iota
	pickNotInvokable
	pickNotReady
)

// leaderPickState reports whether one agent can take work for the requester:
// online with a usable runtime, and invokable by them. Unlike direct assignment,
// dispatch cannot queue behind an offline machine: the requester is waiting on a
// department, not on a laptop.
func (s *OrgDispatchService) leaderPickState(ctx context.Context, agentID, actorID, wsUUID pgtype.UUID) (leaderPickState, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pickNotReady, nil
	}
	if err != nil {
		return pickNotReady, fmt.Errorf("load dispatch leader: %w", err)
	}
	verdict, err := AgentReadiness(ctx, RuntimeLookup{
		Queries: s.Queries,
		Source:  metrics.RuntimeLookupSourceOther,
	}, agent)
	if err != nil {
		return pickNotReady, fmt.Errorf("check dispatch leader runtime: %w", err)
	}
	if !verdict.Ready() {
		return pickNotReady, nil
	}
	if s.Tasks == nil {
		return pickReady, nil
	}
	allowed, err := memberMayCollaborateWithAgent(ctx, s.Queries, agent, actorID, wsUUID)
	if err != nil {
		return pickNotReady, fmt.Errorf("check leader invocation: %w", err)
	}
	if !allowed {
		return pickNotInvokable, nil
	}
	return pickReady, nil
}

// pickRunnableLeader returns the first leader candidate that is online and
// invokable by the requester. No candidates means the department has no agent;
// candidates that all fail readiness mean the department cannot run work now.
func (s *OrgDispatchService) pickRunnableLeader(
	ctx context.Context,
	wsUUID pgtype.UUID,
	agents []db.ListAgentsInOrgUnitsRow,
	units []db.ListOrgUnitsRow,
	unit db.ListOrgUnitsRow,
	actorID pgtype.UUID,
) (pgtype.UUID, error) {
	candidates := leaderCandidates(agents, units, unit)
	if len(candidates) == 0 {
		return pgtype.UUID{}, ErrOrgUnitNoAgents
	}
	sawNotInvokable := false
	for _, id := range candidates {
		state, err := s.leaderPickState(ctx, id, actorID, wsUUID)
		if err != nil {
			return pgtype.UUID{}, err
		}
		switch state {
		case pickReady:
			return id, nil
		case pickNotInvokable:
			sawNotInvokable = true
		}
	}
	// At least one agent is running but this requester cannot call it; that is a
	// permissions answer, not an offline department.
	if sawNotInvokable {
		return pgtype.UUID{}, ErrDispatchLeaderUnavailable
	}
	return pgtype.UUID{}, ErrDispatchLeaderOffline
}

// leaderCandidates orders the agents that could lead a department: the unit
// lead's own agents first, then every agent placed in the subtree.
func leaderCandidates(agents []db.ListAgentsInOrgUnitsRow, units []db.ListOrgUnitsRow, unit db.ListOrgUnitsRow) []pgtype.UUID {
	subtree := orgscope.Subtree(units, unit.ID)
	seen := make(map[pgtype.UUID]bool, len(agents))
	candidates := make([]pgtype.UUID, 0, len(agents))
	add := func(agent db.ListAgentsInOrgUnitsRow) {
		if seen[agent.ID] {
			return
		}
		seen[agent.ID] = true
		candidates = append(candidates, agent.ID)
	}
	if unit.LeadUserID.Valid {
		for _, agent := range agents {
			if agent.OwnerID.Valid && agent.OwnerID == unit.LeadUserID {
				add(agent)
			}
		}
	}
	for _, agent := range agents {
		if subtree[agent.OrgUnitID] {
			add(agent)
		}
	}
	return candidates
}
