package orgscope

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuid(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	parsed, err := parseUUID(value)
	if err != nil {
		t.Fatalf("parse fixture uuid %q: %v", value, err)
	}
	return parsed
}

// parseUUID keeps the fixtures readable without pulling in the util package,
// which imports pgx anyway but adds a dependency cycle risk in tests.
func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func unit(t *testing.T, id, parent, lead string) db.ListOrgUnitsRow {
	t.Helper()
	row := db.ListOrgUnitsRow{ID: uuid(t, id), Name: id}
	if parent != "" {
		row.ParentID = uuid(t, parent)
	}
	if lead != "" {
		row.LeadUserID = uuid(t, lead)
	}
	return row
}

const (
	rootID   = "11111111-1111-1111-1111-111111111111"
	childID  = "22222222-2222-2222-2222-222222222222"
	sibling  = "33333333-3333-3333-3333-333333333333"
	leafID   = "44444444-4444-4444-4444-444444444444"
	leadID   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	otherID  = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	missing  = "99999999-9999-9999-9999-999999999999"
	otherTop = "55555555-5555-5555-5555-555555555555"
)

func fixtures(t *testing.T) []db.ListOrgUnitsRow {
	t.Helper()
	return []db.ListOrgUnitsRow{
		unit(t, rootID, "", leadID),
		unit(t, childID, rootID, ""),
		unit(t, leafID, childID, ""),
		unit(t, sibling, otherTop, leadID),
		unit(t, otherTop, "", ""),
	}
}

func TestLeads(t *testing.T) {
	units := fixtures(t)
	cases := []struct {
		name string
		user string
		unit string
		want bool
	}{
		{name: "direct lead", user: leadID, unit: rootID, want: true},
		{name: "ancestor lead", user: leadID, unit: leafID, want: true},
		{name: "other tree", user: otherID, unit: rootID, want: false},
		{name: "missing unit", user: leadID, unit: missing, want: false},
		{name: "empty user", user: "", unit: rootID, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var user, target pgtype.UUID
			if tc.user != "" {
				user = uuid(t, tc.user)
			}
			if tc.unit != "" {
				target = uuid(t, tc.unit)
			}
			if got := Leads(units, user, target); got != tc.want {
				t.Fatalf("Leads(%s, %s) = %v, want %v", tc.user, tc.unit, got, tc.want)
			}
		})
	}
}

func TestLeadDepth(t *testing.T) {
	units := append(fixtures(t), unit(t, "66666666-6666-6666-6666-666666666666", leafID, otherID))
	cases := []struct {
		name  string
		user  string
		unit  string
		depth int
		ok    bool
	}{
		{name: "direct lead", user: leadID, unit: rootID, depth: 0, ok: true},
		{name: "one ancestor up", user: leadID, unit: childID, depth: 1, ok: true},
		{name: "two ancestors up", user: leadID, unit: leafID, depth: 2, ok: true},
		{name: "closer lead wins on depth", user: otherID, unit: "66666666-6666-6666-6666-666666666666", depth: 0, ok: true},
		{name: "not a lead", user: otherID, unit: rootID, depth: 0, ok: false},
		{name: "missing unit", user: leadID, unit: missing, depth: 0, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			depth, ok := LeadDepth(units, uuid(t, tc.user), uuid(t, tc.unit))
			if ok != tc.ok || depth != tc.depth {
				t.Fatalf("LeadDepth(%s, %s) = (%d, %v), want (%d, %v)",
					tc.user, tc.unit, depth, ok, tc.depth, tc.ok)
			}
		})
	}
}

func TestLeadsAny(t *testing.T) {
	units := fixtures(t)
	if !LeadsAny(units, uuid(t, leadID)) {
		t.Fatal("lead should manage at least one unit")
	}
	if LeadsAny(units, uuid(t, otherID)) {
		t.Fatal("member without a led unit should not manage any")
	}
	if LeadsAny(units, pgtype.UUID{}) {
		t.Fatal("empty user should not manage any unit")
	}
}

func TestVisible(t *testing.T) {
	units := fixtures(t)
	if got := Visible(units, pgtype.UUID{}, uuid(t, otherID), true); got != nil {
		t.Fatal("managers should get an unrestricted view (nil)")
	}

	// A placed member sees their department subtree only: no ancestors, no
	// departments they do not lead.
	got := Visible(units, uuid(t, childID), uuid(t, otherID), false)
	want := map[string]bool{childID: true, leafID: true}
	if len(got) != len(want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
	for id := range want {
		if !got[uuid(t, id)] {
			t.Fatalf("unit %s should be visible", id)
		}
	}

	// A lead sees the subtree of every unit they lead, even outside their own
	// department.
	got = Visible(units, uuid(t, childID), uuid(t, leadID), false)
	for _, id := range []string{childID, leafID, rootID, sibling} {
		if !got[uuid(t, id)] {
			t.Fatalf("unit %s should be visible to its lead", id)
		}
	}
	if got[uuid(t, otherTop)] {
		t.Fatal("a sibling's parent should not become visible")
	}
}

func TestSubtreeAndAncestors(t *testing.T) {
	units := fixtures(t)
	subtree := Subtree(units, uuid(t, childID))
	if !subtree[uuid(t, childID)] || !subtree[uuid(t, leafID)] || subtree[uuid(t, rootID)] {
		t.Fatalf("subtree = %v", subtree)
	}
	chain := Ancestors(units, uuid(t, leafID))
	want := []pgtype.UUID{uuid(t, leafID), uuid(t, childID), uuid(t, rootID)}
	if len(chain) != len(want) {
		t.Fatalf("ancestors = %v, want %v", chain, want)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("ancestors[%d] = %v, want %v", i, chain[i], want[i])
		}
	}
	if got := Ancestors(units, uuid(t, missing)); len(got) != 0 {
		t.Fatalf("missing unit should have no ancestors, got %v", got)
	}
}
