package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type operatorLoginBody struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// OperatorLogin signs in the developer account. The built-in account name is
// admin. MULTICA_OPERATOR_NAME and MULTICA_OPERATOR_PASSWORD override it.
// Permission checks treat that session as an owner of every workspace.
func (h *Handler) OperatorLogin(w http.ResponseWriter, r *http.Request) {
	if !auth.OperatorConfigured() {
		writeError(w, http.StatusServiceUnavailable, "operator login is not configured")
		return
	}
	var body operatorLoginBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if !auth.OperatorPasswordMatches(name, body.Password) {
		writeError(w, http.StatusUnauthorized, "operator login failed")
		return
	}
	user, err := h.Queries.GetUserByEmail(r.Context(), auth.OperatorMailbox())
	if isNotFound(err) {
		user, err = h.Queries.CreateUser(r.Context(), db.CreateUserParams{
			Name: auth.OperatorName(), Email: auth.OperatorMailbox(),
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store login")
		return
	}
	if !user.OnboardedAt.Valid {
		user, err = h.Queries.MarkUserOnboarded(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not store login")
			return
		}
	}
	token, err := h.issueJWT(user)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "could not start session")
		return
	}
	if err := auth.SetAuthCookies(w, token); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, http.StatusOK, feishuLoginResponse{ID: uuidToString(user.ID), Name: user.Name})
}

func requestIsOperator(r *http.Request) bool {
	return r.Header.Get(auth.OperatorHeader) == "1"
}

func operatorMember(userID, workspaceID string) db.Member {
	return db.Member{
		ID:          parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Role:        "owner",
	}
}

// listOperatorWorkspaces returns every workspace. The developer account is
// not inserted into member rows.
func (h *Handler) listOperatorWorkspaces(r *http.Request) ([]db.Workspace, error) {
	rows, err := h.DB.Query(r.Context(), `
SELECT id, name, slug, description, settings,
       created_at, updated_at, context, repos,
       issue_prefix, issue_counter, avatar_url, attribution_fail_closed
FROM workspace
ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []db.Workspace{}
	for rows.Next() {
		var item db.Workspace
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Slug, &item.Description, &item.Settings,
			&item.CreatedAt, &item.UpdatedAt, &item.Context, &item.Repos,
			&item.IssuePrefix, &item.IssueCounter, &item.AvatarUrl, &item.AttributionFailClosed,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
