package postgres

import (
	"encoding/json"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5/pgtype"
)

func domainFlag(row sqlc.Flag) domain.Flag {
	return domain.Flag{
		ID:              row.ID,
		Value:           row.Flag,
		TeamID:          row.TeamID,
		TaskID:          row.TaskID,
		Round:           row.Round,
		PublicData:      row.PublicFlagData,
		PrivateData:     row.PrivateFlagData,
		VulnerabilityID: row.VulnNumber,
	}
}

func domainTeam(row sqlc.ListGameTeamsRow) domain.Team {
	return domain.Team{
		ID:          row.ID,
		Name:        row.Name,
		IP:          row.Ip,
		Token:       row.Token,
		Kind:        row.Kind,
		Highlighted: row.Highlighted,
		Active:      row.Active,
		LogoPath:    row.LogoPath,
	}
}

func domainTask(row sqlc.ListGameTasksRow) domain.Task {
	return domain.Task{
		ID:             row.ID,
		Name:           row.Name,
		Checker:        row.Checker,
		EnvPath:        row.EnvPath,
		Gets:           row.Gets,
		Puts:           row.Puts,
		Places:         row.Places,
		CheckerTimeout: row.CheckerTimeout,
		CheckerType:    row.CheckerType,
		GetPeriod:      row.GetPeriod,
		DefaultScore:   row.DefaultScore,
		Active:         row.Active,
	}
}

func nullableTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  value,
		Valid: true,
	}
}

func timestampNow() pgtype.Timestamptz {
	return timestamp(time.Now())
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}
