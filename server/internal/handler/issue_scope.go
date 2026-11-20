package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// requestIssueScope resolves the caller's department scope for issue reads in
// the request's workspace. A nil scope means every issue is visible; errors
// fail closed for the caller to report.
func (h *Handler) requestIssueScope(r *http.Request) (*workspaceaccess.IssueScope, error) {
	access, ok := ctxWorkspaceAccess(r.Context())
	if !ok {
		userID := requestUserID(r)
		workspaceID := h.resolveWorkspaceID(r)
		userUUID, err := util.ParseUUID(userID)
		if err != nil {
			return nil, err
		}
		wsUUID, err := util.ParseUUID(workspaceID)
		if err != nil {
			return nil, err
		}
		access, err = workspaceaccess.Resolve(r.Context(), h.Queries, wsUUID, userUUID, requestIsOperator(r))
		if err != nil {
			return nil, err
		}
	}
	return workspaceaccess.ResolveIssueScope(r.Context(), h.Queries, access)
}

// requireIssueScope is requestIssueScope for list handlers: a failure writes
// 500 and returns ok=false.
func (h *Handler) requireIssueScope(w http.ResponseWriter, r *http.Request) (*workspaceaccess.IssueScope, bool) {
	scope, err := h.requestIssueScope(r)
	if err != nil {
		slog.Warn("resolve issue scope failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to resolve issue visibility")
		return nil, false
	}
	return scope, true
}

// issueVisible reports whether scope admits issue. A nil scope admits all.
func (h *Handler) issueVisible(ctx context.Context, scope *workspaceaccess.IssueScope, issue db.Issue) (bool, error) {
	return scope.Allows(ctx, h.DB, issue)
}

// filterVisible keeps the items whose issue id scope admits, in order. One
// query covers the whole page.
func filterVisible[T any](ctx context.Context, h *Handler, scope *workspaceaccess.IssueScope, items []T, issueID func(T) pgtype.UUID) ([]T, error) {
	if scope == nil || len(items) == 0 {
		return items, nil
	}
	ids := make([]pgtype.UUID, len(items))
	for i, item := range items {
		ids[i] = issueID(item)
	}
	visible, err := scope.VisibleIDs(ctx, h.DB, ids)
	if err != nil {
		return nil, err
	}
	kept := make([]T, 0, len(items))
	for _, item := range items {
		if visible[issueID(item)] {
			kept = append(kept, item)
		}
	}
	return kept, nil
}

// visibleChildIssues drops the children outside the caller's scope.
func (h *Handler) visibleChildIssues(w http.ResponseWriter, r *http.Request, children []db.Issue) ([]db.Issue, bool) {
	scope, ok := h.requireIssueScope(w, r)
	if !ok {
		return nil, false
	}
	visible, err := filterVisible(r.Context(), h, scope, children, func(i db.Issue) pgtype.UUID { return i.ID })
	if err != nil {
		slog.Warn("filter child issues failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list child issues")
		return nil, false
	}
	return visible, true
}

// requireIssueIDVisible answers 404 with notFound when issueID names an
// organization issue outside the caller's scope. An unset issueID (a
// workspace-level attachment) is always visible.
func (h *Handler) requireIssueIDVisible(w http.ResponseWriter, r *http.Request, issueID pgtype.UUID, notFound string) bool {
	if !issueID.Valid {
		return true
	}
	issue, err := h.Queries.GetIssue(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, notFound)
		return false
	}
	if !issue.OrgUnitID.Valid {
		return true
	}
	scope, err := h.requestIssueScope(r)
	if err == nil {
		var visible bool
		visible, err = h.issueVisible(r.Context(), scope, issue)
		if err == nil && visible {
			return true
		}
	}
	if err != nil {
		slog.Warn("issue visibility check failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	writeError(w, http.StatusNotFound, notFound)
	return false
}
