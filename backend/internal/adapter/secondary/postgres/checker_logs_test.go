package postgres

import (
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestCheckerLogValidationAllowsPracticeRound(t *testing.T) {
	started := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	log := domain.CheckerLog{
		Round: 0, Action: "check", Stage: string(domain.JobActionCheckOnly),
		StartedAt: started, FinishedAt: started, Status: 101,
	}
	if !validCheckerLog(log, 0) {
		t.Fatal("valid practice-round checker log was rejected")
	}
}
