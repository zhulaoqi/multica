package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resolveWorkspaceAccess is the service-layer entry point to the shared project
// access policy. Background paths (wakeups, dispatch, integrations) must use
// the same department scope as request handlers instead of checking for a bare
// member row.
func resolveWorkspaceAccess(ctx context.Context, q *db.Queries, workspaceID, userID pgtype.UUID) (workspaceaccess.Access, error) {
	return workspaceaccess.Resolve(ctx, q, workspaceID, userID, false)
}
