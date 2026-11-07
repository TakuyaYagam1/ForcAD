package postgres

import (
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool:    pool,
		queries: sqlc.New(pool),
	}
}

var (
	_ outbound.TeamRepository      = (*Repository)(nil)
	_ outbound.FlagRepository      = (*Repository)(nil)
	_ outbound.AttackRepository    = (*Repository)(nil)
	_ outbound.SessionRepository   = (*Repository)(nil)
	_ outbound.JobRepository       = (*Repository)(nil)
	_ outbound.OutboxRepository    = (*Repository)(nil)
	_ outbound.SchedulerRepository = (*Repository)(nil)
)
