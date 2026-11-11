package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type intakeModuleInput struct {
	Product       string `json:"product"`
	Module        string `json:"module"`
	Scope         string `json:"scope"`
	AgentID       string `json:"agent_id"`
	BackupAgentID string `json:"backup_agent_id"`
	Position      int32  `json:"position"`
}

type intakeModulesBody struct {
	ProjectID string              `json:"project_id"`
	Modules   []intakeModuleInput `json:"modules"`
}

type intakeMembersBody struct {
	UserIDs []string `json:"user_ids"`
}

func (h *Handler) ListIntakeModules(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("project_id"), "project_id")
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
		memberOfProject, err := h.Queries.IntakeProjectHasMember(r.Context(), db.IntakeProjectHasMemberParams{
			WorkspaceID: parseUUID(workspaceID), ProjectID: projectID, UserID: parseUUID(userID),
		})
		if err != nil || !memberOfProject {
			writeError(w, http.StatusForbidden, "cannot view modules")
			return
		}
	}
	rows, err := h.Queries.ListIntakeModulesForProject(r.Context(), db.ListIntakeModulesForProjectParams{
		WorkspaceID: parseUUID(workspaceID), ProjectID: projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list modules")
		return
	}
	type item struct {
		ID            string `json:"id"`
		Product       string `json:"product"`
		Module        string `json:"module"`
		Scope         string `json:"scope"`
		AgentID       string `json:"agent_id"`
		BackupAgentID string `json:"backup_agent_id,omitempty"`
	}
	out := make([]item, 0, len(rows))
	for _, row := range rows {
		backup := ""
		if row.BackupAgentID.Valid {
			backup = uuidToString(row.BackupAgentID)
		}
		out = append(out, item{ID: uuidToString(row.ID), Product: row.Product, Module: row.Module, Scope: row.Scope, AgentID: uuidToString(row.AgentID), BackupAgentID: backup})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) ReplaceIntakeModules(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var body intakeModulesBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, body.ProjectID, "project_id")
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
	if project.LeadType.String != "member" || !project.LeadID.Valid {
		writeErrorCode(w, http.StatusUnprocessableEntity, "intake_project_lead_missing", "project has no member lead")
		return
	}
	if !intakeCanMaintain(member, project, userID) {
		writeError(w, http.StatusForbidden, "cannot edit modules")
		return
	}
	rows := make([]db.InsertIntakeModuleParams, 0, len(body.Modules))
	seen := map[string]struct{}{}
	for i, item := range body.Modules {
		product := strings.TrimSpace(item.Product)
		module := strings.TrimSpace(item.Module)
		scope := strings.TrimSpace(item.Scope)
		if product == "" || module == "" || scope == "" {
			writeError(w, http.StatusUnprocessableEntity, "product, module, and scope are required")
			return
		}
		key := product + "\x00" + module
		if _, ok := seen[key]; ok {
			writeError(w, http.StatusUnprocessableEntity, "duplicate product and module")
			return
		}
		seen[key] = struct{}{}
		agentID, ok := parseUUIDOrBadRequest(w, item.AgentID, "agent_id")
		if !ok {
			return
		}
		if !h.intakeAgentAssignable(r, w, parseUUID(workspaceID), agentID, userID) {
			return
		}
		backup := pgtype.UUID{}
		if strings.TrimSpace(item.BackupAgentID) != "" {
			backup, ok = parseUUIDOrBadRequest(w, item.BackupAgentID, "backup_agent_id")
			if !ok {
				return
			}
			if uuidToString(backup) == uuidToString(agentID) {
				writeError(w, http.StatusUnprocessableEntity, "backup must be a different agent")
				return
			}
			if !h.intakeAgentAssignable(r, w, parseUUID(workspaceID), backup, userID) {
				return
			}
		}
		rows = append(rows, db.InsertIntakeModuleParams{
			WorkspaceID: parseUUID(workspaceID), ProjectID: projectID,
			Product: product, Module: module, Scope: scope,
			AgentID: agentID, BackupAgentID: backup, Position: int32(i),
		})
		if item.Position != 0 {
			rows[len(rows)-1].Position = item.Position
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save modules")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if err := q.DeleteIntakeModulesForProject(r.Context(), db.DeleteIntakeModulesForProjectParams{
		WorkspaceID: parseUUID(workspaceID), ProjectID: projectID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save modules")
		return
	}
	for _, row := range rows {
		if err := q.InsertIntakeModule(r.Context(), row); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save modules")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save modules")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": len(rows)})
}

func (h *Handler) ReplaceIntakeMembers(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusForbidden, "cannot edit members")
		return
	}
	var body intakeMembersBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ids := make([]pgtype.UUID, 0, len(body.UserIDs))
	seen := map[string]struct{}{}
	for _, raw := range body.UserIDs {
		id, ok := parseUUIDOrBadRequest(w, raw, "user_id")
		if !ok {
			return
		}
		if uuidToString(id) == uuidToString(project.LeadID) {
			continue
		}
		if _, ok := seen[uuidToString(id)]; ok {
			continue
		}
		seen[uuidToString(id)] = struct{}{}
		if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID: id, WorkspaceID: parseUUID(workspaceID),
		}); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "user is not a workspace member")
			return
		}
		ids = append(ids, id)
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save members")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if err := q.DeleteIntakeProjectMembers(r.Context(), db.DeleteIntakeProjectMembersParams{
		WorkspaceID: parseUUID(workspaceID), ProjectID: projectID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save members")
		return
	}
	for _, id := range ids {
		if err := q.InsertIntakeProjectMember(r.Context(), db.InsertIntakeProjectMemberParams{
			WorkspaceID: parseUUID(workspaceID), ProjectID: projectID, UserID: id,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save members")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": len(ids)})
}

func (h *Handler) intakeCallerMaintains(r *http.Request, userID string) bool {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" || userID == "" {
		return false
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID: parseUUID(userID), WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return false
	}
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	n, err := h.Queries.CountIntakeLeadProjects(r.Context(), db.CountIntakeLeadProjectsParams{
		WorkspaceID: parseUUID(workspaceID), LeadID: parseUUID(userID),
	})
	return err == nil && n > 0
}

func (h *Handler) ListIntakeMaintainProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if !h.intakeCallerMaintains(r, userID) {
		writeError(w, http.StatusForbidden, "cannot maintain projects")
		return
	}
	rows, err := h.Queries.ListIntakeMaintainProjects(r.Context(), db.ListIntakeMaintainProjectsParams{
		WorkspaceID: parseUUID(workspaceID), Column2: member.Role, LeadID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list projects")
		return
	}
	type item struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	out := make([]item, 0, len(rows))
	for _, row := range rows {
		out = append(out, item{ID: uuidToString(row.ID), Title: row.Title})
	}
	writeJSON(w, http.StatusOK, out)
}

func intakeCanMaintain(member db.Member, project db.Project, userID string) bool {
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	return project.LeadType.String == "member" && uuidToString(project.LeadID) == userID
}

func (h *Handler) intakeAgentAssignable(r *http.Request, w http.ResponseWriter, workspaceID, agentID pgtype.UUID, callerID string) bool {
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "agent is not in this workspace")
		return false
	}
	if !agent.IntakeCan.Valid || strings.TrimSpace(agent.IntakeCan.String) == "" {
		writeErrorCode(w, http.StatusUnprocessableEntity, "intake_identity_missing", "agent has no duty")
		return false
	}
	if uuidToString(agent.OwnerID) == callerID && !requestIsOperator(r) {
		writeError(w, http.StatusUnprocessableEntity, "agent owner cannot attach this agent")
		return false
	}
	return true
}
