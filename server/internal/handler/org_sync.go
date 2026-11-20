package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/feishu/contact"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// OrgDirectory is the Feishu contact surface organisation sync needs. Tests
// substitute a fake; production wires the contact client built from the same
// self-built app the workspace uses for Feishu login.
type OrgDirectory interface {
	ListDepartments(ctx context.Context) ([]contact.Department, error)
	ListDepartmentUsers(ctx context.Context, departmentIDs []string) ([]contact.User, error)
}

// orgSyncReport is both the POST response and the stored last-run record. Lists
// are bounded by what the directory returned, and they name what changed so an
// admin can spot mistakes without diffing the tree by hand.
type orgSyncReport struct {
	CreatedUnits        []string `json:"created_units"`
	UpdatedUnits        []string `json:"updated_units"`
	LeadsAssigned       []string `json:"leads_assigned"`
	MembersAdded        []string `json:"members_added"`
	MembersMoved        []string `json:"members_moved"`
	MembersWithoutEmail []string `json:"members_without_email"`
	DetachedDepartments []string `json:"detached_departments"`
	DetachedUsers       []string `json:"detached_users"`
	// OwnersOutsideDepartment and AccessRevoked are retained for wire
	// compatibility and stay empty: sync writes only the organization
	// workspace, and an explicit project member row keeps access wherever the
	// directory places the person.
	OwnersOutsideDepartment []string `json:"owners_outside_department"`
	AccessRevoked           []string `json:"access_revoked"`
	SyncedAt                string   `json:"synced_at"`
}

type orgSyncStatusResponse struct {
	Configured bool           `json:"configured"`
	LastRun    *orgSyncReport `json:"last_run"`
}

func newOrgSyncReport() orgSyncReport {
	return orgSyncReport{
		CreatedUnits:            []string{},
		UpdatedUnits:            []string{},
		LeadsAssigned:           []string{},
		MembersAdded:            []string{},
		MembersMoved:            []string{},
		MembersWithoutEmail:     []string{},
		DetachedDepartments:     []string{},
		DetachedUsers:           []string{},
		OwnersOutsideDepartment: []string{},
		AccessRevoked:           []string{},
	}
}

