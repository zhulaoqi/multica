package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// envFromApollo is copied into the process only when that variable is still empty.
// redis.host / redis.port / redis.password / redis.db are assembled into REDIS_URL.
// A key missing from this list is ignored even when Apollo has a value for it.
var envFromApollo = []string{
	"DATABASE_URL",
	"JWT_SECRET",
	"REDIS_URL",
	"MULTICA_APP_URL",
	"FRONTEND_ORIGIN",
	"CORS_ALLOWED_ORIGINS",
	"MULTICA_PUBLIC_URL",
	"MULTICA_TRUSTED_PROXIES",
	"MULTICA_FEISHU_APP_ID",
	"MULTICA_FEISHU_APP_SECRET",
	"MULTICA_FEISHU_REDIRECT_URI",
	"MULTICA_LARK_SECRET_KEY",
	"MULTICA_WECOM_SECRET_KEY",
	"MULTICA_DINGTALK_SECRET_KEY",
	"MULTICA_SLACK_SECRET_KEY",
	"MULTICA_TELEGRAM_SECRET_KEY",
	"MULTICA_VCS_SECRET_KEY",
	"MULTICA_VCS_INTEGRATION_ENABLED",
	"GITHUB_APP_SLUG",
	"GITHUB_WEBHOOK_SECRET",
	"GITHUB_APP_ID",
	"GITHUB_APP_PRIVATE_KEY",
	"MULTICA_PLUGIN_SECRET_KEY",
	"COMPOSIO_API_KEY",
	"RESEND_API_KEY",
	"SMTP_HOST",
	"SMTP_PORT",
	"SMTP_USERNAME",
	"SMTP_PASSWORD",
	"SMTP_FROM_EMAIL",
	"S3_BUCKET",
	"S3_REGION",
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_ENDPOINT_URL",
}

// ApplyProfile loads the company profile file and Apollo.
// An empty PROFILE keeps the local process on environment variables only.
// test-ali and prod-ali require a Redis address after the file and Apollo are merged.
func ApplyProfile(profile, dir string) error {
	name, err := profileFile(profile)
	if err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	if dir == "" {
		dir, err = configDir()
		if err != nil {
			return err
		}
	}
	props, err := loadYAML(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	if props["apollo.bootstrap.enabled"] == "true" {
		loaded, fetchErr := fetchApollo(context.Background(), props["apollo.meta"], props["app.id"], props["apollo.bootstrap.namespaces"])
		if fetchErr != nil && strings.TrimSpace(props["redis.host"]) == "" {
			return fetchErr
		}
		if fetchErr != nil {
			slog.Warn("apollo: config service unavailable, using the profile file", "error", fetchErr)
		} else {
			for key, value := range loaded {
				props[key] = value
			}
			slog.Info("apollo: loaded", "app", props["app.id"], "namespace", props["apollo.bootstrap.namespaces"], "keys", len(loaded))
		}
	}
	applyEnv(props)
	if strings.TrimSpace(os.Getenv("REDIS_URL")) == "" {
		redisURL, err := redisURL(props)
		if err != nil {
			return err
		}
		if err := os.Setenv("REDIS_URL", redisURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(os.Getenv("REDIS_URL")) == "" {
		return errors.New("missing redis.host")
	}
	return nil
}

func profileFile(profile string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "":
		return "", nil
	case "test", "test-ali", "dev":
		return "application-test-ali.yml", nil
	case "prod", "pro", "prod-ali":
		return "application-prod-ali.yml", nil
	default:
		return "", fmt.Errorf("unknown PROFILE %q", profile)
	}
}

func configDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("MULTICA_CONFIG_DIR")); dir != "" {
		return dir, nil
	}
	for _, candidate := range []string{"config", filepath.Join("server", "config")} {
		if st, err := os.Stat(filepath.Join(candidate, "application-test-ali.yml")); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("config directory not found; set MULTICA_CONFIG_DIR")
}

func loadYAML(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	props := map[string]string{}
	flatten("", tree, props)
	return props, nil
}

func flatten(prefix string, value any, out map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flatten(next, child, out)
		}
	case nil:
		if prefix != "" {
			out[prefix] = ""
		}
	default:
		if prefix != "" {
			out[prefix] = fmt.Sprint(typed)
		}
	}
}

func applyEnv(props map[string]string) {
	for _, key := range envFromApollo {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}
		if value := strings.TrimSpace(props[key]); value != "" {
			_ = os.Setenv(key, value)
		}
	}
}

func redisURL(props map[string]string) (string, error) {
	host := strings.TrimSpace(props["redis.host"])
	if host == "" {
		return "", errors.New("missing redis.host")
	}
	port := 6379
	if raw := strings.TrimSpace(props["redis.port"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			return "", errors.New("invalid redis.port")
		}
		port = parsed
	}
	db := 0
	if raw := strings.TrimSpace(props["redis.db"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 15 {
			return "", errors.New("redis.db must be between 0 and 15")
		}
		db = parsed
	}
	u := &url.URL{
		Scheme: "redis",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + strconv.Itoa(db),
	}
	if password := props["redis.password"]; password != "" {
		u.User = url.UserPassword("", password)
	}
	return u.String(), nil
}

type apolloService struct {
	HomepageURL string `json:"homepageUrl"`
}

type apolloRelease struct {
	Configurations map[string]string `json:"configurations"`
}

func fetchApollo(ctx context.Context, meta, appID, namespace string) (map[string]string, error) {
	meta = strings.TrimRight(strings.TrimSpace(meta), "/")
	appID = strings.TrimSpace(appID)
	namespace = strings.TrimSpace(namespace)
	if meta == "" || appID == "" || namespace == "" {
		return nil, errors.New("apollo meta, app id, and namespace are required")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	servicesURL := meta + "/services/config"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, servicesURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apollo meta: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apollo meta: status %d", resp.StatusCode)
	}
	var services []apolloService
	if err := json.Unmarshal(body, &services); err != nil {
		return nil, fmt.Errorf("apollo meta: %w", err)
	}
	if len(services) == 0 || strings.TrimSpace(services[0].HomepageURL) == "" {
		return nil, errors.New("apollo meta returned no config service")
	}
	configURL := strings.TrimRight(services[0].HomepageURL, "/") + "/configs/" + url.PathEscape(appID) + "/default/" + url.PathEscape(namespace)
	cfgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, err
	}
	cfgResp, err := client.Do(cfgReq)
	if err != nil {
		return nil, fmt.Errorf("apollo config: %w", err)
	}
	defer cfgResp.Body.Close()
	cfgBody, err := io.ReadAll(io.LimitReader(cfgResp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if cfgResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apollo config: status %d", cfgResp.StatusCode)
	}
	var release apolloRelease
	if err := json.Unmarshal(cfgBody, &release); err != nil {
		return nil, fmt.Errorf("apollo config: %w", err)
	}
	if release.Configurations == nil {
		return map[string]string{}, nil
	}
	return release.Configurations, nil
}
