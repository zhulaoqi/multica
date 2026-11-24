package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3/option"
)

// decisionQuestion is the key of the single question Choose asks.
const decisionQuestion = "target"

// decisionErrorBodyLimit caps how much of a failed response an error carries.
const decisionErrorBodyLimit = 300

// DecisionConfig configures a Decider: a client for gateways that expose a
// structured decision endpoint (POST {BaseURL}/decision) next to Chat
// Completions. Decision models such as jev answer typed questions — here a
// single choice among labelled options — and report a confidence, instead of
// free text the caller has to parse.
type DecisionConfig struct {
	// APIKey is sent as a Bearer token when set.
	APIKey string
	// BaseURL is the gateway root the decision path is appended to.
	BaseURL string
	// Model names the decision model, e.g. "jev-latest".
	Model string
	// HTTPClient replaces http.DefaultClient. Primarily a test seam.
	HTTPClient option.HTTPClient
}

// Decider calls a decision endpoint. It is safe for concurrent use. A Decider
// with no base URL or no model is disabled: every call returns
// ErrNotConfigured before a request is built.
type Decider struct {
	apiKey  string
	baseURL string
	model   string
	http    option.HTTPClient
}

// NewDecider builds a Decider from cfg. Like New it never fails; an incomplete
// config yields a disabled Decider.
func NewDecider(cfg DecisionConfig) *Decider {
	d := &Decider{
		apiKey:  strings.TrimSpace(cfg.APIKey),
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		model:   strings.TrimSpace(cfg.Model),
		http:    cfg.HTTPClient,
	}
	if d.http == nil {
		d.http = http.DefaultClient
	}
	return d
}

// Enabled reports whether the Decider has both a base URL and a model.
func (d *Decider) Enabled() bool { return d != nil && d.baseURL != "" && d.model != "" }

// Model returns the configured decision model. Safe to log.
func (d *Decider) Model() string {
	if d == nil {
		return ""
	}
	return d.model
}

type decisionRequest struct {
	Model     string                          `json:"model"`
	State     map[string]any                  `json:"state"`
	Questions map[string]decisionQuestionSpec `json:"questions"`
}

type decisionQuestionSpec struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type decisionResponse struct {
	Answers map[string]struct {
		Type       string  `json:"type"`
		Choice     string  `json:"choice"`
		Confidence float64 `json:"confidence"`
	} `json:"answers"`
	Error string `json:"error"`
}

// Choose asks the decision model to pick one of options for state. options
// maps each choice key to the description the model judges it by. It returns
// the chosen key, which is always one of options, and the model's confidence
// in it (0..1).
func (d *Decider) Choose(ctx context.Context, state map[string]any, instructions string, options map[string]string) (string, float64, error) {
	if !d.Enabled() {
		return "", 0, ErrNotConfigured
	}
	if len(options) == 0 {
		return "", 0, errors.New("llm: decision needs at least one option")
	}
	payload, err := json.Marshal(decisionRequest{
		Model: d.model,
		State: state,
		Questions: map[string]decisionQuestionSpec{
			decisionQuestion: {Type: "choice", Instructions: instructions, Criteria: options},
		},
	})
	if err != nil {
		return "", 0, fmt.Errorf("llm: encode decision request: %w", err)
	}

	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/decision", bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("llm: build decision request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if d.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiKey)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("llm: decision request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, fmt.Errorf("llm: read decision response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", 0, fmt.Errorf("llm: decision upstream status %d: %s", resp.StatusCode, truncateBody(body))
	}
	var parsed decisionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", 0, fmt.Errorf("llm: decode decision response: %w", err)
	}
	if parsed.Error != "" {
		return "", 0, fmt.Errorf("llm: decision upstream error: %s", truncateBody([]byte(parsed.Error)))
	}
	answer, ok := parsed.Answers[decisionQuestion]
	if !ok || answer.Choice == "" {
		return "", 0, errors.New("llm: decision response has no choice")
	}
	if _, ok := options[answer.Choice]; !ok {
		return "", 0, fmt.Errorf("llm: decision chose unknown option %q", truncateBody([]byte(answer.Choice)))
	}
	return answer.Choice, answer.Confidence, nil
}

func truncateBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if runes := []rune(text); len(runes) > decisionErrorBodyLimit {
		text = string(runes[:decisionErrorBodyLimit]) + "…"
	}
	return text
}
