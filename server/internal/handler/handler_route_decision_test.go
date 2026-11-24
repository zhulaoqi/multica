package handler

import "testing"

func TestRouteDecisionConfigDefaultsToTheChatEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name              string
		cfg               Config
		wantBase, wantKey string
	}{
		{
			name:     "inherits base url and key",
			cfg:      Config{LLMBaseURL: "https://gw/v1", LLMAPIKey: "chat-key", RouteDecisionModel: "jev-latest"},
			wantBase: "https://gw/v1", wantKey: "chat-key",
		},
		{
			name:     "own key on the chat base url",
			cfg:      Config{LLMBaseURL: "https://gw/v1", LLMAPIKey: "chat-key", RouteDecisionAPIKey: "decide-key"},
			wantBase: "https://gw/v1", wantKey: "decide-key",
		},
		{
			name:     "a different host never gets the chat key",
			cfg:      Config{LLMBaseURL: "https://gw/v1", LLMAPIKey: "chat-key", RouteDecisionBaseURL: "https://other/v1"},
			wantBase: "https://other/v1", wantKey: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := routeDecisionConfig(tc.cfg)
			if got.BaseURL != tc.wantBase || got.APIKey != tc.wantKey {
				t.Fatalf("config = %q/%q, want %q/%q", got.BaseURL, got.APIKey, tc.wantBase, tc.wantKey)
			}
		})
	}
}
