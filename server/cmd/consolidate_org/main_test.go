package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func testUUID(b byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{0x0A, b}, Valid: true}
}

func text(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func TestPlanUnitRemaps(t *testing.T) {
	org, project, flat := testUUID(1), testUUID(2), testUUID(3)
	units := []unitRow{
		{ID: testUUID(10), WorkspaceID: org, Name: "公司", ExternalID: text("od-root")},
		{ID: testUUID(11), WorkspaceID: org, ParentID: testUUID(10), Name: "研发", ExternalID: text("od-rd")},
		{ID: testUUID(12), WorkspaceID: org, ParentID: testUUID(11), Name: "前端"},

		{ID: testUUID(20), WorkspaceID: project, Name: "公司", ExternalID: text("od-root"), InProject: true},
		{ID: testUUID(21), WorkspaceID: project, ParentID: testUUID(20), Name: "研发", ExternalID: text("od-rd"), InProject: true},
		{ID: testUUID(22), WorkspaceID: project, ParentID: testUUID(21), Name: "前端", InProject: true},
		{ID: testUUID(23), WorkspaceID: project, ParentID: testUUID(20), Name: "手工", InProject: true},
		{ID: testUUID(24), WorkspaceID: project, ParentID: testUUID(20), Name: "未引用", InProject: true},

		{ID: testUUID(30), WorkspaceID: flat, Name: "公司", ExternalID: text("od-root")},
	}
	referenced := []pgtype.UUID{testUUID(21), testUUID(22), testUUID(23), testUUID(30), testUUID(11), testUUID(99)}

	got := planUnitRemaps(units, org, referenced)

	want := []unitRemap{
		{From: testUUID(23), Path: "公司/手工"},
		{From: testUUID(21), Path: "公司/研发", To: testUUID(11), By: "external_id"},
		{From: testUUID(22), Path: "公司/研发/前端", To: testUUID(12), By: "path"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d remaps, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("remap %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestUnitPathStopsOnCycle(t *testing.T) {
	a := unitRow{ID: testUUID(1), ParentID: testUUID(2), Name: "a"}
	b := unitRow{ID: testUUID(2), ParentID: testUUID(1), Name: "b"}
	byID := map[pgtype.UUID]unitRow{a.ID: a, b.ID: b}
	if got := unitPath(byID, a); got != "b/a" {
		t.Fatalf("unitPath = %q, want %q", got, "b/a")
	}
}

func TestPlanAgentMoves(t *testing.T) {
	ada, bob := testUUID(1), testUUID(2)
	projectAgents := []agentRow{
		{ID: testUUID(10), OwnerID: ada, Name: "Ada", IsPrimary: true, WorkspaceSlug: "henry-he"},
		{ID: testUUID(11), OwnerID: bob, Name: "Helper", WorkspaceSlug: "henry-he"},
		{ID: testUUID(12), OwnerID: ada, Name: "Ada", IsPrimary: true, WorkspaceSlug: "adclaw"},
		{ID: testUUID(13), OwnerID: bob, Name: "Existing", WorkspaceSlug: "adclaw"},
	}
	orgAgents := []db.Agent{
		{ID: testUUID(20), OwnerID: testUUID(3), Name: "Helper", Kind: "user"},
		{ID: testUUID(21), OwnerID: bob, Name: "Existing", Kind: "user"},
		{ID: testUUID(22), OwnerID: bob, Name: "Projection", Kind: "user", SourceAgentID: testUUID(30)},
	}

	got := planAgentMoves(projectAgents, orgAgents)

	want := []agentMove{
		{Agent: projectAgents[0], Name: "Ada", Group: testUUID(10)},
		{Agent: projectAgents[1], Name: "Helper (henry-he)", Group: testUUID(11)},
		{Agent: projectAgents[2], Group: testUUID(10)},
		{Agent: projectAgents[3], Existing: testUUID(21), Group: testUUID(13)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d moves, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Agent.ID != w.Agent.ID || g.Existing != w.Existing || g.Name != w.Name || g.Group != w.Group {
			t.Errorf("move %d = {existing %v name %q group %v}, want {existing %v name %q group %v}",
				i, g.Existing, g.Name, g.Group, w.Existing, w.Name, w.Group)
		}
	}
}

func TestFreeName(t *testing.T) {
	taken := map[string]bool{"Ada": true, "Ada (p)": true, "Ada (p 2)": true}
	cases := []struct{ name, want string }{
		{"Bob", "Bob"},
		{"Ada", "Ada (p 3)"},
	}
	for _, c := range cases {
		if got := freeName(c.name, "p", taken); got != c.want {
			t.Errorf("freeName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
