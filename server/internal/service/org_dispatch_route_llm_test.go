package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type stubRouteLLM struct {
	answer string
	err    error
	prompt string
}

func (s *stubRouteLLM) GenerateText(_ context.Context, _, _, user string) (string, error) {
	s.prompt = user
	return s.answer, s.err
}

func routeLLMFixture(t *testing.T) (db.ListOrgUnitsRow, routeProjectCandidate, map[pgtype.UUID]string, map[pgtype.UUID]int) {
	t.Helper()
	unit := db.ListOrgUnitsRow{
		ID:   mustUUID(t, "11111111-1111-1111-1111-111111111111"),
		Name: "研发部",
	}
	project := routeProjectCandidate{
		project: db.ListDepartmentProjectsRow{
			ID:        mustUUID(t, "22222222-2222-2222-2222-222222222222"),
			Name:      "AIHUB",
			Slug:      "aihub",
			OrgUnitID: unit.ID,
		},
		unit: unit,
		path: "研发部",
	}
	return unit, project,
		map[pgtype.UUID]string{unit.ID: "研发部"},
		map[pgtype.UUID]int{unit.ID: 2}
}

func TestRouteWithLLMPicksTheProjectItNames(t *testing.T) {
	unit, project, paths, counts := routeLLMFixture(t)
	stub := &stubRouteLLM{
		answer: `{"target_type":"project","target_id":"22222222-2222-2222-2222-222222222222"}`,
	}
	svc := &OrgDispatchService{LLM: stub}

	choice, ok := svc.routeWithLLM(
		context.Background(),
		"AIHUB现在接了哪些模型",
		[]db.ListOrgUnitsRow{unit},
		paths,
		counts,
		[]routeProjectCandidate{project},
	)
	if !ok || choice.Project == nil {
		t.Fatalf("choice = %+v, ok = %v, want the AIHUB project", choice, ok)
	}
	if choice.Project.project.ID != project.project.ID {
		t.Fatalf("project = %s, want %s", uuidString(choice.Project.project.ID), uuidString(project.project.ID))
	}
	// Both target kinds reach the model so it can reason across them.
	if !strings.Contains(stub.prompt, "department\t"+uuidString(unit.ID)) {
		t.Fatalf("prompt missing the department catalog:\n%s", stub.prompt)
	}
	if !strings.Contains(stub.prompt, "project\t"+uuidString(project.project.ID)+"\tAIHUB") {
		t.Fatalf("prompt missing the project catalog:\n%s", stub.prompt)
	}
}

func TestRouteWithLLMPicksADepartment(t *testing.T) {
	unit, project, paths, counts := routeLLMFixture(t)
	stub := &stubRouteLLM{
		answer: `{"target_type":"department","target_id":"11111111-1111-1111-1111-111111111111"}`,
	}
	svc := &OrgDispatchService{LLM: stub}

	choice, ok := svc.routeWithLLM(
		context.Background(),
		"接入一个新的模型供应商",
		[]db.ListOrgUnitsRow{unit},
		paths,
		counts,
		[]routeProjectCandidate{project},
	)
	if !ok || choice.Project != nil || choice.Unit.ID != unit.ID {
		t.Fatalf("choice = %+v, ok = %v, want the department", choice, ok)
	}
}

func TestRouteWithLLMFallsBackOnUnknownTargets(t *testing.T) {
	unit, project, paths, counts := routeLLMFixture(t)
	for _, tc := range []struct {
		name string
		stub *stubRouteLLM
	}{
		{"unlisted id", &stubRouteLLM{answer: `{"target_type":"project","target_id":"33333333-3333-3333-3333-333333333333"}`}},
		{"empty answer", &stubRouteLLM{answer: `{"target_type":"","target_id":""}`}},
		{"prose", &stubRouteLLM{answer: "AIHUB 比较合适"}},
		{"upstream error", &stubRouteLLM{err: errors.New("upstream down")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OrgDispatchService{LLM: tc.stub}
			choice, ok := svc.routeWithLLM(
				context.Background(),
				"AIHUB现在接了哪些模型",
				[]db.ListOrgUnitsRow{unit},
				paths,
				counts,
				[]routeProjectCandidate{project},
			)
			if ok {
				t.Fatalf("choice = %+v, want the deterministic fallbacks", choice)
			}
		})
	}
}
