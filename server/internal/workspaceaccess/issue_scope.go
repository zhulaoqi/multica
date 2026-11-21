package workspaceaccess

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueScope narrows which issues a viewer reads in the organization
// workspace. Every department dispatches there, so an issue carrying a
// department (issue.org_unit_id) is visible only to:
//
//   - members placed in that department or one of its sub-teams,
//   - leads of that department or one of its ancestors,
//   - the people directly involved: creator, assignee (or the owner of the
//     assigned agent, or a member of the assigned squad), and subscribers,
//   - the requester behind the work: the creator of any ancestor issue reads
//     the whole tree the leader decomposes it into, even when those child
//     issues are assigned to agents in another department.
//
// Issues without a department stay visible to the whole workspace. A nil scope
// means "no narrowing": projects, the organization's owners/admins, and the
// platform operator.
//
// ListIssueAudience (issue_scope.sql) mirrors this predicate for realtime
// delivery; change both together.
type IssueScope struct {
	ViewerID pgtype.UUID
	// Units are the departments whose issues the viewer reads: their own unit
	// and its ancestors, plus every unit they lead and its descendants.
	Units []pgtype.UUID
}

// IssueScopeQueries is the slice of generated queries ResolveIssueScope needs.
type IssueScopeQueries interface {
	ListViewerVisibleUnits(ctx context.Context, arg db.ListViewerVisibleUnitsParams) ([]pgtype.UUID, error)
}

// ResolveIssueScope returns the viewer's scope for the workspace in access, or
// nil when every issue there is visible to them.
func ResolveIssueScope(ctx context.Context, q IssueScopeQueries, access Access) (*IssueScope, error) {
	if !access.IsOrganization || access.IsOperator || access.IsAdmin() {
		return nil, nil
	}
	units, err := q.ListViewerVisibleUnits(ctx, db.ListViewerVisibleUnitsParams{
		WorkspaceID: access.WorkspaceID,
		UserID:      access.UserID,
	})
	if err != nil {
		return nil, fmt.Errorf("list visible units: %w", err)
	}
	if units == nil {
		units = []pgtype.UUID{}
	}
	return &IssueScope{ViewerID: access.UserID, Units: units}, nil
}

// Predicate returns a SQL boolean over the issue aliased `alias`, binding its
// parameters through addArg. A nil scope returns "TRUE".
func (s *IssueScope) Predicate(alias string, addArg func(any) string) string {
	if s == nil {
		return "TRUE"
	}
	viewer := addArg(s.ViewerID)
	units := addArg(s.Units)
	return fmt.Sprintf(`(
    %[1]s.org_unit_id IS NULL
    OR %[1]s.org_unit_id = ANY(%[3]s::uuid[])
    OR (%[1]s.creator_type = 'member' AND %[1]s.creator_id = %[2]s::uuid)
    OR (%[1]s.assignee_type = 'member' AND %[1]s.assignee_id = %[2]s::uuid)
    OR (%[1]s.creator_type = 'agent' AND EXISTS (
        SELECT 1 FROM agent scope_a WHERE scope_a.id = %[1]s.creator_id AND scope_a.owner_id = %[2]s::uuid
    ))
    OR (%[1]s.assignee_type = 'agent' AND EXISTS (
        SELECT 1 FROM agent scope_a WHERE scope_a.id = %[1]s.assignee_id AND scope_a.owner_id = %[2]s::uuid
    ))
    OR (%[1]s.assignee_type = 'squad' AND (
        EXISTS (
            SELECT 1 FROM squad_member scope_sm
            WHERE scope_sm.squad_id = %[1]s.assignee_id
              AND scope_sm.member_type = 'member'
              AND scope_sm.member_id = %[2]s::uuid
        )
        OR EXISTS (
            SELECT 1 FROM squad_member scope_sm
            JOIN agent scope_a ON scope_a.id = scope_sm.member_id
            WHERE scope_sm.squad_id = %[1]s.assignee_id
              AND scope_sm.member_type = 'agent'
              AND scope_a.owner_id = %[2]s::uuid
        )
        OR EXISTS (
            SELECT 1 FROM squad scope_s
            JOIN agent scope_a ON scope_a.id = scope_s.leader_id
            WHERE scope_s.id = %[1]s.assignee_id AND scope_a.owner_id = %[2]s::uuid
        )
    ))
    OR EXISTS (
        SELECT 1 FROM issue_subscriber scope_sub
        WHERE scope_sub.issue_id = %[1]s.id
          AND scope_sub.user_type = 'member'
          AND scope_sub.user_id = %[2]s::uuid
          AND scope_sub.unsubscribed_at IS NULL
    )
    OR EXISTS (
        WITH RECURSIVE scope_chain AS (
            SELECT %[1]s.parent_issue_id AS id
            WHERE %[1]s.parent_issue_id IS NOT NULL
            UNION
            SELECT scope_parent.parent_issue_id
            FROM scope_chain
            JOIN issue scope_parent ON scope_parent.id = scope_chain.id
            WHERE scope_parent.parent_issue_id IS NOT NULL
        )
        SELECT 1
        FROM scope_chain
        JOIN issue scope_ancestor ON scope_ancestor.id = scope_chain.id
        WHERE scope_ancestor.creator_type = 'member'
          AND scope_ancestor.creator_id = %[2]s::uuid
    )
)`, alias, viewer, units)
}

// RowQuerier runs the visibility checks; *pgxpool.Pool and pgx.Tx satisfy it.
type RowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// VisibleIDs returns which of ids the viewer may read, for static queries that
// cannot compose Predicate. A nil scope admits every id.
func (s *IssueScope) VisibleIDs(ctx context.Context, q RowQuerier, ids []pgtype.UUID) (map[pgtype.UUID]bool, error) {
	visible := make(map[pgtype.UUID]bool, len(ids))
	if s == nil {
		for _, id := range ids {
			visible[id] = true
		}
		return visible, nil
	}
	if len(ids) == 0 {
		return visible, nil
	}
	args := []any{ids}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	query := fmt.Sprintf(`SELECT i.id FROM issue i WHERE i.id = ANY($1::uuid[]) AND %s`, s.Predicate("i", addArg))
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("check issue visibility: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan visible issue: %w", err)
		}
		visible[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("check issue visibility: %w", err)
	}
	return visible, nil
}

// Allows reports whether the viewer may read the issue. The common cases are
// answered without a query.
func (s *IssueScope) Allows(ctx context.Context, q RowQuerier, issue db.Issue) (bool, error) {
	if s == nil || !issue.OrgUnitID.Valid {
		return true, nil
	}
	for _, unit := range s.Units {
		if unit == issue.OrgUnitID {
			return true, nil
		}
	}
	if (issue.CreatorType == "member" && issue.CreatorID == s.ViewerID) ||
		(issue.AssigneeType.String == "member" && issue.AssigneeID == s.ViewerID) {
		return true, nil
	}
	args := []any{issue.ID}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM issue i WHERE i.id = $1 AND %s)`, s.Predicate("i", addArg))
	var visible bool
	if err := q.QueryRow(ctx, query, args...).Scan(&visible); err != nil {
		return false, fmt.Errorf("check issue visibility: %w", err)
	}
	return visible, nil
}
