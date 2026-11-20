// Package workspaceaccess is the single policy for "may this user act in this
// workspace?". A workspace is a project owned by one department. An explicit
// member row grants access regardless of the person's department, so a project
// manager can pull in colleagues from other teams; department leads above the
// owning department inherit access upward without a member row. Legacy
// workspaces (no owning department) keep the flat member-row rule.
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
	// IsOrganization marks the organization workspace, whose issues are scoped
	// by department (see IssueScope).
	IsOrganization bool

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
	// AncestorLead reports that the caller leads the owning department or one
	// of its ancestors. This grants upward-inherited access: a parent
	// department's lead may use a project owned by a child department, without
	// a member row. Descendants never inherit.
	AncestorLead bool
	IsOperator   bool
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
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return Access{}, err
	}
	return Access{
		WorkspaceID:    row.WorkspaceID,
		IsOrganization: row.IsOrganization,
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
		AncestorLead:   row.AncestorLead,
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

// Implicit reports access without a member row: the caller is a department
// lead above the owning department. They may use the project but do not hold a
// seat or appear in the member table.
func (a Access) Implicit() bool { return a.AncestorLead && !a.HasMember }

// Allowed may read project content (issues, agents, runtimes, …).
//
// An explicit member row always grants access, wherever the person is placed:
// project managers may pull in colleagues from other departments. Department
// leads above the owning department get the same content access implicitly.
func (a Access) Allowed() bool {
	if a.IsOperator {
		return true
	}
	if a.Legacy() {
		return a.HasMember
	}
	if !a.RootExists {
		return false
	}
	return a.HasMember || a.AncestorLead
}

// Viewer may open the settings pages (general, members, invitations) even when
// the organisation moved them outside the subtree; content routes still use
// Allowed.
func (a Access) Viewer() bool { return a.Allowed() || a.IsManager() }

// EffectiveMember is the member identity handlers use for reads and writes.
// Implicit ancestor access has no member row, so it gets a synthetic identity
// keyed by the user (the operator path does the same). Manager-only handlers
// still require a real row, so the synthetic identity never administers the
// project or mutates member records.
func (a Access) EffectiveMember() db.Member {
	if a.HasMember {
		return db.Member{
			ID:          a.MemberID,
			WorkspaceID: a.WorkspaceID,
			UserID:      a.UserID,
			Role:        a.Role,
			CreatedAt:   a.MemberSince,
			OrgUnitID:   a.MemberUnit,
		}
	}
	return db.Member{
		ID:          a.UserID,
		WorkspaceID: a.WorkspaceID,
		UserID:      a.UserID,
		Role:        "member",
	}
}

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
