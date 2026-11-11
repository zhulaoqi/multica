package auth

import (
	"crypto/subtle"
	"os"
	"strings"
)

// OperatorHeader is set by the auth middleware when the session belongs to
// the developer account. Handlers must not trust a client-supplied value;
// the middleware deletes any incoming header before setting its own.
const OperatorHeader = "X-Operator"

const (
	defaultOperatorName     = "admin"
	defaultOperatorPassword = "admin123456"
)

// OperatorConfigured reports that the developer login can accept a password.
func OperatorConfigured() bool {
	return operatorPassword() != ""
}

// OperatorName is the account name used at the developer login.
func OperatorName() string {
	return operatorName()
}

// OperatorMailbox is the address stored on the user row. The login form
// asks for the account name, not this address.
func OperatorMailbox() string {
	return strings.ToLower(operatorName()) + "@operator.local"
}

// IsOperatorEmail reports that a session address belongs to the developer account.
func IsOperatorEmail(email string) bool {
	if !OperatorConfigured() {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(email), OperatorMailbox())
}

// OperatorPasswordMatches checks the developer login by account name.
func OperatorPasswordMatches(name, password string) bool {
	if !OperatorConfigured() {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(name), operatorName()) {
		return false
	}
	want := operatorPassword()
	if len(password) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(want)) == 1
}

func operatorName() string {
	name := strings.TrimSpace(os.Getenv("MULTICA_OPERATOR_NAME"))
	if name == "" {
		return defaultOperatorName
	}
	return name
}

func operatorPassword() string {
	password := os.Getenv("MULTICA_OPERATOR_PASSWORD")
	if password == "" {
		return defaultOperatorPassword
	}
	return password
}
