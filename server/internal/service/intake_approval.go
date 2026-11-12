package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IntakeStepAwaitsPerson is true when the workflow step for status hands the
// issue to a person other than the requester: a named member, or a project
// lead who is a member. The creator's own step is acceptance, not approval.
func IntakeStepAwaitsPerson(def *issueworkflow.Definition, project db.Project, status string) bool {
	if def == nil {
		return false
	}
	step, ok := def.Step(status)
	if !ok {
		return false
	}
	switch step.Handler.Type {
	case issueworkflow.HandlerMember:
		return true
	case issueworkflow.HandlerProjectLead:
		return project.LeadType.Valid && project.LeadType.String == "member" && project.LeadID.Valid
	}
	return false
}

// IntakeApprovals answers IntakeStepAwaitsPerson for many intake issues,
// loading each project's workflow once. Use one per request or sweep.
type IntakeApprovals struct {
	q         issueworkflow.Querier
	workflows map[[16]byte]intakeProjectWorkflow
}

type intakeProjectWorkflow struct {
	def     *issueworkflow.Definition
	project db.Project
	err     error
}

func NewIntakeApprovals(q issueworkflow.Querier) *IntakeApprovals {
	return &IntakeApprovals{q: q, workflows: map[[16]byte]intakeProjectWorkflow{}}
}

// Awaits reports whether an issue of projectID in status waits for a person.
// A workflow that fails to load reports false with the error, so callers can
// fall back to plain progress.
func (a *IntakeApprovals) Awaits(ctx context.Context, workspaceID, projectID pgtype.UUID, status string) (bool, error) {
	if !projectID.Valid {
		return false, nil
	}
	wf, ok := a.workflows[projectID.Bytes]
	if !ok {
		def, project, err := issueworkflow.ForProject(ctx, a.q, workspaceID, projectID)
		wf = intakeProjectWorkflow{def: def, project: project, err: err}
		a.workflows[projectID.Bytes] = wf
	}
	if wf.err != nil {
		return false, wf.err
	}
	return IntakeStepAwaitsPerson(wf.def, wf.project, status), nil
}
