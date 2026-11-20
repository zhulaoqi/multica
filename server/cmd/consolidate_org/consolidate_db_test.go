package main

import (
	"context"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestConsolidateMovesProjectIntoOrganization(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	ctx := context.Background()
	pool := testutil.IsolatedDB(t, os.Getenv("DATABASE_URL"))
	f := testutil.New(pool, "", "")

	ada := f.User(t, "Ada", "ada@example.com")
	org := f.Workspace(t, "Org", "org", testutil.Cols{"is_organization": true})
	orgRoot := f.Insert(t, "org_unit", testutil.Cols{"workspace_id": org, "name": "公司", "external_id": "od-root"})
	orgRD := f.Insert(t, "org_unit", testutil.Cols{"workspace_id": org, "parent_id": orgRoot, "name": "研发", "external_id": "od-rd"})

	project := f.Workspace(t, "Project", "project")
	projectRoot := f.Insert(t, "org_unit", testutil.Cols{"workspace_id": project, "name": "公司", "external_id": "od-root"})
	projectRD := f.Insert(t, "org_unit", testutil.Cols{"workspace_id": project, "parent_id": projectRoot, "name": "研发", "external_id": "od-rd"})
	f.Exec(t, `UPDATE workspace SET org_unit_id = $1 WHERE id = $2`, projectRD, project)
	f.Member(t, project, ada, "owner", testutil.Cols{"org_unit_id": projectRD})
	f.Insert(t, "workspace_directory_member", testutil.Cols{"workspace_id": project, "user_id": ada, "org_unit_id": projectRD})

	runtime := f.Runtime(t, "Claude", testutil.Cols{
		"workspace_id": project, "owner_id": ada, "daemon_id": "daemon-1",
		"runtime_mode": "local", "provider": "claude",
	})
	agent := f.Agent(t, "Ada", runtime, testutil.Cols{
		"workspace_id": project, "owner_id": ada, "is_primary": true,
		"instructions": "be Ada", "org_unit_id": projectRD,
	})
	f.Squad(t, "BI", agent, testutil.Cols{"workspace_id": project, "creator_id": ada, "org_unit_id": projectRD})

	q := db.New(pool)
	newRunner := func(apply bool) *runner {
		r := &runner{opts: options{apply: apply, skipSync: true}, pool: pool, q: q}
		r.h = newHandler(q, pool)
		return r
	}
	agents := func() int {
		return f.Count(t, `SELECT count(*) FROM agent`)
	}

	if err := newRunner(false).run(ctx); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if n := f.Count(t, `SELECT count(*) FROM org_unit`); n != 4 || agents() != 1 {
		t.Fatalf("dry run changed data: %d units, %d agents", n, agents())
	}

	if err := newRunner(true).run(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var projectUnit, sourceID, orgUnit, orgRuntimeWorkspace, instructions string
	var orgPrimary bool
	f.QueryRow(t, `SELECT org_unit_id::text FROM workspace WHERE id = $1`, project).Scan(&projectUnit)
	if projectUnit != orgRD {
		t.Errorf("project unit = %s, want organization unit %s", projectUnit, orgRD)
	}
	f.QueryRow(t, `SELECT source_agent_id::text FROM agent WHERE id = $1`, agent).Scan(&sourceID)
	f.QueryRow(t, `
		SELECT a.org_unit_id::text, r.workspace_id::text, a.instructions, a.is_primary
		FROM agent a JOIN agent_runtime r ON r.id = a.runtime_id
		WHERE a.id = $1 AND a.workspace_id = $2`, sourceID, org).
		Scan(&orgUnit, &orgRuntimeWorkspace, &instructions, &orgPrimary)
	if orgUnit != orgRD || orgRuntimeWorkspace != org || instructions != "be Ada" || !orgPrimary {
		t.Errorf("organization copy = unit %s, runtime workspace %s, instructions %q, primary %t", orgUnit, orgRuntimeWorkspace, instructions, orgPrimary)
	}
	if n := f.Count(t, `SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, org, ada); n != 1 {
		t.Errorf("owner organization member rows = %d, want 1", n)
	}
	if n := f.Count(t, `SELECT count(*) FROM squad WHERE org_unit_id = $1`, orgRD); n != 1 {
		t.Errorf("squads on the organization unit = %d, want 1", n)
	}
	if n := f.Count(t, `SELECT count(*) FROM org_unit WHERE workspace_id = $1`, project); n != 0 {
		t.Errorf("project units left = %d", n)
	}
	if n := f.Count(t, `SELECT count(*) FROM workspace_directory_member`); n != 0 {
		t.Errorf("directory rows left = %d", n)
	}

	before := agents()
	if err := newRunner(true).run(ctx); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if agents() != before {
		t.Errorf("second apply created agents: %d -> %d", before, agents())
	}
}

func TestConsolidateCreatesOrganizationAndStops(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	ctx := context.Background()
	pool := testutil.IsolatedDB(t, os.Getenv("DATABASE_URL"))
	f := testutil.New(pool, "", "")
	ada := f.User(t, "Ada", "ada@example.com")
	q := db.New(pool)
	opts := options{name: "Org", slug: "org", ownerEmail: "ADA@example.com", skipSync: true}

	dry := &runner{opts: opts, pool: pool, q: q, h: newHandler(q, pool)}
	if err := dry.run(ctx); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if n := f.Count(t, `SELECT count(*) FROM workspace`); n != 0 {
		t.Fatalf("dry run created %d workspaces", n)
	}

	opts.apply = true
	apply := &runner{opts: opts, pool: pool, q: q, h: newHandler(q, pool)}
	if err := apply.run(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if n := f.Count(t, `
		SELECT count(*) FROM workspace w JOIN member m ON m.workspace_id = w.id
		WHERE w.slug = 'org' AND w.is_organization AND m.user_id = $1 AND m.role = 'owner'`, ada); n != 1 {
		t.Fatalf("organization with owner = %d, want 1", n)
	}
}
