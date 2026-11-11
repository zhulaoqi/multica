package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseIntakeSplit_AcceptsOneModuleAndOrderedTasks(t *testing.T) {
	allowed := map[string]struct{}{"mod-refund": {}}
	got, err := ParseIntakeSplit(`{"module_id":"mod-refund","tasks":[" 写退款接口 ","写对账备注"]}`, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModuleID != "mod-refund" {
		t.Fatalf("module = %s", got.ModuleID)
	}
	if len(got.Tasks) != 2 || got.Tasks[0] != "写退款接口" || got.Tasks[1] != "写对账备注" {
		t.Fatalf("tasks = %#v", got.Tasks)
	}
}

func TestParseIntakeSplit_RejectsUnknownModuleAndEmptyTasks(t *testing.T) {
	allowed := map[string]struct{}{"mod-refund": {}}
	if _, err := ParseIntakeSplit(`{"module_id":"mod-login","tasks":["做登录"]}`, allowed); !errors.Is(err, ErrIntakeSplitEmpty) {
		t.Fatalf("unknown module err = %v", err)
	}
	if _, err := ParseIntakeSplit(`{"module_id":"mod-refund","tasks":["  "]}`, allowed); !errors.Is(err, ErrIntakeSplitEmpty) {
		t.Fatalf("blank task err = %v", err)
	}
	if _, err := ParseIntakeSplit(`{"module_id":"mod-refund","tasks":[]}`, allowed); !errors.Is(err, ErrIntakeSplitEmpty) {
		t.Fatalf("empty list err = %v", err)
	}
}

func TestParseIntakeSplit_StripsFence(t *testing.T) {
	allowed := map[string]struct{}{"mod-refund": {}}
	raw := "```json\n{\"module_id\":\"mod-refund\",\"tasks\":[\"只做退款\"]}\n```"
	got, err := ParseIntakeSplit(raw, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tasks[0] != "只做退款" {
		t.Fatalf("task = %q", got.Tasks[0])
	}
}

type stubSplitter struct {
	text string
	err  error
	got  string
}

func (s *stubSplitter) GenerateText(_ context.Context, _, _, user string) (string, error) {
	s.got = user
	return s.text, s.err
}

func TestSplitIntakeUtterance_UsesOnlyModuleCatalog(t *testing.T) {
	stub := &stubSplitter{text: `{"module_id":"mod-refund","tasks":["写退款"]}`}
	got, err := SplitIntakeUtterance(context.Background(), stub, []IntakeModuleChoice{{
		ID: "mod-refund", Product: "支付", Module: "退款", Scope: "只处理退款",
	}}, "退款成功后记一笔")
	if err != nil {
		t.Fatal(err)
	}
	if got.ModuleID != "mod-refund" || len(got.Tasks) != 1 {
		t.Fatalf("got = %+v", got)
	}
	if strings.Contains(stub.got, "Agent") || !strings.Contains(stub.got, "只处理退款") {
		t.Fatalf("prompt = %s", stub.got)
	}
}

func TestDispatchIntake_OneAssignee(t *testing.T) {
	if got := DispatchIntake("a", "b", true, true, false); got.AssigneeID != "a" || !got.Enqueue || got.SwitchedToBackup {
		t.Fatalf("primary online = %+v", got)
	}
	if got := DispatchIntake("a", "b", false, true, false); got.AssigneeID != "b" || !got.Enqueue || !got.SwitchedToBackup {
		t.Fatalf("backup = %+v", got)
	}
	if got := DispatchIntake("a", "b", false, false, false); got.AssigneeID != "a" || got.Enqueue {
		t.Fatalf("both offline = %+v", got)
	}
	if got := DispatchIntake("a", "b", true, true, true); got.AssigneeID != "a" || got.Enqueue {
		t.Fatalf("future = %+v", got)
	}
}

func TestIntakeTaskDescription_OneBranchOneDeploy(t *testing.T) {
	got := IntakeTaskDescription([]string{"写退款接口", "补对账备注"})
	if !strings.Contains(got, "1. 写退款接口\n2. 补对账备注\n在同一个分支上按顺序做完，只部署一次。") || !strings.Contains(got, "intake-blocker") {
		t.Fatalf("description = %q", got)
	}
}

func TestSplitIntakeUtterance_ModelFailureCreatesNothing(t *testing.T) {
	_, err := SplitIntakeUtterance(context.Background(), &stubSplitter{err: errors.New("down")}, []IntakeModuleChoice{{
		ID: "mod-refund", Product: "支付", Module: "退款", Scope: "退款",
	}}, "做一下")
	if !errors.Is(err, ErrIntakeSplitEmpty) {
		t.Fatalf("err = %v", err)
	}
}
