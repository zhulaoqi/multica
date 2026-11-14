package handler

import (
	"log/slog"
	"net/http"
	"time"
)

// operatorChannelCount is one channel type's bot count inside a workspace.
type operatorChannelCount struct {
	Type    string `json:"type"`
	Active  int64  `json:"active"`
	Revoked int64  `json:"revoked"`
}

// operatorUsageSummary is the trailing-window token and cost rollup for one
// workspace. cost_usd_ticks is 1e-10 USD per tick.
type operatorUsageSummary struct {
	TaskCount        int64 `json:"task_count"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	CostUSDTicks     int64 `json:"cost_usd_ticks"`
}

// operatorWorkspaceOverview is one row of the platform console.
type operatorWorkspaceOverview struct {
	ID          string                 `json:"id"`
	Slug        string                 `json:"slug"`
	Name        string                 `json:"name"`
	CreatedAt   time.Time              `json:"created_at"`
	OwnerEmails []string               `json:"owner_emails"`
	OwnerCount  int64                  `json:"owner_count"`
	AdminCount  int64                  `json:"admin_count"`
	MemberCount int64                  `json:"member_count"`
	AgentCount  int64                  `json:"agent_count"`
	Channels    []operatorChannelCount `json:"channels"`
	Usage7d     operatorUsageSummary   `json:"usage_7d"`
}

type operatorOverviewResponse struct {
	Workspaces  []operatorWorkspaceOverview `json:"workspaces"`
	GeneratedAt time.Time                   `json:"generated_at"`
}

// operatorUsageWindow is how far back the console's usage column looks.
const operatorUsageWindow = "7 days"

// OperatorOverview returns every workspace with the counts an operator needs
// to triage: who owns it, how many people and agents it has, which channel
// bots are installed, and what it burned in the last week. It is read-only;
// row actions link into the workspace's own settings pages.
func (h *Handler) OperatorOverview(w http.ResponseWriter, r *http.Request) {
	rows, err := h.operatorWorkspaceRows(r)
	if err != nil {
		slog.Error("operator overview: workspace rows", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load overview")
		return
	}
	owners, err := h.operatorOwnerEmails(r)
	if err != nil {
		slog.Error("operator overview: owners", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load overview")
		return
	}
	channels, err := h.operatorChannelCounts(r)
	if err != nil {
		slog.Error("operator overview: channels", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load overview")
		return
	}
	usage, err := h.operatorUsage7d(r)
	if err != nil {
		slog.Error("operator overview: usage", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load overview")
		return
	}

	for i := range rows {
		ws := &rows[i]
		ws.OwnerEmails = owners[ws.ID]
		if ws.OwnerEmails == nil {
			ws.OwnerEmails = []string{}
		}
		ws.Channels = channels[ws.ID]
		if ws.Channels == nil {
			ws.Channels = []operatorChannelCount{}
		}
		if u, ok := usage[ws.ID]; ok {
			ws.Usage7d = u
		}
	}
	if rows == nil {
		rows = []operatorWorkspaceOverview{}
	}

	writeJSON(w, http.StatusOK, operatorOverviewResponse{
		Workspaces:  rows,
		GeneratedAt: time.Now().UTC(),
	})
}

func (h *Handler) operatorWorkspaceRows(r *http.Request) ([]operatorWorkspaceOverview, error) {
	rows, err := h.DB.Query(r.Context(), `
SELECT w.id::text, w.slug, w.name, w.created_at,
       COUNT(*) FILTER (WHERE m.role = 'owner')  AS owner_count,
       COUNT(*) FILTER (WHERE m.role = 'admin')  AS admin_count,
       COUNT(*) FILTER (WHERE m.role = 'member') AS member_count,
       (SELECT COUNT(*) FROM agent a WHERE a.workspace_id = w.id) AS agent_count
FROM workspace w
LEFT JOIN member m ON m.workspace_id = w.id
GROUP BY w.id, w.slug, w.name, w.created_at
ORDER BY w.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []operatorWorkspaceOverview{}
	for rows.Next() {
		var item operatorWorkspaceOverview
		if err := rows.Scan(
			&item.ID, &item.Slug, &item.Name, &item.CreatedAt,
			&item.OwnerCount, &item.AdminCount, &item.MemberCount, &item.AgentCount,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) operatorOwnerEmails(r *http.Request) (map[string][]string, error) {
	rows, err := h.DB.Query(r.Context(), `
SELECT m.workspace_id::text, u.email
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.role = 'owner'
ORDER BY m.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var workspaceID, email string
		if err := rows.Scan(&workspaceID, &email); err != nil {
			return nil, err
		}
		out[workspaceID] = append(out[workspaceID], email)
	}
	return out, rows.Err()
}

func (h *Handler) operatorChannelCounts(r *http.Request) (map[string][]operatorChannelCount, error) {
	rows, err := h.DB.Query(r.Context(), `
SELECT workspace_id::text, channel_type,
       COUNT(*) FILTER (WHERE status = 'active')  AS active,
       COUNT(*) FILTER (WHERE status = 'revoked') AS revoked
FROM channel_installation
GROUP BY workspace_id, channel_type
ORDER BY channel_type ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]operatorChannelCount{}
	for rows.Next() {
		var workspaceID string
		var item operatorChannelCount
		if err := rows.Scan(&workspaceID, &item.Type, &item.Active, &item.Revoked); err != nil {
			return nil, err
		}
		out[workspaceID] = append(out[workspaceID], item)
	}
	return out, rows.Err()
}

func (h *Handler) operatorUsage7d(r *http.Request) (map[string]operatorUsageSummary, error) {
	rows, err := h.DB.Query(r.Context(), `
SELECT workspace_id::text,
       COALESCE(SUM(task_count), 0),
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(cache_read_tokens), 0),
       COALESCE(SUM(cache_write_tokens), 0),
       COALESCE(SUM(cost_usd_ticks), 0)
FROM task_usage_hourly
WHERE bucket_hour >= now() - $1::interval
GROUP BY workspace_id`, operatorUsageWindow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]operatorUsageSummary{}
	for rows.Next() {
		var workspaceID string
		var item operatorUsageSummary
		if err := rows.Scan(
			&workspaceID, &item.TaskCount, &item.InputTokens, &item.OutputTokens,
			&item.CacheReadTokens, &item.CacheWriteTokens, &item.CostUSDTicks,
		); err != nil {
			return nil, err
		}
		out[workspaceID] = item
	}
	return out, rows.Err()
}
