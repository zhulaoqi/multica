package workspaceaccess

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func id(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	var parsed pgtype.UUID
	if err := parsed.Scan(value); err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return parsed
}

const (
	wsID    = "11111111-1111-1111-1111-111111111111"
	rootID  = "22222222-2222-2222-2222-222222222222"
	userID  = "33333333-3333-3333-3333-333333333333"
	leadID  = "44444444-4444-4444-4444-444444444444"
	otherID = "55555555-5555-5555-5555-555555555555"
)

func TestAllowed(t *testing.T) {
	cases := []struct {
		name string
		a    Access
		want bool
	}{
		{
			name: "operator always allowed",
			a:    Access{IsOperator: true},
			want: true,
		},
		{
			name: "no member row is rejected",
			a:    Access{UserID: id(t, userID)},
			want: false,
		},
		{
			name: "legacy workspace with member row",
			a:    Access{HasMember: true, Role: "member"},
			want: true,
		},
		{
			name: "owned subtree member",
			a: Access{
				RootUnitID: id(t, rootID), RootExists: true,
				HasMember: true, Role: "member", InSubtree: true,
			},
			want: true,
		},
		{
			name: "owned out-of-subtree member",
			a: Access{
				RootUnitID: id(t, rootID), RootExists: true,
				HasMember: true, Role: "member",
			},
			want: false,
		},
		{
			name: "owned owner outside subtree keeps access",
			a: Access{
				RootUnitID: id(t, rootID), RootExists: true,
				HasMember: true, Role: "owner",
			},
			want: true,
		},
		{
			name: "owned admin outside subtree keeps access",
			a: Access{
				RootUnitID: id(t, rootID), RootExists: true,
				HasMember: true, Role: "admin",
			},
			want: true,
		},
		{
			name: "missing root fails closed for plain members",
			a: Access{
				RootUnitID: id(t, rootID),
				HasMember:  true, Role: "member", InSubtree: true,
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Allowed(); got != tc.want {
				t.Fatalf("Allowed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestManagerAndViewer(t *testing.T) {
	lead := Access{
		RootUnitID: id(t, rootID), RootExists: true, RootLeadUserID: id(t, leadID),
		UserID: id(t, leadID), HasMember: true, Role: "member",
	}
	if !lead.IsOrgLead() || !lead.IsManager() {
		t.Fatal("owning department lead should manage the project")
	}
	if !lead.Viewer() {
		t.Fatal("manager should open settings even outside the subtree")
	}
	if lead.Allowed() {
		t.Fatal("lead outside the subtree should not read content")
	}

	plain := Access{
		RootUnitID: id(t, rootID), RootExists: true, RootLeadUserID: id(t, leadID),
		UserID: id(t, otherID), HasMember: true, Role: "member", InSubtree: true,
	}
	if plain.IsOrgLead() || plain.IsManager() {
		t.Fatal("plain member must not manage")
	}
	if !plain.Allowed() || !plain.Viewer() {
		t.Fatal("plain subtree member should read content and open settings")
	}

	owner := Access{Role: "owner", HasMember: true, UserID: id(t, otherID)}
	if !owner.IsAdmin() || !owner.IsManager() || !owner.IsOwner() {
		t.Fatal("stored owner should carry admin and manager rights")
	}

	legacyMember := Access{HasMember: true, Role: "member", UserID: id(t, otherID)}
	if legacyMember.IsManager() {
		t.Fatal("legacy plain member must not manage")
	}
}
