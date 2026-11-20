package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Agents live in the organization workspace. A project holds projections:
// agent rows whose source_agent_id points at an organization agent and whose
// identity, instructions and model settings are copied from it. Projections
// are read-only; edits happen on the source and are pushed out from here.

const codeAgentManagedByOrganization = "agent_managed_by_organization"

var errProjectionSourceIneligible = errors.New("agent cannot be projected")

// rejectProjectionWrite answers 409 when a write targets a projection.
func rejectProjectionWrite(w http.ResponseWriter, agent db.Agent) bool {
	if !agent.SourceAgentID.Valid {
		return false
	}
	writeErrorCode(w, http.StatusConflict, codeAgentManagedByOrganization, "this agent is managed by the organization; edit it there")
	return true
}

// rejectProjectAgentCreate answers 409 when a project tries to create its own
// agent. Projects add agents from the organization instead.
func (h *Handler) rejectProjectAgentCreate(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID) bool {
	ws, err := h.Queries.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return true
	}
	if !isOrganizationProject(ws) {
		return false
	}
	if _, ok, err := h.organizationWorkspace(r.Context()); err != nil || !ok {
		// Until the organization exists, projects keep their own agents.
		return false
	}
	writeErrorCode(w, http.StatusConflict, codeAgentManagedByOrganization, "add agents to this project from the organization")
	return true
}

// isOrganizationSource reports whether a can be projected: an owned user agent
// that lives in the organization workspace.
func (h *Handler) isOrganizationSource(ctx context.Context, a db.Agent) bool {
	if a.SourceAgentID.Valid || a.Kind != "user" || a.SystemKey.String != "" || !a.OwnerID.Valid {
		return false
	}
	ws, err := h.Queries.GetWorkspace(ctx, a.WorkspaceID)
	return err == nil && ws.IsOrganization
}

func (h *Handler) publishProjection(eventType string, a db.Agent) {
	h.publish(eventType, uuidToString(a.WorkspaceID), "system", "", map[string]any{
		"agent": broadcastAgentResponse(h.agentToResponse(a)),
	})
}

