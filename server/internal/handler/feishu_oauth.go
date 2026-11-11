package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const feishuTokenURL = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
const feishuUserInfoURL = "https://open.feishu.cn/open-apis/authen/v1/user_info"
const feishuAuthorizeURL = "https://open.feishu.cn/open-apis/authen/v1/authorize"

// FeishuOAuth exchanges an authorization code with Feishu and reads the
// signed-in person's profile. Empty configuration means login stays disabled.
type FeishuOAuth struct {
	AppID       string
	AppSecret   string
	RedirectURI string
	HTTP        *http.Client
}

func NewFeishuOAuthFromEnv() *FeishuOAuth {
	appID := strings.TrimSpace(os.Getenv("MULTICA_FEISHU_APP_ID"))
	secret := strings.TrimSpace(os.Getenv("MULTICA_FEISHU_APP_SECRET"))
	redirect := strings.TrimSpace(os.Getenv("MULTICA_FEISHU_REDIRECT_URI"))
	if appID == "" || secret == "" || redirect == "" {
		return nil
	}
	return &FeishuOAuth{
		AppID: appID, AppSecret: secret, RedirectURI: redirect,
		HTTP: &http.Client{Timeout: 10 * time.Second},
	}
}

func (f *FeishuOAuth) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("app_id", f.AppID)
	q.Set("redirect_uri", f.RedirectURI)
	q.Set("state", state)
	return feishuAuthorizeURL + "?" + q.Encode()
}

func (f *FeishuOAuth) Exchange(ctx context.Context, code string) (FeishuProfile, error) {
	if f == nil || f.HTTP == nil {
		return FeishuProfile{}, fmt.Errorf("feishu oauth is not configured")
	}
	body, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     f.AppID,
		"client_secret": f.AppSecret,
		"code":          code,
		"redirect_uri":  f.RedirectURI,
	})
	if err != nil {
		return FeishuProfile{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, feishuTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return FeishuProfile{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return FeishuProfile{}, err
	}
	defer resp.Body.Close()
	var token struct {
		Code        int    `json:"code"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return FeishuProfile{}, err
	}
	if token.Code != 0 || token.AccessToken == "" {
		return FeishuProfile{}, fmt.Errorf("feishu token exchange failed")
	}
	infoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, feishuUserInfoURL, nil)
	if err != nil {
		return FeishuProfile{}, err
	}
	infoReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	infoResp, err := f.HTTP.Do(infoReq)
	if err != nil {
		return FeishuProfile{}, err
	}
	defer infoResp.Body.Close()
	var info struct {
		Code int `json:"code"`
		Data struct {
			Name      string `json:"name"`
			OpenID    string `json:"open_id"`
			UnionID   string `json:"union_id"`
			Email     string `json:"email"`
			AvatarURL string `json:"avatar_url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(infoResp.Body).Decode(&info); err != nil {
		return FeishuProfile{}, err
	}
	if info.Code != 0 || info.Data.OpenID == "" {
		return FeishuProfile{}, fmt.Errorf("feishu user info failed")
	}
	email := strings.TrimSpace(info.Data.Email)
	if email == "" {
		email = info.Data.OpenID + "@feishu.local"
	}
	return FeishuProfile{
		OpenID: info.Data.OpenID, UnionID: info.Data.UnionID,
		Name: info.Data.Name, Email: email, AvatarURL: info.Data.AvatarURL,
	}, nil
}

func (h *Handler) FeishuLoginStart(w http.ResponseWriter, r *http.Request) {
	oauth, ok := h.FeishuLoginExchanger.(*FeishuOAuth)
	if !ok || oauth == nil {
		writeError(w, http.StatusServiceUnavailable, "feishu login is not configured")
		return
	}
	http.Redirect(w, r, oauth.AuthorizeURL("intake"), http.StatusFound)
}

func (h *Handler) FeishuLoginCallback(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	r.Body = http.NoBody
	payload, _ := json.Marshal(feishuLoginBody{Code: code})
	r2 := r.Clone(r.Context())
	r2.Body = ioNopCloser(strings.NewReader(string(payload)))
	r2.ContentLength = int64(len(payload))
	r2.Header.Set("Content-Type", "application/json")
	rec := &redirectRecorder{ResponseWriter: w, header: w.Header()}
	h.FeishuLogin(rec, r2)
	if rec.status >= 400 || rec.status == 0 {
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

type redirectRecorder struct {
	http.ResponseWriter
	header http.Header
	status int
}

func (r *redirectRecorder) WriteHeader(status int) { r.status = status }

func ioNopCloser(r *strings.Reader) nopCloser { return nopCloser{r} }

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }
