// Package workspaceaccess is the single policy for "may this user act in this
// workspace?". A workspace is a project owned by one department; its member
// scope is the owning department's subtree. Legacy workspaces (no owning
// department) keep the flat member-row rule.
//
// Every read path that used to equate "has a member row" with "may read the
// workspace" must resolve through this package instead.
package workspaceaccess

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Access is the resolved answer for one (workspace, user) pair.
type Access struct {
	WorkspaceID pgtype.UUID

	// Root is the owning department's root unit. RootUnitID is invalid for a
	// legacy workspace; RootExists is false when a configured root no longer
	// resolves to a unit in this workspace, which fails closed.
	RootUnitID     pgtype.UUID
	RootExists     bool
	RootLeadUserID pgtype.UUID

	UserID      pgtype.UUID
	HasMember   bool
	MemberID    pgtype.UUID
	MemberSince pgtype.Timestamptz
	Role        string
	MemberUnit  pgtype.UUID
	InSubtree   bool
	IsOperator  bool
}

// Queries is the slice of generated queries Resolve needs. Integrations with
// their own narrow query interfaces use ResolveWith so every path shares one
// policy instead of re-checking member rows.
type Queries interface {
	GetWorkspaceAccess(ctx context.Context, arg db.GetWorkspaceAccessParams) (db.GetWorkspaceAccessRow, error)
}

// Resolve loads the access row once. Operator status comes from the request and
// bypasses membership; the query still verifies the workspace exists.
func Resolve(ctx context.Context, q *db.Queries, workspaceID, userID pgtype.UUID, isOperator bool) (Access, error) {
	return ResolveWith(ctx, q, workspaceID, userID, isOperator)
}

// ResolveWith is Resolve over the narrow Queries interface.
func ResolveWith(ctx context.Context, q Queries, workspaceID, userID pgtype.UUID, isOperator bool) (Access, error) {
	row, err := q.GetWorkspaceAccess(ctx, db.GetWorkspaceAccessParams{
		ID:     workspaceID,
		UserID: userID,
	})
	if err != nil {
		return Access{}, err
	}
	return Access{
		WorkspaceID:    row.WorkspaceID,
		RootUnitID:     row.RootUnitID,
		RootExists:     row.RootExists,
		RootLeadUserID: row.RootLeadUserID,
		UserID:         userID,
		HasMember:      row.MemberID.Valid,
		MemberID:       row.MemberID,
		MemberSince:    row.MemberCreatedAt,
		Role:           row.Role,
		MemberUnit:     row.MemberOrgUnitID,
		InSubtree:      row.InSubtree,
		IsOperator:     isOperator,
	}, nil
}

// Legacy reports a workspace without an owning department.
func (a Access) Legacy() bool { return !a.RootUnitID.Valid }

// IsOwner is the stored project-owner role, not department leadership.
func (a Access) IsOwner() bool { return a.Role == "owner" }

// IsAdmin is the stored project-admin role used for the strict surfaces
// (integrations, plugins, MCP, share links, billing).
func (a Access) IsAdmin() bool { return a.Role == "owner" || a.Role == "admin" }

// IsOrgLead reports that the caller leads the owning department. The member row
// is normally promoted to owner; this is the fallback when the role drifts.
func (a Access) IsOrgLead() bool {
	return a.HasMember && a.RootExists && a.RootLeadUserID.Valid && a.RootLeadUserID == a.UserID
}

// IsManager may administer the project: members, roles, settings.
func (a Access) IsManager() bool {
	return a.IsOperator || a.IsAdmin() || a.IsOrgLead()
}

// Allowed may read project content (issues, agents, runtimes, …).
func (a Access) Allowed() bool {
	if a.IsOperator {
		return true
	}
	if !a.HasMember {
		return false
	}
	if a.Legacy() {
		return true
	}
	if !a.RootExists {
		return false
	}
	return a.IsAdmin() || a.InSubtree
}

// Viewer may open the settings pages (general, members, invitations) even when
// the organisation moved them outside the subtree; content routes still use
// Allowed.
func (a Access) Viewer() bool { return a.Allowed() || a.IsManager() }

// SubtreeUnits returns the owning department's subtree as the shared unit rows
// used by orgscope. A legacy workspace returns nil; callers then read the whole
// tree.
func SubtreeUnits(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) ([]db.ListOrgUnitsRow, error) {
	rows, err := q.ListWorkspaceSubtreeUnits(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	units := make([]db.ListOrgUnitsRow, len(rows))
	for i, row := range rows {
		units[i] = db.ListOrgUnitsRow{
			ID:          row.ID,
			ParentID:    row.ParentID,
			Name:        row.Name,
			LeadUserID:  row.LeadUserID,
			Description: row.Description,
			Position:    row.Position,
		}
	}
	return units, nil
}

// UnitIDs maps a subtree to its id set for roster/directory filtering.
func UnitIDs(units []db.ListOrgUnitsRow) map[pgtype.UUID]bool {
	ids := make(map[pgtype.UUID]bool, len(units))
	for _, unit := range units {
		ids[unit.ID] = true
	}
	return ids
}
