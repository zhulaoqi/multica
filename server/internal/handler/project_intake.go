package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const codeProjectIntakeUnavailable = "project_intake_unavailable"

type projectIntakeSourceResponse struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type projectIntakeSourcesResponse struct {
	Sources []projectIntakeSourceResponse `json:"sources"`
	// Inherited reports that the project has no list of its own and follows
	// its owning department.
	Inherited bool   `json:"inherited"`
	UnitID    string `json:"unit_id"`
	UnitPath  string `json:"unit_path"`
}

type projectIntakeSourcesBody struct {
	Sources []projectIntakeSourceResponse `json:"sources"`
}

type projectIntakeCandidateUnit struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type projectIntakeCandidateMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type projectIntakeCandidatesResponse struct {
	Units   []projectIntakeCandidateUnit   `json:"units"`
	Members []projectIntakeCandidateMember `json:"members"`
}

type requestableProjectResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	UnitID   string `json:"unit_id"`
	UnitPath string `json:"unit_path"`
}

type requestableProjectsResponse struct {
	Projects []requestableProjectResponse `json:"projects"`
}

// projectIntakeContext is the current workspace resolved as a
// department-owned project, with the organization that owns its tree.
type projectIntakeContext struct {
	project db.Workspace
	org     db.Workspace
	access  workspaceaccess.Access
	units   []db.ListOrgUnitsRow
}

// projectIntakeContext resolves the request's workspace as a project and
// checks that the caller manages it. Anything that is not a
// department-owned project answers 409: the organization owns its tree and a
// legacy workspace has no department to hand work to.
func (h *Handler) projectIntakeContext(w http.ResponseWriter, r *http.Request, requireManage bool) (projectIntakeContext, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return projectIntakeContext{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return projectIntakeContext{}, false
	}
	project, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return projectIntakeContext{}, false
	}
	if err != nil {
		slog.Error("load project failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load the project")
		return projectIntakeContext{}, false
	}
	if !isOrganizationProject(project) {
		writeErrorCode(w, http.StatusConflict, codeProjectIntakeUnavailable, "intake lists need a project owned by a department")
		return projectIntakeContext{}, false
	}
	access, err := workspaceaccess.Resolve(r.Context(), h.Queries, wsUUID, member.UserID, requestIsOperator(r))
	if err != nil {
		slog.Error("resolve project access failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not check access")
		return projectIntakeContext{}, false
	}
	if requireManage && !access.IsManager() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return projectIntakeContext{}, false
	}
	if !requireManage && !access.Viewer() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return projectIntakeContext{}, false
	}
	org, found, err := h.organizationWorkspace(r.Context())
	if err != nil {
		slog.Error("load organization workspace failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load the organization")
		return projectIntakeContext{}, false
	}
	if !found {
		writeErrorCode(w, http.StatusConflict, codeOrganizationMissing, errOrganizationMissing.Error())
		return projectIntakeContext{}, false
	}
	units, err := h.Queries.ListOrgUnits(r.Context(), org.ID)
	if err != nil {
		slog.Error("list org units failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list organisation units")
		return projectIntakeContext{}, false
	}
	return projectIntakeContext{project: project, org: org, access: access, units: units}, true
}

// GetProjectIntakeSources reads who may hand the project work. An empty list
// is not "nobody": the project then follows its owning department.
func (h *Handler) GetProjectIntakeSources(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.projectIntakeContext(w, r, false)
	if !ok {
		return
	}
	sources, err := h.Queries.ListWorkspaceIntakeSources(r.Context(), ctx.project.ID)
	if err != nil {
		slog.Error("list project intake sources failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load the intake list")
		return
	}
	writeJSON(w, http.StatusOK, projectIntakeSourcesResponse{
		Sources:   projectIntakeSourceRefs(sources),
		Inherited: len(sources) == 0,
		UnitID:    uuidToString(ctx.project.OrgUnitID),
		UnitPath:  dispatchUnitPath(ctx.units, ctx.project.OrgUnitID),
	})
}

