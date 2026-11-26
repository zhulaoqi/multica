package auth

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrCLIDeviceNotFound        = errors.New("cli device login not found")
	ErrCLIDevicePending         = errors.New("cli device login pending")
	ErrCLIDeviceAlreadyApproved = errors.New("cli device login already approved")
	ErrCLIDeviceCodeConflict    = errors.New("cli device login code conflict")
	ErrCLIDeviceCapacity        = errors.New("cli device login capacity reached")
)

// CLIDeviceStore coordinates a browser approval with a headless CLI. The
// device digest is the secret lookup key; the short user code is only for the
// browser confirmation page. Consume is destructive so an approval can mint
// at most one session token.
type CLIDeviceStore interface {
	Create(ctx context.Context, deviceDigest, userCode string, ttl time.Duration) error
	Approve(ctx context.Context, userCode, userID string) error
	Get(ctx context.Context, userCode, deviceDigest string) (CLIDeviceApproval, error)
	Complete(ctx context.Context, userCode, deviceDigest, token string, ttl time.Duration) (string, error)
}

type CLIDeviceApproval struct {
	UserID string
	Token  string
}

type memoryCLIDeviceSession struct {
	deviceDigest string
	userID       string
	token        string
	expiresAt    time.Time
}

type MemoryCLIDeviceStore struct {
	mu          sync.Mutex
	sessions    map[string]memoryCLIDeviceSession
	now         func() time.Time
	maxSessions int
}

func NewMemoryCLIDeviceStore() *MemoryCLIDeviceStore {
	return &MemoryCLIDeviceStore{
		sessions:    make(map[string]memoryCLIDeviceSession),
		now:         time.Now,
		maxSessions: 10_000,
	}
}

func (s *MemoryCLIDeviceStore) Create(_ context.Context, deviceDigest, userCode string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	if _, exists := s.sessions[userCode]; exists {
		return ErrCLIDeviceCodeConflict
	}
	if len(s.sessions) >= s.maxSessions {
		return ErrCLIDeviceCapacity
	}
	s.sessions[userCode] = memoryCLIDeviceSession{
		deviceDigest: deviceDigest,
		expiresAt:    s.now().Add(ttl),
	}
	return nil
}

func (s *MemoryCLIDeviceStore) Approve(_ context.Context, userCode, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	session, ok := s.sessions[userCode]
	if !ok {
		return ErrCLIDeviceNotFound
	}
	if session.userID != "" {
		if session.userID == userID {
			return nil
		}
		return ErrCLIDeviceAlreadyApproved
	}
	session.userID = userID
	s.sessions[userCode] = session
	return nil
}

func (s *MemoryCLIDeviceStore) Get(_ context.Context, userCode, deviceDigest string) (CLIDeviceApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	session, ok := s.sessions[userCode]
	if !ok || session.deviceDigest != deviceDigest {
		return CLIDeviceApproval{}, ErrCLIDeviceNotFound
	}
	if session.userID == "" {
		return CLIDeviceApproval{}, ErrCLIDevicePending
	}
	return CLIDeviceApproval{UserID: session.userID, Token: session.token}, nil
}

func (s *MemoryCLIDeviceStore) Complete(_ context.Context, userCode, deviceDigest, token string, ttl time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	session, ok := s.sessions[userCode]
	if !ok || session.deviceDigest != deviceDigest {
		return "", ErrCLIDeviceNotFound
	}
	if session.userID == "" {
		return "", ErrCLIDevicePending
	}
	if session.token == "" {
		session.token = token
	}
	session.expiresAt = s.now().Add(ttl)
	s.sessions[userCode] = session
	return session.token, nil
}

func (s *MemoryCLIDeviceStore) pruneLocked() {
	now := s.now()
	for userCode, session := range s.sessions {
		if !session.expiresAt.After(now) {
			delete(s.sessions, userCode)
		}
	}
}

var _ CLIDeviceStore = (*MemoryCLIDeviceStore)(nil)
