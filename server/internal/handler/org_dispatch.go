package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
)

type orgDispatchRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type orgDispatchResponse struct {
	Issue IssueResponse `json:"issue"`
}

// DispatchToOrgUnit creates one dispatch for a department. The issue is
// assigned to the department's squad, so its leader decomposes the request and
// assigns the children; the requester is told when it reaches review or done.
// Chat bots call the same service, which keeps visibility and leader checks in
// one place.
func (h *Handler) DispatchToOrgUnit(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	unitID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "unitId"), "unit id")
	if !ok {
		return
	}
	var body orgDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(body.Title)
	description := strings.TrimSpace(body.Description)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if description == "" {
		writeError(w, http.StatusBadRequest, "description is required")
		return
	}

	result, err := h.OrgDispatchService.Dispatch(r.Context(), service.OrgDispatchParams{
		WorkspaceID: wsUUID,
		ActorID:     member.UserID,
		UnitID:      unitID,
		Title:       title,
		Description: description,
		Platform:    func() string { p, _, _ := middleware.ClientMetadataFromContext(r.Context()); return p }(),
	})
	if err != nil {
		switch {
		// An invisible unit and a missing one answer the same: the caller must
		// not be able to enumerate departments outside their scope.
		case errors.Is(err, service.ErrOrgUnitNotFound), errors.Is(err, service.ErrOrgUnitNoAccess):
			writeError(w, http.StatusNotFound, "org unit not found")
		case errors.Is(err, service.ErrDispatchRequesterNotInTeam):
			writeError(w, http.StatusForbidden, "you cannot dispatch work to this department")
		case errors.Is(err, service.ErrDispatchRequesterUnbound):
			writeErrorCode(w, http.StatusForbidden, "agent_binding_required", "bind an agent before dispatching work")
		case errors.Is(err, service.ErrOrgUnitNoAgents):
			writeError(w, http.StatusConflict, "this department has no agent to lead the work; ask its lead to register one")
		case errors.Is(err, service.ErrDispatchLeaderUnavailable):
			writeError(w, http.StatusForbidden, "you do not have access to the squad leader of this department; ask its lead to share the agent with you")
		case errors.Is(err, service.ErrDispatchLeaderOffline):
			writeErrorCode(w, http.StatusConflict, "org_dispatch_leader_offline", "no agent in this department is online to take the work")
		default:
			slog.Error("dispatch to org unit failed", append(logger.RequestAttrs(r), "error", err, "unit_id", uuidToString(unitID))...)
			writeError(w, http.StatusInternalServerError, "failed to dispatch work")
		}
		return
	}

	resp := issueToResponse(result.Issue, h.getIssuePrefix(r.Context(), result.Issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), result.Issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusCreated, orgDispatchResponse{Issue: resp})
}
