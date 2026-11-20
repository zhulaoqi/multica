package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A department-owned project may only be read by members placed in the owning
// department's subtree. These helpers keep every department write path on the
// same rules.

const (
	codeDepartmentRequired    = "department_required"
	codeDepartmentForbidden   = "department_forbidden"
	codeMemberOutsideDept     = "member_outside_department"
	codeMemberNotInDirectory  = "member_not_in_directory"
	codeDirectAddOnly         = "direct_add_only"
	codeLastOwnerRequired     = "last_owner_required"
	codeOrgLeadCannotBeRemove = "org_lead_cannot_be_removed"
)

// DepartmentOption is one department a caller may create a project for.
type DepartmentOption struct {
	SourceWorkspaceID   string `json:"source_workspace_id"`
	SourceWorkspaceName string `json:"source_workspace_name"`
	UnitID              string `json:"unit_id"`
	Path                string `json:"path"`
}

type WorkspaceCreationOptionsResponse struct {
	CanCreate           bool               `json:"can_create"`
	CanCreateUnassigned bool               `json:"can_create_unassigned"`
	Departments         []DepartmentOption `json:"departments"`
}

// ListWorkspaceCreateOptions answers what the create-project screen may offer:
// departments of the organization tree the caller leads, or every department
// for the platform operator.
func (h *Handler) ListWorkspaceCreateOptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	resp := WorkspaceCreationOptionsResponse{Departments: []DepartmentOption{}}
	if !h.canCreateWorkspaces(r, userID) {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	operator := requestIsOperator(r)
	resp.CanCreateUnassigned = operator
	org, found, err := h.organizationWorkspace(r.Context())
	if err != nil {
		slog.Error("load organization workspace failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list departments")
		return
	}
	if !found {
		resp.CanCreate = operator
		writeJSON(w, http.StatusOK, resp)
		return
	}
	units, err := h.Queries.ListOrgUnits(r.Context(), org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list departments")
		return
	}
	userUUID := parseUUID(userID)
	paths := orgscope.Paths(units)
	for _, unit := range units {
		if !operator && !orgscope.Leads(units, userUUID, unit.ID) {
			continue
		}
		resp.Departments = append(resp.Departments, DepartmentOption{
			SourceWorkspaceID:   uuidToString(org.ID),
			SourceWorkspaceName: org.Name,
			UnitID:              uuidToString(unit.ID),
			Path:                paths[unit.ID],
		})
	}
	resp.CanCreate = operator || len(resp.Departments) > 0
	writeJSON(w, http.StatusOK, resp)
}

// canCreateWorkspaces mirrors the instance gates in CreateWorkspace.
func (h *Handler) canCreateWorkspaces(r *http.Request, userID string) bool {
	if h.cfg.DisableWorkspaceCreation {
		return false
	}
	if len(h.cfg.WorkspaceCreationAllowedEmails) == 0 {
		return true
	}
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		return false
	}
	return contains(h.cfg.WorkspaceCreationAllowedEmails, strings.TrimSpace(user.Email))
}

// createDepartment is the organization department a new project belongs to.
type createDepartment struct {
	unit db.ListOrgUnitsRow
}

// resolveCreateDepartment validates the requested department against the
// organization tree. It writes the HTTP error and returns ok=false on failure.
// A nil result means "create a legacy workspace without a department", which
// only the operator may do. sourceID is accepted for older clients and must
// name the organization workspace when given.
func (h *Handler) resolveCreateDepartment(w http.ResponseWriter, r *http.Request, userID, sourceID, unitID string) (*createDepartment, bool) {
	operator := requestIsOperator(r)

	if unitID == "" {
		if sourceID != "" {
			writeErrorCode(w, http.StatusBadRequest, codeDepartmentRequired, "org_unit_id is required")
			return nil, false
		}
		if !operator {
			writeErrorCode(w, http.StatusForbidden, codeDepartmentRequired, "a department is required to create a project")
			return nil, false
		}
		return nil, true
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, unitID, "org_unit_id")
	if !ok {
		return nil, false
	}
	org, ok := h.requireOrganizationWorkspace(w, r)
	if !ok {
		return nil, false
	}
	if sourceID != "" && sourceID != uuidToString(org.ID) {
		writeErrorCode(w, http.StatusBadRequest, codeDepartmentForbidden, "departments come from the organization workspace")
		return nil, false
	}
	units, err := h.Queries.ListOrgUnits(r.Context(), org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load departments")
		return nil, false
	}
	var unit db.ListOrgUnitsRow
	found := false
	for _, candidate := range units {
		if candidate.ID == targetUUID {
			unit = candidate
			found = true
			break
		}
	}
	if !found {
		writeErrorCode(w, http.StatusBadRequest, codeDepartmentForbidden, "department does not belong to the organization")
		return nil, false
	}
	if !operator && !orgscope.Leads(units, parseUUID(userID), targetUUID) {
		writeErrorCode(w, http.StatusForbidden, codeDepartmentForbidden, "you do not lead this department")
		return nil, false
	}
	return &createDepartment{unit: unit}, true
}

