package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// teamRuntimeFixture builds the four-actor world a department lead needs to
// see their team's machines: a lead over department A, a subordinate in A
// with a private runtime, and a peer member in department B (whose lead is a
// different user) with another private runtime. The lead must see both
// subordinate machines read-only, the peer must see neither, and nobody gains
// write access anywhere.
type teamRuntimeFixture struct {
	leadUserID    string
	subUserID     string
	peerUserID    string
	subRuntimeID  string
	peerRuntimeID string
}

func teamRuntimeTestUsers(t *testing.T) map[string]string {
	t.Helper()
	emails := []string{
		"team-lead@multica.test",
		"team-sub@multica.test",
		"team-peer@multica.test",
	}
	ids := make(map[string]string, len(emails))
	for _, email := range emails {
		var id string
		if err := testPool.QueryRow(context.Background(),
			`INSERT INTO "user" (name, email) VALUES ($1, $1) RETURNING id`, email,
		).Scan(&id); err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		ids[email] = id
		if _, err := testPool.Exec(context.Background(),
			`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
			testWorkspaceID, id,
		); err != nil {
			t.Fatalf("add member %s: %v", email, err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email LIKE 'team-%@multica.test'`)
	})
	return ids
}

func teamRuntimeUnit(t *testing.T, name, leadUserID string) string {
	t.Helper()
	var unitID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO org_unit (workspace_id, name, lead_user_id, position)
		 VALUES ($1, $2, $3, 0) RETURNING id`, testWorkspaceID, name, leadUserID,
	).Scan(&unitID); err != nil {
		t.Fatalf("create org unit %s: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM org_unit WHERE id = $1`, unitID)
	})
	return unitID
}

func teamRuntimePrivateRuntime(t *testing.T, ownerID string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, 'Team Visibility Test Runtime', 'cloud',
			'team_visibility_test_provider', 'online', 'team visibility test',
			'{}'::jsonb, $2, 'private', now())
		RETURNING id`, testWorkspaceID, ownerID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("create team visibility runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func buildTeamRuntimeFixture(t *testing.T) teamRuntimeFixture {
	t.Helper()
	users := teamRuntimeTestUsers(t)
	leadID := users["team-lead@multica.test"]
	subID := users["team-sub@multica.test"]
	peerID := users["team-peer@multica.test"]

	unitA := teamRuntimeUnit(t, "team-visibility-a", leadID)
	unitB := teamRuntimeUnit(t, "team-visibility-b", peerID)

	for _, row := range [][2]string{{subID, unitA}, {peerID, unitB}} {
		if _, err := testPool.Exec(context.Background(),
			`UPDATE member SET org_unit_id = $3 WHERE workspace_id = $1 AND user_id = $2`,
			testWorkspaceID, row[0], row[1],
		); err != nil {
			t.Fatalf("assign org unit: %v", err)
		}
	}

	return teamRuntimeFixture{
		leadUserID:    leadID,
		subUserID:     subID,
		peerUserID:    peerID,
		subRuntimeID:  teamRuntimePrivateRuntime(t, subID),
		peerRuntimeID: teamRuntimePrivateRuntime(t, peerID),
	}
}

func runtimeByID(t *testing.T, asUserID string, wantID string) *AgentRuntimeResponse {
	t.Helper()
	call := testutil.Call(t, testHandler.ListAgentRuntimes,
		newRequestAs(asUserID, http.MethodGet, "/api/runtimes", nil),
	).Want(http.StatusOK)
	var runtimes []AgentRuntimeResponse
	call.JSON(&runtimes)
	for _, runtime := range runtimes {
		if runtime.ID == wantID {
			return &runtime
		}
	}
	return nil
}

func TestListAgentRuntimes_DepartmentLeadSeesTeamRuntimesReadOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := buildTeamRuntimeFixture(t)

	if got := runtimeByID(t, f.leadUserID, f.subRuntimeID); got == nil || !got.ReadOnly {
		t.Fatalf("lead must see subordinate private runtime read_only=true, got %#v", got)
	}
	// The lead is NOT the peer department's lead, so the peer machine stays
	// hidden even though they see their own department's machine.
	if got := runtimeByID(t, f.leadUserID, f.peerRuntimeID); got != nil {
		t.Fatalf("lead must not see peer department's runtime, got %#v", got)
	}
}

func TestListAgentRuntimes_SubordinateRuntimeOwnerNotMarkedReadOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := buildTeamRuntimeFixture(t)

	if got := runtimeByID(t, f.subUserID, f.subRuntimeID); got == nil || got.ReadOnly {
		t.Fatalf("owner must see own runtime without read_only, got %#v", got)
	}
	// A member who leads no department sees no machine but their own.
	if got := runtimeByID(t, f.peerUserID, f.subRuntimeID); got != nil {
		t.Fatalf("peer must not see subordinate's private runtime, got %#v", got)
	}
}

// TestListAgentRuntimes_AdminsNeverMarkedReadOnly covers the governance branch:
// workspace admins get every row and none of them is read-only, so the UI
// keeps offering management actions for all machines.
func TestListAgentRuntimes_AdminsNeverMarkedReadOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	buildTeamRuntimeFixture(t)

	call := testutil.Call(t, testHandler.ListAgentRuntimes,
		newRequestAs(testUserID, http.MethodGet, "/api/runtimes", nil),
	).Want(http.StatusOK)
	var runtimes []AgentRuntimeResponse
	call.JSON(&runtimes)
	for _, runtime := range runtimes {
		if runtime.ReadOnly {
			t.Fatalf("admin runtime %s must never be read_only", runtime.ID)
		}
	}
}

// Team visibility widens the LIST only: the per-runtime usage read stays
// owner-scoped, so a lead who saw a row still cannot read the machine's data.
func TestDepartmentLeadCannotReadTeamRuntimeUsage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := buildTeamRuntimeFixture(t)

	// The gate answers 404 (not 403) so a non-owner cannot probe whether an
	// id names a real machine.
	req := withURLParam(
		newRequestAs(f.leadUserID, http.MethodGet, "/api/runtimes/"+f.subRuntimeID+"/usage", nil),
		"runtimeId", f.subRuntimeID,
	)
	testutil.Call(t, testHandler.GetRuntimeUsage, req).Want(http.StatusNotFound)
}
