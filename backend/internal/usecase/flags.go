package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

const (
	maxBatchSize       = 100
	maxFlagLength      = 32
	eventQueueCapacity = 256
)

var (
	ErrNotStarted   = errors.New("game has not started")
	ErrFinished     = errors.New("game has finished")
	ErrPaused       = errors.New("game is paused")
	ErrLimited      = errors.New("flag submission rate exceeded")
	ErrInvalidBatch = errors.New("expected at most 100 strings of at most 32 characters")
)

type LifecycleReader interface {
	Lifecycle(ctx context.Context) (domain.Lifecycle, error)
}
type Limiter interface {
	Allow(ctx context.Context, scope string, cost, rate, burst int) (bool, error)
}

type Observer interface {
	ResolveTeam(ctx context.Context, team uuid.UUID)
	ObserveFlags(team uuid.UUID, verdict string, count int)
}

type Flags struct {
	teams       outbound.TeamRepository
	attacks     outbound.AttackRepository
	lifecycle   LifecycleReader
	limiter     Limiter
	publisher   Publisher
	logger      *slog.Logger
	rate, burst int
	events      chan domain.AttackResult
	observer    Observer
}

type FlagResponse struct {
	Message string
	Flag    string
	Verdict string
}

// SetObserver is called during bootstrap, before the service handles requests.
func (f *Flags) SetObserver(observer Observer) { f.observer = observer }

func NewFlags(teams outbound.TeamRepository, attacks outbound.AttackRepository, lifecycle LifecycleReader, limiter Limiter, publisher Publisher, logger *slog.Logger, rate, burst int) *Flags {
	return &Flags{teams: teams, attacks: attacks, lifecycle: lifecycle, limiter: limiter, publisher: publisher, logger: logger, rate: rate, burst: burst, events: make(chan domain.AttackResult, eventQueueCapacity)}
}

var flagPattern = regexp.MustCompile(`^[A-Z0-9]{31}=$`)
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (f *Flags) Submit(ctx context.Context, token string, values []string) (responses []FlagResponse, submitErr error) {
	const location = "usecase.Flags.Submit"
	receivedAt := time.Now()
	team, life, err := f.resolveSubmission(ctx, token)
	defer func() { f.observeSubmission(team, len(values), responses, submitErr) }()
	if err != nil {
		return nil, fmt.Errorf("%s - resolve submission: %w", location, err)
	}
	if err := validateSubmissionLifecycle(life); err != nil {
		return nil, fmt.Errorf("%s - validate game lifecycle: %w", location, err)
	}
	if err := validateFlagBatch(values); err != nil {
		return nil, fmt.Errorf("%s - validate submission: %w", location, err)
	}
	if err := f.allowSubmission(ctx, team, max(1, len(values))); err != nil {
		return nil, fmt.Errorf("%s - check submission allowance: %w", location, err)
	}
	return f.submitBatch(ctx, domain.FlagSubmission{
		TeamID: team, Values: values, ReceivedAt: receivedAt, Generation: life.Session.Generation,
	})
}

func (f *Flags) submitBatch(ctx context.Context, submission domain.FlagSubmission) ([]FlagResponse, error) {
	const location = "usecase.Flags.submitBatch"
	values := submission.Values
	valid := make([]string, 0, len(values))
	indexes := make([]int, 0, len(values))
	results := make([]FlagResponse, len(values))
	for i, value := range values {
		results[i] = FlagResponse{Message: "[" + value + "] Flag is invalid or too old.", Flag: value, Verdict: "invalid"}
		if flagPattern.MatchString(value) {
			valid = append(valid, value)
			indexes = append(indexes, i)
		}
	}
	if len(valid) == 0 {
		return results, nil
	}
	submission.Values = valid
	outcomes, err := f.attacks.SubmitBatch(ctx, submission)
	if err != nil {
		return nil, fmt.Errorf("%s - commit flag batch: %w", location, err)
	}
	if len(outcomes) != len(valid) {
		return nil, fmt.Errorf("%s - storage returned an incomplete batch", location)
	}
	for i, outcome := range outcomes {
		message, err := submissionMessage(outcome)
		if err != nil {
			return nil, fmt.Errorf("%s - format verdict: %w", location, err)
		}
		results[indexes[i]].Message = "[" + valid[i] + "] " + message
		results[indexes[i]].Verdict = flagVerdict(outcome.Err)
	}
	for _, outcome := range outcomes {
		if outcome.Err == nil {
			select {
			case f.events <- outcome.Attack:
			default:
			}
		}
	}
	return results, nil
}

