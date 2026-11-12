package service

import (
	"strings"
	"testing"
)

func TestParseOrgPath(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr string
	}{
		{name: "single segment", raw: "平台部", want: []string{"平台部"}},
		{name: "nested", raw: "平台部/支付组", want: []string{"平台部", "支付组"}},
		{name: "trims whitespace", raw: " 平台部 / 支付组 ", want: []string{"平台部", "支付组"}},
		{name: "empty", raw: "   ", wantErr: "empty segment"},
		{name: "blank segment", raw: "平台部//支付组", wantErr: "empty segment"},
		{name: "trailing slash", raw: "平台部/", wantErr: "empty segment"},
		{name: "too deep", raw: strings.Repeat("a/", 8) + "a", wantErr: "deeper than 8"},
		{name: "segment too long", raw: strings.Repeat("部", 101), wantErr: "name too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOrgPath(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseOrgPath(%q) err = %v, want containing %q", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseOrgPath(%q) unexpected error: %v", tt.raw, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseOrgPath(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ParseOrgPath(%q) = %v, want %v", tt.raw, got, tt.want)
				}
			}
		})
	}
}

func TestPlanOrgImport(t *testing.T) {
	t.Run("dedupes shared parents and keeps parent first", func(t *testing.T) {
		units, agents, err := PlanOrgImport([]OrgImportRow{
			{Path: "平台部/支付组", LeadEmail: " lead@example.com ", Agents: []string{" Mika ", ""}},
			{Path: "平台部/结算组", Agents: []string{"Cursor"}},
			{Path: "平台部", Agents: []string{"Arch"}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(units) != 3 {
			t.Fatalf("units = %d, want 3", len(units))
		}
		if got := strings.Join(units[0].Path, "/"); got != "平台部/支付组" {
			t.Fatalf("first unit = %q", got)
		}
		if units[0].LeadEmail != "lead@example.com" {
			t.Fatalf("lead = %q, want trimmed", units[0].LeadEmail)
		}
		if got := strings.Join(agents["Mika"], "/"); got != "平台部/支付组" {
			t.Fatalf("Mika unit = %q", got)
		}
		if got := strings.Join(agents["Cursor"], "/"); got != "平台部/结算组" {
			t.Fatalf("Cursor unit = %q", got)
		}
		if len(agents) != 3 {
			t.Fatalf("agents = %v, want 3 entries", agents)
		}
	})

	t.Run("rejects agent in two units", func(t *testing.T) {
		_, _, err := PlanOrgImport([]OrgImportRow{
			{Path: "平台部/支付组", Agents: []string{"Mika"}},
			{Path: "平台部/结算组", Agents: []string{"Mika"}},
		})
		if err == nil || !strings.Contains(err.Error(), "more than one unit") {
			t.Fatalf("err = %v, want duplicate agent error", err)
		}
	})

	t.Run("same agent repeated in one unit is fine", func(t *testing.T) {
		_, agents, err := PlanOrgImport([]OrgImportRow{
			{Path: "平台部/支付组", Agents: []string{"Mika"}},
			{Path: "平台部/支付组", Agents: []string{"Mika"}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(agents) != 1 {
			t.Fatalf("agents = %v", agents)
		}
	})

	t.Run("reports failing row number", func(t *testing.T) {
		_, _, err := PlanOrgImport([]OrgImportRow{
			{Path: "平台部"},
			{Path: ""},
		})
		if err == nil || !strings.Contains(err.Error(), "row 2") {
			t.Fatalf("err = %v, want row 2", err)
		}
	})

	t.Run("rejects empty and oversized payloads", func(t *testing.T) {
		if _, _, err := PlanOrgImport(nil); err == nil {
			t.Fatal("want error for empty rows")
		}
		rows := make([]OrgImportRow, orgImportMaxRows+1)
		for i := range rows {
			rows[i] = OrgImportRow{Path: "平台部"}
		}
		if _, _, err := PlanOrgImport(rows); err == nil || !strings.Contains(err.Error(), "too many rows") {
			t.Fatalf("err = %v, want too many rows", err)
		}
	})
}