// ensureProjection makes sure workspaceID has an active projection of source.
// An archived projection is restored rather than duplicated. created reports
// whether a row became active.
func (h *Handler) ensureProjection(ctx context.Context, workspaceID pgtype.UUID, source db.Agent) (db.Agent, bool, error) {
	existing, err := h.Queries.GetAgentProjection(ctx, db.GetAgentProjectionParams{
		WorkspaceID:   workspaceID,
		SourceAgentID: source.ID,
	})
	if err == nil {
		if !existing.ArchivedAt.Valid {
			return existing, false, nil
		}
		restored, err := h.Queries.RestoreAgent(ctx, existing.ID)
		if err != nil {
			return db.Agent{}, false, fmt.Errorf("restore projection: %w", err)
		}
		synced, err := h.Queries.SyncAgentProjection(ctx, restored.ID)
		if err != nil {
			return db.Agent{}, false, fmt.Errorf("sync restored projection: %w", err)
		}
		return synced, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Agent{}, false, fmt.Errorf("load projection: %w", err)
	}
	created, err := h.Queries.CreateAgentProjection(ctx, db.CreateAgentProjectionParams{
		WorkspaceID:   workspaceID,
		SourceAgentID: source.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Agent{}, false, errProjectionSourceIneligible
	}
	if err != nil {
		return db.Agent{}, false, fmt.Errorf("create projection: %w", err)
	}
	return created, true, nil
}

// syncProjectionsForSource pushes the source's current state to every active
// projection. Stale primaries are cleared first so a moved primary can land on
// the new source's projection.
func (h *Handler) syncProjectionsForSource(ctx context.Context, source db.Agent) {
	if !h.isOrganizationSource(ctx, source) {
		return
	}
	cleared, err := h.Queries.ClearStaleProjectionPrimaries(ctx, source.OwnerID)
	if err != nil {
		slog.Warn("clear stale projection primaries failed", "error", err, "owner_id", uuidToString(source.OwnerID))
	}
	for _, p := range cleared {
		h.publishProjection(protocol.EventAgentUpdated, p)
	}
	projections, err := h.Queries.ListAgentProjectionsForSource(ctx, source.ID)
	if err != nil {
		slog.Warn("list projections failed", "error", err, "source_agent_id", uuidToString(source.ID))
		return
	}
	for _, p := range projections {
		if p.ArchivedAt.Valid {
			continue
		}
		synced, err := h.Queries.SyncAgentProjection(ctx, p.ID)
		if err != nil {
			slog.Warn("sync projection failed", "error", err, "agent_id", uuidToString(p.ID), "source_agent_id", uuidToString(source.ID))
			continue
		}
		h.publishProjection(protocol.EventAgentUpdated, synced)
	}
}

// projectPrimaryTwin projects the owner's primary twin into every department
// project the owner has joined, then syncs all of its projections. Called when
// an organization agent becomes primary.
func (h *Handler) projectPrimaryTwin(ctx context.Context, source db.Agent) {
	if !source.IsPrimary || source.ArchivedAt.Valid || !h.isOrganizationSource(ctx, source) {
		return
	}
	// Clear the old primary's projections first so the new ones can take the
	// flag on insert.
	if cleared, err := h.Queries.ClearStaleProjectionPrimaries(ctx, source.OwnerID); err != nil {
		slog.Warn("clear stale projection primaries failed", "error", err, "owner_id", uuidToString(source.OwnerID))
	} else {
		for _, p := range cleared {
			h.publishProjection(protocol.EventAgentUpdated, p)
		}
	}
	projects, err := h.Queries.ListMemberProjectWorkspaces(ctx, source.OwnerID)
	if err != nil {
		slog.Warn("list member projects failed", "error", err, "owner_id", uuidToString(source.OwnerID))
		return
	}
	for _, projectID := range projects {
		projection, created, err := h.ensureProjection(ctx, projectID, source)
		if err != nil {
			slog.Warn("project primary twin failed", "error", err, "workspace_id", uuidToString(projectID), "source_agent_id", uuidToString(source.ID))
			continue
		}
		if created {
			h.publishProjection(protocol.EventAgentCreated, projection)
		}
	}
	h.syncProjectionsForSource(ctx, source)
}

// archiveProjectionsForSource archives the source's active projections with
// the source's own archived_at, so restoring the source restores exactly them.
func (h *Handler) archiveProjectionsForSource(ctx context.Context, source db.Agent) {
	if source.SourceAgentID.Valid || !source.ArchivedAt.Valid {
		return
	}
	archived, err := h.Queries.ArchiveAgentProjectionsForSource(ctx, db.ArchiveAgentProjectionsForSourceParams{
		ArchivedAt:    source.ArchivedAt,
		ArchivedBy:    source.ArchivedBy,
		SourceAgentID: source.ID,
	})
	if err != nil {
		slog.Warn("archive projections failed", "error", err, "source_agent_id", uuidToString(source.ID))
		return
	}
	for _, p := range archived {
		if _, err := h.TaskService.CancelTasksForArchivedAgent(ctx, p.ID); err != nil {
			slog.Warn("cancel projection tasks on archive failed", "error", err, "agent_id", uuidToString(p.ID))
		}
		h.publishProjection(protocol.EventAgentArchived, p)
	}
}

// restoreProjectionsForSource brings back the projections archived together
// with the source. archivedAt is the source's archived_at before the restore.
func (h *Handler) restoreProjectionsForSource(ctx context.Context, source db.Agent, archivedAt pgtype.Timestamptz) {
	if source.SourceAgentID.Valid || !archivedAt.Valid {
		return
	}
	restored, err := h.Queries.RestoreAgentProjectionsForSource(ctx, db.RestoreAgentProjectionsForSourceParams{
		SourceAgentID: source.ID,
		ArchivedAt:    archivedAt,
	})
	if err != nil {
		slog.Warn("restore projections failed", "error", err, "source_agent_id", uuidToString(source.ID))
		return
	}
	for _, p := range restored {
		h.publishProjection(protocol.EventAgentRestored, p)
	}
}

// syncProjectionsForDaemon remaps the runtimes of projections whose source
// runs on daemonID. A daemon registering in a project is when the project
// runtime those projections map to appears.
func (h *Handler) syncProjectionsForDaemon(ctx context.Context, workspaceID pgtype.UUID, daemonID string) {
	if daemonID == "" {
		return
	}
	projections, err := h.Queries.ListAgentProjectionsForDaemon(ctx, db.ListAgentProjectionsForDaemonParams{
		WorkspaceID: workspaceID,
		DaemonID:    pgtype.Text{String: daemonID, Valid: true},
	})
	if err != nil {
		slog.Warn("list projections for daemon failed", "error", err, "workspace_id", uuidToString(workspaceID), "daemon_id", daemonID)
		return
	}
	for _, p := range projections {
		synced, err := h.Queries.SyncAgentProjection(ctx, p.ID)
		if err != nil {
			slog.Warn("sync projection for daemon failed", "error", err, "agent_id", uuidToString(p.ID))
			continue
		}
		if synced.RuntimeID != p.RuntimeID {
			h.publishProjection(protocol.EventAgentUpdated, synced)
		}
	}
}

// ensurePrimaryTwinInWorkspace gives a member their digital twin in a newly
// joined workspace. In a department project that is a projection of their
// organization primary twin; elsewhere the oldest owned agent is promoted. The
// function is best-effort: callers log failures and still complete the join,
// because binding is a separate step the member can finish later.
func (h *Handler) ensurePrimaryTwinInWorkspace(ctx context.Context, workspaceID, userID pgtype.UUID) error {
	if !workspaceID.Valid || !userID.Valid {
		return nil
	}
	org, ok, err := h.organizationWorkspace(ctx)
	if err != nil {
		return err
	}
	if !ok || org.ID == workspaceID {
		_, err := h.Queries.EnsurePrimaryAgentForOwner(ctx, db.EnsurePrimaryAgentForOwnerParams{
			WorkspaceID: workspaceID,
			OwnerID:     userID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	source, err := h.Queries.GetPrimaryAgentForOwner(ctx, db.GetPrimaryAgentForOwnerParams{
		WorkspaceID: org.ID,
		OwnerID:     userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	projection, created, err := h.ensureProjection(ctx, workspaceID, source)
	if err != nil {
		return err
	}
	if created {
		h.publishProjection(protocol.EventAgentCreated, projection)
	}
	return nil
}

type projectionCandidateResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	AvatarURL   *string `json:"avatar_url"`
	OwnerID     string  `json:"owner_id"`
	IsPrimary   bool    `json:"is_primary"`
	Projected   bool    `json:"projected"`
}

// projectionProject resolves the caller's current workspace as a department
// project they manage, plus the organization it projects from.
func (h *Handler) projectionProject(w http.ResponseWriter, r *http.Request) (db.Workspace, db.Workspace, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceManager(w, r, workspaceID, "workspace not found"); !ok {
		return db.Workspace{}, db.Workspace{}, false
	}
	project, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return db.Workspace{}, db.Workspace{}, false
	}
	if !isOrganizationProject(project) {
		writeErrorCode(w, http.StatusConflict, codeOrganizationManaged, "only department projects take agents from the organization")
		return db.Workspace{}, db.Workspace{}, false
	}
	org, ok := h.requireOrganizationWorkspace(w, r)
	if !ok {
		return db.Workspace{}, db.Workspace{}, false
	}
	return project, org, true
}

// ListProjectionCandidates lists the organization agents a project manager may
// add: those owned by people who have joined the project.
func (h *Handler) ListProjectionCandidates(w http.ResponseWriter, r *http.Request) {
	project, org, ok := h.projectionProject(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListProjectionCandidates(r.Context(), db.ListProjectionCandidatesParams{
		ProjectID:      project.ID,
		OrganizationID: org.ID,
	})
	if err != nil {
		slog.Warn("list projection candidates failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list organization agents")
		return
	}
	out := make([]projectionCandidateResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, projectionCandidateResponse{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Description: row.Description,
			AvatarURL:   h.resolveAvatarURLPtr(textToPtr(row.AvatarUrl)),
			OwnerID:     uuidToString(row.OwnerID),
			IsPrimary:   row.IsPrimary,
			Projected:   row.Projected,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type createAgentProjectionsRequest struct {
	AgentIDs []string `json:"agent_ids"`
}

// CreateAgentProjections adds organization agents to the current project.
// Every id must be a current candidate; the request is rejected as a whole
// otherwise.
func (h *Handler) CreateAgentProjections(w http.ResponseWriter, r *http.Request) {
	project, org, ok := h.projectionProject(w, r)
	if !ok {
		return
	}
	var req createAgentProjectionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.AgentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "agent_ids is required")
		return
	}
	rows, err := h.Queries.ListProjectionCandidates(r.Context(), db.ListProjectionCandidatesParams{
		ProjectID:      project.ID,
		OrganizationID: org.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list organization agents")
		return
	}
	candidates := make(map[pgtype.UUID]db.Agent, len(rows))
	for _, row := range rows {
		candidates[row.ID] = candidateAgent(row)
	}
	sources := make([]db.Agent, 0, len(req.AgentIDs))
	for _, raw := range req.AgentIDs {
		id, ok := parseUUIDOrBadRequest(w, raw, "agent_ids")
		if !ok {
			return
		}
		source, ok := candidates[id]
		if !ok {
			writeError(w, http.StatusNotFound, "organization agent not found among this project's members")
			return
		}
		sources = append(sources, source)
	}

	out := make([]AgentResponse, 0, len(sources))
	for _, source := range sources {
		projection, created, err := h.ensureProjection(r.Context(), project.ID, source)
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, fmt.Sprintf("this project already has an agent named %q", source.Name))
				return
			}
			slog.Warn("create projection failed", append(logger.RequestAttrs(r), "error", err, "source_agent_id", uuidToString(source.ID))...)
			writeError(w, http.StatusInternalServerError, "failed to add agent")
			return
		}
		if created {
			h.publishProjection(protocol.EventAgentCreated, projection)
		}
		resp := h.agentToResponse(projection)
		redactMcpConfig(&resp)
		out = append(out, resp)
	}
	slog.Info("agent projections added", append(logger.RequestAttrs(r), "workspace_id", uuidToString(project.ID), "count", len(out))...)
	writeJSON(w, http.StatusCreated, out)
}

// candidateAgent keeps the fields ensureProjection reads from a candidate row.
func candidateAgent(row db.ListProjectionCandidatesRow) db.Agent {
	return db.Agent{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		OwnerID:     row.OwnerID,
		Kind:        row.Kind,
		IsPrimary:   row.IsPrimary,
	}
}
