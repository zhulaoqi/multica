// Package contact reads a Feishu tenant's department tree and the people in
// each department. It is the directory source for organisation sync and stays
// separate from the chat bridge in internal/integrations/lark, which is bound
// to installation credentials and messaging APIs.
package contact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL  = "https://open.feishu.cn"
	departmentsPath = "/open-apis/contact/v3/departments"
	departmentUsers = "/open-apis/contact/v3/users/find_by_department"
	pageSize        = 50
	// rootDepartmentID is what Feishu uses as the parent of top-level departments.
	rootDepartmentID = "0"
	// maxPages bounds a runaway paginator; 200 pages covers ten thousand records.
	maxPages = 200
)

// Config carries the self-built app credentials and the knobs tests need.
type Config struct {
	AppID        string
	AppSecret    string
	BaseURL      string
	HTTP         *http.Client
	Now          func() time.Time
	PageInterval time.Duration
}

// Department is one node of the Feishu department tree.
type Department struct {
	// ID is the open_department_id, stable for the app and stored as the
	// workspace unit's external_id.
	ID       string
	ParentID string
	Name     string
	// LeadOpenID is the department leader's open_id, empty when unset or when
	// the app may read departments but not their leaders.
	LeadOpenID string
}

// User is one person in a department. Email is empty when the app lacks the
// email field scope or the tenant hides it.
type User struct {
	OpenID    string
	UnionID   string
	Name      string
	Email     string
	AvatarURL string
	// DepartmentID is the open_department_id of the first department the person
	// appeared in; that department becomes their unit.
	DepartmentID string
}

// Client calls the Feishu contact APIs with a cached tenant_access_token.
type Client struct {
	cfg Config
	mu  sync.Mutex
	// token and tokenExpiresAt hold the tenant_access_token shared by every
	// contact call; it is refreshed lazily once inside the safety margin.
	token          string
	tokenExpiresAt time.Time
}

// NewFromEnv builds a client from MULTICA_FEISHU_APP_ID and
// MULTICA_FEISHU_APP_SECRET, the same self-built login app the workspace
// already uses. It returns nil when either credential is missing so callers can
// report "not configured" instead of failing every request.
func NewFromEnv() *Client {
	appID := strings.TrimSpace(os.Getenv("MULTICA_FEISHU_APP_ID"))
	secret := strings.TrimSpace(os.Getenv("MULTICA_FEISHU_APP_SECRET"))
	if appID == "" || secret == "" {
		return nil
	}
	return New(Config{AppID: appID, AppSecret: secret})
}

// New builds a client. Zero values in cfg fall back to production defaults so
// only tests need to populate the optional fields.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PageInterval == 0 {
		cfg.PageInterval = 100 * time.Millisecond
	}
	return &Client{cfg: cfg}
}

// ListDepartments walks the whole tree under the tenant root, following
// pagination. The returned order is Feishu's; callers sort by depth themselves.
func (c *Client) ListDepartments(ctx context.Context) ([]Department, error) {
	type item struct {
		OpenDepartmentID string `json:"open_department_id"`
		ParentID         string `json:"parent_department_id"`
		Name             string `json:"name"`
		LeaderUserID     string `json:"leader_user_id"`
	}
	var out []Department
	var pageToken string
	for page := 0; page < maxPages; page++ {
		if err := c.waitBetweenPages(ctx, page); err != nil {
			return nil, err
		}
		query := url.Values{}
		query.Set("parent_department_id", rootDepartmentID)
		query.Set("fetch_child", "true")
		query.Set("department_id_type", "open_department_id")
		query.Set("user_id_type", "open_id")
		query.Set("page_size", fmt.Sprintf("%d", pageSize))
		if pageToken != "" {
			query.Set("page_token", pageToken)
		}
		var data struct {
			Items     []item `json:"items"`
			HasMore   bool   `json:"has_more"`
			PageToken string `json:"page_token"`
		}
		if err := c.get(ctx, departmentsPath+"?"+query.Encode(), &data); err != nil {
			return nil, fmt.Errorf("feishu contact: list departments: %w", err)
		}
		for _, entry := range data.Items {
			if entry.OpenDepartmentID == "" {
				continue
			}
			out = append(out, Department{
				ID:         entry.OpenDepartmentID,
				ParentID:   normalizeParent(entry.ParentID),
				Name:       entry.Name,
				LeadOpenID: entry.LeaderUserID,
			})
		}
		if !data.HasMore || data.PageToken == "" {
			return out, nil
		}
		pageToken = data.PageToken
	}
	return nil, fmt.Errorf("feishu contact: department pagination exceeded %d pages", maxPages)
}