// createProjectFromDepartment points the new project at its organization
// department and makes the creator and the department lead owners placed at
// the department root.
func (h *Handler) createProjectFromDepartment(ctx context.Context, qtx *db.Queries, ws db.Workspace, dept *createDepartment, creatorID string) (db.Workspace, error) {
	rootUnitID := dept.unit.ID
	updated, err := qtx.UpdateWorkspaceOrgUnit(ctx, db.UpdateWorkspaceOrgUnitParams{
		ID:        ws.ID,
		OrgUnitID: rootUnitID,
	})
	if err != nil {
		return ws, err
	}

	creatorUUID := parseUUID(creatorID)
	if _, err := qtx.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: ws.ID,
		UserID:      creatorUUID,
		Role:        "owner",
		OrgUnitID:   rootUnitID,
	}); err != nil {
		return ws, err
	}

	if dept.unit.LeadUserID.Valid && dept.unit.LeadUserID != creatorUUID {
		// The tree may name a lead who is no longer a user (no FK on
		// lead_user_id). Refuse to create an orphan member row: access must
		// stay visible in the roster.
		if _, err := qtx.GetUser(ctx, dept.unit.LeadUserID); err == nil {
			if _, err := qtx.CreateMember(ctx, db.CreateMemberParams{
				WorkspaceID: ws.ID,
				UserID:      dept.unit.LeadUserID,
				Role:        "owner",
				OrgUnitID:   rootUnitID,
			}); err != nil {
				return ws, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ws, err
		}
	}

	if err := issuestatus.Ensure(ctx, qtx, ws.ID); err != nil {
		return ws, err
	}
	return updated, nil
}

// AddProjectMemberDirectRequest names one organization member to pull in.
type AddProjectMemberDirectRequest struct {
	UserID string `json:"user_id"`
}

