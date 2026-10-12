package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestGameDBErrorPreservesSentinelsAndContext(t *testing.T) {
	if err := gameDBError(nil); err != nil {
		t.Fatalf("gameDBError(nil) = %v, want nil", err)
	}

	missing := gameDBError(pgx.ErrNoRows)
	if !errors.Is(missing, domain.ErrNotFound) {
		t.Fatalf("gameDBError(ErrNoRows) = %v, want ErrNotFound", missing)
	}
	if !strings.HasPrefix(missing.Error(), "postgres.gameDBError - map missing database row:") {
		t.Fatalf("missing-row error is missing its origin: %v", missing)
	}

	conflict := gameDBError(&pgconn.PgError{Code: "23505"})
	if !errors.Is(conflict, domain.ErrConflict) {
		t.Fatalf("gameDBError(unique violation) = %v, want ErrConflict", conflict)
	}

	canceled := gameDBError(context.Canceled)
	if !errors.Is(canceled, context.Canceled) {
		t.Fatalf("gameDBError(context.Canceled) = %v, want context.Canceled", canceled)
	}
}
