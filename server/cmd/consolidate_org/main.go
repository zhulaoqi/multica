// consolidate_org performs the one-time conversion to a single organization
// workspace: the organization owns the only department tree and every agent,
// and department projects point into it.
//
//  1. Creates the organization workspace when none exists (-name, -slug,
//     -owner-email). A run that creates it stops after the directory sync so
//     the remap below can be reviewed against the synced tree.
//  2. Runs the Feishu directory sync into the organization (needs
//     MULTICA_FEISHU_APP_ID / MULTICA_FEISHU_APP_SECRET; -skip-sync skips it).
//  3. Remaps every row that points at a project-local unit onto the
//     organization unit with the same external_id, falling back to the same
//     name path. Any unmatched unit blocks -apply.
//  4. Moves project agents into the organization: each owned user agent gets
//     an organization copy (or links to the organization agent with the same
//     owner and name) and becomes its projection. Row ids do not change, so
//     task history, squads and chats keep working.
//  5. Deletes the project-local units and the retired directory rows.
//
// Default is a read-only dry run. Pass -apply to execute. Every step is
// idempotent, so a partial run can be resumed. Restart the API afterwards so
// its caches drop the old trees; daemons pick up the organization workspace on
// their next workspace sync (at most 30 minutes, or on restart).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/feishu/contact"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type options struct {
	apply      bool
	name       string
	slug       string
	ownerEmail string
	skipSync   bool
}

type runner struct {
	opts options
	pool *pgxpool.Pool
	q    *db.Queries
	h    *handler.Handler
}

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("consolidate_org failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var opts options
	flag.BoolVar(&opts.apply, "apply", false, "execute the conversion (default is a read-only dry run)")
	flag.StringVar(&opts.name, "name", "", "organization workspace name, when it has to be created")
	flag.StringVar(&opts.slug, "slug", "", "organization workspace slug, when it has to be created")
	flag.StringVar(&opts.ownerEmail, "owner-email", "", "operator who owns a newly created organization workspace")
	flag.BoolVar(&opts.skipSync, "skip-sync", false, "do not run the Feishu directory sync")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	r := &runner{opts: opts, pool: pool, q: db.New(pool)}
	r.h = newHandler(r.q, pool)
	return r.run(ctx)
}

// newHandler builds a handler for the shared organization logic. Nothing
// subscribes to its bus, so the events it publishes go nowhere; the restart
// afterwards refreshes connected clients.
func newHandler(q *db.Queries, pool *pgxpool.Pool) *handler.Handler {
	hub := realtime.NewHub()
	go hub.Run()
	h := handler.New(q, pool, hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, handler.Config{})
	if directory := contact.NewFromEnv(); directory != nil {
		h.OrgDirectory = directory
	}
	return h
}

func (r *runner) run(ctx context.Context) error {
	org, created, err := r.stepOrganization(ctx)
	if errors.Is(err, errStopAfterDryRun) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := r.stepSync(ctx, org); err != nil {
		return err
	}
	if created {
		fmt.Println("\nThe organization workspace was just created and synced. Re-run without -apply to review the remap, then with -apply to finish.")
		return nil
	}
	if err := r.stepRemapUnits(ctx, org); err != nil {
		return err
	}
	if err := r.stepMoveAgents(ctx, org); err != nil {
		return err
	}
	if err := r.stepDeleteProjectTrees(ctx); err != nil {
		return err
	}
	if !r.opts.apply {
		fmt.Println("\ndry run: nothing changed. Re-run with -apply to execute.")
		return nil
	}
	if err := r.h.FollowOrganization(ctx); err != nil {
		return fmt.Errorf("follow organization: %w", err)
	}
	fmt.Println("\nDone. Restart the API now.")
	return nil
}

