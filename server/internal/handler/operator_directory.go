package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// operatorDirectoryWorkspace is one workspace's directory as the console shows it.
type operatorDirectoryWorkspace struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	UnitCount     int    `json:"unit_count"`
	PlacedMembers int    `json:"placed_members"`
	LastSyncedAt  string `json:"last_synced_at"`
	CreatedUnits  int    `json:"created_units"`
	UpdatedUnits  int    `json:"updated_units"`
	MembersAdded  int    `json:"members_added"`
	MembersMoved  int    `json:"members_moved"`
}

type operatorDirectoryResponse struct {
	Configured bool                         `json:"configured"`
	Schedule   string                       `json:"schedule"`
	Workspaces []operatorDirectoryWorkspace `json:"workspaces"`
}

// OperatorDirectories lists every workspace's organisation and the last
// Feishu directory sync. Only the operator account can call it.
func (h *Handler) OperatorDirectories(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(r.Context(), `
SELECT w.id, w.name, w.slug,
       (SELECT count(*) FROM org_unit u WHERE u.workspace_id = w.id)::int,
       (SELECT count(*) FROM member m WHERE m.workspace_id = w.id AND m.org_unit_id IS NOT NULL)::int,
       COALESCE(run.summary->>'synced_at', ''),
       COALESCE(jsonb_array_length(run.summary->'created_units'), 0),
       COALESCE(jsonb_array_length(run.summary->'updated_units'), 0),
       COALESCE(jsonb_array_length(run.summary->'members_added'), 0),
       COALESCE(jsonb_array_length(run.summary->'members_moved'), 0)
FROM workspace w
LEFT JOIN LATERAL (
    SELECT summary FROM org_sync_run
    WHERE workspace_id = w.id
    ORDER BY created_at DESC
    LIMIT 1
) run ON true
ORDER BY w.name ASC`)
	if err != nil {
		slog.Error("operator directories", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list directories")
		return
	}
	defer rows.Close()
	items := []operatorDirectoryWorkspace{}
	for rows.Next() {
		var item operatorDirectoryWorkspace
		var id pgtype.UUID
		if err := rows.Scan(
			&id, &item.Name, &item.Slug,
			&item.UnitCount, &item.PlacedMembers, &item.LastSyncedAt,
			&item.CreatedUnits, &item.UpdatedUnits, &item.MembersAdded, &item.MembersMoved,
		); err != nil {
			slog.Error("operator directories scan", "error", err)
			writeError(w, http.StatusInternalServerError, "could not list directories")
			return
		}
		item.ID = uuidToString(id)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("operator directories", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list directories")
		return
	}
	writeJSON(w, http.StatusOK, operatorDirectoryResponse{
		Configured: h.OrgDirectory != nil,
		Schedule:   "0 0 * * * Asia/Shanghai",
		Workspaces: items,
	})
}

// OperatorSyncDirectory runs one workspace's Feishu directory sync from the
// operator console. The route is operator-only; workspace role is not required.
func (h *Handler) OperatorSyncDirectory(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspace id")
	if !ok {
		return
	}
	var exists bool
	if err := h.DB.QueryRow(r.Context(), `SELECT true FROM workspace WHERE id = $1`, wsUUID).Scan(&exists); err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	report, err := h.syncDirectory(r.Context(), wsUUID)
	if err != nil {
		h.writeDirectorySyncError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
