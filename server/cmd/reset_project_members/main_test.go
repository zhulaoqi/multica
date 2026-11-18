package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func testUUID(b byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{0x0A, b}, Valid: true}
}

func testText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func row(ws, root, lead, member, user pgtype.UUID, role string, unit pgtype.UUID) memberRow {
	return memberRow{
		WorkspaceID:     ws,
		WorkspaceName:   "ws-" + string(rune('a'+member.Bytes[1])),
		RootUnitID:      root,
		RootLeadUserID:  lead,
		MemberID:        member,
		UserID:          user,
		Role:            testText(role),
		MemberOrgUnitID: unit,
		MemberUnitName:  testText("unit"),
		MemberName:      testText("person"),
		MemberEmail:     testText("person@example.com"),
	}
}

func TestPlanWorkspacesPromotesLeadBeforeComputingKeep(t *testing.T) {
	ws, root := testUUID(1), testUUID(2)
	lead, owner, admin, outsider := testUUID(3), testUUID(4), testUUID(5), testUUID(6)

	rows := []memberRow{
		row(ws, root, lead, testUUID(10), owner, "owner", root),
		row(ws, root, lead, testUUID(11), lead, "member", root),
		row(ws, root, lead, testUUID(12), admin, "admin", testUUID(7)),
		row(ws, root, lead, testUUID(13), outsider, "member", testUUID(8)),
	}

	plans := planWorkspaces(rows)
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	plan := plans[0]
	if !plan.promote {
		t.Error("lead with a member role must be promoted")
	}
	if len(plan.keep) != 3 {
		t.Fatalf("keep = %d rows, want 3", len(plan.keep))
	}
	if len(plan.remove) != 1 || plan.remove[0].UserID != outsider {
		t.Fatalf("remove = %+v, want the single outsider", plan.remove)
	}
	if len(plan.reparent) != 0 {
		t.Fatalf("reparent = %d, want 0 (all kept rows are placed)", len(plan.reparent))
	}
}

func TestPlanWorkspacesAddsLeadWithoutMemberRow(t *testing.T) {
	ws, root, lead, member := testUUID(1), testUUID(2), testUUID(3), testUUID(4)
	rows := []memberRow{
		row(ws, root, lead, testUUID(10), member, "member", root),
	}

	plan := planWorkspaces(rows)[0]
	if !plan.promote {
		t.Error("lead without a member row must be inserted as owner")
	}
	if plan.leadMemberID.Valid {
		t.Error("leadMemberID must stay empty so apply inserts a row")
	}
	if len(plan.keep) != 0 || len(plan.remove) != 1 {
		t.Fatalf("keep=%d remove=%d, want 0/1", len(plan.keep), len(plan.remove))
	}
}

func TestPlanWorkspacesBackfillsUnplacedKeepers(t *testing.T) {
	ws, root, lead, owner := testUUID(1), testUUID(2), testUUID(3), testUUID(4)
	rows := []memberRow{
		row(ws, root, lead, testUUID(10), lead, "owner", root),
		row(ws, root, lead, testUUID(11), owner, "owner", pgtype.UUID{}),
	}

	plan := planWorkspaces(rows)[0]
	if plan.promote {
		t.Error("an owner lead must not be promoted again")
	}
	if len(plan.reparent) != 1 || plan.reparent[0].UserID != owner {
		t.Fatalf("reparent = %+v, want the unplaced owner", plan.reparent)
	}
}

func TestPlanWorkspacesIsStableAfterCleanup(t *testing.T) {
	ws, root, lead, owner := testUUID(1), testUUID(2), testUUID(3), testUUID(4)
	rows := []memberRow{
		row(ws, root, lead, testUUID(10), lead, "owner", root),
		row(ws, root, lead, testUUID(11), owner, "owner", root),
	}

	plan := planWorkspaces(rows)[0]
	if plan.promote || len(plan.remove) != 0 || len(plan.reparent) != 0 {
		t.Fatalf("clean workspace must be a no-op: promote=%v remove=%d reparent=%d",
			plan.promote, len(plan.remove), len(plan.reparent))
	}
}

func TestPlanWorkspacesHandlesEmptyWorkspace(t *testing.T) {
	ws, root, lead := testUUID(1), testUUID(2), testUUID(3)
	plan := planWorkspaces([]memberRow{row(ws, root, lead, pgtype.UUID{}, pgtype.UUID{}, "", pgtype.UUID{})})[0]
	if !plan.promote {
		t.Error("a workspace with no member rows must still get its lead")
	}
	if len(plan.keep) != 0 || len(plan.remove) != 0 {
		t.Fatalf("keep=%d remove=%d, want 0/0", len(plan.keep), len(plan.remove))
	}
}
