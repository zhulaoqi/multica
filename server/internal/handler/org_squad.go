package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/orgsquad"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Org squad sync keeps one implicit squad per department: the leader agent is
// chosen once by a department manager, and every agent the department covers
// joins as a worker. The roster reconciliation itself lives in orgsquad so
// dispatch can create the same squad without going through this handler.

const (
	squadMaxChildrenMax = 100
	squadWIPLimitMax    = 50
)

type orgUnitSquadRequest struct {
	// Name is required when the unit has no squad yet. Syncs may omit it and
	// keep the existing name.
	Name         string  `json:"name"`
	LeaderID     string  `json:"leader_id"`
	Instructions *string `json:"instructions"`
	WipLimit     *int32  `json:"wip_limit"`
	MaxChildren  *int32  `json:"max_children"`
}

type orgUnitSquadResponse struct {
	Squad   orgSquadResponse `json:"squad"`
	Added   int              `json:"added"`
	Updated int              `json:"updated"`
	Removed int              `json:"removed"`
	Created bool             `json:"created"`
}

// GenerateOrgUnitSquad creates the department's squad when it does not exist
// and re-syncs its roster either way. Idempotent: the org page can call it on
// every "generate" or "sync" click.
func (h *Handler) GenerateOrgUnitSquad(w http.ResponseWriter, r *http.Request) {
	access, ok := h.orgAccess(w, r)
	if !ok {
		return
	}
	if !access.managesAny() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	unitID, ok := h.orgUnitRef(w, r, access.wsUUID, chi.URLParam(r, "unitId"))
	if !ok {
		return
	}
	if !access.manages(unitID) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var body orgUnitSquadRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.WipLimit != nil && (*body.WipLimit < 1 || *body.WipLimit > squadWIPLimitMax) {
		writeError(w, http.StatusBadRequest, "wip_limit must be between 1 and 50")
		return
	}
	if body.MaxChildren != nil && (*body.MaxChildren < 1 || *body.MaxChildren > squadMaxChildrenMax) {
		writeError(w, http.StatusBadRequest, "max_children must be between 1 and 100")
		return
	}

	caller, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      access.userID,
		WorkspaceID: access.wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resolve workspace member")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate squad")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	squad, err := qtx.GetSquadByOrgUnit(r.Context(), db.GetSquadByOrgUnitParams{
		WorkspaceID: access.wsUUID,
		OrgUnitID:   unitID,
	})
	var previousLeader pgtype.UUID
	if err == nil {
		previousLeader = squad.LeaderID
	}
	created := false
	if errors.Is(err, pgx.ErrNoRows) {
		name := strings.TrimSpace(body.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		leaderID, ok := h.orgSquadLeaderRef(w, r, caller, access.wsUUID, body.LeaderID, true)
		if !ok {
			return
		}
		squad, err = qtx.CreateSquad(r.Context(), db.CreateSquadParams{
			WorkspaceID: access.wsUUID,
			Name:        name,
			LeaderID:    leaderID,
			CreatorID:   access.userID,
		})
		if err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		if err := qtx.SetSquadOrgUnit(r.Context(), db.SetSquadOrgUnitParams{
			ID:          squad.ID,
			WorkspaceID: access.wsUUID,
			OrgUnitID:   unitID,
		}); err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		squad.OrgUnitID = unitID
		created = true
	} else if err != nil {
		h.importOrgFailed(w, r, err)
		return
	} else if body.LeaderID != "" {
		leaderID, ok := h.orgSquadLeaderRef(w, r, caller, access.wsUUID, body.LeaderID, false)
		if !ok {
			return
		}
		if leaderID != squad.LeaderID {
			updated, err := qtx.UpdateSquad(r.Context(), db.UpdateSquadParams{
				ID:       squad.ID,
				LeaderID: leaderID,
			})
			if err != nil {
				h.importOrgFailed(w, r, err)
				return
			}
			squad = updated
		}
	}

	// Generating or syncing a department squad adopts it into the org mirror,
	// so later placements keep the roster current. Hand-attached squads that
	// never went through this endpoint stay manual.
	if err := qtx.MarkSquadOrgManaged(r.Context(), db.MarkSquadOrgManagedParams{
		ID:          squad.ID,
		WorkspaceID: access.wsUUID,
	}); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}

	if body.Name != "" && strings.TrimSpace(body.Name) != squad.Name {
		updated, err := qtx.UpdateSquad(r.Context(), db.UpdateSquadParams{
			ID:   squad.ID,
			Name: pgtype.Text{String: strings.TrimSpace(body.Name), Valid: true},
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "小队名称已存在")
				return
			}
			h.importOrgFailed(w, r, err)
			return
		}
		squad = updated
	}
	if body.Instructions != nil {
		updated, err := qtx.UpdateSquad(r.Context(), db.UpdateSquadParams{
			ID:           squad.ID,
			Instructions: pgtype.Text{String: *body.Instructions, Valid: true},
		})
		if err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		squad = updated
	}
	if body.WipLimit != nil || body.MaxChildren != nil {
		params := db.UpdateSquadParams{ID: squad.ID}
		if body.WipLimit != nil {
			params.WipLimit = pgtype.Int4{Int32: *body.WipLimit, Valid: true}
		}
		if body.MaxChildren != nil {
			params.MaxChildren = pgtype.Int4{Int32: *body.MaxChildren, Valid: true}
		}
		updated, err := qtx.UpdateSquad(r.Context(), params)
		if err != nil {
			h.importOrgFailed(w, r, err)
			return
		}
		squad = updated
	}

	sync, err := orgsquad.SyncMembers(r.Context(), qtx, access.units, squad, unitID, previousLeader)
	if err != nil {
		h.importOrgFailed(w, r, err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		h.importOrgFailed(w, r, err)
		return
	}

	resp, err := h.orgSquadSnapshot(r.Context(), access.wsUUID, squad.ID)
	if err != nil {
		h.importOrgFailed(w, r, err)
		return
	}
	eventSquad, err := h.squadToResponseWithPreview(r.Context(), squad)
	if err != nil {
		eventSquad = h.squadToResponse(squad)
	}
	eventType := protocol.EventSquadUpdated
	status := http.StatusOK
	if created {
		eventType = protocol.EventSquadCreated
		status = http.StatusCreated
	}
	h.publish(eventType, uuidToString(access.wsUUID), "member", uuidToString(access.userID), map[string]any{"squad": eventSquad})
	writeJSON(w, status, orgUnitSquadResponse{
		Squad:   resp,
		Added:   sync.Added,
		Updated: sync.Updated,
		Removed: sync.Removed,
		Created: created,
	})
}

