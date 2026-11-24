package lark

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	orgBotStageTTL          = 10 * time.Minute
	orgBotRecentIssueWindow = 2 * time.Minute
	orgBotStageLimit        = 3
	orgBotProjectListLimit  = 20
	orgBotNameLookupLimit   = 80
)

// workbenchReply handles a private message, an @ in a group, or a card click
// the way the in-app composer does. A remembered target is reused and named
// in the reply. Files staged just before the sentence are attached to the
// issue this call creates.
func (b *OrgBot) workbenchReply(ctx context.Context, msg InboundMessage, text string) (string, DispatchResult, error) {
	userID, unbound, err := b.senderUser(ctx, msg)
	if err != nil {
		return "", DispatchResult{}, err
	}
	if unbound {
		return b.loginPromptReply(), DispatchResult{Outcome: OutcomeNeedsBinding}, nil
	}
	if b.cfg.Router == nil {
		return orgBotP2PHintCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, nil
	}
	org, err := b.cfg.Organization.GetOrganizationWorkspace(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return orgBotNoOrganizationCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	}
	if err != nil {
		return "", DispatchResult{}, fmt.Errorf("load organization workspace: %w", err)
	}

	state := b.loadTarget(ctx, msg, userID)
	if msg.MessageType == orgBotMenuActionType {
		b.clearMenuPin(msg.SenderOpenID)
	} else if pin, ok := b.takeMenuPin(msg.SenderOpenID); ok {
		state = applyMenuPin(state, pin)
	}
	reply, result, state, err := b.stepWorkbench(ctx, msg, userID, org, strings.TrimSpace(text), state)
	if err != nil {
		return "", DispatchResult{}, err
	}
	b.saveTarget(ctx, msg, userID, state)
	if msg.ChatType == ChatTypeP2P {
		b.publishMenu(ctx, msg, userID, org.ID)
	}
	return reply, result, nil
}

func (b *OrgBot) stepWorkbench(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, text string, state OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	if msg.MessageType == orgBotMenuActionType {
		return b.applyMenuKey(ctx, msg, userID, org, state, text)
	}
	if msg.MessageType == orgBotCardActionType {
		kind, arg, ok := classifyCardAction(text)
		if !ok {
			return "这个按钮已失效。回复「换目标」重新选择。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
		}
		return b.applyChoice(ctx, msg, userID, org, state, kind, arg)
	}
	kind, arg := classifyWorkbenchText(text, msg.ChatType == ChatTypeGroup)
	switch kind {
	case "menu":
		return b.menuReply(ctx, msg, userID, org, state)
	case "cancel":
		state.Awaiting = ""
		state.PendingText = ""
		state.Candidates = nil
		body := "已取消这次选择。"
		if label := state.label(); label != "" {
			body += "当前发给 " + label + "。"
		}
		return body, DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	case "text":
		return b.applyFreeText(ctx, msg, userID, org, text, state)
	default:
		return b.applyChoice(ctx, msg, userID, org, state, kind, arg)
	}
}

