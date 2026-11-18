// reset_project_members performs the one-time cleanup that turns the existing
// workspaces into department projects: every member row is removed except the
// owners/admins and the owning department's lead, who is promoted to owner
// first. It runs the same revocation sequence as DELETE /members, so runtimes,
// agents, tasks, subscriptions, IM bindings and seat intents follow the member
// row instead of being orphaned by a bare DELETE.
//
// Default is a read-only dry run that prints keep/remove per workspace. Pass
// -apply to execute. The run is idempotent: once a workspace holds only
// owners/admins and its lead, a rerun has nothing to remove and only repeats
// the invitation/share-link retirement.
//
// Run it in a maintenance window, then restart the API so removed members'
// realtime connections drop and the API's caches expire. The command does not
// publish in-process events and does not need the API's Hub.
//
// Removed people remain in workspace_directory_member and can be pulled back
// into the project by a manager; their IM bindings are pruned by the
// revocation, so they must re-bind (or wait for the next org sync) afterwards.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/memberrevoke"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type memberRow = db.ListOwnedWorkspaceMemberRowsRow

// workspacePlan is the keep/remove decision for one department project.
type workspacePlan struct {
	workspaceID pgtype.UUID
	name        string
	rootUnitID  pgtype.UUID
	leadID      pgtype.UUID
	leadName    string
	leadEmail   string
	// leadMemberID is the lead's existing member row, if any. A zero value means
	// the promotion inserts a new owner row at the project root.
	leadMemberID pgtype.UUID
	promote      bool
	keep         []memberRow
	remove       []memberRow
	// reparent holds kept owners/admins whose placement is empty; they are
	// backfilled to the project root.
	reparent []memberRow
}

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("reset project members failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	apply := flag.Bool("apply", false, "remove the members; without it this is a read-only dry run")
	releaseCapacity := flag.Bool("release-capacity", true, "enqueue a seat-release intent for every removed member")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	q := db.New(pool)
	rows, err := q.ListOwnedWorkspaceMemberRows(ctx)
	if err != nil {
		return fmt.Errorf("list owned workspace members: %w", err)
	}

	plans := planWorkspaces(rows)
	removed, promoted := 0, 0
	for _, plan := range plans {
		printPlan(plan)
		removed += len(plan.remove)
		if plan.promote {
			promoted++
		}
	}
	fmt.Printf("\n%d department projects, %d members to remove, %d lead promotions\n", len(plans), removed, promoted)
	if !*apply {
		fmt.Println("dry run: nothing changed. Re-run with -apply to execute.")
		return nil
	}

	revoker := memberrevoke.Service{Queries: q, TxStarter: pool}
	for _, plan := range plans {
		wsID := uuidOrEmpty(plan.workspaceID)
		if err := applyPlan(ctx, q, revoker, plan, *releaseCapacity); err != nil {
			return fmt.Errorf("workspace %s (%s): %w", plan.name, wsID, err)
		}
		// Department projects add members directly; any invitation or share link
		// left over from the flat model must not be redeemable afterwards.
		if err := q.ExpireWorkspacePendingInvitations(ctx, plan.workspaceID); err != nil {
			return fmt.Errorf("expire invitations for workspace %s (%s): %w", plan.name, wsID, err)
		}
		if err := q.DeactivateWorkspaceShareLinks(ctx, plan.workspaceID); err != nil {
			return fmt.Errorf("deactivate share links for workspace %s (%s): %w", plan.name, wsID, err)
		}
		slog.Info("project members reset",
			"workspace_id", wsID, "workspace", plan.name,
			"removed", len(plan.remove), "kept", len(plan.keep), "promoted_lead", plan.promote)
	}
	fmt.Printf("done: %d projects processed, %d members removed\n", len(plans), removed)
	return nil
}

