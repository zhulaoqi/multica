package service

import "testing"

func TestIntakeResumeStatus_LegacyBlockerResumesAtTodo(t *testing.T) {
	if got := IntakeResumeStatus(IntakeBlockerResumeStatus("qa")); got != "qa" {
		t.Fatalf("qa resume = %s", got)
	}
	if got := IntakeResumeStatus(IntakeBlockerResumeStatus("blocked")); got != "todo" {
		t.Fatalf("blocked resume = %s", got)
	}
	if got := IntakeResumeStatus(IntakeBlockerResumeStatus("")); got != "todo" {
		t.Fatalf("legacy resume = %s", got)
	}
}