func (b *OrgBot) applyChoice(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, kind, arg string) (string, DispatchResult, OrgBotChatContext, error) {
	switch kind {
	case "menu":
		if msg.ChatType == ChatTypeGroup && msg.MessageType == orgBotCardActionType {
			return "群里要更换目标，请发送「换项目」「换部门」或「换小队」。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
		}
		return b.menuReply(ctx, msg, userID, org, state)
	case "department":
		return b.chooseDepartmentAuto(ctx, msg, userID, org, state)
	case "personal":
		return b.choosePersonal(ctx, msg, userID, org, state)
	case "project":
		return b.listProjects(ctx, msg, userID, org, state)
	case "pin":
		return b.pinByName(ctx, msg, userID, org, state, arg)
	case "unit":
		return b.chooseUnitID(ctx, msg, userID, org, state, arg)
	case "project_id":
		return b.chooseProjectID(ctx, msg, userID, org, state, arg)
	default:
		return "这个选项已失效。回复「换目标」重新选择。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
}

func (b *OrgBot) applyFreeText(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, text string, state OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	switch state.Awaiting {
	case orgBotAwaitAmbiguous:
		if candidate, ok := matchCandidate(text, state.Candidates); ok {
			return b.applyMatchedCandidate(ctx, msg, userID, org, state, candidate)
		}
		return b.repeatCandidates(ctx, msg, state), DispatchResult{Outcome: OutcomeDispatchAmbiguous}, state, nil
	case orgBotAwaitProject:
		if project, ok := b.findProject(ctx, org.ID, userID, text); ok {
			return b.chooseProject(ctx, msg, userID, org, state, project)
		}
		return "没有这个项目。回复列表里的名称，或回复「取消」。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	default:
		if asksForTargetList(text) || (msg.ChatType == ChatTypeGroup && !state.hasConcreteTarget() && asksToSwitchTarget(text)) {
			return b.offerTargets(ctx, msg, userID, org, state, false)
		}
		if state.Awaiting == orgBotAwaitTarget {
			state.Awaiting = ""
		}
		// The sentence is the requirement. It does not name a project,
		// department or squad; those come only from a choice we offered.
		if !state.hasConcreteTarget() {
			state.PendingText = text
			return b.offerTargets(ctx, msg, userID, org, state, true)
		}
		return b.dispatchUsingState(ctx, msg, userID, org, text, state)
	}
}

func (b *OrgBot) menuReply(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	return b.offerTargets(ctx, msg, userID, org, state, false)
}

func (b *OrgBot) chooseDepartmentAuto(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	return b.offerTargets(ctx, msg, userID, org, state, strings.TrimSpace(state.PendingText) != "")
}

func (b *OrgBot) choosePersonal(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	intended := clearWorkbenchTarget(state)
	intended.Kind = orgBotTargetPersonal
	if text := strings.TrimSpace(state.PendingText); text != "" {
		return b.runDispatch(ctx, msg, userID, org, text, "personal", intended, state)
	}
	intended.Awaiting = ""
	intended.PendingText = ""
	intended.Candidates = nil
	return b.presentSelected(ctx, msg, intended), DispatchResult{Outcome: OutcomeDispatchUsage}, intended, nil
}

func (b *OrgBot) listProjects(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	state.Awaiting = orgBotAwaitProject
	projects := b.requestableProjects(ctx, org.ID, userID)
	if len(projects) > orgBotProjectListLimit {
		projects = projects[:orgBotProjectListLimit]
	}
	buttons := make([]orgBotCardButton, 0, len(projects))
	for _, project := range projects {
		id := util.UUIDToString(project.ID)
		if id == "" || strings.TrimSpace(project.Name) == "" {
			continue
		}
		buttons = append(buttons, orgBotCardButton{Label: project.Name, Action: "project:" + id})
	}
	b.sendChoiceCard(ctx, msg, orgBotProjectCard(buttons))
	if len(projects) == 0 {
		return "你现在没有可以交办的项目。回复「部门」或「我的小队」。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
	var body strings.Builder
	body.WriteString("回复名称。")
	for _, project := range projects {
		if project.Path != "" {
			fmt.Fprintf(&body, "\n%s（%s）", project.Name, project.Path)
		} else {
			fmt.Fprintf(&body, "\n%s", project.Name)
		}
	}
	return body.String(), DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
}

func (b *OrgBot) pinByName(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, name string) (string, DispatchResult, OrgBotChatContext, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "回复「指定部门」加部门名称，例如：指定部门 支付组。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
	matches, err := b.cfg.Dispatcher.ResolveUnits(ctx, org.ID, userID, name)
	if err != nil {
		if errors.Is(err, service.ErrDispatchRequesterNotInTeam) {
			return dispatchNoAccessCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, state, nil
		}
		return "", DispatchResult{}, state, fmt.Errorf("resolve department: %w", err)
	}
	exact := exactUnitMatches(matches, name)
	switch len(exact) {
	case 1:
		return b.finishPin(ctx, msg, userID, org, state, exact[0])
	case 0:
		return "没有这个部门。回复完整路径，或回复「换目标」。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	default:
		state.Awaiting = orgBotAwaitAmbiguous
		state.Candidates = orgBotCandidatesFromUnits(exact)
		b.sendChoiceCard(ctx, msg, orgBotCandidateCard(state.Candidates))
		return formatOrgBotCandidates("有多个部门叫这个名字，回复序号：", state.Candidates),
			DispatchResult{Outcome: OutcomeDispatchAmbiguous}, state, nil
	}
}

func (b *OrgBot) chooseUnitID(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, raw string) (string, DispatchResult, OrgBotChatContext, error) {
	id, err := util.ParseUUID(raw)
	if err != nil {
		return "这个部门按钮已失效。回复「换目标」重新选择。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
	path := ""
	for _, candidate := range state.Candidates {
		if candidate.UnitID == raw {
			path = candidate.Path
			break
		}
	}
	return b.finishPin(ctx, msg, userID, org, state, service.UnitMatch{
		Unit: db.ListOrgUnitsRow{ID: id},
		Path: path,
	})
}

func (b *OrgBot) chooseProjectID(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, raw string) (string, DispatchResult, OrgBotChatContext, error) {
	id, err := util.ParseUUID(raw)
	if err != nil {
		return "这个项目按钮已失效。回复「项目」再选一次。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
	project := service.RequestableProject{ID: id}
	for _, item := range b.requestableProjects(ctx, org.ID, userID) {
		if item.ID == id {
			project = item
			break
		}
	}
	if strings.TrimSpace(project.Name) == "" && b.cfg.Projects != nil {
		return "你不能把工作交给这个项目。回复「项目」看可以选的。", DispatchResult{Outcome: OutcomeDispatchNoAccess}, state, nil
	}
	return b.chooseProject(ctx, msg, userID, org, state, project)
}

func (b *OrgBot) chooseProject(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, project service.RequestableProject) (string, DispatchResult, OrgBotChatContext, error) {
	intended := clearWorkbenchTarget(state)
	intended.Kind = orgBotTargetProject
	intended.ProjectID = project.ID
	intended.ProjectName = project.Name
	intended.UnitPath = project.Path
	if text := strings.TrimSpace(state.PendingText); text != "" {
		return b.runDispatch(ctx, msg, userID, org, text, "project", intended, state)
	}
	intended.Awaiting = ""
	intended.PendingText = ""
	intended.Candidates = nil
	return b.presentSelected(ctx, msg, intended), DispatchResult{Outcome: OutcomeDispatchUsage}, intended, nil
}

func (b *OrgBot) finishPin(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, match service.UnitMatch) (string, DispatchResult, OrgBotChatContext, error) {
	intended := clearWorkbenchTarget(state)
	intended.Kind = orgBotTargetDepartment
	intended.UnitID = match.Unit.ID
	intended.UnitPath = match.Path
	if text := strings.TrimSpace(state.PendingText); text != "" {
		return b.runDispatch(ctx, msg, userID, org, text, "unit", intended, state)
	}
	intended.Awaiting = ""
	intended.PendingText = ""
	intended.Candidates = nil
	return b.presentSelected(ctx, msg, intended), DispatchResult{Outcome: OutcomeDispatchUsage}, intended, nil
}

func (b *OrgBot) dispatchUsingState(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, text string, state OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	switch state.Kind {
	case orgBotTargetPersonal:
		return b.runDispatch(ctx, msg, userID, org, text, "personal", state, state)
	case orgBotTargetProject:
		if state.ProjectID.Valid {
			return b.runDispatch(ctx, msg, userID, org, text, "project", state, state)
		}
	case orgBotTargetDepartment:
		if state.UnitID.Valid {
			return b.runDispatch(ctx, msg, userID, org, text, "unit", state, state)
		}
	}
	return b.routeAndDispatch(ctx, msg, userID, org, text, state)
}

func (b *OrgBot) routeAndDispatch(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, text string, state OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	route, err := b.cfg.Router.Route(ctx, org.ID, userID, text, pgtype.UUID{})
	switch {
	case errors.Is(err, service.ErrDispatchAmbiguous):
		state.Awaiting = orgBotAwaitAmbiguous
		state.PendingText = text
		state.Candidates = orgBotCandidatesFromRoute(route.Candidates)
		b.sendChoiceCard(ctx, msg, orgBotCandidateCard(state.Candidates))
		return formatOrgBotCandidates(orgBotP2PAmbiguousCopy, state.Candidates),
			DispatchResult{Outcome: OutcomeDispatchAmbiguous}, state, nil
	case errors.Is(err, service.ErrDispatchUnroutable):
		state.PendingText = text
		return b.offerTargets(ctx, msg, userID, org, state, true)
	case errors.Is(err, service.ErrDispatchTextTooLong):
		return orgBotP2PTooLongCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	case errors.Is(err, service.ErrDispatchTextRequired):
		return orgBotP2PHintCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	case errors.Is(err, service.ErrDispatchRequesterNotInTeam):
		return dispatchNoAccessCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, state, nil
	case err != nil:
		return "", DispatchResult{}, state, fmt.Errorf("route workbench message: %w", err)
	}

	intended := clearWorkbenchTarget(state)
	mode := "unit"
	if route.Project != nil {
		mode = "project"
		intended.Kind = orgBotTargetProject
		intended.ProjectID = route.Project.ID
		intended.ProjectName = route.Project.Name
		intended.UnitID = route.Unit.ID
		intended.UnitPath = route.Path
	} else {
		intended.Kind = orgBotTargetDepartment
		intended.UnitID = route.Unit.ID
		intended.UnitPath = route.Path
	}
	return b.runDispatch(ctx, msg, userID, org, text, mode, intended, state)
}

func (b *OrgBot) runDispatch(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, text, mode string, intended, base OrgBotChatContext) (string, DispatchResult, OrgBotChatContext, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return orgBotP2PHintCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, base, nil
	}
	params := service.OrgDispatchParams{
		WorkspaceID: org.ID,
		ActorID:     userID,
		Title:       service.DispatchTextTitle(text),
		Description: text,
		Origin: service.DispatchOriginInfo{
			ChannelType:    service.DispatchChannelFeishu,
			InstallationID: b.cfg.InstallationID,
			ChatID:         string(msg.ChatID),
			ThreadID:       msg.ThreadID,
			MessageID:      msg.MessageID,
		},
		Platform: string(channel.TypeFeishu),
	}
	label := intended.label()
	createdPath := label
	switch mode {
	case "personal":
		params.Personal = true
		label = "我的小队"
		createdPath = label
	case "project":
		params.ProjectID = intended.ProjectID
		if intended.UnitPath != "" {
			createdPath = intended.UnitPath
		}
	default:
		params.UnitID = intended.UnitID
		if intended.UnitPath != "" {
			label = intended.UnitPath
			createdPath = intended.UnitPath
		}
	}
	if label == "" {
		label = "部门"
	}
	if createdPath == "" {
		createdPath = label
	}

	result, err := b.cfg.Dispatcher.Dispatch(ctx, params)
	switch {
	case errors.Is(err, service.ErrDispatchRequesterUnbound):
		return dispatchUnboundCopy, DispatchResult{Outcome: OutcomeDispatchUnbound}, base, nil
	case errors.Is(err, service.ErrOrgUnitNotFound),
		errors.Is(err, service.ErrOrgUnitNoAccess),
		errors.Is(err, service.ErrDispatchRequesterNotInTeam):
		return dispatchNoAccessCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, base, nil
	case errors.Is(err, service.ErrOrgUnitNoAgents):
		return dispatchNoAgentsCopy, DispatchResult{Outcome: OutcomeDispatchNoAgents}, base, nil
	case errors.Is(err, service.ErrDispatchLeaderUnavailable):
		return dispatchLeaderUnavailableCopy, DispatchResult{Outcome: OutcomeDispatchLeaderUnavailable}, base, nil
	case errors.Is(err, service.ErrDispatchLeaderOffline):
		return dispatchLeaderOfflineCopy, DispatchResult{Outcome: OutcomeDispatchLeaderOffline}, base, nil
	case err != nil:
		return "", DispatchResult{}, base, fmt.Errorf("dispatch workbench message: %w", err)
	}

	identifier, slug := dispatchedIssueRef(org, result)
	note := b.attachStagedFiles(ctx, msg, userID, org.ID, result.Issue.ID)
	reply := b.presentDispatched(ctx, msg, label, result.Issue.Title, identifier, createdPath, slug, note)
	next := intended
	next.Awaiting = ""
	next.PendingText = ""
	next.Candidates = nil
	next.LastIssueID = result.Issue.ID
	next.LastIssueIdentifier = identifier
	if mode == "personal" {
		next.Kind = orgBotTargetPersonal
	}
	return reply, DispatchResult{
		Outcome:            OutcomeDispatchCreated,
		IssueID:            result.Issue.ID,
		IssueNumber:        result.Issue.Number,
		IssueIdentifier:    identifier,
		IssueWorkspaceSlug: slug,
		IssueTitle:         result.Issue.Title,
		DispatchUnitName:   result.Unit.Name,
	}, next, nil
}

func (b *OrgBot) offerTargets(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, keepPending bool) (string, DispatchResult, OrgBotChatContext, error) {
	pending := ""
	if keepPending {
		pending = strings.TrimSpace(state.PendingText)
	}
	state.Awaiting = orgBotAwaitAmbiguous
	state.PendingText = pending
	state.Candidates = b.targetChoices(ctx, org.ID, userID)
	b.sendChoiceCard(ctx, msg, orgBotCandidateCard(state.Candidates))
	if len(state.Candidates) == 0 {
		return "现在没有可以点选的部门、小队或项目。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
	intro := "回复序号。"
	if label := state.label(); label != "" && state.Kind != "" {
		intro = "当前发给 " + label + "。\n" + intro
	}
	return formatOrgBotCandidates(intro, state.Candidates), DispatchResult{Outcome: OutcomeDispatchAmbiguous}, state, nil
}

func (b *OrgBot) targetChoices(ctx context.Context, wsUUID, userID pgtype.UUID) []OrgBotCandidate {
	choices := []OrgBotCandidate{{
		Kind: orgBotTargetPersonal,
		Name: "我的小队",
		Path: "我的小队",
	}}
	if b.cfg.Departments != nil {
		units, err := b.cfg.Departments.ListRequestableDepartments(ctx, wsUUID, userID)
		if err != nil {
			b.cfg.Logger.Warn("lark org bot: list departments", "error", err)
		}
		for _, unit := range units {
			path := strings.TrimSpace(unit.Path)
			choices = append(choices, OrgBotCandidate{
				Kind:   orgBotTargetDepartment,
				UnitID: strings.TrimSpace(unit.UnitID),
				Path:   path,
				Name:   pathBase(path),
			})
		}
	}
	for _, project := range b.requestableProjects(ctx, wsUUID, userID) {
		id := util.UUIDToString(project.ID)
		if id == "" || strings.TrimSpace(project.Name) == "" {
			continue
		}
		choices = append(choices, OrgBotCandidate{
			Kind:      orgBotTargetProject,
			ProjectID: id,
			Name:      project.Name,
			Path:      project.Path,
		})
	}
	if len(choices) > orgBotProjectListLimit {
		choices = choices[:orgBotProjectListLimit]
	}
	return choices
}

func (b *OrgBot) applyMatchedCandidate(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, candidate OrgBotCandidate) (string, DispatchResult, OrgBotChatContext, error) {
	switch candidate.Kind {
	case orgBotTargetPersonal:
		return b.choosePersonal(ctx, msg, userID, org, state)
	case orgBotTargetProject:
		id, err := util.ParseUUID(candidate.ProjectID)
		if err != nil {
			return b.repeatCandidates(ctx, msg, state), DispatchResult{Outcome: OutcomeDispatchAmbiguous}, state, nil
		}
		return b.chooseProject(ctx, msg, userID, org, state, service.RequestableProject{
			ID:   id,
			Name: candidate.Name,
			Path: candidate.Path,
		})
	default:
		id, path, ok := b.resolveCandidate(ctx, org.ID, userID, candidate)
		if !ok {
			return b.repeatCandidates(ctx, msg, state), DispatchResult{Outcome: OutcomeDispatchAmbiguous}, state, nil
		}
		return b.finishPin(ctx, msg, userID, org, state, service.UnitMatch{
			Unit: db.ListOrgUnitsRow{ID: id},
			Path: path,
		})
	}
}

func asksForTargetList(text string) bool {
	text = strings.TrimSpace(text)
	switch text {
	case "有哪些部门", "部门列表", "列出部门", "返回部门":
		return true
	}
	if !strings.Contains(text, "选择") {
		return false
	}
	return strings.Contains(text, "部门") || strings.Contains(text, "小队") || strings.Contains(text, "项目")
}

func (b *OrgBot) repeatCandidates(ctx context.Context, msg InboundMessage, state OrgBotChatContext) string {
	b.sendChoiceCard(ctx, msg, orgBotCandidateCard(state.Candidates))
	return formatOrgBotCandidates("没有这个选项。回复序号或部门名称，你的原话会交给它：", state.Candidates)
}

func (b *OrgBot) requestableProjects(ctx context.Context, wsUUID, userID pgtype.UUID) []service.RequestableProject {
	if b.cfg.Projects == nil {
		return nil
	}
	projects, err := b.cfg.Projects.ListRequestableProjects(ctx, wsUUID, userID)
	if err != nil {
		b.cfg.Logger.Warn("lark org bot: list projects", "error", err)
		return nil
	}
	return projects
}

func (b *OrgBot) findProject(ctx context.Context, wsUUID, userID pgtype.UUID, text string) (service.RequestableProject, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return service.RequestableProject{}, false
	}
	for _, project := range b.requestableProjects(ctx, wsUUID, userID) {
		if strings.EqualFold(strings.TrimSpace(project.Name), text) {
			return project, true
		}
	}
	return service.RequestableProject{}, false
}

func (b *OrgBot) lookupExactDepartment(ctx context.Context, wsUUID, userID pgtype.UUID, text string) (service.UnitMatch, bool) {
	name := strings.TrimSpace(text)
	if name == "" || strings.Contains(name, "\n") || len([]rune(name)) > orgBotNameLookupLimit {
		return service.UnitMatch{}, false
	}
	matches, err := b.cfg.Dispatcher.ResolveUnits(ctx, wsUUID, userID, name)
	if err != nil {
		return service.UnitMatch{}, false
	}
	exact := exactUnitMatches(matches, name)
	if len(exact) != 1 {
		return service.UnitMatch{}, false
	}
	return exact[0], true
}

func (b *OrgBot) resolveCandidate(ctx context.Context, wsUUID, userID pgtype.UUID, candidate OrgBotCandidate) (pgtype.UUID, string, bool) {
	if candidate.UnitID != "" {
		id, err := util.ParseUUID(candidate.UnitID)
		if err == nil {
			path := candidate.Path
			if path == "" {
				path = candidate.Name
			}
			return id, path, true
		}
	}
	query := candidate.Path
	if query == "" {
		query = candidate.Name
	}
	if query == "" {
		return pgtype.UUID{}, "", false
	}
	matches, err := b.cfg.Dispatcher.ResolveUnits(ctx, wsUUID, userID, query)
	if err != nil || len(matches) != 1 {
		return pgtype.UUID{}, "", false
	}
	return matches[0].Unit.ID, matches[0].Path, true
}

func (b *OrgBot) loadTarget(ctx context.Context, msg InboundMessage, userID pgtype.UUID) OrgBotChatContext {
	if b.cfg.Context == nil || !userID.Valid || msg.ChatID == "" {
		return OrgBotChatContext{}
	}
	state, err := b.cfg.Context.Load(ctx, b.cfg.InstallationID, string(msg.ChatID), userID)
	if err != nil && !errors.Is(err, ErrOrgBotContextMissing) {
		b.cfg.Logger.Warn("lark org bot: load chat context", "error", err)
		return OrgBotChatContext{}
	}
	return state
}

func (b *OrgBot) saveTarget(ctx context.Context, msg InboundMessage, userID pgtype.UUID, state OrgBotChatContext) {
	if b.cfg.Context == nil || !userID.Valid || msg.ChatID == "" {
		return
	}
	state.UpdatedAt = time.Now()
	if err := b.cfg.Context.Save(ctx, b.cfg.InstallationID, string(msg.ChatID), userID, state); err != nil {
		b.cfg.Logger.Warn("lark org bot: save chat context", "error", err)
	}
}

func (b *OrgBot) sendChoiceCard(ctx context.Context, msg InboundMessage, card string) {
	if err := b.sendCard(ctx, msg, card); err != nil {
		b.cfg.Logger.Warn("lark org bot: choice card failed", "chat_id", string(msg.ChatID), "error", err)
	}
}

func (b *OrgBot) sendCard(ctx context.Context, msg InboundMessage, card string) error {
	if strings.TrimSpace(card) == "" || b.cfg.Client == nil {
		return errors.New("empty card")
	}
	if msg.ChatID == "" {
		return SendPersonalCard(b.cfg.Client, ctx, b.cfg.Credentials, string(msg.SenderOpenID), card)
	}
	return sendWithReplyFallback(b.cfg.Logger, "send org bot card", inboundReplyTarget(msg), func(target ReplyTarget) error {
		_, sendErr := b.cfg.Client.SendInteractiveCard(ctx, SendCardParams{
			InstallationID: b.cfg.Credentials,
			ChatID:         msg.ChatID,
			CardJSON:       card,
			ReplyTarget:    target,
		})
		return sendErr
	})
}

func (b *OrgBot) presentSelected(ctx context.Context, msg InboundMessage, state OrgBotChatContext) string {
	card, err := orgBotSelectedCard(state, msg.ChatType != ChatTypeGroup)
	if err != nil {
		card = ""
	}
	label := state.label()
	if label == "" {
		label = "部门"
	}
	return b.presentCard(ctx, msg, card, "已选中 "+label+"。直接发送需求。")
}

func (b *OrgBot) presentDispatched(ctx context.Context, msg InboundMessage, label, requirement, identifier, handedTo, slug, attachment string) string {
	link := channel.IssueWebLink(b.cfg.AppURL, slug, identifier)
	card, err := orgBotDispatchedCard(label, requirement, identifier, handedTo, link, attachment, msg.ChatType != ChatTypeGroup)
	if err != nil {
		card = ""
	}
	return b.presentCard(ctx, msg, card, dispatchedFallback(label, requirement, identifier, handedTo, link, attachment))
}

func (b *OrgBot) presentCard(ctx context.Context, msg InboundMessage, card, fallback string) string {
	if err := b.sendCard(ctx, msg, card); err != nil {
		if b.cfg.Logger != nil {
			b.cfg.Logger.Warn("lark org bot: card failed", "chat_id", string(msg.ChatID), "error", err)
		}
		return fallback
	}
	return ""
}

func dispatchedIssueRef(org db.Workspace, result service.OrgDispatchResult) (identifier, slug string) {
	if result.Project != nil {
		return service.IssueIdentifier(result.Project.IssuePrefix, result.Issue.Number), strings.TrimSpace(result.Project.Slug)
	}
	return service.IssueIdentifier(org.IssuePrefix, result.Issue.Number), org.Slug
}

func dispatchedFallback(label, requirement, identifier, handedTo, link, attachment string) string {
	var body strings.Builder
	fmt.Fprintf(&body, "已交给 %s", strings.TrimSpace(label))
	if requirement = strings.TrimSpace(requirement); requirement != "" {
		fmt.Fprintf(&body, "\n需求：%s", requirement)
	}
	if identifier = strings.TrimSpace(identifier); identifier != "" {
		fmt.Fprintf(&body, "\n编号：%s", identifier)
	}
	handedTo = strings.TrimSpace(handedTo)
	if handedTo != "" && handedTo != strings.TrimSpace(label) {
		fmt.Fprintf(&body, "\n交给：%s", handedTo)
	}
	if attachment = strings.TrimSpace(attachment); attachment != "" {
		fmt.Fprintf(&body, "\n%s", attachment)
	}
	if link = strings.TrimSpace(link); link != "" {
		fmt.Fprintf(&body, "\n%s", link)
	}
	return body.String()
}

func classifyWorkbenchText(text string, group bool) (string, string) {
	trimmed := strings.TrimSpace(text)
	switch {
	case group && asksToNameASwitch(trimmed):
		return "menu", ""
	case !group && asksToSwitchTarget(trimmed):
		return "menu", ""
	}
	if trimmed == "取消" {
		return "cancel", ""
	}
	return "text", trimmed
}

// asksToSwitchTarget is a request to pick another project, department or
// squad. The whole message has to be that request, so a real requirement
// that merely mentions changing something still goes to the current target.
func asksToSwitchTarget(text string) bool {
	return switchCore(text, isSwitchPhrase)
}

func switchCore(text string, match func(string) bool) bool {
	text = strings.Trim(strings.TrimSpace(text), "。！？!?，,~ ")
	if text == "" {
		return false
	}
	if match(text) {
		return true
	}
	for _, prefix := range []string{"我想", "我要", "帮我", "给我", "请", "想", "要"} {
		rest, ok := strings.CutPrefix(text, prefix)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest = strings.TrimSuffix(rest, "一下")
		rest = strings.TrimSuffix(rest, "吧")
		rest = strings.Trim(strings.TrimSpace(rest), "。！？!?，,~ ")
		if match(rest) {
			return true
		}
	}
	return false
}

func asksToNameASwitch(text string) bool {
	return switchCore(text, isNamedSwitchPhrase)
}

func isSwitchPhrase(text string) bool {
	return isNamedSwitchPhrase(text) || isGenericSwitchPhrase(text)
}

func isNamedSwitchPhrase(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "换项目", "换部门", "换小队", "换个项目", "换个部门", "换个小队",
		"切换项目", "切换部门", "切换小队":
		return true
	default:
		return false
	}
}

func isGenericSwitchPhrase(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "换目标", "选择目标", "/目标", "目标", "发给谁", "重新选择", "换一个", "切换", "切换目标":
		return true
	default:
		return false
	}
}

func classifyCardAction(token string) (string, string, bool) {
	switch strings.TrimSpace(token) {
	case "target:menu":
		return "menu", "", true
	case "target:department":
		return "department", "", true
	case "target:personal":
		return "personal", "", true
	case "target:project":
		return "project", "", true
	}
	if id, ok := strings.CutPrefix(token, "unit:"); ok && strings.TrimSpace(id) != "" {
		return "unit", strings.TrimSpace(id), true
	}
	if id, ok := strings.CutPrefix(token, "project:"); ok && strings.TrimSpace(id) != "" {
		return "project_id", strings.TrimSpace(id), true
	}
	return "", "", false
}

func clearWorkbenchTarget(state OrgBotChatContext) OrgBotChatContext {
	state.UnitID = pgtype.UUID{}
	state.UnitPath = ""
	state.ProjectID = pgtype.UUID{}
	state.ProjectName = ""
	return state
}

func formatOrgBotCandidates(intro string, candidates []OrgBotCandidate) string {
	var body strings.Builder
	body.WriteString(intro)
	n := 0
	for _, candidate := range candidates {
		label := candidate.display()
		if label == "" {
			continue
		}
		n++
		fmt.Fprintf(&body, "\n%d. %s", n, label)
	}
	return body.String()
}

func orgBotCandidatesFromRoute(in []service.RouteCandidate) []OrgBotCandidate {
	out := make([]OrgBotCandidate, 0, len(in))
	for _, candidate := range in {
		path := strings.TrimSpace(candidate.Path)
		out = append(out, OrgBotCandidate{
			Kind:   orgBotTargetDepartment,
			UnitID: strings.TrimSpace(candidate.UnitID),
			Path:   path,
			Name:   pathBase(path),
		})
	}
	return out
}

func orgBotCandidatesFromUnits(matches []service.UnitMatch) []OrgBotCandidate {
	out := make([]OrgBotCandidate, 0, len(matches))
	for _, match := range matches {
		out = append(out, OrgBotCandidate{
			Kind:   orgBotTargetDepartment,
			UnitID: util.UUIDToString(match.Unit.ID),
			Path:   match.Path,
			Name:   strings.TrimSpace(match.Unit.Name),
		})
	}
	return out
}

func pathBase(path string) string {
	path = strings.TrimSpace(path)
	if i := strings.LastIndex(path, "/"); i >= 0 && i < len(path)-1 {
		return path[i+1:]
	}
	return path
}

func matchCandidate(text string, candidates []OrgBotCandidate) (OrgBotCandidate, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return OrgBotCandidate{}, false
	}
	shown := make([]OrgBotCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Path) == "" && strings.TrimSpace(candidate.Name) == "" {
			continue
		}
		shown = append(shown, candidate)
	}
	if n, ok := parseChoiceIndex(text); ok && n >= 1 && n <= len(shown) {
		return shown[n-1], true
	}
	for _, candidate := range shown {
		if strings.EqualFold(candidate.Path, text) || strings.EqualFold(candidate.Name, text) || strings.EqualFold(candidate.display(), text) || strings.EqualFold(candidate.UnitID, text) {
			return candidate, true
		}
	}
	return OrgBotCandidate{}, false
}

