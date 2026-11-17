package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
	Text            string   `json:"text"`
	UnitID          string   `json:"unit_id"`
	ClientRequestID string   `json:"client_request_id"`
	AttachmentIDs   []string `json:"attachment_ids"`
}

type dispatchWorkRef struct {
	UnitID   string `json:"unit_id"`
	UnitPath string `json:"unit_path"`
}

type dispatchWorkResponse struct {
	Issue    IssueResponse   `json:"issue"`
	Dispatch dispatchWorkRef `json:"dispatch"`
}

type dispatchListItem struct {
	Issue        IssueResponse `json:"issue"`
	UnitID       string        `json:"unit_id"`
	UnitPath     string        `json:"unit_path"`
	DispatchedAt string        `json:"dispatched_at"`
	Completed    bool          `json:"completed"`
	NotifiedAt   *string       `json:"notified_at,omitempty"`
	LeaderState  string        `json:"leader_state"`
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

	result, err := h.OrgDispatchService.Dispatch(r.Context(), service.OrgDispatchParams{
		WorkspaceID:     wsUUID,
		ActorID:         member.UserID,
		UnitID:          route.Unit.ID,
		Title:           service.DispatchTextTitle(text),
		Description:     text,
		ClientRequestID: pgtype.Text{String: uuidToString(clientID), Valid: true},
		AttachmentIDs:   attachmentIDs,
		Platform:        func() string { p, _, _ := middleware.ClientMetadataFromContext(r.Context()); return p }(),
	})
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
		h.writeDispatchError(w, r, err, uuidToString(route.Unit.ID))
		return
	}

	resp := issueToResponse(result.Issue, h.getIssuePrefix(r.Context(), result.Issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), result.Issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusCreated, dispatchWorkResponse{
		Issue:    resp,
		Dispatch: dispatchWorkRef{UnitID: uuidToString(result.Unit.ID), UnitPath: route.Path},
	})
}

func (h *Handler) writeExistingDispatch(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, origin db.DispatchOrigin) {
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID: origin.IssueID, WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("load existing dispatch issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load dispatch")
		return
	}
	units, _ := h.Queries.ListOrgUnits(r.Context(), wsUUID)
	resp := issueToResponse(issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusOK, dispatchWorkResponse{
		Issue: resp,
		Dispatch: dispatchWorkRef{
			UnitID:   uuidToString(origin.TargetUnitID),
			UnitPath: dispatchUnitPath(units, origin.TargetUnitID),
		},
	})
}

func (h *Handler) writeDispatchError(w http.ResponseWriter, r *http.Request, err error, unitID string) {
	switch {
	// Invisible and missing answer the same, so callers cannot enumerate
	// departments outside their scope.
	case errors.Is(err, service.ErrOrgUnitNotFound), errors.Is(err, service.ErrOrgUnitNoAccess):
		writeErrorCode(w, http.StatusNotFound, "org_dispatch_unit_not_found", "org unit not found")
	case errors.Is(err, service.ErrDispatchRequesterNotInTeam):
		writeErrorCode(w, http.StatusForbidden, "org_dispatch_forbidden", "you cannot dispatch work in this workspace")
	case errors.Is(err, service.ErrOrgUnitNoAgents):
		writeErrorCode(w, http.StatusConflict, "org_dispatch_no_agents", "this department has no agent to lead the work")
	case errors.Is(err, service.ErrDispatchLeaderUnavailable):
		writeErrorCode(w, http.StatusForbidden, "org_dispatch_leader_unavailable", "you cannot use this department's squad leader")
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
	issuesByID := make(map[pgtype.UUID]db.Issue, len(rows))
	if len(rows) > 0 {
		ids := make([]pgtype.UUID, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.IssueID)
		}
		issues, err := h.Queries.ListIssuesByIDs(r.Context(), db.ListIssuesByIDsParams{
			WorkspaceID: wsUUID, IssueIds: ids,
		})
		if err != nil {
			slog.Error("load dispatch issues failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "could not list dispatches")
			return
		}
		for _, issue := range issues {
			issuesByID[issue.ID] = issue
		}
	}
	units, err := h.Queries.ListOrgUnits(r.Context(), wsUUID)
	if err != nil {
		slog.Error("load dispatch units failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	fill := h.newStatusCategoryFiller(r.Context(), wsUUID)

	items := make([]dispatchListItem, 0, len(rows))
	for _, row := range rows {
		issue, ok := issuesByID[row.IssueID]
		if !ok {
			continue
		}
		resp := issueToResponse(issue, prefix)
		fill(&resp)
		item := dispatchListItem{
			Issue:        resp,
			UnitID:       uuidToString(row.TargetUnitID),
			UnitPath:     dispatchUnitPath(units, row.TargetUnitID),
			DispatchedAt: timestampToString(row.CreatedAt),
			Completed:    row.NotifiedAt.Valid,
			LeaderState:  row.LeaderState,
		}
		if row.NotifiedAt.Valid {
			at := timestampToString(row.NotifiedAt)
			item.NotifiedAt = &at
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"dispatches": items})
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
