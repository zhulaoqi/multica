package service

import "github.com/jackc/pgx/v5/pgtype"

// IntakeBlockerResumeStatus is the step a blocker pauses: the issue's status
// when the agent reports it, so continuing returns the issue to that workflow
// step instead of restarting development. An issue already blocked records
// nothing and resumes at todo.
func IntakeBlockerResumeStatus(status string) pgtype.Text {
	if status == "" || status == "blocked" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: status, Valid: true}
}

// IntakeResumeStatus is where a continued blocker puts the issue back.
// Blockers recorded before resume_status existed resume at todo.
func IntakeResumeStatus(stored pgtype.Text) string {
	if stored.Valid && stored.String != "" {
		return stored.String
	}
	return "todo"
}
