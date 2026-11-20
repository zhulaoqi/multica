package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The organization workspace is the single source of the department tree and
// the company roster. Every other department-owned workspace is a project that
// points into that tree.

const (
	codeOrganizationManaged = "organization_managed"
	codeOrganizationMissing = "organization_missing"
)

var errOrganizationMissing = errors.New("no organization workspace is configured")

// organizationWorkspace returns the organization workspace, or ok=false when
// none has been created yet.
func (h *Handler) organizationWorkspace(ctx context.Context) (db.Workspace, bool, error) {
	ws, err := h.Queries.GetOrganizationWorkspace(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Workspace{}, false, nil
	}
	if err != nil {
		return db.Workspace{}, false, fmt.Errorf("load organization workspace: %w", err)
	}
	return ws, true, nil
}

// requireOrganizationWorkspace writes 409 organization_missing when the
// organization workspace does not exist yet.
func (h *Handler) requireOrganizationWorkspace(w http.ResponseWriter, r *http.Request) (db.Workspace, bool) {
	org, ok, err := h.organizationWorkspace(r.Context())
	if err != nil {
		slog.Error("load organization workspace failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load the organization")
		return db.Workspace{}, false
	}
	if !ok {
		writeErrorCode(w, http.StatusConflict, codeOrganizationMissing, errOrganizationMissing.Error())
		return db.Workspace{}, false
	}
	return org, true
}

// writeOrganizationManaged answers a tree edit attempted from a project: the
// organization workspace owns the tree.
func writeOrganizationManaged(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusConflict, codeOrganizationManaged, "edit the department tree in the organization workspace")
}

// isOrganizationProject reports whether ws reads its department tree from the
// organization workspace instead of owning one.
func isOrganizationProject(ws db.Workspace) bool {
	return !ws.IsOrganization && ws.OrgUnitID.Valid
}

// healProjectLeadOwners makes each project's owning-department lead a stored
// owner. unitID narrows the pass to projects owned by one unit; an invalid
// unitID heals every project.
func (h *Handler) healProjectLeadOwners(ctx context.Context, q *db.Queries, unitID pgtype.UUID) ([]string, error) {
	rows, err := q.ListProjectsMissingLeadOwner(ctx, unitID)
	if err != nil {
		return nil, fmt.Errorf("list projects missing their lead: %w", err)
	}
	leads := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, err := q.GetUser(ctx, row.LeadUserID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("load lead: %w", err)
		}
		if err := h.ensureProjectOwner(ctx, q, row.WorkspaceID, row.LeadUserID, row.RootUnitID); err != nil {
			return nil, fmt.Errorf("make lead a project owner: %w", err)
		}
		leads = append(leads, uuidToString(row.LeadUserID))
	}
	return leads, nil
}
