package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// dispatchTextLimit bounds one workbench request. It is a product limit,
	// not a transport limit: the description keeps the whole text.
	dispatchTextLimit = 4000
	// dispatchTitleLimit is the root issue's first-line title budget.
	dispatchTitleLimit = 80
	// dispatchRouteLLMLimit bounds the candidate list sent to the model.
	dispatchRouteLLMLimit = 60
	// dispatchRouteTimeout keeps a slow model from stalling the send. The
	// deterministic fallbacks run after a timeout.
	dispatchRouteTimeout = 12 * time.Second
)

var (
	ErrDispatchTextRequired = errors.New("dispatch text is required")
	ErrDispatchTextTooLong  = errors.New("dispatch text is too long")
	ErrDispatchAmbiguous    = errors.New("dispatch target is ambiguous")
	ErrDispatchUnroutable   = errors.New("no department can take this dispatch")
)

// RouteCandidate is a department the requester can pick when routing did not
// produce a single answer.
type RouteCandidate struct {
	UnitID     string `json:"unit_id"`
	Path       string `json:"path"`
	AgentCount int    `json:"agent_count"`
}

// RouteResult resolves one natural-language dispatch to a department.
type RouteResult struct {
	Unit       db.ListOrgUnitsRow
	Path       string
	Candidates []RouteCandidate
}

// DispatchTextTitle derives the root issue title from the request: first
// non-empty line, whitespace collapsed, capped at dispatchTitleLimit runes.
func DispatchTextTitle(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > dispatchTitleLimit {
			return string(runes[:dispatchTitleLimit])
		}
		return line
	}
	return ""
}

// Route turns a natural-language request into the department that should own
// it. Explicit targets and exact names win; otherwise a single dispatchable
// department is used, then the model picks among the candidates, then a
// deterministic name/path match, then the requester's own department. Anything
// still ambiguous returns candidate departments for the client to offer.
func (s *OrgDispatchService) Route(ctx context.Context, wsUUID, actorID pgtype.UUID, text string, explicitUnitID pgtype.UUID) (RouteResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return RouteResult{}, ErrDispatchTextRequired
	}
	if runes := []rune(text); len(runes) > dispatchTextLimit {
		return RouteResult{}, ErrDispatchTextTooLong
	}

	access, err := workspaceaccess.Resolve(ctx, s.Queries, wsUUID, actorID, false)
	if err != nil || !access.Allowed() {
		return RouteResult{}, ErrDispatchRequesterNotInTeam
	}
	units, err := orgUnitsForUser(ctx, s.Queries, wsUUID, access)
	if err != nil {
		return RouteResult{}, fmt.Errorf("list org units: %w", err)
	}
	paths := orgscope.Paths(units)
	visible := orgscope.Visible(units, access.MemberUnit, access.UserID, access.IsAdmin())

	if explicitUnitID.Valid {
		unit, ok := findUnit(units, explicitUnitID)
		if !ok || !unitVisible(visible, unit.ID) {
			return RouteResult{}, ErrOrgUnitNotFound
		}
		return RouteResult{Unit: unit, Path: paths[unit.ID]}, nil
	}

	agents, err := s.Queries.ListAgentsInOrgUnits(ctx, wsUUID)
	if err != nil {
		return RouteResult{}, fmt.Errorf("list org agents: %w", err)
	}
	agentCounts := unitAgentCounts(units, agents)

	dispatchable := make([]db.ListOrgUnitsRow, 0, len(units))
	for _, unit := range units {
		if unitVisible(visible, unit.ID) && agentCounts[unit.ID] > 0 {
			dispatchable = append(dispatchable, unit)
		}
	}
	if len(dispatchable) == 0 {
		return RouteResult{}, ErrDispatchUnroutable
	}

	// 1. Exact department name or full path.
	needle := strings.ToLower(strings.TrimSpace(text))
	exact := make([]db.ListOrgUnitsRow, 0, 1)
	for _, unit := range units {
		if !unitVisible(visible, unit.ID) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(unit.Name), needle) ||
			strings.EqualFold(strings.TrimSpace(paths[unit.ID]), needle) {
			exact = append(exact, unit)
		}
	}
	switch len(exact) {
	case 0:
	case 1:
		return RouteResult{Unit: exact[0], Path: paths[exact[0].ID]}, nil
	default:
		return RouteResult{Candidates: routeCandidates(exact, paths, agentCounts)}, ErrDispatchAmbiguous
	}

	// 2. Only one department can take work at all.
	if len(dispatchable) == 1 {
		unit := dispatchable[0]
		return RouteResult{Unit: unit, Path: paths[unit.ID]}, nil
	}

	// 3. Let the model choose among the dispatchable departments.
	if unit, ok := s.routeWithLLM(ctx, text, dispatchable, paths, agentCounts); ok {
		return RouteResult{Unit: unit, Path: paths[unit.ID]}, nil
	}

	// 4. Deterministic name/path containment. The longest needle wins so
	// "支付组" does not lose to a parent department also named in the text.
	bestLen := 0
	best := make([]db.ListOrgUnitsRow, 0, 1)
	lowerText := strings.ToLower(text)
	for _, unit := range dispatchable {
		name := strings.ToLower(strings.TrimSpace(unit.Name))
		path := strings.ToLower(strings.TrimSpace(paths[unit.ID]))
		matchLen := 0
		if name != "" && strings.Contains(lowerText, name) {
			matchLen = len([]rune(name))
		}
		if path != "" && strings.Contains(lowerText, path) {
			if n := len([]rune(path)); n > matchLen {
				matchLen = n
			}
		}
		if matchLen == 0 {
			continue
		}
		switch {
		case matchLen > bestLen:
			bestLen = matchLen
			best = []db.ListOrgUnitsRow{unit}
		case matchLen == bestLen:
			best = append(best, unit)
		}
	}
	if len(best) == 1 {
		return RouteResult{Unit: best[0], Path: paths[best[0].ID]}, nil
	}
	if len(best) > 1 {
		return RouteResult{Candidates: routeCandidates(best, paths, agentCounts)}, ErrDispatchAmbiguous
	}

	// 5. The requester's own department.
	for _, unit := range dispatchable {
		if access.MemberUnit.Valid && unit.ID == access.MemberUnit {
			return RouteResult{Unit: unit, Path: paths[unit.ID]}, nil
		}
	}

	return RouteResult{Candidates: routeCandidates(dispatchable, paths, agentCounts)}, ErrDispatchAmbiguous
}

