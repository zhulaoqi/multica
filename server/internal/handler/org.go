package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type orgUnitResponse struct {
	ID         string `json:"id"`
	ParentID   string `json:"parent_id"`
	Name       string `json:"name"`
	LeadUserID string `json:"lead_user_id"`
	// CanManage marks units the requester may edit: the platform operator, or
	// the lead of this unit or one of its ancestors.
	CanManage   bool   `json:"can_manage"`
	Description string `json:"description"`
	Position    int32  `json:"position"`
}

type orgAgentResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	UnitID string `json:"unit_id"`
	// OwnerID binds the agent to its member: the org view matches an agent to
	// the person whose digital twin it is.
	OwnerID string `json:"owner_id"`
	Can     string `json:"can"`
	Cannot  string `json:"cannot"`
	// Status is the agent's runtime availability (idle, working, offline…).
	// The org view reads it to show idle agents as waiting for scheduling.
	Status string `json:"status"`
	// RuntimeMode lets the twin editor keep local/cloud as configured.
	RuntimeMode string `json:"runtime_mode"`
}

// orgSquadResponse is a squad attached to a unit. AgentCount and the working
// count let the client tell "waiting for collaborative scheduling" apart from
// a squad already busy with work; issue counts feed the department board and
// the limits show what the server will let the leader dispatch.
type orgSquadResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	UnitID        string `json:"unit_id"`
	LeaderID      string `json:"leader_id"`
	AgentCount    int32  `json:"agent_count"`
	WorkingAgents int32  `json:"working_agents"`
	WipLimit      int32  `json:"wip_limit"`
	MaxChildren   int32  `json:"max_children"`
	ActiveIssues  int32  `json:"active_issues"`
	ReviewIssues  int32  `json:"review_issues"`
	BlockedIssues int32  `json:"blocked_issues"`
}

