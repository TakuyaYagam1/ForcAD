package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestCheckerLogValidationKeepsDomainSentinelAndLocation(t *testing.T) {
	err := persistCheckerLogs(t.Context(), nil, sqlc.CompleteCheckerJobClaimRow{}, []domain.CheckerLog{{}})
	if !errors.Is(err, domain.ErrTeamTaskMissing) || !strings.HasPrefix(err.Error(), "postgres.persistCheckerLogs - ") {
		t.Fatalf("checker log validation lost attribution or sentinel: %v", err)
	}
}

func TestInvalidEventCursorHasHelperLocationsWithoutInput(t *testing.T) {
	const privateCursor = "private-cursor-value"
	_, err := parseEventCursor(privateCursor)
	if !errors.Is(err, domain.ErrEventCursorInvalid) || !strings.Contains(err.Error(), "postgres.parseEventCursor - ") || !strings.Contains(err.Error(), "postgres.invalidEventCursor - ") {
		t.Fatalf("invalid cursor lost attribution or sentinel: %v", err)
	}
	if strings.Contains(err.Error(), privateCursor) {
		t.Fatalf("invalid cursor exposed its input: %v", err)
	}
}

func TestEventMappingPreservesStoredEventFailure(t *testing.T) {
	_, err := mapEvents([]string{"{}"})
	if !errors.Is(err, domain.ErrNotFound) || !strings.Contains(err.Error(), "postgres.mapEvents - ") || !strings.Contains(err.Error(), "postgres.decodeEvent - ") {
		t.Fatalf("event mapping lost attribution or sentinel: %v", err)
	}
}
