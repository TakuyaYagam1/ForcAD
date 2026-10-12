package flags

import (
	"errors"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (s *Service) observeSubmission(team uuid.UUID, count int, responses []Response, err error) {
	if s.observer == nil {
		return
	}
	if err != nil {
		s.observer.ObserveFlags(team, submissionVerdict(err), count)
		return
	}
	counts := make(map[string]int)
	for _, response := range responses {
		counts[response.Verdict]++
	}
	for verdict, count := range counts {
		s.observer.ObserveFlags(team, verdict, count)
	}
}

func flagVerdict(err error) string {
	switch {
	case err == nil:
		return "accepted"
	case errors.Is(err, domain.ErrFlagAlreadyStolen):
		return "duplicate"
	case errors.Is(err, domain.ErrOwnFlag):
		return "own"
	case errors.Is(err, domain.ErrFlagTooOld):
		return "expired"
	case errors.Is(err, domain.ErrServiceDown):
		return "service_down"
	default:
		return "invalid"
	}
}

func submissionVerdict(err error) string {
	switch {
	case errors.Is(err, domain.ErrTeamTokenInvalid):
		return "invalid_token"
	case errors.Is(err, ErrLimited):
		return "limited"
	case errors.Is(err, ErrInvalidBatch):
		return "invalid_batch"
	case errors.Is(err, ErrNotStarted), errors.Is(err, ErrFinished), errors.Is(err, ErrPaused):
		return "inactive_game"
	default:
		return "unavailable"
	}
}
