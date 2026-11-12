package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIntakeStepAwaitsPerson(t *testing.T) {
	def := &issueworkflow.Definition{Steps: []issueworkflow.Step{
		{StatusKey: "todo", Handler: issueworkflow.Handler{Type: issueworkflow.HandlerNone}},
		{StatusKey: "qa", Handler: issueworkflow.Handler{Type: issueworkflow.HandlerAgent, ID: "00000000-0000-0000-0000-000000000001"}},
		{StatusKey: "release_approval", Handler: issueworkflow.Handler{Type: issueworkflow.HandlerProjectLead}},
		{StatusKey: "sign_off", Handler: issueworkflow.Handler{Type: issueworkflow.HandlerMember, ID: "00000000-0000-0000-0000-000000000002"}},
		{StatusKey: "in_review", Handler: issueworkflow.Handler{Type: issueworkflow.HandlerCreator}},
	}}
	memberLead := db.Project{
		LeadType: pgtype.Text{String: "member", Valid: true},
		LeadID:   pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
	}
	agentLead := db.Project{
		LeadType: pgtype.Text{String: "agent", Valid: true},
		LeadID:   pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
	}
	cases := []struct {
		name    string
		def     *issueworkflow.Definition
		project db.Project
		status  string
		want    bool
	}{
		{"no workflow", nil, memberLead, "release_approval", false},
		{"handlerless step", def, memberLead, "todo", false},
		{"agent step", def, memberLead, "qa", false},
		{"member lead approves", def, memberLead, "release_approval", true},
		{"agent lead is not a person", def, agentLead, "release_approval", false},
		{"named member", def, agentLead, "sign_off", true},
		{"creator acceptance", def, memberLead, "in_review", false},
		{"status outside workflow", def, memberLead, "deploying", false},
	}
	for _, tc := range cases {
		if got := IntakeStepAwaitsPerson(tc.def, tc.project, tc.status); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
