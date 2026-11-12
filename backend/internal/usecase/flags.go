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

	"golang.org/x/sync/semaphore"

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
	Allow(
		ctx context.Context,
		scope string,
		cost, rate, burst int,
	) (bool, error)
}

type Observer interface {
	ResolveTeam(ctx context.Context, team uuid.UUID)
	ObserveFlags(team uuid.UUID, verdict string, count int)
}

type submissionTimingObserver interface {
	ObserveSubmissionPhase(phase, outcome string, elapsed time.Duration)
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
	timing      submissionTimingObserver
	// admission bounds syntactically valid values per receiver, including duplicates.
	// FIFO waiting does not guarantee per-team fairness.
	admission *semaphore.Weighted
}

type FlagResponse struct {
	Message string
	Flag    string
	Verdict string
}

// SetObserver is called during bootstrap, before the service handles requests.
func (f *Flags) SetObserver(observer Observer) {
	f.observer = observer
	f.timing, _ = observer.(submissionTimingObserver)
}

func NewFlags(
	teams outbound.TeamRepository,
	attacks outbound.AttackRepository,
	lifecycle LifecycleReader,
	limiter Limiter,
	publisher Publisher,
	logger *slog.Logger,
	rate, burst int,
) *Flags {
	return &Flags{
		teams:     teams,
		attacks:   attacks,
		lifecycle: lifecycle,
		limiter:   limiter,
		publisher: publisher,
		logger:    logger,
		rate:      rate,
		burst:     burst,
		events:    make(chan domain.AttackResult, eventQueueCapacity),
		admission: semaphore.NewWeighted(maxBatchSize),
	}
}

var flagPattern = regexp.MustCompile(`^[A-Z0-9]{31}=$`)
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (f *Flags) Submit(ctx context.Context, token string, values []string) (responses []FlagResponse, submitErr error) {
	const location = "usecase.Flags.Submit"
	receivedAt := time.Now()
	started := time.Now()
	team, life, err := f.resolveSubmission(ctx, token)
	f.observeSubmissionPhase("resolve", started, err)
	defer func() {
		f.observeSubmission(team, len(values), responses, submitErr)
		f.observeSubmissionPhase("total", receivedAt, submitErr)
	}()
	if err != nil {
		return nil, fmt.Errorf("%s - resolve submission: %w", location, err)
	}
	if err := validateSubmissionLifecycle(life); err != nil {
		return nil, fmt.Errorf("%s - validate game lifecycle: %w", location, err)
	}
	if err := validateFlagBatch(values); err != nil {
		return nil, fmt.Errorf("%s - validate submission: %w", location, err)
	}
	started = time.Now()
	if err := f.allowSubmission(ctx, team, max(1, len(values))); err != nil {
		f.observeSubmissionPhase("rate", started, err)
		return nil, fmt.Errorf("%s - check submission allowance: %w", location, err)
	}
	f.observeSubmissionPhase("rate", started, nil)
	return f.submitBatch(ctx, domain.FlagSubmission{
		TeamID:     team,
		Values:     values,
		ReceivedAt: receivedAt,
		Generation: life.Session.Generation,
	})
}

func (f *Flags) submitBatch(ctx context.Context, submission domain.FlagSubmission) ([]FlagResponse, error) {
	const location = "usecase.Flags.submitBatch"
	values := submission.Values
	valid := make([]string, 0, len(values))
	indexes := make([]int, 0, len(values))
	results := make([]FlagResponse, len(values))
	for i, value := range values {
		results[i] = FlagResponse{
			Message: "[" + value + "] Flag is invalid or too old.",
			Flag:    value,
			Verdict: "invalid",
		}
		if flagPattern.MatchString(value) {
			valid = append(valid, value)
			indexes = append(indexes, i)
		}
	}
	if len(valid) == 0 {
		return results, nil
	}
	submission.Values = valid
	outcomes, err := f.submitAdmittedBatch(ctx, submission)
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
		results[indexes[i]].Verdict = flagVerdict(outcome)
	}
	for _, outcome := range outcomes {
		if outcome.Err == nil && !outcome.NPC {
			select {
			case f.events <- outcome.Attack:
			default:
			}
		}
	}
	return results, nil
}

// submitAdmittedBatch waits before the repository can acquire a connection or
// open a transaction. It returns every permit before formatting or publishing.
func (f *Flags) submitAdmittedBatch(ctx context.Context, submission domain.FlagSubmission) ([]domain.SubmissionResult, error) {
	const location = "usecase.Flags.submitAdmittedBatch"
	weight := int64(len(submission.Values))
	// Public Submit already bounds batches. Also protect direct internal calls:
	// Acquire of a weight above capacity would otherwise wait until cancellation.
	if weight > maxBatchSize {
		return nil, fmt.Errorf("%s - validate admission weight: %w", location, ErrInvalidBatch)
	}
	started := time.Now()
	waitErr := f.admission.Acquire(ctx, weight)
	f.observeSubmissionPhase("admission", started, waitErr)
	if waitErr != nil {
		return nil, fmt.Errorf("%s - wait for submission budget: %w", location, waitErr)
	}
	defer f.admission.Release(weight)
	started = time.Now()
	results, err := f.attacks.SubmitBatch(ctx, submission)
	f.observeSubmissionPhase("storage", started, err)
	if err != nil {
		return results, fmt.Errorf("%s - persist admitted flag batch: %w", location, err)
	}
	return results, nil
}

func (f *Flags) observeSubmissionPhase(phase string, started time.Time, err error) {
	if f.timing == nil {
		return
	}
	outcome := "ok"
	switch {
	case errors.Is(err, context.Canceled):
		outcome = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		outcome = "deadline"
	case err != nil:
		outcome = "error"
	}
	f.timing.ObserveSubmissionPhase(phase, outcome, time.Since(started))
}

func submissionMessage(outcome domain.SubmissionResult) (string, error) {
	const location = "usecase.submissionMessage"
	if outcome.Err != nil {
		if message, known := verdict(outcome.Err); known {
			return message, nil
		}
		return "", fmt.Errorf("%s - unexpected submission failure: %w", location, outcome.Err)
	}
	if outcome.NPC {
		return "Flag accepted (NPC test). No points awarded.", nil
	}
	return fmt.Sprintf("Flag accepted! Earned %s flag points!", strconv.FormatFloat(outcome.Attack.AttackerDelta, 'f', -1, 64)), nil
}

func (f *Flags) resolveSubmission(ctx context.Context, token string) (uuid.UUID, domain.Lifecycle, error) {
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
				EventID       string    `json:"event_id"`
				CreatedAt     time.Time `json:"created_at"`
				AttackerID    uuid.UUID `json:"attacker_id"`
				VictimID      uuid.UUID `json:"victim_id"`
				TaskID        uuid.UUID `json:"task_id"`
				AttackerDelta float64   `json:"attacker_delta"`
				Generation    int64     `json:"generation"`
			}{event.EventID, event.CreatedAt, event.AttackerID, event.VictimID, event.TaskID, event.AttackerDelta, event.Generation}
			if err := f.publisher.Publish(ctx, "flag_stolen", map[string]any{
				"data": data,
			}); err != nil && ctx.Err() == nil {
				f.logger.Warn("live flag event unavailable", "error", err)
			}
		}
	}
}
