package lark

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
)

const (
	orgBotTargetDepartment = "department"
	orgBotTargetPersonal   = "personal"
	orgBotTargetProject    = "project"

	orgBotAwaitTarget    = "target"
	orgBotAwaitProject   = "pick_project"
	orgBotAwaitAmbiguous = "ambiguous"

	orgBotCardActionType = "card_action"
)

// ErrOrgBotContextMissing means this person has not chosen a target in this
// chat yet. A missing table is reported the same way so the bot still answers.
var ErrOrgBotContextMissing = errors.New("org bot context missing")

// OrgBotCandidate is one choice the bot offers: a department, the personal
// squad, or a project. Kind is empty for a department remembered before the
// picker listed all three.
type OrgBotCandidate struct {
	Kind      string `json:"kind,omitempty"`
	UnitID    string `json:"unit_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Name      string `json:"name,omitempty"`
}

func (c OrgBotCandidate) display() string {
	switch c.Kind {
	case orgBotTargetPersonal:
		return "我的小队"
	case orgBotTargetProject:
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = "项目"
		}
		if path := strings.TrimSpace(c.Path); path != "" {
			return "项目 " + name + "（" + path + "）"
		}
		return "项目 " + name
	default:
		if path := strings.TrimSpace(c.Path); path != "" {
			return path
		}
		return strings.TrimSpace(c.Name)
	}
}

// OrgBotChatContext is the target remembered for one person in one chat.
// Later messages use it until they pick again.
type OrgBotChatContext struct {
	Kind                string
	UnitID              pgtype.UUID
	UnitPath            string
	ProjectID           pgtype.UUID
	ProjectName         string
	Awaiting            string
	PendingText         string
	Candidates          []OrgBotCandidate
	LastIssueID         pgtype.UUID
	LastIssueIdentifier string
	UpdatedAt           time.Time
}

func (state OrgBotChatContext) label() string {
	switch state.Kind {
	case orgBotTargetPersonal:
		return "我的小队"
	case orgBotTargetProject:
		if name := strings.TrimSpace(state.ProjectName); name != "" {
			return "项目 " + name
		}
		return "项目"
	case orgBotTargetDepartment:
		if path := strings.TrimSpace(state.UnitPath); path != "" {
			return path
		}
		if state.UnitID.Valid {
			return "部门"
		}
		return "部门（按内容判断）"
	default:
		return ""
	}
}

// OrgBotContextStore persists the remembered target across reconnects.
type OrgBotContextStore interface {
	Load(ctx context.Context, installationID pgtype.UUID, chatID string, userID pgtype.UUID) (OrgBotChatContext, error)
	Save(ctx context.Context, installationID pgtype.UUID, chatID string, userID pgtype.UUID, state OrgBotChatContext) error
}

// OrgBotProjects lists the projects this person may hand work.
type OrgBotProjects interface {
	ListRequestableProjects(ctx context.Context, wsUUID, actorID pgtype.UUID) ([]service.RequestableProject, error)
}

// OrgBotDepartments lists the departments this person may hand work, including
// ones that do not yet have an agent. The picker shows them so the person can
// choose instead of rewriting the sentence.
type OrgBotDepartments interface {
	ListRequestableDepartments(ctx context.Context, wsUUID, actorID pgtype.UUID) ([]service.RouteCandidate, error)
}

func marshalOrgBotCandidates(items []OrgBotCandidate) string {
	if len(items) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func unmarshalOrgBotCandidates(raw string) []OrgBotCandidate {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var items []OrgBotCandidate
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	return items
}
