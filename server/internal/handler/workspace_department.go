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

// ListWorkspaceCreateOptions answers what the create-project screen may offer.
// Only department leads (and the platform operator) see anything.
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

	if requestIsOperator(r) {
		resp.CanCreate = true
		resp.CanCreateUnassigned = true
		sourceID := strings.TrimSpace(r.URL.Query().Get("source_workspace_id"))
		if sourceID == "" {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		sourceUUID, ok := parseUUIDOrBadRequest(w, sourceID, "source_workspace_id")
		if !ok {
			return
		}
		ws, err := h.Queries.GetWorkspace(r.Context(), sourceUUID)
		if err != nil {
			writeError(w, http.StatusNotFound, "source workspace not found")
			return
		}
		units, err := h.Queries.ListOrgUnits(r.Context(), sourceUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list departments")
			return
		}
		paths := orgscope.Paths(units)
		for _, unit := range units {
			resp.Departments = append(resp.Departments, DepartmentOption{
				SourceWorkspaceID:   uuidToString(ws.ID),
				SourceWorkspaceName: ws.Name,
				UnitID:              uuidToString(unit.ID),
				Path:                paths[unit.ID],
			})
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	rows, err := h.Queries.ListOrgUnitsForWorkspaces(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list departments")
		return
	}
	userUUID := parseUUID(userID)
	order := make([]pgtype.UUID, 0, 4)
	unitsByWorkspace := map[pgtype.UUID][]db.ListOrgUnitsRow{}
	meta := map[pgtype.UUID]db.ListOrgUnitsForWorkspacesRow{}
	for _, row := range rows {
		if _, seen := unitsByWorkspace[row.WorkspaceID]; !seen {
			order = append(order, row.WorkspaceID)
			meta[row.WorkspaceID] = row
		}
		unitsByWorkspace[row.WorkspaceID] = append(unitsByWorkspace[row.WorkspaceID], db.ListOrgUnitsRow{
			ID:         row.UnitID,
			ParentID:   row.ParentID,
			Name:       row.Name,
			LeadUserID: row.LeadUserID,
			Position:   row.Position,
		})
	}
	for _, wsID := range order {
		units := unitsByWorkspace[wsID]
		paths := orgscope.Paths(units)
		for _, unit := range units {
			if !orgscope.Leads(units, userUUID, unit.ID) {
				continue
			}
			resp.Departments = append(resp.Departments, DepartmentOption{
				SourceWorkspaceID:   uuidToString(wsID),
				SourceWorkspaceName: meta[wsID].WorkspaceName,
				UnitID:              uuidToString(unit.ID),
				Path:                paths[unit.ID],
			})
		}
	}
	resp.CanCreate = len(resp.Departments) > 0
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

// createDepartment captures the department a new project takes over.
type createDepartment struct {
	sourceWorkspaceID pgtype.UUID
	unitID            pgtype.UUID
	units             []db.ListOrgUnitsForCopyRow
	unit              db.ListOrgUnitsForCopyRow
}

// resolveCreateDepartment validates the requested department. It writes the
// HTTP error and returns ok=false on failure. A nil result means "create a
// legacy workspace without a department", which only the operator may do.
func (h *Handler) resolveCreateDepartment(w http.ResponseWriter, r *http.Request, userID, sourceID, unitID string) (*createDepartment, bool) {
	operator := requestIsOperator(r)

	if sourceID == "" && unitID == "" {
		if !operator {
			writeErrorCode(w, http.StatusForbidden, codeDepartmentRequired, "a department is required to create a project")
			return nil, false
		}
		return nil, true
	}
	if sourceID == "" || unitID == "" {
		writeErrorCode(w, http.StatusBadRequest, codeDepartmentRequired, "source_workspace_id and org_unit_id must be provided together")
		return nil, false
	}
	sourceUUID, ok := parseUUIDOrBadRequest(w, sourceID, "source_workspace_id")
	if !ok {
		return nil, false
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, unitID, "org_unit_id")
	if !ok {
		return nil, false
	}
	units, err := h.Queries.ListOrgUnitsForCopy(r.Context(), sourceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load departments")
		return nil, false
	}
	var unit db.ListOrgUnitsForCopyRow
	found := false
	for _, candidate := range units {
		if candidate.ID == targetUUID {
			unit = candidate
			found = true
			break
		}
	}
	if !found {
		writeErrorCode(w, http.StatusBadRequest, codeDepartmentForbidden, "department does not belong to the source workspace")
		return nil, false
	}

	if !operator {
		userUUID := parseUUID(userID)
		if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      userUUID,
			WorkspaceID: sourceUUID,
		}); err != nil {
			writeErrorCode(w, http.StatusForbidden, codeDepartmentForbidden, "you are not a member of the source workspace")
			return nil, false
		}
		if !orgscope.Leads(toOrgUnits(units), userUUID, targetUUID) {
			writeErrorCode(w, http.StatusForbidden, codeDepartmentForbidden, "you do not lead this department")
			return nil, false
		}
	}
	return &createDepartment{
		sourceWorkspaceID: sourceUUID,
		unitID:            targetUUID,
		units:             units,
		unit:              unit,
	}, true
}

// createProjectFromDepartment copies the whole organisation tree and directory
// of the source workspace into the new one, then makes the creator and the
// department lead owners placed at the project root.
func (h *Handler) createProjectFromDepartment(ctx context.Context, qtx *db.Queries, ws db.Workspace, dept *createDepartment, creatorID string) (db.Workspace, error) {
	remap, err := copyOrgTree(ctx, qtx, ws.ID, dept.units)
	if err != nil {
		return ws, err
	}
	rootUnitID, ok := remap[dept.unitID]
	if !ok {
		return ws, errors.New("copied tree is missing the selected department")
	}
	updated, err := qtx.UpdateWorkspaceOrgUnit(ctx, db.UpdateWorkspaceOrgUnitParams{
		ID:        ws.ID,
		OrgUnitID: rootUnitID,
	})
	if err != nil {
		return ws, err
	}

	if err := copyDirectory(ctx, qtx, ws.ID, dept.sourceWorkspaceID, remap); err != nil {
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
		// The source tree may name a lead who is no longer a user (no FK on
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

// copyOrgTree inserts every source unit, parents first, and returns the
// old-to-new id mapping.
func copyOrgTree(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, units []db.ListOrgUnitsForCopyRow) (map[pgtype.UUID]pgtype.UUID, error) {
	byID := make(map[pgtype.UUID]db.ListOrgUnitsForCopyRow, len(units))
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	children := make(map[pgtype.UUID][]db.ListOrgUnitsForCopyRow, len(units))
	roots := make([]db.ListOrgUnitsForCopyRow, 0, 1)
	for _, unit := range units {
		if unit.ParentID.Valid {
			if _, ok := byID[unit.ParentID]; ok {
				children[unit.ParentID] = append(children[unit.ParentID], unit)
				continue
			}
		}
		roots = append(roots, unit)
	}

	remap := make(map[pgtype.UUID]pgtype.UUID, len(units))
	var copyUnit func(parent pgtype.UUID, unit db.ListOrgUnitsForCopyRow) error
	copyUnit = func(parent pgtype.UUID, unit db.ListOrgUnitsForCopyRow) error {
		if _, done := remap[unit.ID]; done {
			return nil
		}
		newID, err := qtx.CreateOrgUnitClone(ctx, db.CreateOrgUnitCloneParams{
			WorkspaceID: workspaceID,
			ParentID:    parent,
			Name:        unit.Name,
			LeadUserID:  unit.LeadUserID,
			Description: unit.Description,
			Position:    unit.Position,
			ExternalID:  unit.ExternalID,
		})
		if err != nil {
			return err
		}
		remap[unit.ID] = newID
		for _, child := range children[unit.ID] {
			if err := copyUnit(newID, child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := copyUnit(pgtype.UUID{}, root); err != nil {
			return nil, err
		}
	}
	return remap, nil
}

// copyDirectory copies the source workspace's department directory into the
// new project, remapping placements. A row whose unit did not survive the copy
// keeps a NULL placement: it stays a candidate but has no department.
func copyDirectory(ctx context.Context, qtx *db.Queries, workspaceID, sourceWorkspaceID pgtype.UUID, remap map[pgtype.UUID]pgtype.UUID) error {
	rows, err := qtx.ListDirectoryMembersForCopy(ctx, sourceWorkspaceID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	userIDs := make([]pgtype.UUID, 0, len(rows))
	unitIDs := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.UserID)
		if mapped, ok := remap[row.OrgUnitID]; ok {
			unitIDs = append(unitIDs, mapped)
		} else {
			unitIDs = append(unitIDs, pgtype.UUID{})
		}
	}
	return qtx.InsertDirectoryMembers(ctx, db.InsertDirectoryMembersParams{
		WorkspaceID: workspaceID,
		UserIds:     userIDs,
		OrgUnitIds:  unitIDs,
	})
}

func toOrgUnits(units []db.ListOrgUnitsForCopyRow) []db.ListOrgUnitsRow {
	out := make([]db.ListOrgUnitsRow, len(units))
	for i, unit := range units {
		out[i] = db.ListOrgUnitsRow{
			ID:          unit.ID,
			ParentID:    unit.ParentID,
			Name:        unit.Name,
			LeadUserID:  unit.LeadUserID,
			Description: unit.Description,
			Position:    unit.Position,
		}
	}
	return out
}

// AddProjectMemberDirectRequest names one directory user to pull in.
type AddProjectMemberDirectRequest struct {
	UserID string `json:"user_id"`
}

// AddProjectMemberDirect is the department-project member path: managers pull
// a colleague from the department directory instead of sending an email
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
	if !ws.OrgUnitID.Valid {
		writeErrorCode(w, http.StatusConflict, codeDirectAddOnly, "email invitations are available for this workspace")
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

	directoryRow, err := h.Queries.GetDirectoryMember(r.Context(), db.GetDirectoryMemberParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErrorCode(w, http.StatusNotFound, codeMemberNotInDirectory, "user is not in the workspace directory")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load directory member")
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
		// to their directory placement instead of failing the pull-in.
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

// ProjectMemberCandidate is one directory user the project may pull in.
type ProjectMemberCandidate struct {
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	OrgUnitID *string `json:"org_unit_id"`
	Path      string  `json:"path"`
	InProject bool    `json:"in_project"`
}

// ListProjectMemberCandidates searches the company directory. A project
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
	if !ws.OrgUnitID.Valid {
		writeErrorCode(w, http.StatusConflict, codeDirectAddOnly, "this workspace adds members by invitation")
		return
	}
	units, err := h.Queries.ListOrgUnits(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load departments")
		return
	}
	paths := orgscope.Paths(units)
	rows, err := h.Queries.SearchDirectoryMembers(r.Context(), db.SearchDirectoryMembersParams{
		WorkspaceID: wsUUID,
		Query:       strings.TrimSpace(r.URL.Query().Get("q")),
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
	if _, err := h.Queries.GetOrgUnit(r.Context(), db.GetOrgUnitParams{ID: unitUUID, WorkspaceID: wsUUID}); err != nil {
		writeErrorCode(w, http.StatusBadRequest, codeDepartmentForbidden, "department does not belong to this workspace")
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
	units, err := qtx.ListOrgUnits(r.Context(), wsUUID)
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
