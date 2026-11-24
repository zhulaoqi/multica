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

// The model routes by what a target does, so a project row carries its
// description, context and its people's duties, each flattened and capped.
func TestRouteWithLLMCatalogDescribesProjects(t *testing.T) {
	unit, project, paths, counts := routeLLMFixture(t)
	unit.Description = "负责公司研发"
	project.project.Description = "大模型网关：模型和渠道接入"
	project.project.Context = "对接 OpenRouter、QWEN\n等上游"
	project.duties = []string{"aihub 项目开发 问题排查", strings.Repeat("长", dispatchRouteDutyLimit+10)}
	stub := &stubRouteLLM{answer: `{"target_type":"","target_id":""}`}
	svc := &OrgDispatchService{LLM: stub}

	svc.routeWithLLM(context.Background(), "我能接这个模型吗", []db.ListOrgUnitsRow{unit}, paths, counts, []routeProjectCandidate{project})

	for _, want := range []string{
		"department\t" + uuidString(unit.ID) + "\t研发部\tAgent 数 2；简介：负责公司研发\n",
		"project\t" + uuidString(project.project.ID) + "\tAIHUB\t所属部门 研发部；简介：大模型网关：模型和渠道接入；背景：对接 OpenRouter、QWEN 等上游；成员职责：aihub 项目开发 问题排查 / " +
			strings.Repeat("长", dispatchRouteDutyLimit) + "…\n",
	} {
		if !strings.Contains(stub.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, stub.prompt)
		}
	}
}

type stubRouteDecider struct {
	choice     string
	confidence float64
	err        error
	options    map[string]string
	state      map[string]any
}

func (s *stubRouteDecider) Choose(_ context.Context, state map[string]any, _ string, options map[string]string) (string, float64, error) {
	s.state = state
	s.options = options
	return s.choice, s.confidence, s.err
}

func TestRouteWithDeciderWinsOverTheChatModel(t *testing.T) {
	unit, project, paths, counts := routeLLMFixture(t)
	project.project.Description = "大模型网关"
	decider := &stubRouteDecider{choice: uuidString(project.project.ID), confidence: 0.9}
	chat := &stubRouteLLM{answer: `{"target_type":"department","target_id":"11111111-1111-1111-1111-111111111111"}`}
	svc := &OrgDispatchService{Decider: decider, LLM: chat}

	choice, ok := svc.routeWithLLM(context.Background(), "我能接这个模型吗", []db.ListOrgUnitsRow{unit}, paths, counts, []routeProjectCandidate{project})
	if !ok || choice.Project == nil || choice.Project.project.ID != project.project.ID {
		t.Fatalf("choice = %+v, ok = %v, want the project the decider chose", choice, ok)
	}
	if chat.prompt != "" {
		t.Fatalf("chat model was asked after a confident decision:\n%s", chat.prompt)
	}
	if decider.state["request"] != "我能接这个模型吗" {
		t.Fatalf("decider state = %v, want the request", decider.state)
	}
	if got := decider.options[uuidString(project.project.ID)]; got != "项目 AIHUB：所属部门 研发部；简介：大模型网关" {
		t.Fatalf("project option = %q", got)
	}
	if got := decider.options[uuidString(unit.ID)]; got != "部门 研发部：Agent 数 2" {
		t.Fatalf("department option = %q", got)
	}
}

func TestRouteWithDeciderFallsBackToTheChatModel(t *testing.T) {
	unit, project, paths, counts := routeLLMFixture(t)
	for _, tc := range []struct {
		name    string
		decider *stubRouteDecider
	}{
		{"decision failed", &stubRouteDecider{err: errors.New("upstream down")}},
		{"low confidence", &stubRouteDecider{choice: uuidString(project.project.ID), confidence: dispatchRouteMinConfidence - 0.1}},
		{"unlisted id", &stubRouteDecider{choice: "33333333-3333-3333-3333-333333333333", confidence: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat := &stubRouteLLM{answer: `{"target_type":"department","target_id":"11111111-1111-1111-1111-111111111111"}`}
			svc := &OrgDispatchService{Decider: tc.decider, LLM: chat}
			choice, ok := svc.routeWithLLM(context.Background(), "接入一个新的模型供应商", []db.ListOrgUnitsRow{unit}, paths, counts, []routeProjectCandidate{project})
			if !ok || choice.Project != nil || choice.Unit.ID != unit.ID {
				t.Fatalf("choice = %+v, ok = %v, want the chat model's department", choice, ok)
			}
		})
	}

	// With no chat model either, the deterministic matchers take over.
	svc := &OrgDispatchService{Decider: &stubRouteDecider{err: errors.New("upstream down")}}
	if choice, ok := svc.routeWithLLM(context.Background(), "x", []db.ListOrgUnitsRow{unit}, paths, counts, []routeProjectCandidate{project}); ok {
		t.Fatalf("choice = %+v, want no model answer", choice)
	}
}