// AddProjectMemberDirect is the department-project member path: managers pull
// a colleague from the organization roster instead of sending an email
// invitation.
func (h *Handler) AddProjectMemberDirect(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if !isOrganizationProject(ws) {
		writeErrorCode(w, http.StatusConflict, codeDirectAddOnly, "email invitations are available for this workspace")
		return
	}
	org, ok := h.requireOrganizationWorkspace(w, r)
	if !ok {
		return
	}

	var req AddProjectMemberDirectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.UserID), "user_id")
	if !ok {
		return
	}

	directoryRow, err := h.Queries.GetOrganizationMember(r.Context(), db.GetOrganizationMemberParams{
		OrganizationID: org.ID,
		UserID:         userUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErrorCode(w, http.StatusNotFound, codeMemberNotInDirectory, "user is not in the organization")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load organization member")
		return
	}
	actorUserID := requestUserID(r)

	existing, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userUUID,
		WorkspaceID: wsUUID,
	})
	switch {
	case err == nil:
		// A stale row (the person was moved outside the subtree) is restored
		// to their organization placement instead of failing the pull-in.
		access, accessErr := workspaceaccess.Resolve(r.Context(), h.Queries, wsUUID, userUUID, false)
		if accessErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve membership")
			return
		}
		if access.Allowed() {
			writeError(w, http.StatusConflict, "user is already a member")
			return
		}
		if err := h.Queries.SetMemberOrgUnit(r.Context(), db.SetMemberOrgUnitParams{
			WorkspaceID: wsUUID,
			UserID:      userUUID,
			OrgUnitID:   directoryRow.OrgUnitID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add member")
			return
		}
		updated, err := h.Queries.GetMember(r.Context(), existing.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load member")
			return
		}
		h.MembershipCache.Invalidate(r.Context(), uuidToString(userUUID), workspaceID)
		user, err := h.Queries.GetUser(r.Context(), userUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load member")
			return
		}
		if err := h.ensurePrimaryTwinInWorkspace(r.Context(), wsUUID, userUUID, directoryRow.OrgUnitID); err != nil {
			slog.Warn("clone primary twin for restored member failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		}
		memberResp := h.memberWithUserResponse(updated, user)
		h.publish(protocol.EventMemberAdded, workspaceID, "member", actorUserID, map[string]any{
			"member":         memberResp,
			"workspace_name": ws.Name,
		})
		h.notifyDaemonWorkspacesChanged(uuidToString(userUUID))
		writeJSON(w, http.StatusOK, memberResp)
		return
	case !errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusInternalServerError, "failed to load membership")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), userUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	token := uuid.New()
	capacityActive, err := h.beginCapacityConsume(r.Context(), uuid.UUID(wsUUID.Bytes), token, uuid.Nil, uuid.UUID(userUUID.Bytes))
	if err != nil {
		writeSeatCapacityError(w, err)
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		h.compensateCapacityIntent(r.Context(), token)
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	member, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
		Role:        "member",
		OrgUnitID:   directoryRow.OrgUnitID,
	})
	if err == nil && !user.OnboardedAt.Valid {
		_, err = qtx.MarkUserOnboarded(r.Context(), userUUID)
	}
	if err == nil && capacityActive {
		err = transitionCapacityIntentToConfirm(r.Context(), qtx, token, uuid.UUID(member.ID.Bytes), seatcapacity.ActionConsumeInvitation)
	}
	if err != nil {
		_ = tx.Rollback(r.Context())
		h.compensateCapacityIntent(r.Context(), token)
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "user is already a member")
			return
		}
		slog.Warn("direct add member failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.compensateCapacityIntent(r.Context(), token)
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	if capacityActive {
		h.confirmCapacityIntent(r.Context(), uuid.UUID(wsUUID.Bytes), token, uuid.UUID(member.ID.Bytes))
	}

	h.MembershipCache.Invalidate(r.Context(), uuidToString(userUUID), workspaceID)

	if err := h.ensurePrimaryTwinInWorkspace(r.Context(), wsUUID, userUUID, directoryRow.OrgUnitID); err != nil {
		slog.Warn("clone primary twin for new member failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
	}

	memberResp := h.memberWithUserResponse(member, user)
	h.publish(protocol.EventMemberAdded, workspaceID, "member", actorUserID, map[string]any{
		"member":         memberResp,
		"workspace_name": ws.Name,
	})
	h.notifyDaemonWorkspacesChanged(uuidToString(userUUID))

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.TeamInviteSent(
		actorUserID, workspaceID, user.Email, "direct",
	))
	writeJSON(w, http.StatusCreated, memberResp)
}

// ProjectMemberCandidate is one organization member the project may pull in.
type ProjectMemberCandidate struct {
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	OrgUnitID *string `json:"org_unit_id"`
	Path      string  `json:"path"`
	InProject bool    `json:"in_project"`
}

