package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// RouteProject is a project target Route inferred or matched. The work is
// delivered into the project workspace; the owning department only appears as
// its parent and gates who may ask.
type RouteProject struct {
	ID   pgtype.UUID
	Name string
	Slug string
}

// RequestableProject is a department-owned project the caller may hand work.
// Path is the owning department, which is where the work runs.
type RequestableProject struct {
	ID   pgtype.UUID
	Name string
	Path string
}

// RouteResult resolves one natural-language dispatch to a department, or to a
// project owned by one. Unit is the target department, or the project's owning
// department when Project is set; project work runs in the project workspace.
type RouteResult struct {
	Unit       db.ListOrgUnitsRow
	Path       string
	Project    *RouteProject
	Candidates []RouteCandidate
}

// routeProjectCandidate is a project the requester may hand work, joined with
// its owning department for matching and display.
type routeProjectCandidate struct {
	project db.ListDepartmentProjectsRow
	unit    db.ListOrgUnitsRow
	path    string
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
// it, and into the project it is about when the requester may hand that project
// work. Explicit targets and exact names win, then a deterministically named
// project, then a single dispatchable department, then the model picks among
// the departments and projects, then the remaining name/path matches, then the
// requester's own department. Anything still ambiguous returns candidate
// departments for the client to offer.
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
	sources, err := intakeSources(ctx, s.Queries, wsUUID)
	if err != nil {
		return RouteResult{}, err
	}
	visible := requestableUnits(access, units, sources)

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
	// Projects the requester may hand work are targets too: a request that
	// names or describes one belongs to it, not to the department as a whole.
	// A project runs on its own workspace agents, so its owning department does
	// not need an organization agent for the project to be routable.
	projectCandidates, err := s.routeProjectCandidates(ctx, wsUUID, access, units, sources, paths)
	if err != nil {
		return RouteResult{}, err
	}
	if len(dispatchable) == 0 && len(projectCandidates) == 0 {
		return RouteResult{}, ErrDispatchUnroutable
	}
	projectResult := func(candidate routeProjectCandidate) RouteResult {
		return RouteResult{
			Unit: candidate.unit,
			Path: candidate.path,
			Project: &RouteProject{
				ID:   candidate.project.ID,
				Name: candidate.project.Name,
				Slug: candidate.project.Slug,
			},
		}
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

	// 2. Exact project name. Names are the label people use in a request, so
	// an exact one outranks the guesses below.
	if len(projectCandidates) > 0 {
		exactProjects := make([]routeProjectCandidate, 0, 1)
		for _, candidate := range projectCandidates {
			if strings.EqualFold(strings.TrimSpace(candidate.project.Name), needle) {
				exactProjects = append(exactProjects, candidate)
			}
		}
		if len(exactProjects) == 1 {
			return projectResult(exactProjects[0]), nil
		}
	}

	// 3. Deterministic name containment, resolved before the model. A project
	// named in the request is authoritative: the model must not talk it back
	// into the owning department. The longest needle wins so "支付组" does not
	// lose to a parent department also named in the text, and on a tie the
	// project wins because its name is the label the requester used.
	bestUnitLen := 0
	bestUnits := make([]db.ListOrgUnitsRow, 0, 1)
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
		case matchLen > bestUnitLen:
			bestUnitLen = matchLen
			bestUnits = []db.ListOrgUnitsRow{unit}
		case matchLen == bestUnitLen:
			bestUnits = append(bestUnits, unit)
		}
	}
	bestProjectLen := 0
	bestProjects := make([]routeProjectCandidate, 0, 1)
	for _, candidate := range projectCandidates {
		name := strings.ToLower(strings.TrimSpace(candidate.project.Name))
		if name == "" || !strings.Contains(lowerText, name) {
			continue
		}
		matchLen := len([]rune(name))
		switch {
		case matchLen > bestProjectLen:
			bestProjectLen = matchLen
			bestProjects = []routeProjectCandidate{candidate}
		case matchLen == bestProjectLen:
			bestProjects = append(bestProjects, candidate)
		}
	}
	if bestProjectLen > 0 && bestProjectLen >= bestUnitLen && len(bestProjects) == 1 {
		return projectResult(bestProjects[0]), nil
	}

	// 4. Only one department can take work at all. With projects in play the
	// model still decides whether the request is about one of them.
	if len(projectCandidates) == 0 && len(dispatchable) == 1 {
		unit := dispatchable[0]
		return RouteResult{Unit: unit, Path: paths[unit.ID]}, nil
	}

	// 5. Let the model choose among the dispatchable departments and projects.
	if choice, ok := s.routeWithLLM(ctx, text, dispatchable, paths, agentCounts, projectCandidates); ok {
		if choice.Project != nil {
			return projectResult(*choice.Project), nil
		}
		return RouteResult{Unit: choice.Unit, Path: paths[choice.Unit.ID]}, nil
	}

	// 6. The remaining deterministic fallbacks reuse the containment match
	// computed before the model.
	if len(bestUnits) == 1 && bestUnitLen >= bestProjectLen {
		return RouteResult{Unit: bestUnits[0], Path: paths[bestUnits[0].ID]}, nil
	}
	if len(bestUnits) > 1 && bestUnitLen >= bestProjectLen {
		return RouteResult{Candidates: routeCandidates(bestUnits, paths, agentCounts)}, ErrDispatchAmbiguous
	}

	// 7. With projects in the picture the single-department shortcut waits
	// until no project matched, so a named project is never flattened into its
	// department.
	if len(projectCandidates) > 0 && len(dispatchable) == 1 {
		unit := dispatchable[0]
		return RouteResult{Unit: unit, Path: paths[unit.ID]}, nil
	}

	// 8. The requester's own department.
	for _, unit := range dispatchable {
		if access.MemberUnit.Valid && unit.ID == access.MemberUnit {
			return RouteResult{Unit: unit, Path: paths[unit.ID]}, nil
		}
	}

	if len(dispatchable) == 0 {
		return RouteResult{}, ErrDispatchUnroutable
	}
	return RouteResult{Candidates: routeCandidates(dispatchable, paths, agentCounts)}, ErrDispatchAmbiguous
}