// routeWithLLM asks the configured model which candidate id fits the request.
// A disabled model, an error, a timeout, or an id outside the list all fall
// through to the deterministic matchers.
func (s *OrgDispatchService) routeWithLLM(
	ctx context.Context,
	text string,
	units []db.ListOrgUnitsRow,
	paths map[pgtype.UUID]string,
	agentCounts map[pgtype.UUID]int,
) (db.ListOrgUnitsRow, bool) {
	if s.LLM == nil {
		return db.ListOrgUnitsRow{}, false
	}
	candidates := units
	if len(candidates) > dispatchRouteLLMLimit {
		candidates = candidates[:dispatchRouteLLMLimit]
	}
	var b strings.Builder
	b.WriteString("可选部门（id\t路径\tAgent 数）：\n")
	allowed := make(map[string]struct{}, len(candidates))
	byID := make(map[string]db.ListOrgUnitsRow, len(candidates))
	for _, unit := range candidates {
		id := uuidString(unit.ID)
		allowed[id] = struct{}{}
		byID[id] = unit
		fmt.Fprintf(&b, "%s\t%s\t%d\n", id, paths[unit.ID], agentCounts[unit.ID])
	}
	b.WriteString("\n用户请求：\n")
	b.WriteString(text)

	llmCtx, cancel := context.WithTimeout(ctx, dispatchRouteTimeout)
	defer cancel()
	raw, err := s.LLM.GenerateText(llmCtx, "", dispatchRouteSystemPrompt, b.String())
	if err != nil {
		return db.ListOrgUnitsRow{}, false
	}
	parsed, ok := parseDispatchRoute(raw)
	if !ok {
		return db.ListOrgUnitsRow{}, false
	}
	if _, ok := allowed[parsed.UnitID]; !ok {
		return db.ListOrgUnitsRow{}, false
	}
	return byID[parsed.UnitID], true
}

const dispatchRouteSystemPrompt = `你要把一个工作请求分配给最合适的部门。
只返回 JSON：{"unit_id":"..."}。unit_id 必须来自给定列表；无法判断时返回 {"unit_id":""}。
按请求涉及的业务领域选择，不要按部门名称的字面相似度。`

func parseDispatchRoute(raw string) (struct {
	UnitID string `json:"unit_id"`
}, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var parsed struct {
		UnitID string `json:"unit_id"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return parsed, false
	}
	parsed.UnitID = strings.TrimSpace(parsed.UnitID)
	return parsed, parsed.UnitID != ""
}

func unitVisible(visible map[pgtype.UUID]bool, id pgtype.UUID) bool {
	return visible == nil || visible[id]
}

// unitAgentCounts counts the agents a department's squad could draw on,
// including its descendants.
func unitAgentCounts(units []db.ListOrgUnitsRow, agents []db.ListAgentsInOrgUnitsRow) map[pgtype.UUID]int {
	counts := make(map[pgtype.UUID]int, len(units))
	for _, unit := range units {
		subtree := orgscope.Subtree(units, unit.ID)
		count := 0
		for _, agent := range agents {
			if agent.OrgUnitID.Valid && subtree[agent.OrgUnitID] {
				count++
			}
		}
		counts[unit.ID] = count
	}
	return counts
}

func routeCandidates(units []db.ListOrgUnitsRow, paths map[pgtype.UUID]string, agentCounts map[pgtype.UUID]int) []RouteCandidate {
	if len(units) > dispatchMatchLimit {
		units = units[:dispatchMatchLimit]
	}
	candidates := make([]RouteCandidate, 0, len(units))
	for _, unit := range units {
		candidates = append(candidates, RouteCandidate{
			UnitID:     uuidString(unit.ID),
			Path:       paths[unit.ID],
			AgentCount: agentCounts[unit.ID],
		})
	}
	return candidates
}
