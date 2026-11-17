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
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/orgsquad"
	"github.com/multica-ai/multica/server/internal/util"
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

	// dispatchMatchLimit bounds the candidate list a chat reply can render.
	dispatchMatchLimit = 5
)

var (
	ErrOrgUnitNotFound            = errors.New("org unit not found")
	ErrOrgUnitNoAccess            = errors.New("no access to this org unit")
	ErrOrgUnitNoAgents            = errors.New("org unit has no agent to lead the dispatch")
	ErrDispatchLeaderUnavailable  = errors.New("squad leader is not available to this requester")
	ErrDispatchRequesterNotInTeam = errors.New("requester is not a member of this workspace")
)

// DispatchOriginInfo carries the transport coordinates a dispatch came from.
// App dispatches leave them empty; a chat request keeps them so the completion
// notice can return to the same conversation.
type DispatchOriginInfo struct {
	ChannelType    string
	InstallationID pgtype.UUID
	ChatID         string
	ThreadID       string
}

type OrgDispatchParams struct {
	WorkspaceID pgtype.UUID
	ActorID     pgtype.UUID
	UnitID      pgtype.UUID
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
	member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      p.ActorID,
		WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return OrgDispatchResult{}, ErrDispatchRequesterNotInTeam
	}
	units, err := s.Queries.ListOrgUnits(ctx, p.WorkspaceID)
	if err != nil {
		return OrgDispatchResult{}, fmt.Errorf("list org units: %w", err)
	}
	unit, ok := findUnit(units, p.UnitID)
	if !ok {
		return OrgDispatchResult{}, ErrOrgUnitNotFound
	}
	if !canDispatchToUnit(member, units, unit.ID) {
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
	if s.Tasks != nil {
		allowed, err := s.Tasks.MemberMayInvokeAgent(ctx, squad.LeaderID, p.ActorID)
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
	}, IssueCreateOpts{Platform: p.Platform})
	if err != nil {
		return OrgDispatchResult{}, err
	}

	if err := s.recordOrigin(ctx, created.Issue, unit, p); err != nil {
		return OrgDispatchResult{}, err
	}
	return OrgDispatchResult{Issue: created.Issue, Squad: squad, Unit: unit}, nil
}

func (s *OrgDispatchService) recordOrigin(ctx context.Context, issue db.Issue, unit db.ListOrgUnitsRow, p OrgDispatchParams) error {
	channel := p.Origin.ChannelType
	if channel == "" {
		channel = DispatchChannelApp
	}
	_, err := s.Queries.CreateDispatchOrigin(ctx, db.CreateDispatchOriginParams{
		IssueID:         issue.ID,
		WorkspaceID:     p.WorkspaceID,
		RequesterID:     p.ActorID,
		TargetUnitID:    unit.ID,
		ChannelType:     channel,
		InstallationID:  p.Origin.InstallationID,
		ChatID:          pgtype.Text{String: p.Origin.ChatID, Valid: p.Origin.ChatID != ""},
		ThreadID:        pgtype.Text{String: p.Origin.ThreadID, Valid: p.Origin.ThreadID != ""},
		ClientRequestID: p.ClientRequestID,
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
	leaderID, ok := pickDispatchLeader(agents, units, unit)
	if !ok {
		return db.Squad{}, ErrOrgUnitNoAgents
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
	member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      actorID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return nil, ErrDispatchRequesterNotInTeam
	}
	units, err := s.Queries.ListOrgUnits(ctx, wsUUID)
	if err != nil {
		return nil, fmt.Errorf("list org units: %w", err)
	}
	visible := orgscope.Visible(units, member.OrgUnitID, member.UserID, isWorkspaceAdmin(member.Role))
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
	details, _ := json.Marshal(map[string]any{
		"dispatch_unit_id":   util.UUIDToString(notice.Origin.TargetUnitID),
		"dispatch_unit_name": notice.UnitName,
		"identifier":         notice.Identifier,
		"status":             notice.Issue.Status,
	})
	body := fmt.Sprintf("%s delivered your dispatch to %s.", notice.Identifier, notice.UnitName)
	if notice.UnitName == "" {
		body = fmt.Sprintf("%s delivered your dispatch.", notice.Identifier)
	}
	item, inserted, err := inbox.CreateIfEligible(ctx, s.Queries, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   notice.Origin.WorkspaceID,
		RecipientType: "member",
		RecipientID:   notice.Origin.RequesterID,
		Type:          inboxTypeDispatchCompleted,
		Severity:      "info",
		IssueID:       notice.Issue.ID,
		Title:         notice.Issue.Title,
		Body:          pgtype.Text{String: body, Valid: true},
		ActorType:     pgtype.Text{String: "system", Valid: true},
		Details:       details,
	})
	if err != nil {
		return fmt.Errorf("write dispatch inbox item: %w", err)
	}
	if !inserted || s.Bus == nil {
		return nil
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: util.UUIDToString(item.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{"item": map[string]any{
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
			"issue_status":   notice.Issue.Status,
		}},
	})
	return nil
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

func (s *OrgDispatchService) issueIdentifier(ctx context.Context, issue db.Issue) string {
	ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return IssueIdentifier("", issue.Number)
	}
	return IssueIdentifier(ws.IssuePrefix, issue.Number)
}

func findUnit(units []db.ListOrgUnitsRow, unitID pgtype.UUID) (db.ListOrgUnitsRow, bool) {
	for _, unit := range units {
		if unit.ID == unitID {
			return unit, true
		}
	}
	return db.ListOrgUnitsRow{}, false
}

// canDispatchToUnit mirrors the org page's visibility: managers dispatch
// anywhere, everyone else only to their own department subtree and to the
// departments they lead.
func canDispatchToUnit(member db.Member, units []db.ListOrgUnitsRow, unitID pgtype.UUID) bool {
	if isWorkspaceAdmin(member.Role) {
		return true
	}
	visible := orgscope.Visible(units, member.OrgUnitID, member.UserID, false)
	return visible[unitID]
}

func isWorkspaceAdmin(role string) bool {
	return role == "owner" || role == "admin"
}

// pickDispatchLeader prefers the agent owned by the unit lead, then any agent
// covered by the department subtree. Archived agents never lead.
func pickDispatchLeader(agents []db.ListAgentsInOrgUnitsRow, units []db.ListOrgUnitsRow, unit db.ListOrgUnitsRow) (pgtype.UUID, bool) {
	subtree := orgscope.Subtree(units, unit.ID)
	if unit.LeadUserID.Valid {
		for _, agent := range agents {
			if agent.OwnerID.Valid && agent.OwnerID == unit.LeadUserID {
				return agent.ID, true
			}
		}
	}
	for _, agent := range agents {
		if subtree[agent.OrgUnitID] {
			return agent.ID, true
		}
	}
	return pgtype.UUID{}, false
}