func submissionMessage(outcome domain.SubmissionResult) (string, error) {
	const location = "usecase.submissionMessage"
	if outcome.Err != nil {
		if message, known := verdict(outcome.Err); known {
			return message, nil
		}
		return "", fmt.Errorf("%s - unexpected submission failure: %w", location, outcome.Err)
	}
	return fmt.Sprintf("Flag accepted! Earned %s flag points!", strconv.FormatFloat(outcome.Attack.AttackerDelta, 'f', -1, 64)), nil
}

func (f *Flags) resolveSubmission(
	ctx context.Context,
	token string,
) (uuid.UUID, domain.Lifecycle, error) {
	const location = "usecase.Flags.resolveSubmission"
	if !tokenPattern.MatchString(token) {
		return uuid.Nil(), domain.Lifecycle{}, fmt.Errorf("%s - validate team token: %w", location, domain.ErrTeamTokenInvalid)
	}
	team, err := f.teams.ActiveIDByToken(ctx, token)
	if err != nil {
		return uuid.Nil(), domain.Lifecycle{}, fmt.Errorf("%s - resolve submitting team: %w", location, err)
	}
	if f.observer != nil {
		f.observer.ResolveTeam(ctx, team)
	}
	life, err := f.lifecycle.Lifecycle(ctx)
	if err != nil {
		return team, domain.Lifecycle{}, fmt.Errorf("%s - read game lifecycle: %w", location, err)
	}
	return team, life, nil
}

func validateSubmissionLifecycle(life domain.Lifecycle) error {
	const location = "usecase.validateSubmissionLifecycle"
	if life.Finished() {
		return fmt.Errorf("%s - game has finished: %w", location, ErrFinished)
	}
	if life.Session.PausedAt != nil || life.Session.ResetPending {
		return fmt.Errorf("%s - game is paused: %w", location, ErrPaused)
	}
	if !life.Config.GameRunning || life.Config.RealRound < 1 {
		return fmt.Errorf("%s - game has not started: %w", location, ErrNotStarted)
	}
	return nil
}

func validateFlagBatch(values []string) error {
	const location = "usecase.validateFlagBatch"
	if len(values) > maxBatchSize {
		return fmt.Errorf("%s - validate flag batch: %w", location, ErrInvalidBatch)
	}
	for _, value := range values {
		if utf8.RuneCountInString(value) > maxFlagLength {
			return fmt.Errorf("%s - validate flag batch: %w", location, ErrInvalidBatch)
		}
	}
	return nil
}

func (f *Flags) allowSubmission(ctx context.Context, team uuid.UUID, count int) error {
	const location = "usecase.Flags.allowSubmission"
	ok, err := f.limiter.Allow(ctx, "team:"+team.String(), count, f.rate, f.burst)
	if err != nil {
		return fmt.Errorf("%s - check submission rate: %w", location, err)
	}
	if !ok {
		return fmt.Errorf("%s - submission rate exceeded: %w", location, ErrLimited)
	}
	return nil
}

func verdict(err error) (string, bool) {
	for _, pair := range []struct {
		err  error
		text string
	}{
		{domain.ErrFlagNotFound, "Flag is invalid or too old."},
		{domain.ErrFlagTooOld, "Flag is too old"},
		{domain.ErrOwnFlag, "Flag is your own"},
		{domain.ErrFlagAlreadyStolen, "Flag already stolen"},
		{domain.ErrServiceDown, "Cannot submit flags while service is down"},
	} {
		if errors.Is(err, pair.err) {
			return pair.text, true
		}
	}
	return "", false
}

func (f *Flags) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-f.events:
			data := struct {
				AttackerID    uuid.UUID `json:"attacker_id"`
				VictimID      uuid.UUID `json:"victim_id"`
				TaskID        uuid.UUID `json:"task_id"`
				AttackerDelta float64   `json:"attacker_delta"`
				Generation    int64     `json:"generation"`
			}{event.AttackerID, event.VictimID, event.TaskID, event.AttackerDelta, event.Generation}
			if err := f.publisher.Publish(ctx, "flag_stolen", map[string]any{"data": data}); err != nil && ctx.Err() == nil {
				f.logger.Warn("live flag event unavailable", "error", err)
			}
		}
	}
}