// ReplaceProjectIntakeSources sets who may hand the project work. A list
// replaces the owning department's rule for this project; an empty list
// restores it.
func (h *Handler) ReplaceProjectIntakeSources(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.projectIntakeContext(w, r, true)
	if !ok {
		return
	}
	var body projectIntakeSourcesBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Sources) > orgIntakeSourceLimit {
		writeError(w, http.StatusBadRequest, "too many intake sources")
		return
	}
	unitIDs := workspaceaccess.UnitIDs(ctx.units)
	seen := make(map[string]struct{}, len(body.Sources))
	refs := make([]projectIntakeSourceResponse, 0, len(body.Sources))
	for _, source := range body.Sources {
		sourceID, ok := parseUUIDOrBadRequest(w, source.ID, "intake source id")
		if !ok {
			return
		}
		switch source.Type {
		case "user":
			if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID:      sourceID,
				WorkspaceID: ctx.org.ID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeError(w, http.StatusBadRequest, "intake source user is not a member of the organization")
					return
				}
				slog.Error("check intake source user failed", append(logger.RequestAttrs(r), "error", err)...)
				writeError(w, http.StatusInternalServerError, "could not validate the intake list")
				return
			}
		case "org_unit":
			if !unitIDs[sourceID] {
				writeError(w, http.StatusBadRequest, "intake source department is not in this organisation")
				return
			}
		default:
			writeError(w, http.StatusBadRequest, "intake source type must be user or org_unit")
			return
		}
		key := source.Type + ":" + uuidToString(sourceID)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, projectIntakeSourceResponse{Type: source.Type, ID: uuidToString(sourceID)})
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		h.projectIntakeFailed(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	q := db.New(tx)
	if err := q.DeleteWorkspaceIntakeSources(r.Context(), ctx.project.ID); err != nil {
		h.projectIntakeFailed(w, r, err)
		return
	}
	for _, ref := range refs {
		if _, err := q.CreateWorkspaceIntakeSource(r.Context(), db.CreateWorkspaceIntakeSourceParams{
			WorkspaceID: ctx.project.ID,
			SourceType:  ref.Type,
			SourceID:    parseUUID(ref.ID),
		}); err != nil {
			h.projectIntakeFailed(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.projectIntakeFailed(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListProjectIntakeCandidates lists the departments and people a project
// manager may put on the project's intake list: the whole organization, since
// a project may pull in colleagues from any department.
func (h *Handler) ListProjectIntakeCandidates(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.projectIntakeContext(w, r, false)
	if !ok {
		return
	}
	paths := orgscope.Paths(ctx.units)
	units := make([]projectIntakeCandidateUnit, 0, len(ctx.units))
	for _, unit := range ctx.units {
		units = append(units, projectIntakeCandidateUnit{ID: uuidToString(unit.ID), Path: paths[unit.ID]})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Path < units[j].Path })
	members, err := h.Queries.ListMembersWithUser(r.Context(), ctx.org.ID)
	if err != nil {
		slog.Error("list organization members failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list members")
		return
	}
	candidates := make([]projectIntakeCandidateMember, 0, len(members))
	for _, member := range members {
		name := strings.TrimSpace(member.UserName)
		if name == "" {
			name = member.UserEmail
		}
		candidates = append(candidates, projectIntakeCandidateMember{ID: uuidToString(member.UserID), Name: name})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	writeJSON(w, http.StatusOK, projectIntakeCandidatesResponse{Units: units, Members: candidates})
}

// ListRequestableProjects lists the projects the caller may hand work. The
// client only ever renders this list, so a project the caller may not request
// never appears, and a direct dispatch to it answers 404 like a missing one.
func (h *Handler) ListRequestableProjects(w http.ResponseWriter, r *http.Request) {
	org, ok := h.requireOrganizationWorkspace(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if wsUUID != org.ID {
		writeErrorCode(w, http.StatusConflict, "dispatch_organization_only", "dispatch work from the organization workspace")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	access, err := workspaceaccess.Resolve(r.Context(), h.Queries, org.ID, member.UserID, requestIsOperator(r))
	if err != nil || !access.Allowed() {
		writeErrorCode(w, http.StatusForbidden, "org_dispatch_forbidden", "you cannot dispatch work in this workspace")
		return
	}
	units, err := h.Queries.ListOrgUnits(r.Context(), org.ID)
	if err != nil {
		slog.Error("list org units failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list organisation units")
		return
	}
	projects, err := h.Queries.ListDepartmentProjects(r.Context())
	if err != nil {
		slog.Error("list projects failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not list projects")
		return
	}
	departmentSources, err := h.Queries.ListOrgUnitIntakeSources(r.Context(), org.ID)
	if err != nil {
		slog.Error("list department intake sources failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load intake lists")
		return
	}
	projectIDs := make([]pgtype.UUID, len(projects))
	for i, project := range projects {
		projectIDs[i] = project.ID
	}
	sourcesByProject := make(map[pgtype.UUID][]db.ListWorkspaceIntakeSourcesForProjectsRow, len(projects))
	if len(projectIDs) > 0 {
		rows, err := h.Queries.ListWorkspaceIntakeSourcesForProjects(r.Context(), projectIDs)
		if err != nil {
			slog.Error("list project intake sources failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "could not load intake lists")
			return
		}
		for _, row := range rows {
			sourcesByProject[row.WorkspaceID] = append(sourcesByProject[row.WorkspaceID], row)
		}
	}
	memberships, err := h.Queries.ListUserWorkspaceMemberships(r.Context(), member.UserID)
	if err != nil {
		slog.Error("list project memberships failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not check project membership")
		return
	}
	isMember := make(map[pgtype.UUID]bool, len(memberships))
	for _, wsID := range memberships {
		isMember[wsID] = true
	}
	paths := orgscope.Paths(units)
	items := make([]requestableProjectResponse, 0, len(projects))
	for _, project := range projects {
		path, ok := paths[project.OrgUnitID]
		if !ok {
			// The owning department is gone; fail closed rather than offer a
			// target that cannot resolve.
			continue
		}
		workspace := db.Workspace{
			ID:        project.ID,
			Name:      project.Name,
			Slug:      project.Slug,
			OrgUnitID: project.OrgUnitID,
		}
		projectSources := workspaceIntakeSourceRows(sourcesByProject[project.ID])
		if !service.ProjectRequestable(access, units, departmentSources, workspace, projectSources, isMember[project.ID]) {
			continue
		}
		items = append(items, requestableProjectResponse{
			ID:       uuidToString(project.ID),
			Name:     project.Name,
			Slug:     project.Slug,
			UnitID:   uuidToString(project.OrgUnitID),
			UnitPath: path,
		})
	}
	writeJSON(w, http.StatusOK, requestableProjectsResponse{Projects: items})
}

func (h *Handler) projectIntakeFailed(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("project intake update failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "could not save the intake list")
}

func projectIntakeSourceRefs(sources []db.ListWorkspaceIntakeSourcesRow) []projectIntakeSourceResponse {
	refs := make([]projectIntakeSourceResponse, 0, len(sources))
	for _, source := range sources {
		refs = append(refs, projectIntakeSourceResponse{Type: source.SourceType, ID: uuidToString(source.SourceID)})
	}
	return refs
}

// workspaceIntakeSourceRows adapts the batched query rows to the shape the
// permission helper reads, so one decision function serves both paths.
func workspaceIntakeSourceRows(rows []db.ListWorkspaceIntakeSourcesForProjectsRow) []db.ListWorkspaceIntakeSourcesRow {
	sources := make([]db.ListWorkspaceIntakeSourcesRow, len(rows))
	for i, row := range rows {
		sources[i] = db.ListWorkspaceIntakeSourcesRow{
			ID:          row.ID,
			WorkspaceID: row.WorkspaceID,
			SourceType:  row.SourceType,
			SourceID:    row.SourceID,
		}
	}
	return sources
}
