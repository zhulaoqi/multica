// Package orgsquad keeps one implicit squad per department in step with the
// department's roster. Manual squads stay untouched; members a human added by
// hand keep source='manual' and survive later syncs.
package orgsquad

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/orgscope"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const roleMaxRunes = 160

// Querier is the subset of db.Queries roster sync needs, so it can run inside a
// transaction or against the plain connection.
type Querier interface {
	ListOrgUnitMemberIDs(context.Context, pgtype.UUID) ([]db.ListOrgUnitMemberIDsRow, error)
	ListAgentsInOrgUnits(context.Context, pgtype.UUID) ([]db.ListAgentsInOrgUnitsRow, error)
	ListSquadMembers(context.Context, pgtype.UUID) ([]db.SquadMember, error)
	AddSquadMember(context.Context, db.AddSquadMemberParams) (db.SquadMember, error)
	UpsertSquadMember(context.Context, db.UpsertSquadMemberParams) error
	RemoveSyncedSquadMember(context.Context, db.RemoveSyncedSquadMemberParams) (int64, error)
	UpdateSquadMemberRole(context.Context, db.UpdateSquadMemberRoleParams) (db.SquadMember, error)
}

type SyncResult struct {
	Added   int
	Updated int
	Removed int
}

// RoleFromIntake turns the agent's declared capabilities into the role shown in
// the squad roster, bounded so one long intake line cannot bloat members.
func RoleFromIntake(intake pgtype.Text) string {
	role := strings.TrimSpace(intake.String)
	if !intake.Valid {
		role = ""
	}
	if utf8.RuneCountInString(role) > roleMaxRunes {
		role = string([]rune(role)[:roleMaxRunes])
	}
	return role
}

// SyncMembers reconciles one department squad with its unit subtree.
// previousLeader is demoted when it is no longer leading but still belongs to
// the department.
func SyncMembers(
	ctx context.Context,
	q Querier,
	units []db.ListOrgUnitsRow,
	squad db.Squad,
	unitID pgtype.UUID,
	previousLeader pgtype.UUID,
) (SyncResult, error) {
	var result SyncResult

	subtree := orgscope.Subtree(units, unitID)
	memberRows, err := q.ListOrgUnitMemberIDs(ctx, squad.WorkspaceID)
	if err != nil {
		return result, err
	}
	subtreeMembers := make(map[pgtype.UUID]bool)
	for _, row := range memberRows {
		if subtree[row.OrgUnitID] {
			subtreeMembers[row.UserID] = true
		}
	}
	agents, err := q.ListAgentsInOrgUnits(ctx, squad.WorkspaceID)
	if err != nil {
		return result, err
	}
	desired := make(map[pgtype.UUID]string)
	for _, agent := range agents {
		covered := subtree[agent.OrgUnitID] || (agent.OwnerID.Valid && subtreeMembers[agent.OwnerID])
		if !covered {
			continue
		}
		desired[agent.ID] = RoleFromIntake(agent.IntakeCan)
	}
	desired[squad.LeaderID] = "leader"

	existingRows, err := q.ListSquadMembers(ctx, squad.ID)
	if err != nil {
		return result, err
	}
	existing := make(map[pgtype.UUID]db.SquadMember, len(existingRows))
	for _, row := range existingRows {
		if row.MemberType == "agent" {
			existing[row.MemberID] = row
		}
	}

	// A synced row that is no longer covered by the department is pruned; a
	// manual row survives, but an old leader loses the reserved role.
	if previousLeader.Valid && previousLeader != squad.LeaderID {
		if _, wanted := desired[previousLeader]; !wanted {
			if row, ok := existing[previousLeader]; ok {
				if row.Source == "org_sync" {
					removed, err := q.RemoveSyncedSquadMember(ctx, db.RemoveSyncedSquadMemberParams{
						SquadID:    squad.ID,
						MemberType: "agent",
						MemberID:   previousLeader,
						Source:     "org_sync",
					})
					if err != nil {
						return result, err
					}
					result.Removed += int(removed)
				} else {
					if _, err := q.UpdateSquadMemberRole(ctx, db.UpdateSquadMemberRoleParams{
						SquadID:    squad.ID,
						MemberType: "agent",
						MemberID:   previousLeader,
						Role:       "",
					}); err != nil {
						return result, err
					}
					result.Updated++
				}
			}
		}
	}

	// The leader is always a member. Existing manual rows keep their
	// provenance; only the reserved role is enforced.
	if row, ok := existing[squad.LeaderID]; !ok {
		if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
			SquadID:    squad.ID,
			MemberType: "agent",
			MemberID:   squad.LeaderID,
			Role:       "leader",
		}); err != nil {
			return result, err
		}
		result.Added++
	} else if row.Role != "leader" {
		if _, err := q.UpdateSquadMemberRole(ctx, db.UpdateSquadMemberRoleParams{
			SquadID:    squad.ID,
			MemberType: "agent",
			MemberID:   squad.LeaderID,
			Role:       "leader",
		}); err != nil {
			return result, err
		}
		result.Updated++
	}

	for agentID, role := range desired {
		if agentID == squad.LeaderID {
			continue
		}
		row, ok := existing[agentID]
		if ok {
			if row.Source == "manual" {
				continue
			}
			if row.Role == role && row.Source == "org_sync" {
				continue
			}
		}
		if err := q.UpsertSquadMember(ctx, db.UpsertSquadMemberParams{
			SquadID:    squad.ID,
			MemberType: "agent",
			MemberID:   agentID,
			Role:       role,
			Source:     "org_sync",
		}); err != nil {
			return result, err
		}
		if ok {
			result.Updated++
		} else {
			result.Added++
		}
	}

	for agentID, row := range existing {
		if row.Source != "org_sync" || agentID == squad.LeaderID {
			continue
		}
		if _, wanted := desired[agentID]; wanted {
			continue
		}
		removed, err := q.RemoveSyncedSquadMember(ctx, db.RemoveSyncedSquadMemberParams{
			SquadID:    squad.ID,
			MemberType: "agent",
			MemberID:   agentID,
			Source:     "org_sync",
		})
		if err != nil {
			return result, err
		}
		result.Removed += int(removed)
	}
	return result, nil
}