// GetOrgSyncStatus reports whether directory sync is configured and the last
// run's report. Any workspace member may read it; the report never carries
// secrets.
func (h *Handler) GetOrgSyncStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	resp := orgSyncStatusResponse{Configured: h.OrgDirectory != nil}
	run, err := h.Queries.GetLatestOrgSyncRun(r.Context(), wsUUID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		slog.Error("read org sync run failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not read the last organisation sync")
		return
	default:
		report := newOrgSyncReport()
		if err := json.Unmarshal(run.Summary, &report); err != nil {
			slog.Warn("org sync report is unreadable", append(logger.RequestAttrs(r), "error", err)...)
		} else {
			resp.LastRun = &report
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// errDirectoryNotConfigured is returned when the Feishu contact app is unset.
var errDirectoryNotConfigured = errors.New("feishu contact sync is not configured: set MULTICA_FEISHU_APP_ID and MULTICA_FEISHU_APP_SECRET")

type directoryUpstreamError struct{ err error }

func (e directoryUpstreamError) Error() string { return e.err.Error() }
func (e directoryUpstreamError) Unwrap() error { return e.err }

// SyncOrgFromFeishu imports the Feishu department tree and everyone in it
// into the organization workspace. Units are matched by external_id so manual
// units and manual renames survive; people are matched by email. Nothing is
// deleted: units and members that disappeared upstream are reported for an
// admin to handle. Only the platform operator may run it, and only from the
// organization workspace: projects read that tree instead of owning one.
func (h *Handler) SyncOrgFromFeishu(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireOperator(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	h.syncOrganizationFrom(w, r, wsUUID)
}

// syncOrganizationFrom runs the sync when wsUUID is the organization
// workspace and answers 409 organization_managed for a project.
func (h *Handler) syncOrganizationFrom(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) {
	org, ok := h.requireOrganizationWorkspace(w, r)
	if !ok {
		return
	}
	if org.ID != wsUUID {
		writeErrorCode(w, http.StatusConflict, codeOrganizationManaged, "the organization workspace owns the directory")
		return
	}
	report, err := h.syncDirectory(r.Context(), org)
	if err != nil {
		h.writeDirectorySyncError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// syncDirectory pulls the Feishu contact tree into the organization workspace
// and makes every project follow it. Callers enforce who may start it; this
// function only does the import.
func (h *Handler) syncDirectory(ctx context.Context, org db.Workspace) (orgSyncReport, error) {
	if h.OrgDirectory == nil {
		return orgSyncReport{}, errDirectoryNotConfigured
	}
	departments, err := h.OrgDirectory.ListDepartments(ctx)
	if err != nil {
		return orgSyncReport{}, directoryUpstreamError{err: err}
	}
	ordered := orderByDepth(departments)
	departmentIDs := make([]string, 0, len(ordered))
	for _, department := range ordered {
		departmentIDs = append(departmentIDs, department.ID)
	}
	users, err := h.OrgDirectory.ListDepartmentUsers(ctx, departmentIDs)
	if err != nil {
		return orgSyncReport{}, directoryUpstreamError{err: err}
	}

	report := newOrgSyncReport()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return orgSyncReport{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	unitIDs, err := h.upsertDirectoryUnits(ctx, qtx, org.ID, ordered, &report)
	if err != nil {
		return orgSyncReport{}, err
	}
	userIDs, err := h.upsertDirectoryUsers(ctx, qtx, org.ID, users, unitIDs, &report)
	if err != nil {
		return orgSyncReport{}, err
	}
	if err := h.assignDirectoryLeads(ctx, qtx, org.ID, ordered, unitIDs, userIDs, &report); err != nil {
		return orgSyncReport{}, err
	}
	if err := h.reportDetachedEntities(ctx, qtx, org.ID, ordered, users, &report); err != nil {
		return orgSyncReport{}, err
	}
	if err := h.followOrganization(ctx, qtx, org.ID, pgtype.UUID{}); err != nil {
		return orgSyncReport{}, err
	}

	report.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	summary, err := json.Marshal(report)
	if err != nil {
		return orgSyncReport{}, err
	}
	if _, err := qtx.InsertOrgSyncRun(ctx, db.InsertOrgSyncRunParams{
		WorkspaceID: org.ID,
		Summary:     summary,
	}); err != nil {
		return orgSyncReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return orgSyncReport{}, err
	}
	// Directory moves and newly assigned leads invalidate department squads;
	// one pass re-points any leader that can no longer run and re-rosters.
	h.syncAllOrgSquadLeadership(ctx, org.ID)
	return report, nil
}

// followOrganization makes projects agree with the organization workspace:
// project placements and primary twins follow each person's organization
// placement, projections carry their source's unit, and every
// owning-department lead is a project owner. unitID narrows the lead pass to
// projects owned by one unit.
func (h *Handler) followOrganization(ctx context.Context, qtx *db.Queries, orgID, unitID pgtype.UUID) error {
	if _, err := qtx.PropagateOrganizationPlacements(ctx, orgID); err != nil {
		return fmt.Errorf("propagate placements: %w", err)
	}
	if _, err := qtx.AlignProjectTwinUnits(ctx, orgID); err != nil {
		return fmt.Errorf("align project twins: %w", err)
	}
	if _, err := qtx.AlignProjectionUnits(ctx); err != nil {
		return fmt.Errorf("align projections: %w", err)
	}
	if _, err := h.healProjectLeadOwners(ctx, qtx, unitID); err != nil {
		return err
	}
	return nil
}

// SyncAllDirectories refreshes the organization workspace. It returns the
// number of workspaces synced: 0 when no organization workspace exists yet.
func (h *Handler) SyncAllDirectories(ctx context.Context) (int, error) {
	if h.OrgDirectory == nil {
		return 0, nil
	}
	org, ok, err := h.organizationWorkspace(ctx)
	if err != nil {
		return 0, err
	}
	if !ok {
		slog.Warn("scheduled directory sync skipped: no organization workspace")
		return 0, nil
	}
	if _, err := h.syncDirectory(ctx, org); err != nil {
		slog.Error("scheduled directory sync failed", "workspace_id", uuidToString(org.ID), "error", err)
		return 0, err
	}
	return 1, nil
}

func (h *Handler) writeDirectorySyncError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errDirectoryNotConfigured) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	var upstream directoryUpstreamError
	if errors.As(err, &upstream) {
		h.orgSyncUpstreamFailed(w, r, upstream.err)
		return
	}
	h.orgSyncFailed(w, r, err)
}

// upsertDirectoryUnits applies the department tree bottom-up-safe: by depth
// ascending, so a parent's unit exists before its children reference it. Units
// are keyed by external_id; an existing unit is reparented and renamed only
// when the directory changed it.
func (h *Handler) upsertDirectoryUnits(
	ctx context.Context,
	qtx *db.Queries,
	wsUUID pgtype.UUID,
	departments []contact.Department,
	report *orgSyncReport,
) (map[string]pgtype.UUID, error) {
	existing, err := qtx.ListSyncedOrgUnits(ctx, wsUUID)
	if err != nil {
		return nil, fmt.Errorf("list synced units: %w", err)
	}
	byExternal := make(map[string]db.ListSyncedOrgUnitsRow, len(existing))
	for _, unit := range existing {
		if unit.ExternalID.Valid {
			byExternal[unit.ExternalID.String] = unit
		}
	}
	unitIDs := make(map[string]pgtype.UUID, len(departments))
	for _, department := range departments {
		parent := pgtype.UUID{}
		if department.ParentID != "" {
			// A parent outside the visible range is treated as a root rather
			// than dropping the department from the tree.
			parent = unitIDs[department.ParentID]
		}
		if current, ok := byExternal[department.ID]; ok {
			if current.Name != department.Name || !orgUUIDEqual(current.ParentID, parent) {
				if err := qtx.UpdateOrgUnitFromExternal(ctx, db.UpdateOrgUnitFromExternalParams{
					ID:          current.ID,
					WorkspaceID: wsUUID,
					Name:        department.Name,
					ParentID:    parent,
				}); err != nil {
					return nil, fmt.Errorf("update unit %q: %w", department.Name, err)
				}
				report.UpdatedUnits = append(report.UpdatedUnits, department.Name)
			}
			unitIDs[department.ID] = current.ID
			continue
		}
		unitID, err := qtx.CreateOrgUnitWithExternal(ctx, db.CreateOrgUnitWithExternalParams{
			WorkspaceID: wsUUID,
			ParentID:    parent,
			Name:        department.Name,
			ExternalID:  pgtype.Text{String: department.ID, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("create unit %q: %w", department.Name, err)
		}
		unitIDs[department.ID] = unitID
		report.CreatedUnits = append(report.CreatedUnits, department.Name)
	}
	return unitIDs, nil
}

// upsertDirectoryUsers creates missing users, gives every employee a member
// row in the organization workspace and keeps placements current. Users
// without an email cannot be matched or created and are reported instead of
// silently skipped. These rows carry no seat intent: the organization roster is
// the company directory, not a billed seat.
func (h *Handler) upsertDirectoryUsers(
	ctx context.Context,
	qtx *db.Queries,
	wsUUID pgtype.UUID,
	users []contact.User,
	unitIDs map[string]pgtype.UUID,
	report *orgSyncReport,
) (map[string]pgtype.UUID, error) {
	members, err := qtx.ListMembers(ctx, wsUUID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	memberByUser := make(map[pgtype.UUID]db.Member, len(members))
	for _, member := range members {
		memberByUser[member.UserID] = member
	}

	userIDs := make(map[string]pgtype.UUID, len(users))
	for _, person := range users {
		if person.Email == "" {
			report.MembersWithoutEmail = append(report.MembersWithoutEmail, person.Name)
			continue
		}
		email := strings.ToLower(person.Email)
		avatar := pgtype.Text{String: person.AvatarURL, Valid: person.AvatarURL != ""}
		user, err := qtx.GetUserByLowerEmail(ctx, email)
		created := false
		if errors.Is(err, pgx.ErrNoRows) {
			user, err = qtx.CreateUser(ctx, db.CreateUserParams{
				Name:      person.Name,
				Email:     email,
				AvatarUrl: avatar,
			})
			created = true
		}
		if err != nil {
			return nil, fmt.Errorf("resolve user %q: %w", email, err)
		}
		if !created && (user.Name != person.Name || textValue(user.AvatarUrl) != person.AvatarURL) {
			if _, err := qtx.UpdateUserNameAndAvatar(ctx, db.UpdateUserNameAndAvatarParams{
				ID:        user.ID,
				Name:      person.Name,
				AvatarUrl: avatar,
			}); err != nil {
				return nil, fmt.Errorf("update user %q: %w", email, err)
			}
		}
		userIDs[person.OpenID] = user.ID

		unit := unitIDs[person.DepartmentID]
		member, isMember := memberByUser[user.ID]
		switch {
		case !isMember:
			createdMember, err := qtx.CreateMember(ctx, db.CreateMemberParams{
				WorkspaceID: wsUUID,
				UserID:      user.ID,
				Role:        "member",
				OrgUnitID:   unit,
			})
			if err != nil {
				return nil, fmt.Errorf("add member %q: %w", email, err)
			}
			memberByUser[user.ID] = createdMember
			report.MembersAdded = append(report.MembersAdded, person.Name)
		case unit.Valid && !orgUUIDEqual(member.OrgUnitID, unit):
			if err := qtx.SetMemberOrgUnit(ctx, db.SetMemberOrgUnitParams{
				WorkspaceID: wsUUID,
				UserID:      user.ID,
				OrgUnitID:   unit,
			}); err != nil {
				return nil, fmt.Errorf("place member %q: %w", email, err)
			}
			member.OrgUnitID = unit
			memberByUser[user.ID] = member
			report.MembersMoved = append(report.MembersMoved, person.Name)
			// The member's twin represents them in the organization, so a
			// directory move moves it too.
			if _, err := qtx.MovePrimaryTwinToOwnerUnit(ctx, db.MovePrimaryTwinToOwnerUnitParams{
				WorkspaceID: wsUUID,
				OwnerID:     user.ID,
			}); err != nil {
				return nil, fmt.Errorf("move primary twin of %q: %w", email, err)
			}
		}

		if err := qtx.SaveFeishuIdentity(ctx, db.SaveFeishuIdentityParams{
			UserID:    user.ID,
			OpenID:    person.OpenID,
			UnionID:   person.UnionID,
			Email:     email,
			Name:      person.Name,
			AvatarUrl: person.AvatarURL,
		}); err != nil {
			return nil, fmt.Errorf("record feishu identity %q: %w", email, err)
		}
	}
	return userIDs, nil
}

// assignDirectoryLeads fills empty unit leads from the directory's department
// leader, so a synced tree arrives with 负责人 set. A lead the workspace already
// chose is never overwritten.
func (h *Handler) assignDirectoryLeads(
	ctx context.Context,
	qtx *db.Queries,
	wsUUID pgtype.UUID,
	departments []contact.Department,
	unitIDs map[string]pgtype.UUID,
	userIDs map[string]pgtype.UUID,
	report *orgSyncReport,
) error {
	for _, department := range departments {
		leadID, ok := userIDs[department.LeadOpenID]
		if department.LeadOpenID == "" || !ok {
			continue
		}
		unitID, ok := unitIDs[department.ID]
		if !ok {
			continue
		}
		updated, err := qtx.SetOrgUnitLeadIfEmpty(ctx, db.SetOrgUnitLeadIfEmptyParams{
			ID:          unitID,
			WorkspaceID: wsUUID,
			LeadUserID:  leadID,
		})
		if err != nil {
			return fmt.Errorf("assign lead for %q: %w", department.Name, err)
		}
		if updated > 0 {
			report.LeadsAssigned = append(report.LeadsAssigned, department.Name)
		}
	}
	return nil
}

// reportDetachedEntities lists synced units and members the directory no
// longer returned. Sync never deletes them: an admin decides.
func (h *Handler) reportDetachedEntities(
	ctx context.Context,
	qtx *db.Queries,
	wsUUID pgtype.UUID,
	departments []contact.Department,
	users []contact.User,
	report *orgSyncReport,
) error {
	present := make(map[string]struct{}, len(departments))
	for _, department := range departments {
		present[department.ID] = struct{}{}
	}
	units, err := qtx.ListSyncedOrgUnits(ctx, wsUUID)
	if err != nil {
		return fmt.Errorf("list synced units: %w", err)
	}
	for _, unit := range units {
		if unit.ExternalID.Valid {
			if _, ok := present[unit.ExternalID.String]; !ok {
				report.DetachedDepartments = append(report.DetachedDepartments, unit.Name)
			}
		}
	}
	presentEmails := make(map[string]struct{}, len(users))
	for _, person := range users {
		if person.Email != "" {
			presentEmails[strings.ToLower(person.Email)] = struct{}{}
		}
	}
	identities, err := qtx.ListWorkspaceFeishuIdentities(ctx, wsUUID)
	if err != nil {
		return fmt.Errorf("list directory members: %w", err)
	}
	for _, identity := range identities {
		if _, ok := presentEmails[strings.ToLower(identity.Email)]; !ok {
			report.DetachedUsers = append(report.DetachedUsers, identity.Email)
		}
	}
	return nil
}

// orderByDepth sorts departments parents-first so upserts can resolve a
// parent's unit id in the same pass. Malformed cycles are cut by the guard.
func orderByDepth(departments []contact.Department) []contact.Department {
	byID := make(map[string]contact.Department, len(departments))
	for _, department := range departments {
		byID[department.ID] = department
	}
	depth := make(map[string]int, len(departments))
	var resolve func(id string, guard int) int
	resolve = func(id string, guard int) int {
		if value, ok := depth[id]; ok {
			return value
		}
		if guard > len(departments) {
			return 0
		}
		parentID := byID[id].ParentID
		if parentID == "" {
			depth[id] = 0
			return 0
		}
		if _, ok := byID[parentID]; !ok {
			depth[id] = 0
			return 0
		}
		value := resolve(parentID, guard+1) + 1
		depth[id] = value
		return value
	}
	out := append([]contact.Department(nil), departments...)
	for _, department := range out {
		resolve(department.ID, 0)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return depth[out[i].ID] < depth[out[j].ID]
	})
	return out
}

func orgUUIDEqual(a, b pgtype.UUID) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true
	}
	return a.Bytes == b.Bytes
}

func (h *Handler) orgSyncUpstreamFailed(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("feishu contact call failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusBadGateway, err.Error())
}

func (h *Handler) orgSyncFailed(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("org sync failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "could not sync the organisation from Feishu")
}
