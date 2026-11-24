package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
)

type transferDispatchRequest struct {
	// Project is the target project's id or slug.
	Project string `json:"project"`
	// AgentID optionally names the project agent that runs the work.
	AgentID string `json:"agent_id"`
	Note    string `json:"note"`
}

type transferDispatchResponse struct {
	Issue    IssueResponse `json:"issue"`
	Previous IssueResponse `json:"previous"`
	Project  struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"project"`
	ExecutorID string `json:"executor_id"`
}

// TransferDispatchToProject lets a department squad leader hand a dispatch
// that belongs to a project over to that project. Only the leader's own run
// may call it: the request must carry the task token of a leader task on the
// dispatch issue.
func (h *Handler) TransferDispatchToProject(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue_id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	if actorType != "agent" {
		writeErrorCode(w, http.StatusForbidden, "dispatch_transfer_forbidden", "only the department squad leader can transfer this dispatch")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, actorID, "agent id")
	if !ok {
		return
	}
	taskUUID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Task-ID"), "task id")
	if !ok {
		return
	}

	var body transferDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectUUID, ok := h.resolveTransferProject(w, r, body.Project)
	if !ok {
		return
	}
	var preferred pgtype.UUID
	if strings.TrimSpace(body.AgentID) != "" {
		preferred, ok = parseUUIDOrBadRequest(w, body.AgentID, "agent_id")
		if !ok {
			return
		}
	}

	result, err := h.OrgDispatchService.TransferDispatchToProject(r.Context(), service.DispatchTransferParams{
		WorkspaceID:      wsUUID,
		AgentID:          agentUUID,
		TaskID:           taskUUID,
		IssueID:          issueUUID,
		ProjectID:        projectUUID,
		PreferredAgentID: preferred,
		Note:             body.Note,
	})
	if err != nil {
		h.writeDispatchTransferError(w, r, err)
		return
	}
	var resp transferDispatchResponse
	resp.Issue = issueToResponse(result.Issue, result.Project.IssuePrefix)
	h.fillStatusCategory(r.Context(), result.Issue.WorkspaceID, &resp.Issue)
	resp.Previous = issueToResponse(result.Previous, h.getIssuePrefix(r.Context(), result.Previous.WorkspaceID))
	h.fillStatusCategory(r.Context(), result.Previous.WorkspaceID, &resp.Previous)
	resp.Project.ID = uuidToString(result.Project.ID)
	resp.Project.Name = result.Project.Name
	resp.Project.Slug = result.Project.Slug
	resp.ExecutorID = uuidToString(result.Executor)
	writeJSON(w, http.StatusOK, resp)
}

// resolveTransferProject accepts a project id or slug. An unknown slug answers
// like an unrequestable project so projects cannot be enumerated.
func (h *Handler) resolveTransferProject(w http.ResponseWriter, r *http.Request, ref string) (pgtype.UUID, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return pgtype.UUID{}, false
	}
	if id, err := util.ParseUUID(ref); err == nil {
		return id, true
	}
	project, err := h.Queries.GetWorkspaceBySlug(r.Context(), ref)
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, "org_dispatch_unit_not_found", "project not found")
		return pgtype.UUID{}, false
	}
	return project.ID, true
}

func (h *Handler) writeDispatchTransferError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrDispatchTransferForbidden):
		writeErrorCode(w, http.StatusForbidden, "dispatch_transfer_forbidden", "only the department squad leader can transfer this dispatch")
	case errors.Is(err, service.ErrDispatchTransferUnsupported):
		writeErrorCode(w, http.StatusConflict, "dispatch_transfer_unsupported", "this dispatch cannot be transferred to a project")
	case errors.Is(err, service.ErrDispatchTransferAgentUnavailable):
		writeErrorCode(w, http.StatusConflict, "dispatch_transfer_agent_unavailable", "the chosen agent cannot run work in this project")
	case errors.Is(err, service.ErrOrgUnitNotFound), errors.Is(err, service.ErrOrgUnitNoAccess),
		errors.Is(err, service.ErrProjectAgentOffline), errors.Is(err, service.ErrDispatchNotFound):
		h.writeDispatchError(w, r, err, "")
	default:
		slog.Error("dispatch transfer failed", append(logger.RequestAttrs(r), "error", err)...)
		writeErrorCode(w, http.StatusInternalServerError, "dispatch_transfer_failed", "failed to transfer dispatch")
	}
}
