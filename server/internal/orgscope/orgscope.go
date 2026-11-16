// Package orgscope answers tree questions shared by the org page, the
// department squads and dispatch: who may manage which department, which
// departments a caller may see, and which units a department covers.
package orgscope

import (
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Leads reports whether userID leads unitID or one of its ancestors, i.e. the
// caller may administer that department. Missing units are not led.
func Leads(units []db.ListOrgUnitsRow, userID, unitID pgtype.UUID) bool {
	if !userID.Valid || !unitID.Valid {
		return false
	}
	byID := make(map[pgtype.UUID]db.ListOrgUnitsRow, len(units))
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	for current, guard := unitID, 0; current.Valid && guard <= len(units); guard++ {
		unit, ok := byID[current]
		if !ok {
			return false
		}
		if unit.LeadUserID == userID {
			return true
		}
		current = unit.ParentID
	}
	return false
}

// LeadsAny reports whether the caller leads at least one unit.
func LeadsAny(units []db.ListOrgUnitsRow, userID pgtype.UUID) bool {
	if !userID.Valid {
		return false
	}
	for _, unit := range units {
		if unit.LeadUserID == userID {
			return true
		}
	}
	return false
}

// Visible narrows the tree for callers without workspace management: they see
// their own department and the departments they lead, each with its
// descendants. Ancestors stay out: a member should read "my department" as the
// scope, not browse up into their superiors' structure. Managers see every
// unit; nil means unrestricted.
func Visible(units []db.ListOrgUnitsRow, ownUnitID, userID pgtype.UUID, admin bool) map[pgtype.UUID]bool {
	if admin {
		return nil
	}
	byID := make(map[pgtype.UUID]db.ListOrgUnitsRow, len(units))
	children := make(map[pgtype.UUID][]pgtype.UUID, len(units))
	for _, unit := range units {
		byID[unit.ID] = unit
		if unit.ParentID.Valid {
			children[unit.ParentID] = append(children[unit.ParentID], unit.ID)
		}
	}
	visible := make(map[pgtype.UUID]bool, len(units))
	var addSubtree func(pgtype.UUID)
	addSubtree = func(id pgtype.UUID) {
		if !id.Valid || visible[id] {
			return
		}
		if _, ok := byID[id]; !ok {
			return
		}
		visible[id] = true
		for _, child := range children[id] {
			addSubtree(child)
		}
	}
	for _, unit := range units {
		if unit.ID == ownUnitID || Leads(units, userID, unit.ID) {
			addSubtree(unit.ID)
		}
	}
	return visible
}

// Subtree returns root and every unit below it.
func Subtree(units []db.ListOrgUnitsRow, root pgtype.UUID) map[pgtype.UUID]bool {
	children := make(map[pgtype.UUID][]pgtype.UUID, len(units))
	for _, unit := range units {
		if unit.ParentID.Valid {
			children[unit.ParentID] = append(children[unit.ParentID], unit.ID)
		}
	}
	subtree := make(map[pgtype.UUID]bool, len(units))
	var walk func(pgtype.UUID)
	walk = func(id pgtype.UUID) {
		if !id.Valid || subtree[id] {
			return
		}
		subtree[id] = true
		for _, child := range children[id] {
			walk(child)
		}
	}
	walk(root)
	return subtree
}

// Paths renders each unit's full path ("研发中心/支付组") for pickers and bot
// name resolution. Cycles and missing parents stop at the last known name.
func Paths(units []db.ListOrgUnitsRow) map[pgtype.UUID]string {
	byID := make(map[pgtype.UUID]db.ListOrgUnitsRow, len(units))
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	paths := make(map[pgtype.UUID]string, len(units))
	var build func(id pgtype.UUID, guard int) string
	build = func(id pgtype.UUID, guard int) string {
		if !id.Valid || guard > len(units) {
			return ""
		}
		if path, ok := paths[id]; ok {
			return path
		}
		unit, ok := byID[id]
		if !ok {
			return ""
		}
		parent := ""
		if unit.ParentID.Valid {
			parent = build(unit.ParentID, guard+1)
		}
		path := unit.Name
		if parent != "" {
			path = parent + "/" + unit.Name
		}
		paths[id] = path
		return path
	}
	for _, unit := range units {
		build(unit.ID, 0)
	}
	return paths
}

// Ancestors returns unit and each ancestor, so a placement re-syncs every squad
// whose subtree coverage may have changed.
func Ancestors(units []db.ListOrgUnitsRow, unitID pgtype.UUID) []pgtype.UUID {
	byID := make(map[pgtype.UUID]db.ListOrgUnitsRow, len(units))
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	chain := make([]pgtype.UUID, 0, 4)
	for current, guard := unitID, 0; current.Valid && guard <= len(units); guard++ {
		unit, ok := byID[current]
		if !ok {
			break
		}
		chain = append(chain, current)
		current = unit.ParentID
	}
	return chain
}
