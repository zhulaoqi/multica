package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type unitRow = db.ListAllOrgUnitsRow
type agentRow = db.ListProjectAgentsToConsolidateRow

// unitRemap is one project-local unit that rows still point at, and the
// organization unit that replaces it. To is invalid when no match was found.
type unitRemap struct {
	From pgtype.UUID
	Path string
	To   pgtype.UUID
	By   string
}

// planUnitRemaps maps every referenced project-local unit onto the
// organization tree: by external_id first, then by the full name path from the
// root. Units nothing references are deleted without a mapping.
func planUnitRemaps(units []unitRow, organizationID pgtype.UUID, referenced []pgtype.UUID) []unitRemap {
	byID := make(map[pgtype.UUID]unitRow, len(units))
	for _, u := range units {
		byID[u.ID] = u
	}
	orgByExternal := map[string]pgtype.UUID{}
	orgByPath := map[string]pgtype.UUID{}
	for _, u := range units {
		if u.WorkspaceID != organizationID {
			continue
		}
		if u.ExternalID.Valid && u.ExternalID.String != "" {
			orgByExternal[u.ExternalID.String] = u.ID
		}
		orgByPath[unitPath(byID, u)] = u.ID
	}

	var remaps []unitRemap
	for _, id := range referenced {
		u, ok := byID[id]
		if !ok || !u.InProject {
			continue
		}
		remap := unitRemap{From: u.ID, Path: unitPath(byID, u)}
		if target, ok := orgByExternal[u.ExternalID.String]; ok && u.ExternalID.Valid && u.ExternalID.String != "" {
			remap.To, remap.By = target, "external_id"
		} else if target, ok := orgByPath[remap.Path]; ok {
			remap.To, remap.By = target, "path"
		}
		remaps = append(remaps, remap)
	}
	sort.Slice(remaps, func(i, j int) bool { return remaps[i].Path < remaps[j].Path })
	return remaps
}

// unitPath is the unit's name chain from its root, e.g. "公司/研发/前端".
func unitPath(byID map[pgtype.UUID]unitRow, u unitRow) string {
	names := []string{u.Name}
	seen := map[pgtype.UUID]bool{u.ID: true}
	for u.ParentID.Valid {
		parent, ok := byID[u.ParentID]
		if !ok || seen[parent.ID] {
			break
		}
		seen[parent.ID] = true
		names = append(names, parent.Name)
		u = parent
	}
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	return strings.Join(names, "/")
}

// agentMove is one project agent and the organization agent it will mirror.
// When Existing is valid the organization already has the agent (same owner,
// same name) and only the link is written; otherwise a copy named Name is
// created.
type agentMove struct {
	Agent    agentRow
	Existing pgtype.UUID
	Name     string
	// Group is the first project agent that resolves to the same organization
	// agent. Later ones link to whatever the group's move produced.
	Group pgtype.UUID
}

// planAgentMoves decides which organization agent each project agent becomes
// a projection of. Rows arrive primary-first and most recently updated first,
// so the first row of an (owner, name) pair is the one copied. A name taken by
// another owner in the organization gets the project slug appended.
func planAgentMoves(projectAgents []agentRow, organizationAgents []db.Agent) []agentMove {
	type key struct {
		owner pgtype.UUID
		name  string
	}
	existing := map[key]pgtype.UUID{}
	taken := map[string]bool{}
	for _, a := range organizationAgents {
		taken[a.Name] = true
		if a.Kind == "user" && a.OwnerID.Valid && !a.ArchivedAt.Valid && !a.SourceAgentID.Valid {
			existing[key{a.OwnerID, a.Name}] = a.ID
		}
	}

	groups := map[key]pgtype.UUID{}
	moves := make([]agentMove, 0, len(projectAgents))
	for _, a := range projectAgents {
		k := key{a.OwnerID, a.Name}
		move := agentMove{Agent: a}
		if id, ok := existing[k]; ok {
			move.Existing = id
			move.Group = a.ID
		} else if group, ok := groups[k]; ok {
			move.Group = group
		} else {
			move.Group = a.ID
			move.Name = freeName(a.Name, a.WorkspaceSlug, taken)
			taken[move.Name] = true
			groups[k] = a.ID
		}
		moves = append(moves, move)
	}
	return moves
}

func freeName(name, slug string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	candidate := name + " (" + slug + ")"
	for i := 2; taken[candidate]; i++ {
		candidate = name + " (" + slug + " " + strconv.Itoa(i) + ")"
	}
	return candidate
}