// stepOrganization returns the organization workspace, creating it on -apply
// when none exists. created reports that this run made it.
func (r *runner) stepOrganization(ctx context.Context) (db.Workspace, bool, error) {
	fmt.Println("== 1. organization workspace")
	org, err := r.q.GetOrganizationWorkspace(ctx)
	if err == nil {
		fmt.Printf("exists: %s (%s)\n", org.Name, org.Slug)
		return org, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Workspace{}, false, fmt.Errorf("load organization: %w", err)
	}

	name, slug := strings.TrimSpace(r.opts.name), strings.TrimSpace(r.opts.slug)
	if name == "" || slug == "" || r.opts.ownerEmail == "" {
		return db.Workspace{}, false, errors.New("no organization workspace yet: pass -name, -slug and -owner-email")
	}
	if !handler.ValidNewWorkspaceSlug(slug) {
		return db.Workspace{}, false, fmt.Errorf("slug %q is invalid or reserved", slug)
	}
	if _, err := r.q.GetWorkspaceBySlug(ctx, slug); err == nil {
		return db.Workspace{}, false, fmt.Errorf("slug %q is already taken", slug)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Workspace{}, false, fmt.Errorf("check slug: %w", err)
	}
	owner, err := r.q.GetUserByLowerEmail(ctx, r.opts.ownerEmail)
	if err != nil {
		return db.Workspace{}, false, fmt.Errorf("owner %q: %w", r.opts.ownerEmail, err)
	}
	fmt.Printf("create: %s (%s), owner %s\n", name, slug, owner.Email)
	if !r.opts.apply {
		fmt.Println("\ndry run: the later steps are planned once the organization exists and is synced. Re-run with -apply to create it.")
		return db.Workspace{}, false, errStopAfterDryRun
	}

	org, err = r.createOrganization(ctx, name, slug, owner.ID)
	if err != nil {
		return db.Workspace{}, false, err
	}
	slog.Info("organization workspace created", "workspace_id", util.UUIDToString(org.ID), "slug", org.Slug)
	return org, true, nil
}

var errStopAfterDryRun = errors.New("stop")

func (r *runner) createOrganization(ctx context.Context, name, slug string, ownerID pgtype.UUID) (db.Workspace, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return db.Workspace{}, err
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)
	ws, err := qtx.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        name,
		Slug:        slug,
		IssuePrefix: handler.DefaultIssuePrefixFromSlug(slug),
	})
	if err != nil {
		return db.Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	if ws, err = qtx.MarkWorkspaceOrganization(ctx, ws.ID); err != nil {
		return db.Workspace{}, fmt.Errorf("mark organization: %w", err)
	}
	if _, err := qtx.CreateMember(ctx, db.CreateMemberParams{WorkspaceID: ws.ID, UserID: ownerID, Role: "owner"}); err != nil {
		return db.Workspace{}, fmt.Errorf("add owner: %w", err)
	}
	if err := issuestatus.Ensure(ctx, qtx, ws.ID); err != nil {
		return db.Workspace{}, fmt.Errorf("seed issue statuses: %w", err)
	}
	return ws, tx.Commit(ctx)
}

func (r *runner) stepSync(ctx context.Context, org db.Workspace) error {
	fmt.Println("\n== 2. directory sync")
	switch {
	case r.opts.skipSync:
		fmt.Println("skipped (-skip-sync)")
		return nil
	case r.h.OrgDirectory == nil:
		return errors.New("Feishu is not configured (MULTICA_FEISHU_APP_ID / MULTICA_FEISHU_APP_SECRET); pass -skip-sync to plan against the current tree")
	case !r.opts.apply:
		fmt.Printf("would sync the Feishu directory into %s\n", org.Slug)
		return nil
	}
	if _, err := r.h.SyncAllDirectories(ctx); err != nil {
		return fmt.Errorf("directory sync: %w", err)
	}
	fmt.Printf("synced into %s\n", org.Slug)
	return nil
}