type orgMemberResponse struct {
	UserID    string  `json:"user_id"`
	UnitID    string  `json:"unit_id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	// InProject distinguishes a project member from a department colleague the
	// manager may still pull in. Legacy workspaces treat every member row as a
	// project member.
	InProject bool `json:"in_project"`
}

type orgTreeResponse struct {
	Units     []orgUnitResponse   `json:"units"`
	Agents    []orgAgentResponse  `json:"agents"`
	Members   []orgMemberResponse `json:"members"`
	Squads    []orgSquadResponse  `json:"squads"`
	CanManage bool                `json:"can_manage"`
	// MyUnitID is the requester's own unit, so a member who cannot manage
	// anything still lands on their department instead of an empty tree.
	MyUnitID string `json:"my_unit_id"`
}

type orgAgentUnitBody struct {
	UnitID string `json:"unit_id"`
}

type orgUnitBody struct {
	Name       string  `json:"name"`
	ParentID   string  `json:"parent_id"`
	LeadUserID *string `json:"lead_user_id"`
}

type orgImportBody struct {
	Rows []service.OrgImportRow `json:"rows"`
}

type orgImportResponse struct {
	CreatedUnits   int      `json:"created_units"`
	ExistingUnits  int      `json:"existing_units"`
	AgentsAssigned int      `json:"agents_assigned"`
	UnknownAgents  []string `json:"unknown_agents"`
	UnknownLeads   []string `json:"unknown_leads"`
}

// ListOrgUnits returns the flat organisation tree plus every agent that has a
// unit and every member placed in one. Clients assemble the tree; the payload
// stays stable when units move. Only the platform operator receives the whole
// company; every other caller gets the departments they belong to or lead, so
// the org view answers "where am I" without exposing the company directory.
func (h *Handler) ListOrgUnits(w http.ResponseWriter, r *http.Request) {
	access, ok := h.orgAccess(w, r)
	if !ok {
		return
	}
	agents, err := h.Queries.ListAgentsInOrgUnits(r.Context(), access.wsUUID)
	if err != nil {
		slog.Error("list org agents failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list organisation agents")
		return
	}
	members, err := h.Queries.ListOrgUnitMembers(r.Context(), access.wsUUID)
	if err != nil {
		slog.Error("list org members failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list organisation members")
		return
	}
	squads, err := h.Queries.ListSquadsInOrgUnits(r.Context(), access.wsUUID)
	if err != nil {
		slog.Error("list org squads failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list organisation squads")
		return
	}
	resp := orgTreeResponse{
		Units:     make([]orgUnitResponse, 0, len(access.units)),
		Agents:    make([]orgAgentResponse, 0, len(agents)),
		Members:   make([]orgMemberResponse, 0, len(members)),
		Squads:    make([]orgSquadResponse, 0, len(squads)),
		CanManage: access.admin || access.implicitLead,
	}
	var ownUnitID pgtype.UUID
	for _, member := range members {
		if access.owned && !access.unitIDs[member.OrgUnitID] {
			continue
		}
		if member.UserID == access.userID {
			ownUnitID = member.OrgUnitID
			resp.MyUnitID = uuidToString(member.OrgUnitID)
		}
	}
	var visible map[pgtype.UUID]bool
	invitedSquads := map[string]struct{}{}
	if !access.admin && !access.implicitLead {
		visible = orgscope.Visible(access.units, ownUnitID, access.userID, false)
		// A member always sees squads they were added to, even when that squad
		// sits outside the department they belong to.
		joined, err := h.Queries.ListSquadsByMember(r.Context(), db.ListSquadsByMemberParams{
			WorkspaceID: access.wsUUID,
			MemberType:  "member",
			MemberID:    access.userID,
		})
		if err != nil {
			slog.Error("list invited squads failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "could not list organisation squads")
			return
		}
		for _, squad := range joined {
			invitedSquads[uuidToString(squad.ID)] = struct{}{}
		}
	}
	for _, unit := range access.units {
		if visible != nil && !visible[unit.ID] {
			continue
		}
		resp.Units = append(resp.Units, orgUnitResponse{
			ID:          uuidToString(unit.ID),
			ParentID:    uuidToString(unit.ParentID),
			Name:        unit.Name,
			LeadUserID:  uuidToString(unit.LeadUserID),
			CanManage:   access.manages(unit.ID),
			Description: unit.Description,
			Position:    unit.Position,
		})
	}
	for _, agent := range agents {
		if visible != nil && !visible[agent.OrgUnitID] {
			continue
		}
		resp.Agents = append(resp.Agents, orgAgentResponse{
			ID:          uuidToString(agent.ID),
			Name:        agent.Name,
			UnitID:      uuidToString(agent.OrgUnitID),
			OwnerID:     uuidToString(agent.OwnerID),
			Can:         textValue(agent.IntakeCan),
			Cannot:      textValue(agent.IntakeCannot),
			Status:      agent.Status,
			RuntimeMode: agent.RuntimeMode,
		})
	}
	rendered := make(map[pgtype.UUID]bool, len(members))
	for _, member := range members {
		if access.owned && !access.unitIDs[member.OrgUnitID] {
			continue
		}
		if visible != nil && !visible[member.OrgUnitID] {
			continue
		}
		rendered[member.UserID] = true
		resp.Members = append(resp.Members, orgMemberResponse{
			UserID:    uuidToString(member.UserID),
			UnitID:    uuidToString(member.OrgUnitID),
			Name:      member.Name,
			Email:     member.Email,
			AvatarURL: textPtr(member.AvatarUrl),
			InProject: true,
		})
	}
	if access.owned {
		projectIDs, err := h.Queries.ListProjectMemberUserIDs(r.Context(), access.wsUUID)
		if err != nil {
			slog.Error("list project members failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "could not list project members")
			return
		}
		inProject := make(map[pgtype.UUID]bool, len(projectIDs))
		for _, userID := range projectIDs {
			inProject[userID] = true
		}
		merged, err := h.listProjectDirectoryMembers(w, r, access, visible, rendered, inProject)
		if err != nil {
			return
		}
		resp.Members = append(resp.Members, merged...)
		sort.SliceStable(resp.Members, func(i, j int) bool {
			return resp.Members[i].Name < resp.Members[j].Name
		})
	}
	for _, squad := range squads {
		_, invited := invitedSquads[uuidToString(squad.ID)]
		if visible != nil && !visible[squad.OrgUnitID] && !invited {
			continue
		}
		resp.Squads = append(resp.Squads, orgSquadResponse{
			ID:            uuidToString(squad.ID),
			Name:          squad.Name,
			UnitID:        uuidToString(squad.OrgUnitID),
			LeaderID:      uuidToString(squad.LeaderID),
			AgentCount:    squad.AgentCount,
			WorkingAgents: squad.WorkingAgents,
			WipLimit:      squad.WipLimit,
			MaxChildren:   squad.MaxChildren,
			ActiveIssues:  squad.ActiveIssues,
			ReviewIssues:  squad.ReviewIssues,
			BlockedIssues: squad.BlockedIssues,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// listProjectDirectoryMembers merges the department directory into an owned
// project's roster. Directory rows grant no access; in_project marks the people
// a manager can still pull in versus those already working in the project.
func (h *Handler) listProjectDirectoryMembers(
	w http.ResponseWriter,
	r *http.Request,
	access orgAccess,
	visible map[pgtype.UUID]bool,
	rendered map[pgtype.UUID]bool,
	inProject map[pgtype.UUID]bool,
) ([]orgMemberResponse, error) {
	unitIDs := make([]pgtype.UUID, 0, len(access.unitIDs))
	for id := range access.unitIDs {
		unitIDs = append(unitIDs, id)
	}
	rows, err := h.Queries.ListDirectoryMembersInUnits(r.Context(), db.ListDirectoryMembersInUnitsParams{
		WorkspaceID: access.wsUUID,
		UnitIds:     unitIDs,
	})
	if err != nil {
		slog.Error("list directory members failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list the department directory")
		return nil, err
	}
	out := make([]orgMemberResponse, 0, len(rows))
	for _, row := range rows {
		if rendered[row.UserID] {
			continue
		}
		if visible != nil && !visible[row.OrgUnitID] {
			continue
		}
		out = append(out, orgMemberResponse{
			UserID:    uuidToString(row.UserID),
			UnitID:    uuidToString(row.OrgUnitID),
			Name:      row.Name,
			Email:     row.Email,
			AvatarURL: textPtr(row.AvatarUrl),
			InProject: inProject[row.UserID],
		})
	}
	return out, nil
}

// ImportOrgUnits upserts units from row paths, then assigns leads and agents.
// The whole import is one transaction: a partially applied tree would leave
// agents pointing at units the operator did not ask for.
func (h *Handler) ImportOrgUnits(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireOperator(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	var body orgImportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	units, agents, err := service.PlanOrgImport(body.Rows)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start organisation import")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	result := orgImportResponse{UnknownAgents: []string{}, UnknownLeads: []string{}}
	resolved := map[string]pgtype.UUID{}
	for _, unit := range units {
		unitID, ok := h.upsertOrgUnitPath(r, w, qtx, wsUUID, unit.Path, resolved, &result)
		if !ok {
			return
		}
		if unit.LeadEmail == "" {
			continue
		}
		leadID, err := qtx.FindWorkspaceMemberByEmail(r.Context(), db.FindWorkspaceMemberByEmailParams{
			WorkspaceID: wsUUID,
			Lower:       strings.ToLower(unit.LeadEmail),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			result.UnknownLeads = append(result.UnknownLeads, unit.LeadEmail)
			continue
		}
		if err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		if err := qtx.SetOrgUnitLead(r.Context(), db.SetOrgUnitLeadParams{
			ID:          unitID,
			WorkspaceID: wsUUID,
			LeadUserID:  leadID,
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
	}
	for name, path := range agents {
		agentID, err := qtx.FindAgentByName(r.Context(), db.FindAgentByNameParams{
			WorkspaceID: wsUUID,
			Name:        name,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			result.UnknownAgents = append(result.UnknownAgents, name)
			continue
		}
		if err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		if err := qtx.SetAgentOrgUnit(r.Context(), db.SetAgentOrgUnitParams{
			ID:          agentID,
			WorkspaceID: wsUUID,
			OrgUnitID:   resolved[orgPathKey(path)],
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		result.AgentsAssigned++
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// upsertOrgUnitPath ensures every segment of path exists, returning the leaf.
// resolved carries ids across rows so a shared parent is looked up once.
func (h *Handler) upsertOrgUnitPath(
	r *http.Request,
	w http.ResponseWriter,
	qtx *db.Queries,
	wsUUID pgtype.UUID,
	path []string,
	resolved map[string]pgtype.UUID,
	result *orgImportResponse,
) (pgtype.UUID, bool) {
	parent := pgtype.UUID{}
	var leafID pgtype.UUID
	for depth := range path {
		key := orgPathKey(path[:depth+1])
		if id, ok := resolved[key]; ok {
			parent = id
			leafID = id
			continue
		}
		id, err := qtx.GetOrgUnitByParentAndName(r.Context(), db.GetOrgUnitByParentAndNameParams{
			WorkspaceID: wsUUID,
			ParentID:    parent,
			Name:        path[depth],
		})
		switch {
		case err == nil:
			result.ExistingUnits++
		case errors.Is(err, pgx.ErrNoRows):
			id, err = qtx.CreateOrgUnit(r.Context(), db.CreateOrgUnitParams{
				WorkspaceID: wsUUID,
				ParentID:    parent,
				Name:        path[depth],
			})
			if err != nil {
				h.importOrgFailed(w, r, err)
				return pgtype.UUID{}, false
			}
			result.CreatedUnits++
		default:
			h.importOrgFailed(w, r, err)
			return pgtype.UUID{}, false
		}
		resolved[key] = id
		parent = id
		leafID = id
	}
	return leafID, true
}

// CreateOrgUnit adds one unit, optionally under a parent and with a lead.
// Workspace managers may create roots; a unit lead may add children under a
// unit they manage.
func (h *Handler) CreateOrgUnit(w http.ResponseWriter, r *http.Request) {
	access, ok := h.orgAccess(w, r)
	if !ok {
		return
	}
	if !access.managesAny() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var body orgUnitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, ok := h.orgUnitName(w, body.Name)
	if !ok {
		return
	}
	parent := pgtype.UUID{}
	if body.ParentID != "" {
		parent, ok = h.orgUnitRef(w, r, access.wsUUID, body.ParentID)
		if !ok {
			return
		}
	}
	if !access.manages(parent) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	lead := pgtype.UUID{}
	if body.LeadUserID != nil && *body.LeadUserID != "" {
		lead, ok = h.orgLeadRef(w, r, access, *body.LeadUserID)
		if !ok {
			return
		}
	}
	id, err := h.Queries.CreateOrgUnit(r.Context(), db.CreateOrgUnitParams{
		WorkspaceID: access.wsUUID,
		ParentID:    parent,
		Name:        name,
		LeadUserID:  lead,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "同级的部门已有同名")
			return
		}
		h.importOrgFailed(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, http.StatusCreated, map[string]string{"id": uuidToString(id)})
}

// UpdateOrgUnit renames a unit and/or sets its lead; an empty lead clears it.
// The lead of a unit or one of its ancestors may edit it.
func (h *Handler) UpdateOrgUnit(w http.ResponseWriter, r *http.Request) {
	access, ok := h.orgAccess(w, r)
	if !ok {
		return
	}
	if !access.managesAny() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	wsUUID := access.wsUUID
	unitID, ok := h.orgUnitRef(w, r, wsUUID, chi.URLParam(r, "unitId"))
	if !ok {
		return
	}
	if !access.manages(unitID) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var body orgUnitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Name) == "" && body.LeadUserID == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}
	if strings.TrimSpace(body.Name) != "" {
		name, ok := h.orgUnitName(w, body.Name)
		if !ok {
			return
		}
		if err := h.Queries.RenameOrgUnit(r.Context(), db.RenameOrgUnitParams{
			ID:          unitID,
			WorkspaceID: wsUUID,
			Name:        name,
		}); err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "同级的部门已有同名")
				return
			}
			h.importOrgFailed(w, r, err)
			return
		}
	}
	if body.LeadUserID != nil {
		lead := pgtype.UUID{}
		if *body.LeadUserID != "" {
			lead, ok = h.orgLeadRef(w, r, access, *body.LeadUserID)
			if !ok {
				return
			}
		}
		if err := h.Queries.SetOrgUnitLead(r.Context(), db.SetOrgUnitLeadParams{
			ID:          unitID,
			WorkspaceID: wsUUID,
			LeadUserID:  lead,
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		// The lead of the owning department is always a project owner; that is
		// how creation, backfill and the operator's department change arrange
		// it. A previous owner is not demoted.
		if access.rootUnitID.Valid && unitID == access.rootUnitID && lead.Valid {
			if err := h.ensureProjectOwner(r.Context(), h.Queries, wsUUID, lead, access.rootUnitID); err != nil {
				h.importOrgFailed(w, r, err)
				return
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteOrgUnit removes a leaf unit: agents in it become unassigned, while
// units below it and members placed in it must be moved or deleted first.
func (h *Handler) DeleteOrgUnit(w http.ResponseWriter, r *http.Request) {
	access, ok := h.orgAccess(w, r)
	if !ok {
		return
	}
	if !access.managesAny() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	wsUUID := access.wsUUID
	unitID, ok := h.orgUnitRef(w, r, wsUUID, chi.URLParam(r, "unitId"))
	if !ok {
		return
	}
	if !access.manages(unitID) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if access.rootUnitID.Valid && unitID == access.rootUnitID {
		writeError(w, http.StatusConflict, "项目所属部门不能被删除")
		return
	}
	children, err := h.Queries.CountOrgUnitChildren(r.Context(), db.CountOrgUnitChildrenParams{
		WorkspaceID: wsUUID,
		ParentID:    unitID,
	})
	if err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	if children > 0 {
		writeError(w, http.StatusConflict, "先移走或删除下级部门")
		return
	}
	members, err := h.Queries.CountOrgUnitMembers(r.Context(), db.CountOrgUnitMembersParams{
		WorkspaceID: wsUUID,
		OrgUnitID:   unitID,
	})
	if err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	if members > 0 {
		writeError(w, http.StatusConflict, "先移走这个部门里的成员")
		return
	}
	directoryMembers, err := h.Queries.CountDirectoryMembersInUnit(r.Context(), db.CountDirectoryMembersInUnitParams{
		WorkspaceID: wsUUID,
		OrgUnitID:   unitID,
	})
	if err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	if directoryMembers > 0 {
		writeError(w, http.StatusConflict, "先移走这个部门目录里的成员")
		return
	}
	if err := h.Queries.ClearOrgUnitAgents(r.Context(), db.ClearOrgUnitAgentsParams{
		WorkspaceID: wsUUID,
		OrgUnitID:   unitID,
	}); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	if err := h.Queries.ClearOrgUnitSquads(r.Context(), db.ClearOrgUnitSquadsParams{
		WorkspaceID: wsUUID,
		OrgUnitID:   unitID,
	}); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	if err := h.Queries.DeleteOrgUnit(r.Context(), db.DeleteOrgUnitParams{
		ID:          unitID,
		WorkspaceID: wsUUID,
	}); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// orgManageWorkspace gates unit editing to workspace owners and admins.
func (h *Handler) orgManageWorkspace(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return pgtype.UUID{}, false
	}
	return parseUUIDOrBadRequest(w, workspaceID, "workspace id")
}

func (h *Handler) orgUnitName(w http.ResponseWriter, raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return "", false
	}
	if len([]rune(name)) > service.OrgUnitNameMaxRunes {
		writeError(w, http.StatusBadRequest, "name is too long")
		return "", false
	}
	return name, true
}

func (h *Handler) orgUnitRef(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, raw string) (pgtype.UUID, bool) {
	id, ok := parseUUIDOrBadRequest(w, raw, "unit id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetOrgUnit(r.Context(), db.GetOrgUnitParams{ID: id, WorkspaceID: wsUUID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "organisation unit not found")
			return pgtype.UUID{}, false
		}
		h.importOrgFailed(w, r, err)
		return pgtype.UUID{}, false
	}
	return id, true
}

// ensureProjectOwner promotes the owning department's lead to project owner.
// The lead may not have a member row yet (the operator changed departments or
// picked a new lead); the row is created at the department root so the project
// always has someone who can administer it.
func (h *Handler) ensureProjectOwner(ctx context.Context, q *db.Queries, workspaceID, userID, rootUnitID pgtype.UUID) error {
	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_, err := q.CreateMember(ctx, db.CreateMemberParams{
			WorkspaceID: workspaceID,
			UserID:      userID,
			Role:        "owner",
			OrgUnitID:   rootUnitID,
		})
		return err
	case err != nil:
		return err
	case member.Role == "owner":
		return nil
	default:
		_, err := q.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{ID: member.ID, Role: "owner"})
		return err
	}
}

// orgLeadRef accepts a project member, or for a department project a colleague
// from its directory: the directory describes the department, and a manager
// may name a lead before pulling them into the project.
func (h *Handler) orgLeadRef(w http.ResponseWriter, r *http.Request, access orgAccess, raw string) (pgtype.UUID, bool) {
	userID, ok := parseUUIDOrBadRequest(w, raw, "lead user id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: access.wsUUID,
	}); err == nil {
		return userID, true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		h.importOrgFailed(w, r, err)
		return pgtype.UUID{}, false
	}
	if access.owned {
		row, err := h.Queries.GetDirectoryMember(r.Context(), db.GetDirectoryMemberParams{
			WorkspaceID: access.wsUUID,
			UserID:      userID,
		})
		switch {
		case err == nil && access.unitIDs[row.OrgUnitID]:
			return userID, true
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			h.importOrgFailed(w, r, err)
			return pgtype.UUID{}, false
		}
	}
	writeError(w, http.StatusBadRequest, "lead is not a member of this workspace")
	return pgtype.UUID{}, false
}

// SetOrgAgentUnit places an agent in a unit, or clears its unit when unit_id
// is empty. Agents stay in the workspace either way. Placing an agent needs
// manage rights on the target unit; removing it needs manage rights on the
// unit it currently sits in.
func (h *Handler) SetOrgAgentUnit(w http.ResponseWriter, r *http.Request) {
	h.setOrgEntityUnit(w, r, chi.URLParam(r, "agentId"), "agent")
}

// SetOrgMemberUnit places a workspace member in a unit, or clears their unit
// when unit_id is empty, with the same manage-rights split as agents.
func (h *Handler) SetOrgMemberUnit(w http.ResponseWriter, r *http.Request) {
	h.setOrgEntityUnit(w, r, chi.URLParam(r, "userId"), "member")
}

// SetOrgSquadUnit attaches a squad to a unit, or clears the attachment when
// unit_id is empty, using the same manage-rights split as agents.
func (h *Handler) SetOrgSquadUnit(w http.ResponseWriter, r *http.Request) {
	h.setOrgEntityUnit(w, r, chi.URLParam(r, "squadId"), "squad")
}

func (h *Handler) setOrgEntityUnit(w http.ResponseWriter, r *http.Request, entityID string, entityType string) {
	access, ok := h.orgAccess(w, r)
	if !ok {
		return
	}
	// Agent placement also allows a member to file their own agent into their
	// own department (the twin registration flow), so that case is decided
	// after the entity is resolved.
	if entityType != "agent" && !access.managesAny() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var body orgAgentUnitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entityUUID, ok := parseUUIDOrBadRequest(w, entityID, entityType+" id")
	if !ok {
		return
	}
	var currentUnit pgtype.UUID
	var ownerID pgtype.UUID
	switch entityType {
	case "agent":
		agent, err := h.Queries.GetAgent(r.Context(), entityUUID)
		if err != nil || agent.WorkspaceID != access.wsUUID {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		currentUnit = agent.OrgUnitID
		ownerID = agent.OwnerID
	case "member":
		member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      entityUUID,
			WorkspaceID: access.wsUUID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "member not found")
				return
			}
			h.importOrgFailed(w, r, err)
			return
		}
		currentUnit = member.OrgUnitID
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
			ID:          entityUUID,
			WorkspaceID: access.wsUUID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "squad not found")
				return
			}
			h.importOrgFailed(w, r, err)
			return
		}
		currentUnit = squad.OrgUnitID
	}
	unitUUID := pgtype.UUID{}
	if body.UnitID != "" {
		unitUUID, ok = parseUUIDOrBadRequest(w, body.UnitID, "unit id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetOrgUnit(r.Context(), db.GetOrgUnitParams{
			ID:          unitUUID,
			WorkspaceID: access.wsUUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "organisation unit not found")
				return
			}
			h.importOrgFailed(w, r, err)
			return
		}
		if !access.manages(unitUUID) && !access.ownsAgentPlacement(entityType, ownerID, unitUUID) {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	} else if !access.manages(currentUnit) && !access.ownsAgentPlacement(entityType, ownerID, currentUnit) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	switch entityType {
	case "member":
		if err := h.Queries.SetMemberOrgUnit(r.Context(), db.SetMemberOrgUnitParams{
			WorkspaceID: access.wsUUID,
			UserID:      entityUUID,
			OrgUnitID:   unitUUID,
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		// The directory is the department-wide placement record; a manual move
		// in an owned project updates it so the next sync and the candidate
		// picker agree with the tree.
		if access.owned {
			if _, err := h.Queries.UpsertDirectoryMember(r.Context(), db.UpsertDirectoryMemberParams{
				WorkspaceID: access.wsUUID,
				UserID:      entityUUID,
				OrgUnitID:   unitUUID,
			}); err != nil {
				h.importOrgFailed(w, r, err)
				return
			}
		}
	case "squad":
		if err := h.Queries.SetSquadOrgUnit(r.Context(), db.SetSquadOrgUnitParams{
			ID:          entityUUID,
			WorkspaceID: access.wsUUID,
			OrgUnitID:   unitUUID,
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
	default:
		if err := h.Queries.SetAgentOrgUnit(r.Context(), db.SetAgentOrgUnitParams{
			ID:          entityUUID,
			WorkspaceID: access.wsUUID,
			OrgUnitID:   unitUUID,
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
	}
	// Department squads mirror who their unit subtree covers; a member or
	// agent moving in or out changes the roster of this unit and every
	// ancestor squad.
	if entityType != "squad" {
		h.syncOrgSquadsAfterPlacement(r.Context(), access.wsUUID, access.units, currentUnit, unitUUID)
	}
	// Clearing a member's placement moves them out of the project subtree;
	// their member row stays so a lead can place them again.
	if entityType == "member" && access.owned && !access.unitIDs[unitUUID] {
		h.publishAccessRevocations(access.wsUUID, requestUserID(r), h.disconnectMembersOutsideProject(r.Context(), access.wsUUID))
	}
	w.WriteHeader(http.StatusNoContent)
}

// orgAccess carries everything org mutations need: the resolved workspace, the
// caller and their units, so permission decisions stay consistent across the
// org handlers without repeating the load.
type orgAccess struct {
	wsUUID pgtype.UUID
	userID pgtype.UUID
	admin  bool
	// owned is true when the workspace is a department project. units then
	// holds only the owning department's subtree; unitIDs is its id set.
	owned      bool
	rootUnitID pgtype.UUID
	unitIDs    map[pgtype.UUID]bool
	units      []db.ListOrgUnitsRow
	// ownUnitID is the caller's own department, if they are placed in one.
	ownUnitID pgtype.UUID
	// implicitLead marks a lead of a department above the owning department.
	// They read and manage the whole project subtree, the units their own
	// lead position covers, without a member row.
	implicitLead bool
}

func (h *Handler) orgAccess(w http.ResponseWriter, r *http.Request) (orgAccess, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return orgAccess{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return orgAccess{}, false
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return orgAccess{}, false
	}
	var units []db.ListOrgUnitsRow
	if ws.OrgUnitID.Valid {
		units, err = workspaceaccess.SubtreeUnits(r.Context(), h.Queries, wsUUID)
	} else {
		units, err = h.Queries.ListOrgUnits(r.Context(), wsUUID)
	}
	if err != nil {
		slog.Error("list org units failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list organisation units")
		return orgAccess{}, false
	}
	access, accessErr := h.workspaceAccess(r, uuidToString(member.UserID), workspaceID)
	return orgAccess{
		wsUUID: wsUUID,
		userID: member.UserID,
		// Directory editing stays with the platform operator and department
		// leads. A project owner who does not lead a unit manages members, not
		// the company tree.
		admin:        requestIsOperator(r),
		owned:        ws.OrgUnitID.Valid,
		rootUnitID:   ws.OrgUnitID,
		unitIDs:      workspaceaccess.UnitIDs(units),
		units:        units,
		ownUnitID:    member.OrgUnitID,
		implicitLead: accessErr == nil && access.Implicit(),
	}, true
}

// manages reports that the caller may administer the unit: the platform
// operator may, and so may the lead of the unit itself or one of its ancestors.
func (a orgAccess) manages(unitID pgtype.UUID) bool {
	if a.admin || a.implicitLead {
		return true
	}
	return orgscope.Leads(a.units, a.userID, unitID)
}

// managesAny reports whether the caller may administer at least one unit.
// Handlers gate on it before resolving a unit so callers without any rights get
// a stable 403 instead of a 404 that leaks whether the unit exists.
func (a orgAccess) managesAny() bool {
	if a.admin || a.implicitLead {
		return true
	}
	return orgscope.LeadsAny(a.units, a.userID)
}

// ownsAgentPlacement lets a member place their own agent into their own
// department without org-management rights: registering a digital twin must
// not require being a department lead.
func (a orgAccess) ownsAgentPlacement(entityType string, ownerID pgtype.UUID, unitID pgtype.UUID) bool {
	return entityType == "agent" &&
		ownerID.Valid && ownerID == a.userID &&
		unitID.Valid && unitID == a.ownUnitID
}

func (h *Handler) importOrgFailed(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("import org units failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "could not import organisation units")
}

func orgPathKey(path []string) string {
	return strings.Join(path, "\x00")
}

func textValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid || t.String == "" {
		return nil
	}
	return &t.String
}
