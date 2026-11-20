package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stepMoveAgents turns every project-owned user agent into a projection of an
// organization agent. It runs after the unit remap, so the copies inherit
// organization placements.
func (r *runner) stepMoveAgents(ctx context.Context, org db.Workspace) error {
	fmt.Println("\n== 4. agents")
	projectAgents, err := r.q.ListProjectAgentsToConsolidate(ctx)
	if err != nil {
		return fmt.Errorf("list project agents: %w", err)
	}
	orgAgents, err := r.q.ListAllAgentsAnyKind(ctx, org.ID)
	if err != nil {
		return fmt.Errorf("list organization agents: %w", err)
	}
	moves := planAgentMoves(projectAgents, orgAgents)
	ids := make([]pgtype.UUID, 0, len(moves))
	for _, m := range moves {
		ids = append(ids, m.Agent.ID)
		switch {
		case m.Existing.Valid:
			fmt.Printf("  %s/%s -> link to organization agent %s\n", m.Agent.WorkspaceSlug, m.Agent.Name, util.UUIDToString(m.Existing))
		case m.Name != "":
			fmt.Printf("  %s/%s -> copy as %q (primary=%t)\n", m.Agent.WorkspaceSlug, m.Agent.Name, m.Name, m.Agent.IsPrimary)
		default:
			fmt.Printf("  %s/%s -> link to the copy of %s\n", m.Agent.WorkspaceSlug, m.Agent.Name, util.UUIDToString(m.Group))
		}
	}
	fmt.Printf("%d project agents to move\n", len(moves))
	if len(ids) > 0 {
		skills, err := r.q.CountAgentSkillsForAgents(ctx, ids)
		if err != nil {
			return fmt.Errorf("count skills: %w", err)
		}
		if skills > 0 {
			fmt.Printf("warning: %d skill bindings stay on the project rows and are not copied\n", skills)
		}
	}
	if !r.opts.apply || len(moves) == 0 {
		return nil
	}
	targets := map[pgtype.UUID]pgtype.UUID{}
	owners := map[pgtype.UUID]bool{}
	for _, m := range moves {
		target, err := r.moveAgent(ctx, org, m, targets)
		if err != nil {
			return fmt.Errorf("move %s/%s: %w", m.Agent.WorkspaceSlug, m.Agent.Name, err)
		}
		targets[m.Group] = target
		owners[m.Agent.OwnerID] = true
	}

	for owner := range owners {
		if _, err := r.q.EnsurePrimaryAgentForOwner(ctx, db.EnsurePrimaryAgentForOwnerParams{WorkspaceID: org.ID, OwnerID: owner}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("ensure primary for %s: %w", util.UUIDToString(owner), err)
		}
	}
	seen := map[pgtype.UUID]bool{}
	for _, id := range targets {
		if seen[id] {
			continue
		}
		seen[id] = true
		source, err := r.q.GetAgent(ctx, id)
		if err != nil {
			return fmt.Errorf("reload organization agent %s: %w", util.UUIDToString(id), err)
		}
		r.h.ProjectOrganizationAgent(ctx, source)
	}
	slog.Info("agents moved", "project_agents", len(moves), "organization_agents", len(seen))
	return nil
}

// moveAgent resolves the organization agent for one move, creating the copy
// when needed, and links the project row to it in one transaction.
func (r *runner) moveAgent(ctx context.Context, org db.Workspace, m agentMove, targets map[pgtype.UUID]pgtype.UUID) (pgtype.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, err
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.EnsureOrganizationMember(ctx, db.EnsureOrganizationMemberParams{OrganizationID: org.ID, UserID: m.Agent.OwnerID}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("add owner to organization: %w", err)
	}

	target := m.Existing
	if !target.Valid {
		target = targets[m.Group]
	}
	if !target.Valid {
		runtimeID, err := organizationRuntime(ctx, qtx, org.ID, m.Agent.RuntimeID)
		if err != nil {
			return pgtype.UUID{}, err
		}
		copied, err := qtx.CreateOrganizationAgentCopy(ctx, db.CreateOrganizationAgentCopyParams{
			OrganizationID: org.ID,
			Name:           m.Name,
			RuntimeID:      runtimeID,
			SourceID:       m.Agent.ID,
		})
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("copy agent: %w", err)
		}
		if _, err := qtx.CopyAgentInvocationTargets(ctx, db.CopyAgentInvocationTargetsParams{
			TargetAgentID:  copied.ID,
			OrganizationID: org.ID,
			SourceAgentID:  m.Agent.ID,
		}); err != nil {
			return pgtype.UUID{}, fmt.Errorf("copy invocation targets: %w", err)
		}
		target = copied.ID
	}

	if _, err := qtx.LinkAgentToSource(ctx, db.LinkAgentToSourceParams{SourceAgentID: target, ID: m.Agent.ID}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("link projection: %w", err)
	}
	if _, err := qtx.SyncAgentProjection(ctx, m.Agent.ID); err != nil {
		return pgtype.UUID{}, fmt.Errorf("sync projection: %w", err)
	}
	return target, tx.Commit(ctx)
}

// organizationRuntime is the organization runtime on the same daemon as the
// project runtime, pre-created offline when the daemon has not registered in
// the organization yet. Cloud runtimes without a daemon map to none.
func organizationRuntime(ctx context.Context, q *db.Queries, organizationID, projectRuntimeID pgtype.UUID) (pgtype.UUID, error) {
	if !projectRuntimeID.Valid {
		return pgtype.UUID{}, nil
	}
	source, err := q.GetAgentRuntime(ctx, projectRuntimeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, nil
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("load runtime: %w", err)
	}
	if !source.DaemonID.Valid {
		return pgtype.UUID{}, nil
	}

	var runtime db.AgentRuntime
	if source.ProfileID.Valid {
		profile, err := q.EnsureOrganizationRuntimeProfile(ctx, db.EnsureOrganizationRuntimeProfileParams{
			OrganizationID:  organizationID,
			SourceProfileID: source.ProfileID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, nil
		}
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("copy runtime profile: %w", err)
		}
		runtime, err = q.EnsureOrganizationProfileRuntime(ctx, db.EnsureOrganizationProfileRuntimeParams{
			OrganizationID:        organizationID,
			OrganizationProfileID: profile.ID,
			SourceRuntimeID:       source.ID,
		})
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("copy profile runtime: %w", err)
		}
		return runtime.ID, nil
	}
	runtime, err = q.EnsureOrganizationRuntime(ctx, db.EnsureOrganizationRuntimeParams{
		OrganizationID:  organizationID,
		SourceRuntimeID: source.ID,
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("copy runtime: %w", err)
	}
	return runtime.ID, nil
}