// routeProjectCandidates loads the department-owned projects the requester is
// allowed to hand work. The project runs on its own workspace agents, so the
// owning department's organization agents play no part in the match.
func (s *OrgDispatchService) routeProjectCandidates(
	ctx context.Context,
	wsUUID pgtype.UUID,
	access workspaceaccess.Access,
	units []db.ListOrgUnitsRow,
	departmentSources []db.ListOrgUnitIntakeSourcesRow,
	paths map[pgtype.UUID]string,
) ([]routeProjectCandidate, error) {
	// Projects only name targets inside the organization; a project workspace
	// dispatches through its department.
	if err := s.requireOrganizationWorkspace(ctx, wsUUID); err != nil {
		return nil, nil
	}
	projects, err := s.Queries.ListDepartmentProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	if len(projects) == 0 {
		return nil, nil
	}
	projectIDs := make([]pgtype.UUID, len(projects))
	for i, project := range projects {
		projectIDs[i] = project.ID
	}
	rows, err := s.Queries.ListWorkspaceIntakeSourcesForProjects(ctx, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("list project intake sources: %w", err)
	}
	sourcesByProject := make(map[pgtype.UUID][]db.ListWorkspaceIntakeSourcesRow, len(projects))
	for _, row := range rows {
		sourcesByProject[row.WorkspaceID] = append(sourcesByProject[row.WorkspaceID], db.ListWorkspaceIntakeSourcesRow{
			ID:          row.ID,
			WorkspaceID: row.WorkspaceID,
			SourceType:  row.SourceType,
			SourceID:    row.SourceID,
		})
	}
	memberships, err := s.Queries.ListUserWorkspaceMemberships(ctx, access.UserID)
	if err != nil {
		return nil, fmt.Errorf("list project memberships: %w", err)
	}
	isMember := make(map[pgtype.UUID]bool, len(memberships))
	for _, id := range memberships {
		isMember[id] = true
	}
	candidates := make([]routeProjectCandidate, 0, len(projects))
	for _, project := range projects {
		unit, ok := findUnit(units, project.OrgUnitID)
		if !ok {
			continue
		}
		path, ok := paths[unit.ID]
		if !ok {
			continue
		}
		workspace := db.Workspace{
			ID:        project.ID,
			Name:      project.Name,
			Slug:      project.Slug,
			OrgUnitID: project.OrgUnitID,
		}
		if !ProjectRequestable(access, units, departmentSources, workspace, sourcesByProject[project.ID], isMember[project.ID]) {
			continue
		}
		candidates = append(candidates, routeProjectCandidate{project: project, unit: unit, path: path})
	}
	return candidates, nil
}