// stepRemapUnits points rows at organization units instead of project-local
// ones.
func (r *runner) stepRemapUnits(ctx context.Context, org db.Workspace) error {
	fmt.Println("\n== 3. department remap")
	units, err := r.q.ListAllOrgUnits(ctx)
	if err != nil {
		return fmt.Errorf("list units: %w", err)
	}
	referenced, err := r.q.ListReferencedOrgUnitIDs(ctx)
	if err != nil {
		return fmt.Errorf("list referenced units: %w", err)
	}
	remaps := planUnitRemaps(units, org.ID, referenced)
	unmatched := 0
	for _, m := range remaps {
		if m.To.Valid {
			fmt.Printf("  %s -> %s (by %s)\n", m.Path, util.UUIDToString(m.To), m.By)
		} else {
			unmatched++
			fmt.Printf("  %s -> NO MATCH (%s)\n", m.Path, util.UUIDToString(m.From))
		}
	}
	fmt.Printf("%d referenced project units, %d unmatched\n", len(remaps), unmatched)
	if unmatched > 0 && r.opts.apply {
		return fmt.Errorf("%d units have no organization match: add them to the organization tree or clear their references, then re-run", unmatched)
	}
	if !r.opts.apply {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)
	for _, m := range remaps {
		if err := remapUnit(ctx, qtx, m.From, m.To); err != nil {
			return fmt.Errorf("remap %s: %w", m.Path, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Info("units remapped", "count", len(remaps))
	return nil
}

func remapUnit(ctx context.Context, q *db.Queries, from, to pgtype.UUID) error {
	if _, err := q.RemapWorkspaceUnits(ctx, db.RemapWorkspaceUnitsParams{OldID: from, NewID: to}); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	if _, err := q.RemapMemberUnits(ctx, db.RemapMemberUnitsParams{OldID: from, NewID: to}); err != nil {
		return fmt.Errorf("member: %w", err)
	}
	if _, err := q.RemapAgentUnits(ctx, db.RemapAgentUnitsParams{OldID: from, NewID: to}); err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	if _, err := q.RemapSquadUnits(ctx, db.RemapSquadUnitsParams{OldID: from, NewID: to}); err != nil {
		return fmt.Errorf("squad: %w", err)
	}
	if _, err := q.RemapIssueUnits(ctx, db.RemapIssueUnitsParams{OldID: from, NewID: to}); err != nil {
		return fmt.Errorf("issue: %w", err)
	}
	if _, err := q.RemapDispatchOriginUnits(ctx, db.RemapDispatchOriginUnitsParams{OldID: from, NewID: to}); err != nil {
		return fmt.Errorf("dispatch origin: %w", err)
	}
	return nil
}

func (r *runner) stepDeleteProjectTrees(ctx context.Context) error {
	fmt.Println("\n== 5. retire project trees")
	units, err := r.q.ListAllOrgUnits(ctx)
	if err != nil {
		return fmt.Errorf("list units: %w", err)
	}
	directory, err := r.q.CountDirectoryMembers(ctx)
	if err != nil {
		return fmt.Errorf("count directory rows: %w", err)
	}
	projectUnits := map[pgtype.UUID]bool{}
	for _, u := range units {
		if u.InProject {
			projectUnits[u.ID] = true
		}
	}
	fmt.Printf("delete %d project-local units and %d directory rows\n", len(projectUnits), directory)
	if !r.opts.apply {
		return nil
	}

	referenced, err := r.q.ListReferencedOrgUnitIDs(ctx)
	if err != nil {
		return fmt.Errorf("list referenced units: %w", err)
	}
	for _, id := range referenced {
		if projectUnits[id] {
			return fmt.Errorf("unit %s is still referenced after the remap; nothing deleted", util.UUIDToString(id))
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)
	deletedUnits, err := qtx.DeleteProjectOrgUnits(ctx)
	if err != nil {
		return fmt.Errorf("delete project units: %w", err)
	}
	deletedDirectory, err := qtx.DeleteAllDirectoryMembers(ctx)
	if err != nil {
		return fmt.Errorf("delete directory rows: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Info("project trees retired", "units", deletedUnits, "directory_rows", deletedDirectory)
	return nil
}
