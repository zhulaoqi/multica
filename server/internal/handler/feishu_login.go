package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FeishuProfile is the identity Feishu returns at login. It is stored on the
// user row and is not copied into intake responses or cards.
type FeishuProfile struct {
	OpenID    string
	UnionID   string
	Name      string
	Email     string
	AvatarURL string
}

// FeishuExchanger turns an authorization code into a profile. Tests supply a fake.
type FeishuExchanger interface {
	Exchange(ctx context.Context, code string) (FeishuProfile, error)
}

type feishuLoginBody struct {
	Code string `json:"code"`
}

type feishuLoginResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *Handler) FeishuLogin(w http.ResponseWriter, r *http.Request) {
	var body feishuLoginBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	if h.FeishuLoginExchanger == nil {
		writeError(w, http.StatusServiceUnavailable, "feishu login is not configured")
		return
	}
	profile, err := h.FeishuLoginExchanger.Exchange(r.Context(), strings.TrimSpace(body.Code))
	if err != nil || strings.TrimSpace(profile.OpenID) == "" || strings.TrimSpace(profile.Email) == "" {
		writeError(w, http.StatusUnauthorized, "feishu login failed")
		return
	}
	user, err := h.upsertFeishuUser(r.Context(), profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store login")
		return
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

func (h *Handler) upsertFeishuUser(ctx context.Context, profile FeishuProfile) (db.User, error) {
	identity, err := h.Queries.GetFeishuIdentityByOpenID(ctx, profile.OpenID)
	if err == nil {
		user, err := h.Queries.GetUser(ctx, identity.UserID)
		if err != nil {
			return db.User{}, err
		}
		if err := h.Queries.SaveFeishuIdentity(ctx, db.SaveFeishuIdentityParams{
			UserID: user.ID, OpenID: profile.OpenID, UnionID: profile.UnionID,
			Email: profile.Email, Name: profile.Name, AvatarUrl: profile.AvatarURL,
		}); err != nil {
			return db.User{}, err
		}
		return h.storeFeishuProfileOnUser(ctx, user.ID, profile)
	}
	if !isNotFound(err) {
		return db.User{}, err
	}
	user, err := h.Queries.GetUserByEmail(ctx, profile.Email)
	if isNotFound(err) {
		user, err = h.Queries.CreateUser(ctx, db.CreateUserParams{
			Name: profile.Name, Email: profile.Email,
			AvatarUrl: pgtype.Text{String: profile.AvatarURL, Valid: profile.AvatarURL != ""},
		})
	}
	if err != nil {
		return db.User{}, err
	}
	if err := h.Queries.SaveFeishuIdentity(ctx, db.SaveFeishuIdentityParams{
		UserID: user.ID, OpenID: profile.OpenID, UnionID: profile.UnionID,
		Email: profile.Email, Name: profile.Name, AvatarUrl: profile.AvatarURL,
	}); err != nil {
		return db.User{}, err
	}
	return h.storeFeishuProfileOnUser(ctx, user.ID, profile)
}

func (h *Handler) storeFeishuProfileOnUser(ctx context.Context, id pgtype.UUID, profile FeishuProfile) (db.User, error) {
	return h.Queries.UpdateUserNameAndAvatar(ctx, db.UpdateUserNameAndAvatarParams{
		ID: id, Name: profile.Name,
		AvatarUrl: pgtype.Text{String: profile.AvatarURL, Valid: profile.AvatarURL != ""},
	})
}