func parseChoiceIndex(text string) (int, bool) {
	if text == "" || len(text) > 3 {
		return 0, false
	}
	n := 0
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, n > 0
}

func exactUnitMatches(matches []service.UnitMatch, name string) []service.UnitMatch {
	name = strings.TrimSpace(name)
	exact := make([]service.UnitMatch, 0, 1)
	for _, match := range matches {
		if strings.EqualFold(strings.TrimSpace(match.Path), name) || strings.EqualFold(strings.TrimSpace(match.Unit.Name), name) {
			exact = append(exact, match)
		}
	}
	return exact
}

type stagedOrgBotFile struct {
	Filename    string
	ContentType string
	Data        []byte
	At          time.Time
}

type orgBotFileBuffer struct {
	mu    sync.Mutex
	byKey map[string][]stagedOrgBotFile
}

func (b *OrgBot) stageKey(chatID string, userID pgtype.UUID) string {
	return util.UUIDToString(b.cfg.InstallationID) + "\x00" + chatID + "\x00" + util.UUIDToString(userID)
}

func (b *OrgBot) rememberFiles(key string, file stagedOrgBotFile) {
	b.files.mu.Lock()
	defer b.files.mu.Unlock()
	if b.files.byKey == nil {
		b.files.byKey = map[string][]stagedOrgBotFile{}
	}
	now := time.Now()
	kept := make([]stagedOrgBotFile, 0, len(b.files.byKey[key])+1)
	for _, item := range b.files.byKey[key] {
		if now.Sub(item.At) <= orgBotStageTTL {
			kept = append(kept, item)
		}
	}
	kept = append(kept, file)
	if len(kept) > orgBotStageLimit {
		kept = kept[len(kept)-orgBotStageLimit:]
	}
	b.files.byKey[key] = kept
}