// ListProjectMemberCandidates searches the organization roster. A project
// manager may pull in a colleague from another department; the person becomes
// an explicit member with access wherever they are placed.
func (h *Handler) ListProjectMemberCandidates(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if !isOrganizationProject(ws) {
		writeErrorCode(w, http.StatusConflict, codeDirectAddOnly, "this workspace adds members by invitation")
		return
	}
	org, ok := h.requireOrganizationWorkspace(w, r)
	if !ok {
		return
	}
	units, err := h.Queries.ListOrgUnits(r.Context(), org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load departments")
		return
	}
	paths := orgscope.Paths(units)
	rows, err := h.Queries.SearchOrganizationMembers(r.Context(), db.SearchOrganizationMembersParams{
		ProjectID:      wsUUID,
		OrganizationID: org.ID,
		Query:          strings.TrimSpace(r.URL.Query().Get("q")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search the employee directory")
		return
	}
	resp := make([]ProjectMemberCandidate, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, ProjectMemberCandidate{
			UserID:    uuidToString(row.UserID),
			Name:      row.Name,
			Email:     row.Email,
			AvatarURL: h.resolveAvatarURLPtr(textToPtr(row.AvatarUrl)),
			OrgUnitID: uuidToPtr(row.OrgUnitID),
			Path:      paths[row.OrgUnitID],
			InProject: row.InProject,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

type SetWorkspaceDepartmentRequest struct {
	OrgUnitID string `json:"org_unit_id"`
}

// SetWorkspaceDepartment is operator-only. It never clears the department:
// a NULL would turn the project back into a legacy flat workspace where every
// historical member row regains access.
func (h *Handler) SetWorkspaceDepartment(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var req SetWorkspaceDepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	unitUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.OrgUnitID), "org_unit_id")
	if !ok {
		return
	}
	target, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if target.IsOrganization {
		writeErrorCode(w, http.StatusConflict, codeOrganizationManaged, "the organization workspace has no owning department")
		return
	}
	org, ok := h.requireOrganizationWorkspace(w, r)
	if !ok {
		return
	}
	if _, err := h.Queries.GetOrgUnit(r.Context(), db.GetOrgUnitParams{ID: unitUUID, WorkspaceID: org.ID}); err != nil {
		writeErrorCode(w, http.StatusBadRequest, codeDepartmentForbidden, "department does not belong to the organization")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change department")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	ws, err := qtx.UpdateWorkspaceOrgUnit(r.Context(), db.UpdateWorkspaceOrgUnitParams{ID: wsUUID, OrgUnitID: unitUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change department")
		return
	}
	// The new department's lead takes over the project. This mirrors creation
	// and the migration backfill: the lead is always an owner, so the project
	// cannot be left with nobody able to administer it.
	units, err := qtx.ListOrgUnits(r.Context(), org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change department")
		return
	}
	for _, unit := range units {
		if unit.ID != unitUUID || !unit.LeadUserID.Valid {
			continue
		}
		if err := h.ensureProjectOwner(r.Context(), qtx, wsUUID, unit.LeadUserID, unitUUID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to change department")
			return
		}
	}
	if err := qtx.ExpireWorkspacePendingInvitations(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change department")
		return
	}
	if err := qtx.DeactivateWorkspaceShareLinks(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change department")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change department")
		return
	}

	removed := h.disconnectMembersOutsideProject(r.Context(), wsUUID)
	h.publishAccessRevocations(wsUUID, requestUserID(r), removed)
	writeJSON(w, http.StatusOK, h.workspaceToResponse(ws))
}

// publishAccessRevocations tells connected clients and daemons that these
// members no longer reach the project. Their member rows remain so the
// department lead can pull them back in without losing history.
func (h *Handler) publishAccessRevocations(workspaceID pgtype.UUID, actorID string, removed []db.Member) {
	for _, member := range removed {
		h.publish(protocol.EventMemberRemoved, uuidToString(workspaceID), "member", actorID, map[string]any{
			"member_id":    uuidToString(member.ID),
			"workspace_id": uuidToString(workspaceID),
			"user_id":      uuidToString(member.UserID),
		})
		h.notifyDaemonWorkspacesChanged(uuidToString(member.UserID))
	}
}

// disconnectMembersOutsideProject closes live sessions and caches for members
// that no longer pass the project access predicate.
func (h *Handler) disconnectMembersOutsideProject(ctx context.Context, workspaceID pgtype.UUID) []db.Member {
	members, err := h.Queries.ListMembers(ctx, workspaceID)
	if err != nil {
		slog.WarnContext(ctx, "list members after department change failed", "workspace_id", uuidToString(workspaceID), "error", err)
		return nil
	}
	allowed, err := h.Queries.ListProjectMemberUserIDs(ctx, workspaceID)
	if err != nil {
		slog.WarnContext(ctx, "list project members after department change failed", "workspace_id", uuidToString(workspaceID), "error", err)
		return nil
	}
	keep := make(map[pgtype.UUID]bool, len(allowed))
	for _, userID := range allowed {
		keep[userID] = true
	}
	removed := make([]db.Member, 0)
	for _, member := range members {
		if keep[member.UserID] {
			continue
		}
		h.Hub.DisconnectMember(uuidToString(member.WorkspaceID), uuidToString(member.UserID), uuidToString(member.ID))
		h.MembershipCache.Invalidate(ctx, uuidToString(member.UserID), uuidToString(member.WorkspaceID))
		removed = append(removed, member)
	}
	return removed
}
