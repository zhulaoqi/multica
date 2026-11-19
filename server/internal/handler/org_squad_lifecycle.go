package handler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/orgsquad"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// syncOrgSquadLeadership re-points department squads after a lead change, a
// placement move or a departure and re-runs their rosters. A squad's current
// leader survives when it can still run; otherwise the unit lead's primary
// twin takes over, then any other online agent the subtree covers.
//
// Callers pass the units they already loaded (with the new lead applied) and
// the units whose ancestry changed. Failures are logged, not returned: the
// change already committed, and dispatch rotates a stale leader on its next
// attempt anyway.
func (h *Handler) syncOrgSquadLeadership(ctx context.Context, wsUUID pgtype.UUID, units []db.ListOrgUnitsRow, unitIDs ...pgtype.UUID) {
	candidates, err := h.Queries.ListRunnableLeaderAgents(ctx, wsUUID)
	if err != nil {
		slog.Error("list runnable agents for squad leadership failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return
	}
	seen := make(map[pgtype.UUID]bool)
	for _, unitID := range unitIDs {
		for _, ancestor := range orgscope.Ancestors(units, unitID) {
			if seen[ancestor] {
				continue
			}
			seen[ancestor] = true
			squad, err := h.Queries.GetSquadByOrgUnit(ctx, db.GetSquadByOrgUnitParams{
				WorkspaceID: wsUUID,
				OrgUnitID:   ancestor,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				slog.Error("load department squad for leadership sync failed", "workspace_id", uuidToString(wsUUID), "unit_id", uuidToString(ancestor), "error", err)
				continue
			}
			if !squad.OrgManaged {
				continue
			}
			h.reconcileOrgSquad(ctx, wsUUID, units, candidates, squad, ancestor)
		}
	}
}

// syncAllOrgSquadLeadership reconciles every department squad in one pass.
// Directory sync moves people in bulk and can assign leads, so it heals all
// squads after the transaction instead of tracking each affected unit.
func (h *Handler) syncAllOrgSquadLeadership(ctx context.Context, wsUUID pgtype.UUID) {
	ws, err := h.Queries.GetWorkspace(ctx, wsUUID)
	if err != nil {
		slog.Error("load workspace for squad leadership failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return
	}
	var units []db.ListOrgUnitsRow
	if ws.OrgUnitID.Valid {
		units, err = workspaceaccess.SubtreeUnits(ctx, h.Queries, wsUUID)
	} else {
		units, err = h.Queries.ListOrgUnits(ctx, wsUUID)
	}
	if err != nil {
		slog.Error("list org units for squad leadership failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return
	}
	candidates, err := h.Queries.ListRunnableLeaderAgents(ctx, wsUUID)
	if err != nil {
		slog.Error("list runnable agents for squad leadership failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return
	}
	squads, err := h.Queries.ListOrgManagedSquads(ctx, wsUUID)
	if err != nil {
		slog.Error("list department squads failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return
	}
	for _, squad := range squads {
		if !squad.OrgUnitID.Valid {
			continue
		}
		h.reconcileOrgSquad(ctx, wsUUID, units, candidates, squad, squad.OrgUnitID)
	}
}

// reconcileOrgSquad rotates a department squad to a runnable leader when its
// current one cannot run and re-runs the roster. previousLeader is the leader
// the caller saw, so the sync demotes it correctly after a rotation.
func (h *Handler) reconcileOrgSquad(
	ctx context.Context,
	wsUUID pgtype.UUID,
	units []db.ListOrgUnitsRow,
	candidates []db.ListRunnableLeaderAgentsRow,
	squad db.Squad,
	unitID pgtype.UUID,
) {
	unit, ok := findOrgUnit(units, unitID)
	if !ok {
		return
	}
	current := squad
	if next := chooseOrgSquadLeader(unit, units, candidates, squad.LeaderID); next.Valid && next != squad.LeaderID {
		updated, err := h.Queries.UpdateSquad(ctx, db.UpdateSquadParams{
			ID:       squad.ID,
			LeaderID: next,
		})
		if err != nil {
			slog.Error("rotate department squad leader failed", "workspace_id", uuidToString(wsUUID), "squad_id", uuidToString(squad.ID), "error", err)
			return
		}
		current = updated
	}
	if _, err := orgsquad.SyncMembers(ctx, h.Queries, units, current, unitID, squad.LeaderID); err != nil {
		slog.Error("sync department squad failed", "workspace_id", uuidToString(wsUUID), "unit_id", uuidToString(unitID), "squad_id", uuidToString(squad.ID), "error", err)
	}
}

func findOrgUnit(units []db.ListOrgUnitsRow, id pgtype.UUID) (db.ListOrgUnitsRow, bool) {
	for _, unit := range units {
		if unit.ID == id {
			return unit, true
		}
	}
	return db.ListOrgUnitsRow{}, false
}

// chooseOrgSquadLeader resolves the next leader against the agents that can
// actually run right now. Zero means "keep the current leader": a department
// with no online agent keeps its pointer so a later dispatch can explain the
// offline state instead of the squad silently losing its leader.
func chooseOrgSquadLeader(
	unit db.ListOrgUnitsRow,
	units []db.ListOrgUnitsRow,
	candidates []db.ListRunnableLeaderAgentsRow,
	current pgtype.UUID,
) pgtype.UUID {
	subtree := orgscope.Subtree(units, unit.ID)
	covered := func(agent db.ListRunnableLeaderAgentsRow) bool {
		return subtree[agent.OrgUnitID] || (unit.LeadUserID.Valid && agent.OwnerID == unit.LeadUserID)
	}
	runnable := make(map[pgtype.UUID]bool, len(candidates))
	for _, agent := range candidates {
		runnable[agent.ID] = true
	}

	// The department lead represents the department, so their primary twin
	// leads first, then any other runnable agent they own.
	if unit.LeadUserID.Valid {
		for _, agent := range candidates {
			if agent.OwnerID == unit.LeadUserID && agent.IsPrimary {
				return agent.ID
			}
		}
		for _, agent := range candidates {
			if agent.OwnerID == unit.LeadUserID {
				return agent.ID
			}
		}
	}
	if current.Valid && runnable[current] {
		return current
	}
	for _, agent := range candidates {
		if covered(agent) {
			return agent.ID
		}
	}
	return pgtype.UUID{}
}

// applyMemberUnitMove keeps the member's primary twin in step with their
// department placement. It runs after member moves so the twin re-appears in
// the target department's squad roster.
func (h *Handler) applyMemberUnitMove(ctx context.Context, wsUUID, userID pgtype.UUID) {
	if _, err := h.Queries.MovePrimaryTwinToOwnerUnit(ctx, db.MovePrimaryTwinToOwnerUnitParams{
		WorkspaceID: wsUUID,
		OwnerID:     userID,
	}); err != nil {
		slog.Error("move primary twin with member failed", "workspace_id", uuidToString(wsUUID), "user_id", uuidToString(userID), "error", err)
	}
}

// afterMemberDeparture rotates any department squad the departing member's
// agents led and re-runs its roster. Their archived agents cannot lead, so the
// next online agent takes over; a member without a placement heals every
// department squad instead.
func (h *Handler) afterMemberDeparture(ctx context.Context, wsUUID, unitID pgtype.UUID) {
	if !unitID.Valid {
		h.syncAllOrgSquadLeadership(ctx, wsUUID)
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, wsUUID)
	if err != nil {
		slog.Error("load workspace after member departure failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return
	}
	var units []db.ListOrgUnitsRow
	if ws.OrgUnitID.Valid {
		units, err = workspaceaccess.SubtreeUnits(ctx, h.Queries, wsUUID)
	} else {
		units, err = h.Queries.ListOrgUnits(ctx, wsUUID)
	}
	if err != nil {
		slog.Error("list org units after member departure failed", "workspace_id", uuidToString(wsUUID), "error", err)
		return
	}
	h.syncOrgSquadLeadership(ctx, wsUUID, units, unitID)
}