// planWorkspaces groups the flat rows by workspace and computes the keep set
// after the lead promotion, matching what the apply phase will leave behind.
func planWorkspaces(rows []memberRow) []*workspacePlan {
	byID := make(map[pgtype.UUID]*workspacePlan)
	order := make([]*workspacePlan, 0)
	for _, row := range rows {
		plan, ok := byID[row.WorkspaceID]
		if !ok {
			plan = &workspacePlan{
				workspaceID: row.WorkspaceID,
				name:        row.WorkspaceName,
				rootUnitID:  row.RootUnitID,
				leadID:      row.RootLeadUserID,
				leadName:    textOrEmpty(row.RootLeadName),
				leadEmail:   textOrEmpty(row.RootLeadEmail),
			}
			byID[row.WorkspaceID] = plan
			order = append(order, plan)
		}
		if !row.MemberID.Valid {
			continue
		}
		if plan.leadID.Valid && row.UserID == plan.leadID {
			plan.leadMemberID = row.MemberID
			plan.promote = row.Role.String != "owner"
		}
	}
	// A lead with no member row is promoted by inserting one.
	for _, plan := range order {
		if plan.leadID.Valid && !plan.leadMemberID.Valid {
			plan.promote = true
		}
	}

	for _, row := range rows {
		plan := byID[row.WorkspaceID]
		if !row.MemberID.Valid {
			continue
		}
		isLead := plan.leadID.Valid && row.UserID == plan.leadID
		switch {
		case isLead || row.Role.String == "owner" || row.Role.String == "admin":
			plan.keep = append(plan.keep, row)
			if !row.MemberOrgUnitID.Valid {
				plan.reparent = append(plan.reparent, row)
			}
		default:
			plan.remove = append(plan.remove, row)
		}
	}
	return order
}

func printPlan(plan *workspacePlan) {
	fmt.Printf("\n%s (%s) root=%s\n", plan.name, uuidOrEmpty(plan.workspaceID), uuidOrEmpty(plan.rootUnitID))
	switch {
	case plan.promote && plan.leadMemberID.Valid:
		fmt.Printf("  promote %s <%s> to owner\n", plan.leadName, plan.leadEmail)
	case plan.promote:
		fmt.Printf("  add %s <%s> as owner (department lead has no member row)\n", plan.leadName, plan.leadEmail)
	}
	for _, row := range plan.keep {
		fmt.Printf("  keep   %s <%s> role=%s unit=%s\n",
			textOrEmpty(row.MemberName), textOrEmpty(row.MemberEmail), row.Role.String, unitLabel(row))
	}
	for _, row := range plan.remove {
		fmt.Printf("  remove %s <%s> role=%s unit=%s\n",
			textOrEmpty(row.MemberName), textOrEmpty(row.MemberEmail), row.Role.String, unitLabel(row))
	}
}

func applyPlan(ctx context.Context, q *db.Queries, revoker memberrevoke.Service, plan *workspacePlan, releaseCapacity bool) error {
	switch {
	case plan.promote && plan.leadMemberID.Valid:
		if _, err := q.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{ID: plan.leadMemberID, Role: "owner"}); err != nil {
			return fmt.Errorf("promote lead to owner: %w", err)
		}
	case plan.promote:
		if _, err := q.CreateMember(ctx, db.CreateMemberParams{
			WorkspaceID: plan.workspaceID,
			UserID:      plan.leadID,
			Role:        "owner",
			OrgUnitID:   plan.rootUnitID,
		}); err != nil {
			return fmt.Errorf("insert lead as owner: %w", err)
		}
	}
	for _, row := range plan.reparent {
		if err := q.SetMemberOrgUnit(ctx, db.SetMemberOrgUnitParams{
			WorkspaceID: row.WorkspaceID,
			UserID:      row.UserID,
			OrgUnitID:   plan.rootUnitID,
		}); err != nil {
			return fmt.Errorf("place kept member at project root: %w", err)
		}
	}
	for _, row := range plan.remove {
		if _, err := revoker.Revoke(ctx, memberrevoke.Params{
			WorkspaceID:     row.WorkspaceID,
			UserID:          row.UserID,
			MemberID:        row.MemberID,
			ReleaseCapacity: releaseCapacity,
		}); err != nil {
			return fmt.Errorf("remove member %s <%s>: %w", textOrEmpty(row.MemberName), textOrEmpty(row.MemberEmail), err)
		}
		slog.Info("project member removed",
			"workspace_id", uuidOrEmpty(row.WorkspaceID),
			"user_id", uuidOrEmpty(row.UserID),
			"email", textOrEmpty(row.MemberEmail))
	}
	return nil
}

func unitLabel(row memberRow) string {
	if !row.MemberOrgUnitID.Valid {
		return "(none)"
	}
	if row.MemberUnitName.Valid && row.MemberUnitName.String != "" {
		return row.MemberUnitName.String
	}
	return uuidOrEmpty(row.MemberOrgUnitID)
}

func uuidOrEmpty(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return util.UUIDToString(id)
}

func textOrEmpty(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
