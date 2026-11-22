package service

import (
	"strings"
	"testing"
)

func TestDispatchTextTitle(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{"first non-empty line", "\n\n  Prepare   onboarding\tmaterials \nsecond line", "Prepare onboarding materials"},
		{"whole single line", "Investigate checkout failures", "Investigate checkout failures"},
		{"empty", "   \n\t", ""},
		{
			"caps at the title budget",
			strings.Repeat("长", 100),
			strings.Repeat("长", dispatchTitleLimit),
		},
		{
			"counts runes, not bytes",
			strings.Repeat("字", dispatchTitleLimit+5),
			strings.Repeat("字", dispatchTitleLimit),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DispatchTextTitle(tc.text); got != tc.want {
				t.Fatalf("DispatchTextTitle(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestParseDispatchRoute(t *testing.T) {
	for _, tc := range []struct {
		name     string
		raw      string
		ok       bool
		wantType string
		wantID   string
	}{
		{"department", `{"target_type":"department","target_id":"unit-1"}`, true, "department", "unit-1"},
		{"project", `{"target_type":"project","target_id":"proj-1"}`, true, "project", "proj-1"},
		{"legacy unit id", `{"unit_id":"unit-1"}`, true, "department", "unit-1"},
		{"fenced json", "```json\n{\"target_type\":\"project\",\"target_id\":\"proj-1\"}\n```", true, "project", "proj-1"},
		{"bare fence", "```\n{\"unit_id\":\"unit-2\"}\n```", true, "department", "unit-2"},
		{"trims the id", `{"target_type":"project","target_id":" proj-3 "}`, true, "project", "proj-3"},
		{"empty id declines", `{"target_type":"project","target_id":""}`, false, "project", ""},
		{"empty target declines", `{"target_type":"","target_id":"unit-1"}`, false, "", "unit-1"},
		{"missing id declines", `{}`, false, "", ""},
		{"prose declines", "I think unit-1 fits", false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDispatchRoute(tc.raw)
			if ok != tc.ok || got.TargetType != tc.wantType || got.TargetID != tc.wantID {
				t.Fatalf(
					"parseDispatchRoute(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.raw, got.TargetType, got.TargetID, ok, tc.wantType, tc.wantID, tc.ok,
				)
			}
		})
	}
}
