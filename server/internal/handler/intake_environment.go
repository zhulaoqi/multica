package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type intakeEnvironmentBody struct {
	URL string `json:"url"`
}

func (h *Handler) SetIntakeTestEnvironment(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "projectId"), "project_id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectID, WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !intakeCanMaintain(member, project, userID) {
		writeError(w, http.StatusForbidden, "cannot edit the test environment")
		return
	}
	var body intakeEnvironmentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	ref, err := json.Marshal(map[string]string{"url": strings.TrimSpace(body.URL)})
	if err != nil {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	resources, err := h.Queries.ListProjectResourcesInWorkspace(r.Context(), db.ListProjectResourcesInWorkspaceParams{
		ProjectID: projectID, WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the test environment")
		return
	}
	for _, resource := range resources {
		if resource.ResourceType != "test_environment" {
			continue
		}
		if _, err := h.Queries.UpdateProjectResource(r.Context(), db.UpdateProjectResourceParams{
			ID: resource.ID, ResourceRef: ref, Label: resource.Label, Position: resource.Position,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save the test environment")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"url": strings.TrimSpace(body.URL)})
		return
	}
	if _, err := h.Queries.CreateProjectResource(r.Context(), db.CreateProjectResourceParams{
		ProjectID: projectID, WorkspaceID: parseUUID(workspaceID), ResourceType: "test_environment",
		ResourceRef: ref, Label: pgtype.Text{String: "测试环境", Valid: true}, Position: 0, CreatedBy: parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the test environment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": strings.TrimSpace(body.URL)})
}
