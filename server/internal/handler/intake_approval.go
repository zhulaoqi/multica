package handler

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// intakeApprovalLookup scopes service.IntakeApprovals to one list request.
type intakeApprovalLookup struct {
	r           *http.Request
	workspaceID pgtype.UUID
	approvals   *service.IntakeApprovals
}

func (h *Handler) newIntakeApprovalLookup(r *http.Request, workspaceID pgtype.UUID) *intakeApprovalLookup {
	return &intakeApprovalLookup{r: r, workspaceID: workspaceID, approvals: service.NewIntakeApprovals(h.Queries)}
}

func (l *intakeApprovalLookup) awaits(projectID pgtype.UUID, status string) bool {
	awaiting, err := l.approvals.Awaits(l.r.Context(), l.workspaceID, projectID, status)
	if err != nil {
		// The list still renders; the item just shows its plain progress.
		slog.Warn("load intake project workflow failed", append(logger.RequestAttrs(l.r), "error", err, "project_id", uuidToString(projectID))...)
	}
	return awaiting
}
