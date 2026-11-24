package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// projectDispatchRoutingNote is the handoff note carried by the runs of a
// dispatch delivered into a project. The dispatch already chose the project by
// name; this note tells the receiving agent to route one more time — inside the
// project, to the agent whose declared responsibilities best match the request
// — instead of answering everything itself. The roster carries the exact
// mention markdown so the handoff comment triggers the chosen agent.
//
// Responsibilities are free text their owners wrote on the agent
// (intake_can / intake_cannot, falling back to the description the team tree
// also shows as the duty). An empty pair is a generalist and must not win a
// match on its own. A note that cannot be built is not fatal: the run keeps the
// request and answers it directly.
//
// Rows whose owner leads a department are tagged: when duties overlap, the work
// goes to the member who does it hands-on rather than to the lead, who
// coordinates.
//
// The roster holds primary twins plus the executor. The requester holds no
// member row in the project workspace, so the mention gate opens only primary
// twins (and the agents the requester happens to own or lead); listing a
// secondary private agent would invite a handover the gate then refuses,
// leaving the request with nobody.
func (s *OrgDispatchService) projectDispatchRoutingNote(ctx context.Context, projectID, executorID pgtype.UUID) string {
	agents, err := s.Queries.ListAgents(ctx, projectID)
	if err != nil {
		slog.Warn("project dispatch: list agents for the routing note failed",
			"project_id", util.UUIDToString(projectID), "error", err)
		return ""
	}
	units, err := s.Queries.ListOrgUnits(ctx, projectID)
	if err != nil {
		slog.Warn("project dispatch: list org units for the routing note failed",
			"project_id", util.UUIDToString(projectID), "error", err)
	}
	var b strings.Builder
	b.WriteString("## Project Dispatch Routing\n\n")
	b.WriteString("This request already belongs to this project. Route it once more, inside the project: hand it to the agent whose declared responsibilities best match it instead of answering everything yourself.\n\n")
	b.WriteString("1. Read the issue and identify what is actually being asked.\n")
	b.WriteString("2. Read the Project Roster below. Match the request against the responsibilities each owner declared, and pick the single agent whose responsibilities cover it best. A row with \"(none declared)\" is a generalist and is not a match by itself.\n")
	b.WriteString("3. When several agents cover it equally, pick a row without \"department lead\": leads coordinate, members do the hands-on work. A lead keeps only requests that need a decision across the department.\n")
	b.WriteString("4. If another agent clearly fits, hand the request over by posting ONE comment on this issue that @mentions that agent with the exact markdown from its roster row. State the goal, the inputs, the acceptance check, and the boundary, then stop: do not also do the work in this turn.\n")
	b.WriteString("5. If your own responsibilities fit best, or no other agent is a clear match, do the work yourself and reply with the result as usual.\n")
	b.WriteString("6. Never mention yourself, and never hand the request back to the agent that sent it.\n\n")
	b.WriteString("## Project Roster\n\n")
	for _, agent := range agents {
		if agent.ID != executorID && !IsPrimaryTwin(agent) {
			continue
		}
		owner := ""
		if user, err := s.Queries.GetUser(ctx, agent.OwnerID); err == nil {
			owner = strings.TrimSpace(user.Name)
		}
		row := fmt.Sprintf("- %s — agent", agent.Name)
		if owner != "" {
			row += " — owner: " + owner
		}
		if orgscope.LeadsAny(units, agent.OwnerID) {
			row += " — department lead"
		}
		row += " — responsibilities: " + agentResponsibilities(agentDuty(agent), agent.IntakeCannot.String)
		row += fmt.Sprintf(" — [@%s](mention://agent/%s)", agent.Name, util.UUIDToString(agent.ID))
		if agent.ID == executorID {
			row += " (you)"
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	return b.String()
}

// agentDuty is what the agent declared it takes on. Owners who never opened
// the duty dialog wrote it as the description, which the team tree shows as the
// duty too, so the roster must read it the same way.
func agentDuty(agent db.Agent) string {
	if can := strings.TrimSpace(agent.IntakeCan.String); can != "" {
		return can
	}
	return agent.Description
}

// agentResponsibilities renders the declared duty pair as one roster segment.
func agentResponsibilities(can, cannot string) string {
	can = strings.TrimSpace(can)
	cannot = strings.TrimSpace(cannot)
	switch {
	case can == "" && cannot == "":
		return "(none declared)"
	case cannot == "":
		return can
	case can == "":
		return "not: " + cannot
	default:
		return can + "; not: " + cannot
	}
}
