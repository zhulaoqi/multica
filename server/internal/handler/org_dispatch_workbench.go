package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultDispatchListLimit = 30
	maxDispatchListLimit     = 50
)

type dispatchWorkRequest struct {
	Text   string `json:"text"`
	UnitID string `json:"unit_id"`
	// ProjectID names a project the caller may request work for. The work is
	// created and run inside that project's workspace.
	ProjectID       string   `json:"project_id"`
	Personal        bool     `json:"personal"`
	SquadID         string   `json:"squad_id"`
	ClientRequestID string   `json:"client_request_id"`
	AttachmentIDs   []string `json:"attachment_ids"`
}

type dispatchWorkRef struct {
	UnitID   string `json:"unit_id"`
	UnitPath string `json:"unit_path"`
	// ProjectID names the project workspace the work was delivered into.
	ProjectID   string `json:"project_id,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	// SquadID names the personal squad a personal dispatch went to; unit_id
	// stays empty in that case.
	SquadID  string `json:"squad_id,omitempty"`
	Personal bool   `json:"personal,omitempty"`
}

type dispatchWorkResponse struct {
	Issue    IssueResponse   `json:"issue"`
	Dispatch dispatchWorkRef `json:"dispatch"`
}

type dispatchListItem struct {
	Issue        IssueResponse `json:"issue"`
	UnitID       string        `json:"unit_id"`
	UnitPath     string        `json:"unit_path"`
	ProjectID    string        `json:"project_id,omitempty"`
	ProjectName  string        `json:"project_name,omitempty"`
	SquadID      string        `json:"squad_id,omitempty"`
	Personal     bool          `json:"personal,omitempty"`
	DispatchedAt string        `json:"dispatched_at"`
	Completed    bool          `json:"completed"`
	NotifiedAt   *string       `json:"notified_at,omitempty"`
	LeaderState  string        `json:"leader_state"`
	// Blocker is the open intake blocker, when there is one: it is why the
	// work is parked, so both the requester and the department see it.
	Blocker *dispatchBlocker `json:"blocker,omitempty"`
	// Progress counts the root's sub-issues, resolved in the issue's own
	// workspace so a project dispatch reports the project's progress.
	Progress *dispatchProgress `json:"progress,omitempty"`
}

type dispatchProgress struct {
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

type dispatchBlocker struct {
	Done        string `json:"done"`
	Missing     string `json:"missing"`
	Consequence string `json:"consequence"`
}

// DispatchWork creates one department dispatch from the workbench's
// natural-language input. Routing and the dispatch itself share the service
// the chat /dispatch command uses, so visibility and leader checks stay in one
// place.
func (h *Handler) DispatchWork(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var body dispatchWorkRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErrorCode(w, http.StatusBadRequest, "dispatch_text_required", "text is required")
		return
	}
	clientID, ok := parseUUIDOrBadRequest(w, body.ClientRequestID, "client_request_id")
	if !ok {
		return
	}
	var explicitUnit pgtype.UUID
	if strings.TrimSpace(body.UnitID) != "" {
		explicitUnit, ok = parseUUIDOrBadRequest(w, body.UnitID, "unit_id")
		if !ok {
			return
		}
	}
	var explicitProject pgtype.UUID
	if strings.TrimSpace(body.ProjectID) != "" {
		explicitProject, ok = parseUUIDOrBadRequest(w, body.ProjectID, "project_id")
		if !ok {
			return
		}
	}
	var personalSquad pgtype.UUID
	if strings.TrimSpace(body.SquadID) != "" {
		personalSquad, ok = parseUUIDOrBadRequest(w, body.SquadID, "squad_id")
		if !ok {
			return
		}
	}
	personal := body.Personal || personalSquad.Valid
	targets := 0
	for _, chosen := range []bool{personal, explicitUnit.Valid, explicitProject.Valid} {
		if chosen {
			targets++
		}
	}
	if targets > 1 {
		writeErrorCode(w, http.StatusBadRequest, "org_dispatch_target_conflict", "choose one of a department, a project, or a personal squad")
		return
	}
	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, body.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}

	// A retried send may arrive after the first attempt committed but before
	// its response reached the client.
	if existing, err := h.Queries.GetDispatchByClient(r.Context(), db.GetDispatchByClientParams{
		WorkspaceID:     wsUUID,
		RequesterID:     member.UserID,
		ClientRequestID: pgtype.Text{String: uuidToString(clientID), Valid: true},
	}); err == nil {
		h.writeExistingDispatch(w, r, wsUUID, existing)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("load dispatch by client failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load dispatch")
		return
	}

	params := service.OrgDispatchParams{
		WorkspaceID:     wsUUID,
		ActorID:         member.UserID,
		ProjectID:       explicitProject,
		Personal:        personal,
		SquadID:         personalSquad,
		Title:           service.DispatchTextTitle(text),
		Description:     text,
		ClientRequestID: pgtype.Text{String: uuidToString(clientID), Valid: true},
		AttachmentIDs:   attachmentIDs,
		Platform:        func() string { p, _, _ := middleware.ClientMetadataFromContext(r.Context()); return p }(),
	}
	unitPath := ""
	// An explicit project skips free-text routing. Free text is still routed:
	// Route only ever names a project the caller may hand work, and the
	// dispatch re-checks that choice.
	if !personal && !explicitProject.Valid {
		route, err := h.OrgDispatchService.Route(r.Context(), wsUUID, member.UserID, text, explicitUnit)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrDispatchAmbiguous):
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
					"error":      "more than one department can take this request",
					"code":       "org_dispatch_ambiguous",
					"candidates": route.Candidates,
				})
			case errors.Is(err, service.ErrDispatchUnroutable):
				writeErrorCode(w, http.StatusUnprocessableEntity, "org_dispatch_unroutable", "no department can take this request")
			case errors.Is(err, service.ErrDispatchTextRequired):
				writeErrorCode(w, http.StatusBadRequest, "dispatch_text_required", "text is required")
			case errors.Is(err, service.ErrDispatchTextTooLong):
				writeErrorCode(w, http.StatusBadRequest, "dispatch_text_too_long", "text is too long")
			default:
				h.writeDispatchError(w, r, err, uuidToString(explicitUnit))
			}
			return
		}
		if route.Project != nil {
			// The routed project only picks the department and the intake rule;
			// Dispatch resolves and re-checks both.
			params.ProjectID = route.Project.ID
		} else {
			params.UnitID = route.Unit.ID
		}
		unitPath = route.Path
	}

	result, err := h.OrgDispatchService.Dispatch(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			if existing, loadErr := h.Queries.GetDispatchByClient(r.Context(), db.GetDispatchByClientParams{
				WorkspaceID:     wsUUID,
				RequesterID:     member.UserID,
				ClientRequestID: pgtype.Text{String: uuidToString(clientID), Valid: true},
			}); loadErr == nil {
				h.writeExistingDispatch(w, r, wsUUID, existing)
				return
			}
		}
		h.writeDispatchError(w, r, err, uuidToString(params.UnitID))
		return
	}

	ref := dispatchWorkRef{UnitID: uuidToString(result.Unit.ID), UnitPath: unitPath}
	if result.Personal {
		ref = dispatchWorkRef{SquadID: uuidToString(result.Squad.ID), Personal: true}
	} else if ref.UnitPath == "" {
		units, _ := h.Queries.ListOrgUnits(r.Context(), wsUUID)
		ref.UnitPath = dispatchUnitPath(units, result.Unit.ID)
	}
	if result.Project != nil {
		ref.ProjectID = uuidToString(result.Project.ID)
		ref.ProjectName = result.Project.Name
	}
	resp := issueToResponse(result.Issue, h.getIssuePrefix(r.Context(), result.Issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), result.Issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusCreated, dispatchWorkResponse{Issue: resp, Dispatch: ref})
}

type reassignDispatchRequest struct {
	UnitID string `json:"unit_id"`
}

// ReassignDispatch moves the requester's own dispatch to another department.
// The new leader is woken in the same request, so the issue never sits in a
// queue nobody is watching.
func (h *Handler) ReassignDispatch(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue_id")
	if !ok {
		return
	}
	var body reassignDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	unitUUID, ok := parseUUIDOrBadRequest(w, body.UnitID, "unit_id")
	if !ok {
		return
	}

	result, err := h.OrgDispatchService.Reassign(r.Context(), service.OrgDispatchReassignParams{
		WorkspaceID: wsUUID,
		ActorID:     member.UserID,
		IssueID:     issueUUID,
		UnitID:      unitUUID,
	})
	if err != nil {
		h.writeDispatchError(w, r, err, body.UnitID)
		return
	}
	units, _ := h.Queries.ListOrgUnits(r.Context(), wsUUID)
	resp := issueToResponse(result.Issue, h.getIssuePrefix(r.Context(), result.Issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), result.Issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusOK, dispatchWorkResponse{
		Issue: resp,
		Dispatch: dispatchWorkRef{
			UnitID:   uuidToString(result.Unit.ID),
			UnitPath: dispatchUnitPath(units, result.Unit.ID),
		},
	})
}

func (h *Handler) writeExistingDispatch(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, origin db.DispatchOrigin) {
	issue, err := h.Queries.GetIssue(r.Context(), origin.IssueID)
	if err != nil {
		slog.Error("load existing dispatch issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load dispatch")
		return
	}
	units, _ := h.Queries.ListOrgUnits(r.Context(), wsUUID)
	resp := issueToResponse(issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, &resp)
	ref := dispatchWorkRef{
		UnitID:   uuidToString(origin.TargetUnitID),
		UnitPath: dispatchUnitPath(units, origin.TargetUnitID),
	}
	if !origin.TargetUnitID.Valid {
		ref = dispatchWorkRef{SquadID: uuidToString(origin.TargetSquadID), Personal: true}
	}
	if origin.TargetProjectID.Valid {
		if project, err := h.Queries.GetWorkspace(r.Context(), origin.TargetProjectID); err == nil {
			ref.ProjectID = uuidToString(project.ID)
			ref.ProjectName = project.Name
		}
	}
	writeJSON(w, http.StatusOK, dispatchWorkResponse{Issue: resp, Dispatch: ref})
}

func (h *Handler) writeDispatchError(w http.ResponseWriter, r *http.Request, err error, unitID string) {
	switch {
	// Invisible and missing answer the same, so callers cannot enumerate
	// departments outside their scope.
	case errors.Is(err, service.ErrOrgUnitNotFound), errors.Is(err, service.ErrOrgUnitNoAccess):
		writeErrorCode(w, http.StatusNotFound, "org_dispatch_unit_not_found", "org unit not found")
	case errors.Is(err, service.ErrDispatchOrganizationOnly):
		writeErrorCode(w, http.StatusConflict, "dispatch_organization_only", "dispatch work from the organization workspace")
	case errors.Is(err, service.ErrPersonalSquadNotFound):
		writeErrorCode(w, http.StatusNotFound, "personal_squad_not_found", "personal squad not found")
	case errors.Is(err, service.ErrDispatchRequesterNotInTeam):
		writeErrorCode(w, http.StatusForbidden, "org_dispatch_forbidden", "you cannot dispatch work in this workspace")
	case errors.Is(err, service.ErrDispatchRequesterUnbound):
		writeErrorCode(w, http.StatusForbidden, "agent_binding_required", "bind an agent before dispatching work")
	case errors.Is(err, service.ErrOrgUnitNoAgents):
		writeErrorCode(w, http.StatusConflict, "org_dispatch_no_agents", "this department has no agent to lead the work")
	case errors.Is(err, service.ErrDispatchLeaderUnavailable):
		writeErrorCode(w, http.StatusForbidden, "org_dispatch_leader_unavailable", "you cannot use this department's squad leader")
	case errors.Is(err, service.ErrDispatchLeaderOffline):
		writeErrorCode(w, http.StatusConflict, "org_dispatch_leader_offline", "no agent in this department is online to take the work")
	case errors.Is(err, service.ErrProjectAgentOffline):
		writeErrorCode(w, http.StatusConflict, "org_dispatch_project_agent_offline", "no agent in this project is online to take the work")
	case errors.Is(err, service.ErrDispatchAttachmentNotFound):
		writeErrorCode(w, http.StatusBadRequest, "attachment_not_found", "some attachments are not available to this request")
	case errors.Is(err, service.ErrDispatchNotFound):
		writeErrorCode(w, http.StatusNotFound, "dispatch_not_found", "dispatch not found")
	case errors.Is(err, service.ErrDispatchReassignForbidden):
		writeErrorCode(w, http.StatusForbidden, "org_dispatch_reassign_forbidden", "only the requester can reassign this dispatch")
	case errors.Is(err, service.ErrDispatchProjectReassignUnsupported):
		writeErrorCode(w, http.StatusConflict, "dispatch_project_reassign_unsupported", "a dispatch delivered to a project cannot be reassigned")
	default:
		slog.Error("dispatch work failed", append(logger.RequestAttrs(r), "error", err, "unit_id", unitID)...)
		writeErrorCode(w, http.StatusInternalServerError, "org_dispatch_failed", "failed to dispatch work")
	}
}

// ListMyDispatches backs the workbench history. Only the requester's own
// dispatches are returned; the root issue carries status, and leader_state
// surfaces a squad-leader run that failed after its retries.
func (h *Handler) ListMyDispatches(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	limit := int32(defaultDispatchListLimit)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = int32(min(n, maxDispatchListLimit))
	}

	rows, err := h.Queries.ListDispatchesByRequester(r.Context(), db.ListDispatchesByRequesterParams{
		WorkspaceID: wsUUID,
		RequesterID: member.UserID,
		Limit:       limit,
	})
	if err != nil {
		slog.Error("list dispatches failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list dispatches")
		return
	}
	ctx := r.Context()
	// Dispatches delivered into a project live in the project workspace, so the
	// batch read cannot be scoped to the origin workspace.
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.IssueID)
	}
	issuesByID := make(map[pgtype.UUID]db.Issue, len(rows))
	if len(ids) > 0 {
		issues, err := h.Queries.ListIssuesByIDsAnyWorkspace(ctx, ids)
		if err != nil {
			slog.Error("load dispatch issues failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "could not list dispatches")
			return
		}
		for _, issue := range issues {
			issuesByID[issue.ID] = issue
		}
	}
	// Blockers are recorded in the workspace that owns the issue, and only an
	// organization-workspace issue can be opened from this inbox.
	var orgIssueIDs []pgtype.UUID
	for _, issue := range issuesByID {
		if issue.WorkspaceID == wsUUID {
			orgIssueIDs = append(orgIssueIDs, issue.ID)
		}
	}
	blockersByIssue := make(map[pgtype.UUID]dispatchBlocker, len(orgIssueIDs))
	if len(orgIssueIDs) > 0 {
		blockers, err := h.Queries.ListOpenIntakeBlockersByIssue(ctx, db.ListOpenIntakeBlockersByIssueParams{
			WorkspaceID: wsUUID,
			IssueIds:    orgIssueIDs,
		})
		if err != nil {
			// The history stays useful without blockers; log and continue.
			slog.Warn("load dispatch blockers failed", append(logger.RequestAttrs(r), "error", err)...)
		}
		for _, blocker := range blockers {
			blockersByIssue[blocker.IssueID] = dispatchBlocker{
				Done:        blocker.DoneText,
				Missing:     blocker.MissingText,
				Consequence: blocker.ConsequenceText,
			}
		}
	}
	progressByIssue := h.dispatchProgress(ctx, issuesByID)
	units, err := h.Queries.ListOrgUnits(ctx, wsUUID)
	if err != nil {
		slog.Error("load dispatch units failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	prefixByWorkspace := make(map[pgtype.UUID]string, 1)
	fillsByWorkspace := make(map[pgtype.UUID]func(*IssueResponse), 1)
	prefixFor := func(workspaceID pgtype.UUID) string {
		if prefix, ok := prefixByWorkspace[workspaceID]; ok {
			return prefix
		}
		prefix := h.getIssuePrefix(ctx, workspaceID)
		prefixByWorkspace[workspaceID] = prefix
		return prefix
	}
	fillFor := func(workspaceID pgtype.UUID) func(*IssueResponse) {
		if fill, ok := fillsByWorkspace[workspaceID]; ok {
			return fill
		}
		fill := h.newStatusCategoryFiller(ctx, workspaceID)
		fillsByWorkspace[workspaceID] = fill
		return fill
	}

	items := make([]dispatchListItem, 0, len(rows))
	for _, row := range rows {
		issue, ok := issuesByID[row.IssueID]
		if !ok {
			continue
		}
		resp := issueToResponse(issue, prefixFor(issue.WorkspaceID))
		fillFor(issue.WorkspaceID)(&resp)
		item := dispatchListItem{
			Issue:        resp,
			UnitID:       uuidToString(row.TargetUnitID),
			UnitPath:     dispatchUnitPath(units, row.TargetUnitID),
			ProjectID:    uuidToString(row.TargetProjectID),
			ProjectName:  row.ProjectName.String,
			DispatchedAt: timestampToString(row.CreatedAt),
			Completed:    row.NotifiedAt.Valid,
			LeaderState:  row.LeaderState,
			Progress:     progressByIssue[row.IssueID],
		}
		if blocker, ok := blockersByIssue[row.IssueID]; ok {
			item.Blocker = &blocker
		}
		if !row.TargetUnitID.Valid {
			item.Personal = true
			item.SquadID = uuidToString(row.TargetSquadID)
		}
		if row.NotifiedAt.Valid {
			at := timestampToString(row.NotifiedAt)
			item.NotifiedAt = &at
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"dispatches": items})
}

// dispatchProgress counts each root issue's sub-issues with the status catalog
// of the workspace the issue lives in.
func (h *Handler) dispatchProgress(ctx context.Context, issuesByID map[pgtype.UUID]db.Issue) map[pgtype.UUID]*dispatchProgress {
	progressByIssue := make(map[pgtype.UUID]*dispatchProgress, len(issuesByID))
	if len(issuesByID) == 0 {
		return progressByIssue
	}
	parentIDs := make([]pgtype.UUID, 0, len(issuesByID))
	for id := range issuesByID {
		parentIDs = append(parentIDs, id)
	}
	children, err := h.Queries.ListChildStatusesByParents(ctx, parentIDs)
	if err != nil {
		// Progress is a hint; history stays useful without it.
		slog.Warn("load dispatch child progress failed", "error", err)
		return progressByIssue
	}
	terminalByWorkspace := make(map[pgtype.UUID]map[string]bool)
	terminalFor := func(workspaceID pgtype.UUID) map[string]bool {
		if keys, ok := terminalByWorkspace[workspaceID]; ok {
			return keys
		}
		keys := make(map[string]bool)
		list, err := h.terminalIssueStatusKeys(ctx, workspaceID)
		if err != nil {
			slog.Warn("load terminal status keys failed", "workspace_id", uuidToString(workspaceID), "error", err)
		}
		for _, key := range list {
			keys[key] = true
		}
		terminalByWorkspace[workspaceID] = keys
		return keys
	}
	for _, child := range children {
		parent, ok := issuesByID[child.ParentIssueID]
		if !ok {
			continue
		}
		entry := progressByIssue[child.ParentIssueID]
		if entry == nil {
			entry = &dispatchProgress{}
			progressByIssue[child.ParentIssueID] = entry
		}
		entry.Total++
		if terminalFor(parent.WorkspaceID)[child.Status] {
			entry.Done++
		}
	}
	return progressByIssue
}

// dispatchQueueItem is a department-side view of one dispatch: who asked, what
// it is waiting on, and which queue the department is expected to work it from.
type dispatchQueueItem struct {
	dispatchListItem
	RequesterID   string `json:"requester_id"`
	RequesterName string `json:"requester_name"`
	Stage         string `json:"stage"`
}

// Department queue stages, matching the three buckets the department works
// through: unstarted = waiting to be taken, started = in progress, and
// in_review = waiting on acceptance.
const (
	dispatchStagePending    = "pending"
	dispatchStageInProgress = "in_progress"
	dispatchStageReview     = "review"
)

// ListDepartmentDispatches backs the department side of the workbench: open
// dispatches aimed at a unit the caller leads, split into the queues the
// department works through. Workspace managers see every unit; anyone else
// sees only the units they lead or inherit leadership of.
func (h *Handler) ListDepartmentDispatches(w http.ResponseWriter, r *http.Request) {
	access, ok := h.orgAccess(w, r)
	if !ok {
		return
	}
	limit := int32(defaultDispatchListLimit)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = int32(min(n, maxDispatchListLimit))
	}

	wsAccess, err := h.workspaceAccess(r, uuidToString(access.userID), uuidToString(access.wsUUID))
	if err != nil {
		slog.Error("resolve workspace access failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list dispatches")
		return
	}
	manager := access.admin || wsAccess.IsManager()
	unitIDs := make([]pgtype.UUID, 0, len(access.units))
	for _, unit := range access.units {
		if manager || access.implicitLead || orgscope.Leads(access.units, access.userID, unit.ID) {
			unitIDs = append(unitIDs, unit.ID)
		}
	}
	if len(unitIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"dispatches": []dispatchQueueItem{}})
		return
	}

	rows, err := h.Queries.ListDispatchesForUnits(r.Context(), db.ListDispatchesForUnitsParams{
		WorkspaceID: access.wsUUID,
		Column2:     unitIDs,
		Limit:       limit,
	})
	if err != nil {
		slog.Error("list department dispatches failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list dispatches")
		return
	}
	items, err := h.buildDispatchQueue(r, access.wsUUID, rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list dispatches")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dispatches": items})
}

func (h *Handler) buildDispatchQueue(r *http.Request, wsUUID pgtype.UUID, rows []db.ListDispatchesForUnitsRow) ([]dispatchQueueItem, error) {
	items := make([]dispatchQueueItem, 0, len(rows))
	if len(rows) == 0 {
		return items, nil
	}
	ctx := r.Context()
	issueIDs := make([]pgtype.UUID, 0, len(rows))
	requesterIDs := make([]pgtype.UUID, 0, len(rows))
	seenRequester := make(map[pgtype.UUID]bool, len(rows))
	for _, row := range rows {
		issueIDs = append(issueIDs, row.IssueID)
		if !seenRequester[row.RequesterID] {
			seenRequester[row.RequesterID] = true
			requesterIDs = append(requesterIDs, row.RequesterID)
		}
	}

	issues, err := h.Queries.ListIssuesByIDs(ctx, db.ListIssuesByIDsParams{
		WorkspaceID: wsUUID, IssueIds: issueIDs,
	})
	if err != nil {
		slog.Error("load dispatch issues failed", append(logger.RequestAttrs(r), "error", err)...)
		return nil, err
	}
	issuesByID := make(map[pgtype.UUID]db.Issue, len(issues))
	for _, issue := range issues {
		issuesByID[issue.ID] = issue
	}

	requesters, err := h.Queries.GetUsersByIDs(ctx, requesterIDs)
	if err != nil {
		slog.Error("load dispatch requesters failed", append(logger.RequestAttrs(r), "error", err)...)
		return nil, err
	}
	namesByID := make(map[pgtype.UUID]string, len(requesters))
	for _, user := range requesters {
		namesByID[user.ID] = user.Name
	}

	blockers, err := h.Queries.ListOpenIntakeBlockersByIssue(ctx, db.ListOpenIntakeBlockersByIssueParams{
		WorkspaceID: wsUUID,
		IssueIds:    issueIDs,
	})
	if err != nil {
		slog.Error("load dispatch blockers failed", append(logger.RequestAttrs(r), "error", err)...)
		return nil, err
	}
	blockersByIssue := make(map[pgtype.UUID]dispatchBlocker, len(blockers))
	for _, blocker := range blockers {
		blockersByIssue[blocker.IssueID] = dispatchBlocker{
			Done:        blocker.DoneText,
			Missing:     blocker.MissingText,
			Consequence: blocker.ConsequenceText,
		}
	}

	units, err := h.Queries.ListOrgUnits(ctx, wsUUID)
	if err != nil {
		// The queue stays useful without unit paths; keep going with empty ones.
		slog.Warn("load dispatch units failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	prefix := h.getIssuePrefix(ctx, wsUUID)
	fill := h.newStatusCategoryFiller(ctx, wsUUID)

	for _, row := range rows {
		issue, ok := issuesByID[row.IssueID]
		if !ok {
			continue
		}
		resp := issueToResponse(issue, prefix)
		fill(&resp)
		item := dispatchQueueItem{
			dispatchListItem: dispatchListItem{
				Issue:        resp,
				UnitID:       uuidToString(row.TargetUnitID),
				UnitPath:     dispatchUnitPath(units, row.TargetUnitID),
				DispatchedAt: timestampToString(row.CreatedAt),
				Completed:    row.NotifiedAt.Valid,
				LeaderState:  row.LeaderState,
			},
			RequesterID:   uuidToString(row.RequesterID),
			RequesterName: namesByID[row.RequesterID],
			Stage:         dispatchQueueStage(row.StatusCategory, issue.Status),
		}
		if row.NotifiedAt.Valid {
			at := timestampToString(row.NotifiedAt)
			item.NotifiedAt = &at
		}
		if blocker, ok := blockersByIssue[row.IssueID]; ok {
			item.Blocker = &blocker
		}
		items = append(items, item)
	}
	return items, nil
}

func dispatchQueueStage(category, status string) string {
	switch category {
	case "unstarted":
		return dispatchStagePending
	case "started":
		if status == issuestatus.InReview {
			return dispatchStageReview
		}
	}
	return dispatchStageInProgress
}

func dispatchUnitPath(units []db.ListOrgUnitsRow, unitID pgtype.UUID) string {
	paths := orgscope.Paths(units)
	for _, unit := range units {
		if unit.ID == unitID {
			return paths[unit.ID]
		}
	}
	return ""
}
