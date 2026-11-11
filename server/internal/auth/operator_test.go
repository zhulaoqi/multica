package auth

import "testing"

func TestOperatorDefaultsToAdmin(t *testing.T) {
	t.Setenv("MULTICA_OPERATOR_NAME", "")
	t.Setenv("MULTICA_OPERATOR_PASSWORD", "")
	if OperatorName() != "admin" || OperatorMailbox() != "admin@operator.local" {
		t.Fatalf("name = %s mailbox = %s", OperatorName(), OperatorMailbox())
	}
	if !OperatorPasswordMatches("admin", "admin123456") {
		t.Fatal("expected the built-in admin password to match")
	}
	if OperatorPasswordMatches("admin", "other") {
		t.Fatal("a different password must not match")
	}
}

func TestOperatorNameOverride(t *testing.T) {
	t.Setenv("MULTICA_OPERATOR_NAME", "Ops")
	t.Setenv("MULTICA_OPERATOR_PASSWORD", "secret-value")
	if !OperatorPasswordMatches("ops", "secret-value") {
		t.Fatal("expected the configured account name to match")
	}
	if IsOperatorEmail("ops") {
		t.Fatal("the account name is not the session address")
	}
	if !IsOperatorEmail("ops@operator.local") {
		t.Fatal("expected the reserved mailbox to identify the session")
	}
}