// ListRequestableProjects returns the projects this person may hand work,
// the same set the workbench offers. The bot's project picker uses it so a
// hidden project is never named.
func (s *OrgDispatchService) ListRequestableProjects(ctx context.Context, wsUUID, actorID pgtype.UUID) ([]RequestableProject, error) {
	access, err := workspaceaccess.Resolve(ctx, s.Queries, wsUUID, actorID, false)
	if err != nil || !access.Allowed() {
		return nil, ErrDispatchRequesterNotInTeam
	}
	units, err := orgUnitsForUser(ctx, s.Queries, wsUUID, access)
	if err != nil {
		return nil, fmt.Errorf("list org units: %w", err)
	}
	paths := orgscope.Paths(units)
	sources, err := intakeSources(ctx, s.Queries, wsUUID)
	if err != nil {
		return nil, err
	}
	candidates, err := s.routeProjectCandidates(ctx, wsUUID, access, units, sources, paths)
	if err != nil {
		return nil, err
	}
	out := make([]RequestableProject, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, RequestableProject{
			ID:   candidate.project.ID,
			Name: candidate.project.Name,
			Path: candidate.path,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// routeChoice is one model answer: a department or a project.
type routeChoice struct {
	Unit    db.ListOrgUnitsRow
	Project *routeProjectCandidate
}

// routeWithLLM asks the configured model which department or project fits the
// request. A disabled model, an error, a timeout, or an id outside the list
// all fall through to the deterministic matchers.
func (s *OrgDispatchService) routeWithLLM(
	ctx context.Context,
	text string,
	units []db.ListOrgUnitsRow,
	paths map[pgtype.UUID]string,
	agentCounts map[pgtype.UUID]int,
	projects []routeProjectCandidate,
) (routeChoice, bool) {
	if s.LLM == nil {
		return routeChoice{}, false
	}
	candidates := units
	if len(candidates) > dispatchRouteLLMLimit {
		candidates = candidates[:dispatchRouteLLMLimit]
	}
	var b strings.Builder
	b.WriteString("可选目标（类型\tid\t名称/路径\t说明）：\n")
	unitsByID := make(map[string]db.ListOrgUnitsRow, len(candidates))
	for _, unit := range candidates {
		id := uuidString(unit.ID)
		unitsByID[id] = unit
		fmt.Fprintf(&b, "department\t%s\t%s\tAgent 数 %d\n", id, paths[unit.ID], agentCounts[unit.ID])
	}
	projectsByID := make(map[string]routeProjectCandidate, len(projects))
	for _, candidate := range projects {
		id := uuidString(candidate.project.ID)
		projectsByID[id] = candidate
		fmt.Fprintf(&b, "project\t%s\t%s\t所属部门 %s\n", id, candidate.project.Name, candidate.path)
	}
	b.WriteString("\n用户请求：\n")
	b.WriteString(text)

	llmCtx, cancel := context.WithTimeout(ctx, dispatchRouteTimeout)
	defer cancel()
	raw, err := s.LLM.GenerateText(llmCtx, "", dispatchRouteSystemPrompt, b.String())
	if err != nil {
		return routeChoice{}, false
	}
	parsed, ok := parseDispatchRoute(raw)
	if !ok {
		return routeChoice{}, false
	}
	if parsed.TargetType == "project" {
		candidate, ok := projectsByID[parsed.TargetID]
		if !ok {
			return routeChoice{}, false
		}
		return routeChoice{Project: &candidate}, true
	}
	unit, ok := unitsByID[parsed.TargetID]
	if !ok {
		return routeChoice{}, false
	}
	return routeChoice{Unit: unit}, true
}

const dispatchRouteSystemPrompt = `你要把一个工作请求分配给最合适的部门或项目。
只返回 JSON：{"target_type":"department","target_id":"..."} 或 {"target_type":"project","target_id":"..."}。
target_id 必须来自给定列表；无法判断时返回 {"target_type":"","target_id":""}。
按请求涉及的业务领域选择，不要按名称的字面相似度；请求明确说到某个项目时优先选该项目。`

// dispatchRouteParse is the model's JSON answer. Older answers used unit_id
// alone; they still parse as a department target.
type dispatchRouteParse struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	UnitID     string `json:"unit_id"`
}

func parseDispatchRoute(raw string) (dispatchRouteParse, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var parsed dispatchRouteParse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return parsed, false
	}
	parsed.TargetType = strings.TrimSpace(parsed.TargetType)
	parsed.TargetID = strings.TrimSpace(parsed.TargetID)
	if parsed.TargetID == "" && parsed.UnitID != "" {
		parsed.TargetType = "department"
		parsed.TargetID = strings.TrimSpace(parsed.UnitID)
	}
	if parsed.TargetType == "" || parsed.TargetID == "" {
		return parsed, false
	}
	return parsed, true
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
