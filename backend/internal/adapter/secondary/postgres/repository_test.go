package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestFindLiveByValuesUsesSingleBatchQuery(t *testing.T) {
	db := &queryCounter{}
	repository := &Repository{queries: sqlc.New(db)}
	values := []string{"flag-a", "flag-b", "flag-c"}

	got, err := repository.FindLiveByValues(context.Background(), values, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("FindLiveByValues() = %v, want empty result from fixture", got)
	}
	if db.calls != 1 {
		t.Fatalf("database queries = %d, want exactly one", db.calls)
	}
	if !strings.Contains(db.statement, "ANY($1::VARCHAR[])") {
		t.Fatalf("query does not use an array batch lookup: %s", db.statement)
	}
	if len(db.args) != 2 || !reflect.DeepEqual(db.args[0], values) || db.args[1] != int64(12) {
		t.Fatalf("query args = %#v, want values and round 12", db.args)
	}
}

func TestFindLiveByValuesSkipsEmptyBatch(t *testing.T) {
	db := &queryCounter{}
	repository := &Repository{queries: sqlc.New(db)}

	got, err := repository.FindLiveByValues(context.Background(), nil, 12)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("FindLiveByValues() = %#v, want a non-nil empty map", got)
	}
	if db.calls != 0 {
		t.Fatalf("database queries = %d, want zero for an empty batch", db.calls)
	}
}

func TestFindLiveByIDWrapsNotFoundAtQueryOrigin(t *testing.T) {
	db := &queryCounter{rowErr: pgx.ErrNoRows}
	repository := &Repository{queries: sqlc.New(db)}

	_, err := repository.FindLiveByID(context.Background(), uuid.NewV7(), 4)
	if !errors.Is(err, domain.ErrFlagNotFound) {
		t.Fatalf("FindLiveByID() error = %v, want ErrFlagNotFound", err)
	}
	if !strings.HasPrefix(err.Error(), "postgres.Repository.FindLiveByID - find live flag by ID:") {
		t.Fatalf("FindLiveByID() error is missing its operation: %v", err)
	}
}

type queryCounter struct {
	calls     int
	statement string
	args      []any
	rowErr    error
}

func (*queryCounter) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}

func (db *queryCounter) Query(_ context.Context, statement string, args ...any) (pgx.Rows, error) {
	db.calls++
	db.statement = statement
	db.args = args
	return emptyRows{}, nil
}

func (db *queryCounter) QueryRow(context.Context, string, ...any) pgx.Row {
	if db.rowErr == nil {
		panic("unexpected QueryRow")
	}
	return queryError{err: db.rowErr}
}

type queryError struct{ err error }

func (r queryError) Scan(...any) error { return r.err }

type emptyRows struct{}

func (emptyRows) Close()                                       {}
func (emptyRows) Err() error                                   { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                                   { return false }
func (emptyRows) Scan(...any) error                            { return nil }
func (emptyRows) Values() ([]any, error)                       { return nil, nil }
func (emptyRows) RawValues() [][]byte                          { return nil }
func (emptyRows) Conn() *pgx.Conn                              { return nil }
func (emptyRows) TypeMap() *pgtype.Map                         { return nil }
