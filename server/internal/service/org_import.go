package service

import (
	"errors"
	"fmt"
	"strings"
)

// OrgImportRow is one import line: a unit path, its lead, and the agents that
// report to it. Every segment of path is a unit; parents are created first.
type OrgImportRow struct {
	Path      string   `json:"path"`
	LeadEmail string   `json:"lead_email"`
	Agents    []string `json:"agents"`
}

// OrgUnitRef is one unit that the import must exist, in parent-first order.
type OrgUnitRef struct {
	Path      []string
	LeadEmail string
}

const (
	orgPathMaxDepth = 8
	// OrgUnitNameMaxRunes bounds one unit name, including names created from a
	// path segment or by the unit editor.
	OrgUnitNameMaxRunes = 100
	orgImportMaxRows    = 500
	orgAgentNameRunes   = 200
)

// PlanOrgImport validates rows and returns the units to create or find, plus
// each agent's unit path. Depth and size limits keep one import bounded; they
// are not a security boundary.
func PlanOrgImport(rows []OrgImportRow) ([]OrgUnitRef, map[string][]string, error) {
	if len(rows) == 0 {
		return nil, nil, errors.New("no rows to import")
	}
	if len(rows) > orgImportMaxRows {
		return nil, nil, fmt.Errorf("too many rows: %d, limit %d", len(rows), orgImportMaxRows)
	}
	seen := map[string]bool{}
	units := make([]OrgUnitRef, 0, len(rows))
	agents := map[string][]string{}
	for i, row := range rows {
		path, err := ParseOrgPath(row.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("row %d: %w", i+1, err)
		}
		key := strings.Join(path, "\x00")
		if !seen[key] {
			seen[key] = true
			units = append(units, OrgUnitRef{Path: path, LeadEmail: strings.TrimSpace(row.LeadEmail)})
		}
		for _, name := range row.Agents {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if len([]rune(name)) > orgAgentNameRunes {
				return nil, nil, fmt.Errorf("row %d: agent name too long", i+1)
			}
			if owner, ok := agents[name]; ok && strings.Join(owner, "/") != strings.Join(path, "/") {
				return nil, nil, fmt.Errorf("agent %q appears in more than one unit", name)
			}
			agents[name] = path
		}
	}
	return units, agents, nil
}

// ParseOrgPath splits "平台部/支付组" into trimmed segments. It rejects blank
// segments, an empty path, and paths deeper than orgPathMaxDepth.
func ParseOrgPath(raw string) ([]string, error) {
	parts := strings.Split(raw, "/")
	if len(parts) > orgPathMaxDepth {
		return nil, fmt.Errorf("organisation path deeper than %d levels", orgPathMaxDepth)
	}
	path := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("organisation path has an empty segment")
		}
		if len([]rune(part)) > OrgUnitNameMaxRunes {
			return nil, errors.New("organisation name too long")
		}
		path = append(path, part)
	}
	return path, nil
}