func (b *OrgBot) takeFiles(key string) []stagedOrgBotFile {
	b.files.mu.Lock()
	defer b.files.mu.Unlock()
	items := b.files.byKey[key]
	delete(b.files.byKey, key)
	now := time.Now()
	out := make([]stagedOrgBotFile, 0, len(items))
	for _, item := range items {
		if now.Sub(item.At) <= orgBotStageTTL {
			out = append(out, item)
		}
	}
	return out
}

func (b *OrgBot) attachStagedFiles(ctx context.Context, msg InboundMessage, userID, workspaceID, issueID pgtype.UUID) string {
	if b.cfg.Attachments == nil || !issueID.Valid {
		return ""
	}
	files := b.takeFiles(b.stageKey(string(msg.ChatID), userID))
	if len(files) == 0 {
		return ""
	}
	names := make([]string, 0, len(files))
	failed := false
	for _, file := range files {
		stored, err := b.cfg.Attachments.AttachToIssue(ctx, OrgBotAttachment{
			ActorID:     userID,
			WorkspaceID: workspaceID,
			IssueID:     issueID,
			Filename:    file.Filename,
			ContentType: file.ContentType,
			Data:        file.Data,
		})
		if err != nil {
			b.cfg.Logger.Warn("lark org bot: attach staged file", "error", err, "filename", file.Filename)
			failed = true
			continue
		}
		names = append(names, stored.Filename)
	}
	if len(names) == 0 {
		return "文件没能附上，可以再发一次。"
	}
	reply := "已附上 " + strings.Join(names, "、") + "。"
	if failed {
		reply += "还有文件没能附上，可以再发一次。"
	}
	return reply
}