// orgSquadLeaderRef validates that raw names an agent the caller may wire into
// a squad. required is false on sync requests that keep the current leader.
func (h *Handler) orgSquadLeaderRef(w http.ResponseWriter, r *http.Request, caller db.Member, wsUUID pgtype.UUID, raw string, required bool) (pgtype.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			writeError(w, http.StatusBadRequest, "leader_id is required")
			return pgtype.UUID{}, false
		}
		return pgtype.UUID{}, true
	}
	leaderID, ok := parseUUIDOrBadRequest(w, raw, "leader_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	leaderAgent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          leaderID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "leader must be a valid agent in this workspace")
		return pgtype.UUID{}, false
	}
	if !h.memberCanWireAgent(r.Context(), caller, leaderAgent, uuidToString(wsUUID)) {
		writeError(w, http.StatusForbidden, "you can only use an agent you have access to as leader")
		return pgtype.UUID{}, false
	}
	return leaderID, true
}

// syncOrgSquadsAfterPlacement re-runs affected department squads after a
// member or agent changes unit, rotating a leader that can no longer run.
func (h *Handler) syncOrgSquadsAfterPlacement(ctx context.Context, wsUUID pgtype.UUID, units []db.ListOrgUnitsRow, unitIDs ...pgtype.UUID) {
	h.syncOrgSquadLeadership(ctx, wsUUID, units, unitIDs...)
}

// orgSquadSnapshot reloads one org squad with live issue counts.
func (h *Handler) orgSquadSnapshot(ctx context.Context, wsUUID pgtype.UUID, squadID pgtype.UUID) (orgSquadResponse, error) {
	rows, err := h.Queries.ListSquadsInOrgUnits(ctx, wsUUID)
	if err != nil {
		return orgSquadResponse{}, err
	}
	for _, row := range rows {
		if row.ID == squadID {
			return orgSquadMapping(row), nil
		}
	}
	return orgSquadResponse{}, pgx.ErrNoRows
}

func orgSquadMapping(row db.ListSquadsInOrgUnitsRow) orgSquadResponse {
	return orgSquadResponse{
		ID:            uuidToString(row.ID),
		Name:          row.Name,
		UnitID:        uuidToString(row.OrgUnitID),
		LeaderID:      uuidToString(row.LeaderID),
		AgentCount:    row.AgentCount,
		WorkingAgents: row.WorkingAgents,
		WipLimit:      row.WipLimit,
		MaxChildren:   row.MaxChildren,
		ActiveIssues:  row.ActiveIssues,
		ReviewIssues:  row.ReviewIssues,
		BlockedIssues: row.BlockedIssues,
	}
}
