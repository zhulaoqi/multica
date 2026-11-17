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
		name string
		raw  string
		ok   bool
		want string
	}{
		{"plain json", `{"unit_id":"unit-1"}`, true, "unit-1"},
		{"fenced json", "```json\n{\"unit_id\":\"unit-1\"}\n```", true, "unit-1"},
		{"bare fence", "```\n{\"unit_id\":\"unit-2\"}\n```", true, "unit-2"},
		{"trims the id", `{"unit_id":" unit-3 "}`, true, "unit-3"},
		{"empty id declines", `{"unit_id":""}`, false, ""},
		{"missing id declines", `{}`, false, ""},
		{"prose declines", "I think unit-1 fits", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDispatchRoute(tc.raw)
			if ok != tc.ok || got.UnitID != tc.want {
				t.Fatalf("parseDispatchRoute(%q) = (%q, %v), want (%q, %v)", tc.raw, got.UnitID, ok, tc.want, tc.ok)
			}
		})
	}
}
