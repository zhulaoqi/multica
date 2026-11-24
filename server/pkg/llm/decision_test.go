package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeciderChooseSendsAChoiceQuestion(t *testing.T) {
	var gotPath, gotAuth string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"model":"typesafe/jev","answers":{"target":{"type":"choice","choice":"p-1","confidence":0.92,"probabilities":{"p-1":0.92,"d-1":0.08}}}}`))
	}))
	t.Cleanup(srv.Close)

	d := NewDecider(DecisionConfig{APIKey: "test-key", BaseURL: srv.URL + "/api/v1/", Model: "jev-latest"})
	choice, confidence, err := d.Choose(context.Background(),
		map[string]any{"request": "AIHUB现在接了哪些模型"},
		"pick the owner",
		map[string]string{"p-1": "项目 AIHUB", "d-1": "部门 研发部"},
	)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if choice != "p-1" || confidence != 0.92 {
		t.Fatalf("choice/confidence = %q/%v, want p-1/0.92", choice, confidence)
	}
	if gotPath != "/api/v1/decision" {
		t.Fatalf("path = %q, want /api/v1/decision", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q, want the bearer key", gotAuth)
	}
	if got["model"] != "jev-latest" {
		t.Fatalf("model = %v, want jev-latest", got["model"])
	}
	question, _ := got["questions"].(map[string]any)["target"].(map[string]any)
	if question["type"] != "choice" || question["instructions"] != "pick the owner" {
		t.Fatalf("question = %v, want a choice with the instructions", question)
	}
	if criteria, _ := question["criteria"].(map[string]any); criteria["p-1"] != "项目 AIHUB" || criteria["d-1"] != "部门 研发部" {
		t.Fatalf("criteria = %v, want both options", question["criteria"])
	}
	if state, _ := got["state"].(map[string]any); state["request"] != "AIHUB现在接了哪些模型" {
		t.Fatalf("state = %v, want the request", got["state"])
	}
}

func TestDeciderChooseRejectsBadAnswers(t *testing.T) {
	options := map[string]string{"a": "A", "b": "B"}
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "upstream status", status: http.StatusBadGateway, body: `{"error":"down"}`, want: "status 502"},
		{name: "upstream error field", status: http.StatusOK, body: `{"error":"jev models require GenerateDecision","status":"failed"}`, want: "upstream error"},
		{name: "no answer", status: http.StatusOK, body: `{"answers":{}}`, want: "no choice"},
		{name: "unknown option", status: http.StatusOK, body: `{"answers":{"target":{"choice":"zzz","confidence":1}}}`, want: "unknown option"},
		{name: "not json", status: http.StatusOK, body: `<html>`, want: "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			d := NewDecider(DecisionConfig{BaseURL: srv.URL, Model: "jev-latest"})
			if _, _, err := d.Choose(context.Background(), nil, "", options); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestUnconfiguredDeciderMakesZeroUpstreamRequests extends the package's
// zero-request contract to the decision endpoint: a deployment that sets only a
// model, only a base URL, or nothing at all sends nothing.
func TestUnconfiguredDeciderMakesZeroUpstreamRequests(t *testing.T) {
	for _, cfg := range []DecisionConfig{{}, {Model: "jev-latest"}, {BaseURL: "http://127.0.0.1:1", APIKey: "k"}} {
		transport := &countingHTTPClient{}
		cfg.HTTPClient = transport
		d := NewDecider(cfg)
		if d.Enabled() {
			t.Fatalf("decider %+v reports enabled", cfg)
		}
		if _, _, err := d.Choose(context.Background(), map[string]any{"request": "private"}, "", map[string]string{"a": "A"}); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("Choose on disabled decider: got %v, want ErrNotConfigured", err)
		}
		if n := transport.requests.Load(); n != 0 {
			t.Fatalf("disabled decider %+v made %d request(s)", cfg, n)
		}
	}

	// Prove the seam is wired, so the zero counts above are not vacuous.
	transport := &countingHTTPClient{}
	d := NewDecider(DecisionConfig{BaseURL: "http://127.0.0.1:1", Model: "jev-latest", HTTPClient: transport})
	if _, _, err := d.Choose(context.Background(), nil, "", map[string]string{"a": "A"}); err == nil {
		t.Fatal("expected the refusing transport to fail a configured decider")
	}
	if transport.requests.Load() == 0 {
		t.Fatal("configured decider sent nothing through the test transport")
	}
}

func TestNilDeciderIsDisabled(t *testing.T) {
	var d *Decider
	if d.Enabled() || d.Model() != "" {
		t.Fatal("nil decider must be disabled")
	}
	if _, _, err := d.Choose(context.Background(), nil, "", map[string]string{"a": "A"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil decider Choose = %v, want ErrNotConfigured", err)
	}
}
