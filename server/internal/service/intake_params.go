package service

import (
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func intakeProgress(id pgtype.UUID, done, remain string) db.SetIntakeProgressParams {
	return db.SetIntakeProgressParams{
		ID:             id,
		ProgressDone:   pgtype.Text{String: done, Valid: true},
		ProgressRemain: pgtype.Text{String: remain, Valid: true},
	}
}