// keepInboundMedia holds a file that arrived outside a dispatch thread until
// the next sentence creates the task, or attaches it to a task created moments
// ago. Feishu delivers the file as its own event.
func (b *OrgBot) keepInboundMedia(ctx context.Context, msg InboundMessage, userID pgtype.UUID) (string, DispatchResult, error) {
	media, reply, result, err := b.downloadInboundMedia(ctx, msg)
	if err != nil || reply != "" {
		return reply, result, err
	}
	state := b.loadTarget(ctx, msg, userID)
	if state.LastIssueID.Valid && state.Awaiting == "" && strings.TrimSpace(state.PendingText) == "" &&
		!state.UpdatedAt.IsZero() && time.Since(state.UpdatedAt) <= orgBotRecentIssueWindow {
		org, orgErr := b.cfg.Organization.GetOrganizationWorkspace(ctx)
		if orgErr == nil {
			stored, attachErr := b.cfg.Attachments.AttachToIssue(ctx, OrgBotAttachment{
				ActorID:     userID,
				WorkspaceID: org.ID,
				IssueID:     state.LastIssueID,
				Filename:    media.Filename,
				ContentType: media.ContentType,
				Data:        media.Data,
			})
			if attachErr != nil {
				return "", DispatchResult{}, fmt.Errorf("attach inbound media: %w", attachErr)
			}
			ident := state.LastIssueIdentifier
			if ident == "" {
				ident = "刚才的任务"
			}
			return fmt.Sprintf("已把 %s 附加到刚才的 %s。", stored.Filename, ident),
				DispatchResult{Outcome: OutcomeIngested}, nil
		}
	}
	b.rememberFiles(b.stageKey(string(msg.ChatID), userID), stagedOrgBotFile{
		Filename:    media.Filename,
		ContentType: media.ContentType,
		Data:        append([]byte(nil), media.Data...),
		At:          time.Now(),
	})
	return fmt.Sprintf("已收到 %s。它会跟你下一条要做的事一起交出去。", media.Filename),
		DispatchResult{Outcome: OutcomeIngested}, nil
}
