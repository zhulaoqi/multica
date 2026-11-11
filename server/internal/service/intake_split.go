package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// ErrIntakeSplitEmpty means the utterance did not become one owned task list.
// Callers must not create an issue.
var ErrIntakeSplitEmpty = errors.New("intake_split_empty")

// IntakeModuleChoice is one row the model may choose. Agent names stay out.
type IntakeModuleChoice struct {
	ID      string
	Product string
	Module  string
	Scope   string
}

// IntakeSplit is one module and the ordered work that module's agent will do
// on a single branch.
type IntakeSplit struct {
	ModuleID string
	Tasks    []string
}

// TextGenerator is the in-process assist client. Execution agents do not use it.
type TextGenerator interface {
	GenerateText(ctx context.Context, model, systemPrompt, userPrompt string) (string, error)
}

const intakeSplitSystemPrompt = `把用户的几句话拆成有顺序的开发任务，并选定一个模块承接。
只返回 JSON：{"module_id":"...","tasks":["..."]}。
module_id 必须来自给定列表。tasks 至少一条，按开发顺序书写。
不要输出执行人、机器或别的模块。`

// SplitIntakeUtterance asks the model to pick one module and write the task list.
func SplitIntakeUtterance(ctx context.Context, gen TextGenerator, modules []IntakeModuleChoice, body string) (IntakeSplit, error) {
	if gen == nil || strings.TrimSpace(body) == "" || len(modules) == 0 {
		return IntakeSplit{}, ErrIntakeSplitEmpty
	}
	allowed := make(map[string]struct{}, len(modules))
	var b strings.Builder
	for _, module := range modules {
		allowed[module.ID] = struct{}{}
		b.WriteString(module.ID)
		b.WriteString("\t")
		b.WriteString(module.Product)
		b.WriteString(" / ")
		b.WriteString(module.Module)
		b.WriteString("\t")
		b.WriteString(module.Scope)
		b.WriteByte('\n')
	}
	b.WriteString("\n用户原文：\n")
	b.WriteString(body)
	raw, err := gen.GenerateText(ctx, "", intakeSplitSystemPrompt, b.String())
	if err != nil {
		return IntakeSplit{}, ErrIntakeSplitEmpty
	}
	return ParseIntakeSplit(raw, allowed)
}

// ParseIntakeSplit keeps a reply only when its module is allowed and it has
// at least one non-blank task.
func ParseIntakeSplit(raw string, allowed map[string]struct{}) (IntakeSplit, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var parsed struct {
		ModuleID string   `json:"module_id"`
		Tasks    []string `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return IntakeSplit{}, ErrIntakeSplitEmpty
	}
	if _, ok := allowed[parsed.ModuleID]; !ok {
		return IntakeSplit{}, ErrIntakeSplitEmpty
	}
	tasks := make([]string, 0, len(parsed.Tasks))
	for _, task := range parsed.Tasks {
		task = strings.TrimSpace(task)
		if task == "" {
			continue
		}
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 {
		return IntakeSplit{}, ErrIntakeSplitEmpty
	}
	return IntakeSplit{ModuleID: parsed.ModuleID, Tasks: tasks}, nil
}

// IntakeTaskDescription is the single issue body: ordered tasks, one branch,
// one deploy.
func IntakeTaskDescription(tasks []string) string {
	var b strings.Builder
	for i, task := range tasks {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(task)
		b.WriteByte('\n')
	}
	b.WriteString("在同一个分支上按顺序做完，只部署一次。\n")
	b.WriteString("要停下来问人，必须调用 intake-blocker，三句都要写：已经做了什么、缺什么、不补会怎样。")
	return b.String()
}

// IntakeDispatch is who runs the single issue, and whether a task is queued now.
type IntakeDispatch struct {
	AssigneeID       string
	Enqueue          bool
	SwitchedToBackup bool
}

// DispatchIntake keeps one assignee. A future start never enqueues. Otherwise
// the primary runs when online, and the backup runs only when the primary is
// offline and the backup is online.
func DispatchIntake(primaryID, backupID string, primaryOnline, backupOnline, future bool) IntakeDispatch {
	if future || primaryID == "" {
		return IntakeDispatch{AssigneeID: primaryID}
	}
	if primaryOnline {
		return IntakeDispatch{AssigneeID: primaryID, Enqueue: true}
	}
	if backupID != "" && backupOnline {
		return IntakeDispatch{AssigneeID: backupID, Enqueue: true, SwitchedToBackup: true}
	}
	return IntakeDispatch{AssigneeID: primaryID}
}
