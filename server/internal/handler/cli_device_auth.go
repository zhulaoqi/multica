package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

const (
	cliDeviceLoginTTL          = 5 * time.Minute
	cliDeviceCompletedTTL      = 30 * time.Second
	cliDeviceExchangeTokenTTL  = 2 * time.Minute
	cliDeviceLoginPollInterval = 3
)

type cliDeviceStartResponse struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
}

func cliDeviceDigest(deviceCode string) string {
	sum := sha256.Sum256([]byte(deviceCode))
	return hex.EncodeToString(sum[:])
}

func newCLIDeviceCodes() (deviceCode, userCode string, err error) {
	userBytes := make([]byte, 5)
	if _, err = rand.Read(userBytes); err != nil {
		return "", "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(userBytes)
	userCode = encoded[:4] + "-" + encoded[4:8]
	deviceBytes := make([]byte, 32)
	if _, err = rand.Read(deviceBytes); err != nil {
		return "", "", err
	}
	deviceCode = userCode + "." + base64.RawURLEncoding.EncodeToString(deviceBytes)
	return deviceCode, userCode, nil
}

func normalizeCLIDeviceUserCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func validCLIDeviceUserCode(code string) bool {
	if len(code) != 9 || code[4] != '-' {
		return false
	}
	for index, char := range code {
		if index == 4 {
			continue
		}
		if (char < 'A' || char > 'Z') && (char < '2' || char > '9') {
			return false
		}
	}
	return true
}

func parseCLIDeviceCode(value string) (userCode, digest string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(value), ".", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	userCode = normalizeCLIDeviceUserCode(parts[0])
	secret, err := base64.RawURLEncoding.DecodeString(parts[1])
	if !validCLIDeviceUserCode(userCode) || err != nil || len(secret) != 32 {
		return "", "", false
	}
	return userCode, cliDeviceDigest(userCode + "." + parts[1]), true
}

func (h *Handler) StartCLIDeviceLogin(w http.ResponseWriter, r *http.Request) {
	if h.CLIDeviceLogins == nil {
		writeError(w, http.StatusServiceUnavailable, "cli device login is unavailable")
		return
	}
	for attempt := 0; attempt < 5; attempt++ {
		deviceCode, userCode, err := newCLIDeviceCodes()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not start cli device login")
			return
		}
		err = h.CLIDeviceLogins.Create(
			r.Context(),
			cliDeviceDigest(deviceCode),
			userCode,
			cliDeviceLoginTTL,
		)
		if errors.Is(err, auth.ErrCLIDeviceCodeConflict) {
			continue
		}
		if errors.Is(err, auth.ErrCLIDeviceCapacity) {
			writeErrorCode(w, http.StatusTooManyRequests, "cli_device_capacity", "too many pending cli device logins")
			return
		}
		if err != nil {
			slog.Warn("cli device login: create session failed", "error", err)
			writeError(w, http.StatusServiceUnavailable, "could not start cli device login")
			return
		}
		writeJSON(w, http.StatusCreated, cliDeviceStartResponse{
			DeviceCode: deviceCode,
			UserCode:   userCode,
			ExpiresIn:  int(cliDeviceLoginTTL / time.Second),
			Interval:   cliDeviceLoginPollInterval,
		})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "could not allocate cli device login code")
}

func (h *Handler) ApproveCLIDeviceLogin(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var body struct {
		UserCode string `json:"user_code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userCode := normalizeCLIDeviceUserCode(body.UserCode)
	if h.CLIDeviceLogins == nil || !validCLIDeviceUserCode(userCode) {
		writeErrorCode(w, http.StatusNotFound, "cli_device_not_found", "cli device login not found")
		return
	}
	err := h.CLIDeviceLogins.Approve(r.Context(), userCode, userID)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
	case errors.Is(err, auth.ErrCLIDeviceNotFound):
		writeErrorCode(w, http.StatusNotFound, "cli_device_not_found", "cli device login not found or expired")
	case errors.Is(err, auth.ErrCLIDeviceAlreadyApproved):
		writeErrorCode(w, http.StatusConflict, "cli_device_already_approved", "cli device login was approved by another user")
	default:
		slog.Warn("cli device login: approve session failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "could not approve cli device login")
	}
}

func (h *Handler) PollCLIDeviceLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceCode string `json:"device_code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.DeviceCode) == "" {
		writeError(w, http.StatusBadRequest, "device_code is required")
		return
	}
	if h.CLIDeviceLogins == nil {
		writeError(w, http.StatusServiceUnavailable, "cli device login is unavailable")
		return
	}
	userCode, digest, ok := parseCLIDeviceCode(body.DeviceCode)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid device_code")
		return
	}
	approval, err := h.CLIDeviceLogins.Get(r.Context(), userCode, digest)
	switch {
	case errors.Is(err, auth.ErrCLIDevicePending):
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "authorization_pending"})
		return
	case errors.Is(err, auth.ErrCLIDeviceNotFound):
		writeErrorCode(w, http.StatusGone, "cli_device_expired", "cli device login not found, expired, or already used")
		return
	case err != nil:
		slog.Warn("cli device login: consume session failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "could not complete cli device login")
		return
	}
	if approval.Token != "" {
		writeJSON(w, http.StatusOK, map[string]string{"token": approval.Token})
		return
	}
	userID := approval.UserID
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	// This bearer exists only long enough for the CLI to create and verify its
	// PAT. Unlike an interactive browser session, it must not live for days.
	token, err := h.issueJWTWithTTL(user, cliDeviceExchangeTokenTTL, false)
	if err != nil {
		if errors.Is(err, auth.ErrTemporarilyDisabledUser) {
			writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
			return
		}
		slog.Warn("cli device login: issue token failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	token, err = h.CLIDeviceLogins.Complete(
		r.Context(),
		userCode,
		digest,
		token,
		cliDeviceCompletedTTL,
	)
	if err != nil {
		slog.Warn("cli device login: complete session failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "could not complete cli device login")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}