// ListDepartmentUsers reads the people in each department, one paginated call
// per department, and returns them deduplicated by open_id in first-seen order.
// Feishu allows one person in several departments; the first department wins.
func (c *Client) ListDepartmentUsers(ctx context.Context, departmentIDs []string) ([]User, error) {
	seen := make(map[string]struct{}, len(departmentIDs))
	out := make([]User, 0, len(departmentIDs))
	first := true
	for _, departmentID := range departmentIDs {
		if departmentID == "" {
			continue
		}
		users, err := c.listDepartmentUsers(ctx, departmentID, &first)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if _, ok := seen[user.OpenID]; ok {
				continue
			}
			seen[user.OpenID] = struct{}{}
			out = append(out, user)
		}
	}
	return out, nil
}

func (c *Client) listDepartmentUsers(ctx context.Context, departmentID string, first *bool) ([]User, error) {
	type item struct {
		OpenID  string `json:"open_id"`
		UnionID string `json:"union_id"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Avatar  struct {
			Avatar240 string `json:"avatar_240"`
			Avatar72  string `json:"avatar_72"`
		} `json:"avatar"`
	}
	var out []User
	var pageToken string
	for page := 0; page < maxPages; page++ {
		if err := c.waitBetweenCalls(ctx, first); err != nil {
			return nil, err
		}
		query := url.Values{}
		query.Set("department_id", departmentID)
		query.Set("department_id_type", "open_department_id")
		query.Set("user_id_type", "open_id")
		query.Set("page_size", fmt.Sprintf("%d", pageSize))
		if pageToken != "" {
			query.Set("page_token", pageToken)
		}
		var data struct {
			Items     []item `json:"items"`
			HasMore   bool   `json:"has_more"`
			PageToken string `json:"page_token"`
		}
		if err := c.get(ctx, departmentUsers+"?"+query.Encode(), &data); err != nil {
			return nil, fmt.Errorf("feishu contact: list users of %s: %w", departmentID, err)
		}
		for _, entry := range data.Items {
			if entry.OpenID == "" {
				continue
			}
			avatar := entry.Avatar.Avatar240
			if avatar == "" {
				avatar = entry.Avatar.Avatar72
			}
			out = append(out, User{
				OpenID:       entry.OpenID,
				UnionID:      entry.UnionID,
				Name:         entry.Name,
				Email:        strings.TrimSpace(entry.Email),
				AvatarURL:    avatar,
				DepartmentID: departmentID,
			})
		}
		if !data.HasMore || data.PageToken == "" {
			return out, nil
		}
		pageToken = data.PageToken
	}
	return nil, fmt.Errorf("feishu contact: user pagination exceeded %d pages", maxPages)
}

// waitBetweenPages keeps consecutive calls under Feishu's per-app rate limits.
// The first call of a run passes page=0 and does not sleep.
func (c *Client) waitBetweenPages(ctx context.Context, page int) error {
	if page == 0 || c.cfg.PageInterval <= 0 {
		return nil
	}
	return c.pause(ctx)
}

// waitBetweenCalls is the department-user variant: the first request of the
// whole run is free, every later one waits.
func (c *Client) waitBetweenCalls(ctx context.Context, first *bool) error {
	if *first {
		*first = false
		return nil
	}
	return c.pause(ctx)
}

func (c *Client) pause(ctx context.Context) error {
	timer := time.NewTimer(c.cfg.PageInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// get performs one authenticated GET and decodes the data envelope. A non-zero
// Feishu code becomes an error that names the failing call and, for the two
// common scope errors, says how to fix it.
func (c *Client) get(ctx context.Context, path string, out any) error {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	if envelope.Code != 0 {
		return feishuError(envelope.Code, envelope.Msg)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	if out == nil || len(envelope.Data) == 0 {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

// feishuError turns a Feishu error code into an actionable message. The scope
// codes are the ones an admin hits first when the contact scopes were never
// granted to the login app.
func feishuError(code int, msg string) error {
	switch code {
	case 99991672, 41050, 99991679:
		return fmt.Errorf("feishu code %d: %s (grant the app contact:department.base:readonly, contact:user.base:readonly and contact:user.email:readonly, then publish a new app version)", code, msg)
	case 99991663:
		return fmt.Errorf("feishu code %d: %s (the tenant_access_token is invalid; check MULTICA_FEISHU_APP_ID/SECRET)", code, msg)
	default:
		return fmt.Errorf("feishu code %d: %s", code, msg)
	}
}

// normalizeParent maps Feishu's root sentinel to the empty parent a workspace
// root unit uses.
func normalizeParent(parent string) string {
	if parent == rootDepartmentID {
		return ""
	}
	return parent
}

// ErrNotConfigured is returned by callers that require a configured client.
var ErrNotConfigured = errors.New("feishu contact is not configured")
