package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

const maxPublishBatch int32 = 256
const defaultRecoveryGrace = 30 * time.Second

type Repository interface {
	outbound.SchedulerRepository
	outbound.OutboxRepository
	outbound.JobRepository
}

type Options struct {
	PollInterval     time.Duration
	RecoveryInterval time.Duration
	OutboxLease      time.Duration
	RecoveryGrace    time.Duration
	BatchSize        int32
	RecoveryLimit    int32
}

type Scheduler struct {
	repository Repository
	lifecycle  outbound.GameRepository
	publisher  outbound.JobPublisher
	logger     *slog.Logger
	options    Options
}

func New(repository Repository, lifecycle outbound.GameRepository, publisher outbound.JobPublisher, logger *slog.Logger, options Options) *Scheduler {
	if options.PollInterval <= 0 {
		options.PollInterval = 100 * time.Millisecond
	}
	if options.RecoveryInterval <= 0 {
		options.RecoveryInterval = 5 * time.Second
	}
	if options.OutboxLease <= 0 {
		options.OutboxLease = 30 * time.Second
	}
	if options.RecoveryGrace <= 0 {
		options.RecoveryGrace = defaultRecoveryGrace
	}
	if options.BatchSize <= 0 || options.BatchSize > maxPublishBatch {
		options.BatchSize = maxPublishBatch
	}
	if options.RecoveryLimit <= 0 {
		options.RecoveryLimit = 100
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{repository: repository, lifecycle: lifecycle, publisher: publisher, logger: logger, options: options}
}

func (s *Scheduler) Run(ctx context.Context) error {
	const location = "scheduler.Scheduler.Run"
	if ctx == nil {
		return fmt.Errorf("%s - scheduler context must not be nil", location)
	}
	if s.repository == nil || s.lifecycle == nil || s.publisher == nil {
		return fmt.Errorf("%s - scheduler dependencies are required", location)
	}
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	nextMaintenance := time.Time{}
	for {
		at := time.Now().UTC()
		recoverExpired := nextMaintenance.IsZero() || !at.Before(nextMaintenance)
		if recoverExpired {
			nextMaintenance = at.Add(s.options.RecoveryInterval)
		}
		if err := s.poll(ctx, at, recoverExpired); err != nil && ctx.Err() == nil {
			s.logger.Error("scheduler iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) poll(ctx context.Context, at time.Time, recoverExpired bool) error {
	const location = "scheduler.Scheduler.poll"
	if recoverExpired {
		if err := s.recoverExpired(ctx, at); err != nil {
			return fmt.Errorf("%s - recover expired jobs: %w", location, err)
		}
	}
	if err := s.maintainSession(ctx, at); err != nil {
		return fmt.Errorf("%s - maintain scheduler state: %w", location, err)
	}
	if err := s.publishBatch(ctx, at); err != nil {
		return fmt.Errorf("%s - publish pending jobs: %w", location, err)
	}
	life, err := s.lifecycle.Lifecycle(ctx)
	if err != nil {
		return fmt.Errorf("%s - read game lifecycle: %w", location, err)
	}
	if !life.Config.GameRunning || life.Session.PausedAt != nil || life.Session.ResetPending {
		return nil
	}
	startAt := effectiveStart(life)
	if startAt.IsZero() {
		return fmt.Errorf("%s - game schedule start time is required", location)
	}
	logicalAt := schedulerTime(life, at)
	if logicalAt.Before(startAt) {
		return nil
	}
	requests, err := scheduleRequests(ctx, s.lifecycle, life, at, startAt)
	if err != nil {
		return fmt.Errorf("%s - build schedule requests: %w", location, err)
	}
	if err := s.runSchedules(ctx, at, logicalAt, requests); err != nil {
		return fmt.Errorf("%s - process due schedules: %w", location, err)
	}
	return nil
}

func (s *Scheduler) maintainSession(ctx context.Context, at time.Time) error {
	const location = "scheduler.Scheduler.maintainSession"
	reset, err := s.repository.Maintain(ctx, at)
	if err != nil {
		return fmt.Errorf("%s - maintain game session: %w", location, err)
	}
	if reset {
		s.logger.Info("practice session reset")
	}
	started, err := s.repository.StartScheduled(ctx, at)
	if err != nil {
		return fmt.Errorf("%s - start scheduled game: %w", location, err)
	}
	if started {
		s.logger.Info("scheduled final session started")
	}
	return nil
}

func (s *Scheduler) runSchedules(ctx context.Context, at, logicalAt time.Time, requests []domain.ScheduleRequest) error {
	const location = "scheduler.Scheduler.runSchedules"
	for _, request := range requests {
		due, err := s.due(ctx, request, logicalAt)
		if err != nil {
			return fmt.Errorf("%s - check schedule deadline: %w", location, err)
		}
		if !due {
			continue
		}
		run, err := s.repository.Prepare(ctx, request)
		if err != nil {
			return fmt.Errorf("%s - prepare schedule %q: %w", location, request.ScheduleID, err)
		}
		if run == nil {
			if request.AdvancesRound {
				break
			}
			continue
		}

		publishErr := s.publishBatch(ctx, at)
		finished, finishErr := s.repository.Finish(ctx, run.ID, logicalAt)
		if finishErr != nil {
			return errors.Join(publishErr, fmt.Errorf("%s - finish schedule %q: %w", location, request.ScheduleID, finishErr))
		}
		if publishErr != nil {
			return publishErr
		}
		if !finished {
			break
		}
	}
	return nil
}

func (s *Scheduler) due(ctx context.Context, request domain.ScheduleRequest, logicalAt time.Time) (bool, error) {
	const location = "scheduler.Scheduler.due"
	if logicalAt.Before(request.StartAt) {
		return false, nil
	}
	lastRun, err := s.repository.LastRun(ctx, request.ScheduleID)
	if err != nil {
		return false, fmt.Errorf("%s - read schedule %q checkpoint: %w", location, request.ScheduleID, err)
	}
	return scheduleDue(lastRun, logicalAt, request.Interval), nil
}

func scheduleDue(lastRun *time.Time, at time.Time, interval time.Duration) bool {
	if at.IsZero() || interval < 0 {
		return false
	}
	if lastRun == nil {
		return true
	}
	return !lastRun.Add(interval).After(at)
}

func effectiveStart(life domain.Lifecycle) time.Time {
	if life.Session.FinalStart != nil {
		return *life.Session.FinalStart
	}
	if life.Session.PracticeStart != nil {
		return *life.Session.PracticeStart
	}
	return life.Config.StartTime
}

func schedulerTime(life domain.Lifecycle, at time.Time) time.Time {
	offset := time.Duration(life.Session.SchedulePausedSeconds * float64(time.Second))
	if life.Session.PausedAt != nil {
		offset += max(0, at.Sub(*life.Session.PausedAt))
	}
	return at.Add(-offset)
}

func scheduleRequests(ctx context.Context, lifecycle outbound.GameRepository, life domain.Lifecycle, at, startAt time.Time) ([]domain.ScheduleRequest, error) {
	const location = "scheduler.scheduleRequests"
	if life.Config.RoundTime <= 0 {
		return nil, fmt.Errorf("%s - invalid round time %d", location, life.Config.RoundTime)
	}
	roundInterval := time.Duration(life.Config.RoundTime) * time.Second
	switch life.Config.Mode {
	case "classic":
		return []domain.ScheduleRequest{{
			ScheduleID: "classic_rounds", At: at, StartAt: startAt,
			Interval: roundInterval, AdvancesRound: true, Workflow: domain.JobActionClassic,
		}}, nil
	case "blitz":
		requests := []domain.ScheduleRequest{{
			ScheduleID: "blitz_rounds", At: at, StartAt: startAt,
			Interval: roundInterval, AdvancesRound: true, Workflow: domain.JobActionPutsOnly,
		}}
		tasks, err := lifecycle.Tasks(ctx, true)
		if err != nil {
			return nil, fmt.Errorf("%s - list blitz tasks: %w", location, err)
		}
		for _, task := range tasks {
			if task.GetPeriod < 0 {
				continue
			}
			if task.ID == uuid.Nil() {
				return nil, fmt.Errorf("%s - blitz task ID must not be nil", location)
			}
			taskID := task.ID
			requests = append(requests, domain.ScheduleRequest{
				ScheduleID: "blitz_check_gets_task_" + task.ID.String(),
				At:         at, StartAt: startAt, Interval: time.Duration(task.GetPeriod) * time.Second,
				Workflow: domain.JobActionCheckGets, TaskID: &taskID,
			})
		}
		return requests, nil
	default:
		return nil, fmt.Errorf("%s - unsupported game mode %q", location, life.Config.Mode)
	}
}

func (s *Scheduler) publishBatch(ctx context.Context, at time.Time) error {
	const location = "scheduler.Scheduler.publishBatch"
	ownerToken, err := newOwnerToken()
	if err != nil {
		return fmt.Errorf("%s - create outbox claim token: %w", location, err)
	}
	jobs, err := s.repository.ClaimPending(ctx, ownerToken, at, at.Add(s.options.OutboxLease), s.options.BatchSize)
	if err != nil {
		return fmt.Errorf("%s - claim outbox jobs: %w", location, err)
	}
	if len(jobs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(jobs))
	claimed := make(map[uuid.UUID]struct{}, len(jobs))
	jobsByID := make(map[uuid.UUID]domain.CheckerJob, len(jobs))
	for _, job := range jobs {
		if job.ID == uuid.Nil() {
			return fmt.Errorf("%s - claimed checker job ID must not be nil", location)
		}
		ids = append(ids, job.ID)
		claimed[job.ID] = struct{}{}
		jobsByID[job.ID] = job
	}
	confirmed, publishErr := s.publisher.Publish(ctx, ids)
	var markErr error
	seen := make(map[uuid.UUID]struct{}, len(confirmed))
	publishedAt := time.Now().UTC()
	for _, id := range confirmed {
		if _, ok := claimed[id]; !ok {
			s.logger.Warn("publisher confirmed an unclaimed job", "job_id", id)
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		job := jobsByID[id]
		deadline, err := initialJobDeadline(job, publishedAt, s.options.RecoveryGrace)
		if err != nil {
			markErr = errors.Join(markErr, fmt.Errorf("%s - set initial deadline for job %q: %w", location, id, err))
			continue
		}
		marked, err := s.repository.MarkPublished(ctx, id, ownerToken, publishedAt, deadline)
		if err != nil {
			markErr = errors.Join(markErr, fmt.Errorf("%s - mark job %q published: %w", location, id, err))
		} else if !marked {
			s.logger.Debug("outbox publication checkpoint was already claimed", "job_id", id)
		}
	}
	if publishErr != nil {
		publishErr = fmt.Errorf("%s - publish checker jobs: %w", location, publishErr)
	}
	return errors.Join(publishErr, markErr)
}

func initialJobDeadline(job domain.CheckerJob, at time.Time, grace time.Duration) (time.Time, error) {
	const location = "scheduler.initialJobDeadline"
	if job.ID == uuid.Nil() || at.IsZero() || grace < 0 {
		return time.Time{}, fmt.Errorf("%s - job, timestamp, and nonnegative grace are required", location)
	}
	var payload domain.JobPayload
	if err := jsonv2.Unmarshal(job.Payload, &payload); err != nil {
		return time.Time{}, fmt.Errorf("%s - decode job payload: %w", location, err)
	}
	if payload.Task.CheckerTimeout <= 0 {
		return time.Time{}, fmt.Errorf("%s - checker timeout must be positive", location)
	}
	return at.Add(time.Duration(payload.Task.CheckerTimeout)*time.Second + grace), nil
}

func (s *Scheduler) recoverExpired(ctx context.Context, at time.Time) error {
	const location = "scheduler.Scheduler.recoverExpired"
	ids, err := s.repository.ExpiredJobIDs(ctx, at, s.options.RecoveryLimit)
	if err != nil {
		return fmt.Errorf("%s - list expired checker jobs: %w", location, err)
	}
	var recovered int
	for _, id := range ids {
		expired, err := s.repository.Expire(ctx, id, at)
		if err != nil {
			return fmt.Errorf("%s - expire checker job %s: %w", location, id, err)
		}
		if expired {
			recovered++
		}
	}
	if recovered > 0 {
		s.logger.Warn("expired checker jobs without SLA penalty", "count", recovered)
	}
	return nil
}

func newOwnerToken() (string, error) {
	const location = "scheduler.newOwnerToken"
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("%s - generate outbox owner token: %w", location, err)
	}
	return hex.EncodeToString(value[:]), nil
}
