package lark

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dbOrgBotContext stores the remembered target in org_bot_chat_context.
// A database that has not applied the migration yet disables the store
// after one warning so chat still works, just without a memory.
type dbOrgBotContext struct {
	q        *db.Queries
	disabled atomic.Bool
}

// NewDBOrgBotContext returns nil when queries is nil so the bot skips memory.
func NewDBOrgBotContext(q *db.Queries) OrgBotContextStore {
	if q == nil {
		return nil
	}
	return &dbOrgBotContext{q: q}
}

func (s *dbOrgBotContext) Load(ctx context.Context, installationID pgtype.UUID, chatID string, userID pgtype.UUID) (OrgBotChatContext, error) {
	if s == nil || s.q == nil || s.disabled.Load() {
		return OrgBotChatContext{}, ErrOrgBotContextMissing
	}
	row, err := s.q.GetOrgBotChatContext(ctx, db.GetOrgBotChatContextParams{
		InstallationID: installationID,
		ChatID:         chatID,
		UserID:         userID,
	})
	if isUndefinedTable(err) {
		s.disable()
		return OrgBotChatContext{}, ErrOrgBotContextMissing
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgBotChatContext{}, ErrOrgBotContextMissing
	}
	if err != nil {
		return OrgBotChatContext{}, err
	}
	state := OrgBotChatContext{
		Kind:                row.TargetKind,
		UnitID:              row.UnitID,
		UnitPath:            row.UnitPath,
		ProjectID:           row.ProjectID,
		ProjectName:         row.ProjectName,
		Awaiting:            row.Awaiting,
		PendingText:         row.PendingText,
		Candidates:          unmarshalOrgBotCandidates(row.PendingCandidates),
		LastIssueID:         row.LastIssueID,
		LastIssueIdentifier: row.LastIssueIdentifier,
	}
	if row.UpdatedAt.Valid {
		state.UpdatedAt = row.UpdatedAt.Time
	}
	return state, nil
}

func (s *dbOrgBotContext) Save(ctx context.Context, installationID pgtype.UUID, chatID string, userID pgtype.UUID, state OrgBotChatContext) error {
	if s == nil || s.q == nil || s.disabled.Load() {
		return nil
	}
	err := s.q.UpsertOrgBotChatContext(ctx, db.UpsertOrgBotChatContextParams{
		InstallationID:      installationID,
		ChatID:              chatID,
		UserID:              userID,
		TargetKind:          state.Kind,
		UnitID:              state.UnitID,
		UnitPath:            state.UnitPath,
		ProjectID:           state.ProjectID,
		ProjectName:         state.ProjectName,
		Awaiting:            state.Awaiting,
		PendingText:         state.PendingText,
		PendingCandidates:   marshalOrgBotCandidates(state.Candidates),
		LastIssueID:         state.LastIssueID,
		LastIssueIdentifier: state.LastIssueIdentifier,
	})
	if isUndefinedTable(err) {
		s.disable()
		return nil
	}
	return err
}

func (s *dbOrgBotContext) disable() {
	if s.disabled.CompareAndSwap(false, true) {
		slog.Warn("lark org bot: chat context table is missing; choices will not be remembered until migration 031 is applied")
	}
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}
