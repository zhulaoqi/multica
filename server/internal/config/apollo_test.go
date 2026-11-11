package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProfileFilesSplitApolloAndRedis(t *testing.T) {
	dir := repoConfigDir(t)
	testProps, err := loadYAML(filepath.Join(dir, "application-test-ali.yml"))
	if err != nil {
		t.Fatal(err)
	}
	prodProps, err := loadYAML(filepath.Join(dir, "application-prod-ali.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if testProps["app.id"] != "multica" || prodProps["app.id"] != "multica" {
		t.Fatalf("app id = %q / %q", testProps["app.id"], prodProps["app.id"])
	}
	if testProps["apollo.meta"] != "http://172.30.10.48:8080" {
		t.Fatalf("test meta = %q", testProps["apollo.meta"])
	}
	if prodProps["apollo.meta"] != "http://confcenter.spellso.com" {
		t.Fatalf("prod meta = %q", prodProps["apollo.meta"])
	}
	if testProps["apollo.bootstrap.namespaces"] != "application" || prodProps["apollo.bootstrap.namespaces"] != "application" {
		t.Fatalf("namespaces = %q / %q", testProps["apollo.bootstrap.namespaces"], prodProps["apollo.bootstrap.namespaces"])
	}
	if testProps["redis.host"] != "172.30.11.78" || testProps["redis.port"] != "6379" || testProps["redis.password"] != "" || testProps["redis.db"] != "0" {
		t.Fatalf("test redis = %#v", testProps)
	}
	if _, ok := prodProps["redis.host"]; ok {
		t.Fatalf("prod file must not contain redis.host, got %q", prodProps["redis.host"])
	}
}

func TestRedisURLUsesEmptyPassword(t *testing.T) {
	got, err := redisURL(map[string]string{
		"redis.host": "172.30.11.78",
		"redis.port": "6379",
		"redis.db":   "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "redis://172.30.11.78:6379/0" {
		t.Fatalf("url = %q", got)
	}
}

func TestApplyProfileLetsApolloReplaceFileRedis(t *testing.T) {
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/config":
			_, _ = w.Write([]byte(`[{"homepageUrl":"http://config.internal"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer meta.Close()
	configService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/configs/multica/default/application" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"configurations":{"redis.host":"10.1.1.1","redis.port":"6379","redis.password":"","redis.db":"0"}}`))
	}))
	defer configService.Close()
	// The meta response points at this test server. Rewrite the homepage to the actual URL.
	meta.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"homepageUrl":"` + configService.URL + `"}]`))
	})

	dir := t.TempDir()
	body := "app:\n  id: multica\napollo:\n  meta: " + meta.URL + "\n  bootstrap:\n    enabled: true\n    namespaces: application\nredis:\n  host: 172.30.11.78\n  port: 6379\n  password: \"\"\n  db: 0\n"
	if err := os.WriteFile(filepath.Join(dir, "application-test-ali.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REDIS_URL", "")
	if err := ApplyProfile("test-ali", dir); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("REDIS_URL"); got != "redis://10.1.1.1:6379/0" {
		t.Fatalf("REDIS_URL = %q", got)
	}
}

func TestProdProfileRefusesStartWithoutApolloRedis(t *testing.T) {
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer meta.Close()
	dir := t.TempDir()
	body := "app:\n  id: multica\napollo:\n  meta: " + meta.URL + "\n  bootstrap:\n    enabled: true\n    namespaces: application\n"
	if err := os.WriteFile(filepath.Join(dir, "application-prod-ali.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REDIS_URL", "")
	if err := ApplyProfile("prod-ali", dir); err == nil {
		t.Fatal("expected prod start to fail when Apollo has no redis.host")
	}
}

func TestEmptyProfileSkipsApollo(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	if err := ApplyProfile("", ""); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("REDIS_URL") != "" {
		t.Fatalf("local profile set REDIS_URL = %q", os.Getenv("REDIS_URL"))
	}
}

func repoConfigDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "config")
}
