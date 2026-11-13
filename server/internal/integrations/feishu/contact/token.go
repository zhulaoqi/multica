package contact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const tokenPath = "/open-apis/auth/v3/tenant_access_token/internal"

// tokenSafetyMargin refreshes the cached token this long before Feishu expires
// it, so a request in flight never carries a token that expires mid-call.
const tokenSafetyMargin = 5 * time.Minute

// tenantAccessToken returns a cached tenant token or mints a new one. The
// cached path only reads under the mutex; concurrent cold callers each mint a
// token rather than serialize on a network round trip, which is harmless
// because Feishu tokens are valid for roughly two hours.
func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	if c.cfg.AppID == "" || c.cfg.AppSecret == "" {
		return "", ErrNotConfigured
	}
	now := c.cfg.Now()
	c.mu.Lock()
	if c.token != "" && c.tokenExpiresAt.After(now) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	body, err := json.Marshal(map[string]string{
		"app_id":     c.cfg.AppID,
		"app_secret": c.cfg.AppSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+tokenPath, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("feishu contact: tenant_access_token: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("feishu contact: tenant_access_token: %w", err)
	}
	var payload struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int64  `json:"expire"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("feishu contact: tenant_access_token: decode response (status %d): %w", resp.StatusCode, err)
	}
	if payload.Code != 0 || payload.TenantAccessToken == "" {
		return "", fmt.Errorf("feishu contact: tenant_access_token: %w", feishuError(payload.Code, payload.Msg))
	}

	expire := time.Duration(payload.Expire) * time.Second
	// A misbehaving upstream must not produce an already-expired cache entry.
	if expire < tokenSafetyMargin*2 {
		expire = tokenSafetyMargin * 2
	}
	c.mu.Lock()
	c.token = payload.TenantAccessToken
	c.tokenExpiresAt = c.cfg.Now().Add(expire - tokenSafetyMargin)
	c.mu.Unlock()
	return payload.TenantAccessToken, nil
}
