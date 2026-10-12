package checker

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

const (
	defaultLeaseDuration  = 30 * time.Second
	defaultRenewInterval  = 10 * time.Second
	defaultRecoveryGrace  = 30 * time.Second
	defaultWorkflowLimit  = 30 * time.Minute
	defaultActionLimit    = 16
	maxWorkflowActions    = 1024
	maxCheckerRound       = int64(1<<31 - 1)
	stateCheckTimeout     = 3 * time.Second
	checkFailedStatus     = int32(processchecker.StatusCheckFailed)
	workflowTimeoutStatus = checkFailedStatus
	flagAlphabet          = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var (
	ErrLeaseLost               = errors.New("checker job lease lost")
	ErrNoActions               = errors.New("checker workflow has no actions")
	ErrWorkflowDeadline        = errors.New("checker workflow deadline exceeded")
	ErrWorkflowRecoveryExpired = errors.New("checker workflow recovery grace expired")
)

// Runner executes one external checker protocol action.
type Runner interface {
	Run(ctx context.Context, request processchecker.Request) (processchecker.Result, error)
}

// Repository is the durable job and team surface used by a checker worker.
type Repository interface {
	outbound.JobRepository
	IsActiveTeam(ctx context.Context, teamID uuid.UUID) (bool, error)
	SetDeadline(ctx context.Context, jobID uuid.UUID, ownerToken string, now, deadline time.Time) (bool, error)
}

// FlagStore persists PUT output and loads a flag from a selected round.
type FlagStore interface {
	Create(ctx context.Context, flag domain.Flag, owner *domain.CheckerJobOwner) (domain.Flag, error)
	FindRandomRound(ctx context.Context, teamID, taskID uuid.UUID, round int32) (domain.Flag, error)
}

// LifecycleReader supplies the current flag lifetime for GET selection.
type LifecycleReader interface {
	Lifecycle(ctx context.Context) (domain.Lifecycle, error)
}

// Options controls durable claim renewal, bounded recovery and workflow runtime.
type Options struct {
	LeaseDuration       time.Duration
	RenewInterval       time.Duration
	RecoveryGrace       time.Duration
	MaxWorkflowDuration time.Duration
	WorkConcurrency     int
}

// Worker executes one complete durable checker workflow per queue delivery.
type Worker struct {
	repo      Repository
	flags     FlagStore
	lifecycle LifecycleReader
	runner    Runner
	logger    *slog.Logger
	options   Options
	actions   chan struct{}
}

// New constructs a worker. The action semaphore belongs to this instance and
// therefore limits concurrent checker processes across all handled jobs.
func New(repo Repository, flags FlagStore, lifecycle LifecycleReader, runner Runner, options Options, logger *slog.Logger) *Worker {
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaultLeaseDuration
	}
	if options.RecoveryGrace <= 0 {
		options.RecoveryGrace = defaultRecoveryGrace
	}
	if options.MaxWorkflowDuration <= 0 {
		options.MaxWorkflowDuration = defaultWorkflowLimit
	}
	if options.RenewInterval <= 0 {
		options.RenewInterval = defaultRenewInterval
	}
	if options.RenewInterval >= options.LeaseDuration {
		options.RenewInterval = options.LeaseDuration / 3
	}
	if options.RenewInterval <= 0 {
		options.RenewInterval = time.Second
	}
	if options.WorkConcurrency <= 0 {
		options.WorkConcurrency = defaultActionLimit
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		repo: repo, flags: flags, lifecycle: lifecycle, runner: runner, logger: logger,
		options: options, actions: make(chan struct{}, options.WorkConcurrency),
	}
}

// Handle claims and runs the whole workflow for a stable queue job ID. It
// returns nil only after the result is durable or the job is known to be closed.
func (w *Worker) Handle(ctx context.Context, jobID uuid.UUID) error {
	const location = "checker.Worker.Handle"
	if ctx == nil {
		return fmt.Errorf("%s - checker worker context must not be nil", location)
	}
	if jobID == uuid.Nil() {
		return fmt.Errorf("%s - checker job ID must not be nil", location)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - accept checker job: %w", location, err)
	}

	claim, claimed, err := w.claimJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("%s - claim checker job: %w", location, err)
	}
	if !claimed {
		return nil
	}

	workCtx, cancel := context.WithCancelCause(ctx)
	workflowDeadline := claim.claimedAt.Add(w.options.MaxWorkflowDuration)
	guard := &leaseGuard{
		worker: w, jobID: jobID, ownerToken: claim.ownerToken, ctx: workCtx,
		cancelFn: cancel, done: make(chan struct{}), round: claim.job.Round, workflowDeadline: workflowDeadline,
		recoveryDeadline: workflowDeadline.Add(w.options.RecoveryGrace),
	}
	go guard.run()
	workflowCtx, cancelWorkflow := context.WithDeadlineCause(workCtx, workflowDeadline, ErrWorkflowDeadline)
	result, executeErr := w.execute(workflowCtx, guard, claim.job, claim.ownerToken)
	cancelWorkflow()
	if err := w.finish(ctx, guard, result, executeErr); err != nil {
		return fmt.Errorf("%s - finish checker job: %w", location, err)
	}
	return nil
}

type jobClaim struct {
	job        domain.CheckerJob
	ownerToken string
	claimedAt  time.Time
}

func (w *Worker) claimJob(ctx context.Context, jobID uuid.UUID) (jobClaim, bool, error) {
	const location = "checker.Worker.claimJob"
	ownerToken, err := newOwnerToken()
	if err != nil {
		return jobClaim{}, false, fmt.Errorf("%s - create checker claim token: %w", location, err)
	}
	now := time.Now()
	job, err := w.repo.Claim(ctx, jobID, ownerToken, now, now.Add(w.options.LeaseDuration))
	if err != nil {
		if settleErr := w.settleClaimFailure(ctx, jobID, err); settleErr != nil {
			return jobClaim{}, false, fmt.Errorf("%s - claim checker job: %w", location, settleErr)
		}
		return jobClaim{}, false, nil
	}
	if job.ID != jobID || job.ClaimToken == nil || *job.ClaimToken != ownerToken || job.FinishedAt != nil {
		return jobClaim{}, false, fmt.Errorf("%s - verify checker job claim: %w", location, ErrLeaseLost)
	}
	return jobClaim{job: job, ownerToken: ownerToken, claimedAt: now}, true, nil
}

func (w *Worker) finish(ctx context.Context, guard *leaseGuard, result domain.CheckerResult, executeErr error) error {
	const location = "checker.Worker.finish"
	if leaseErr := guard.failure(); leaseErr != nil {
		guard.cancelFn(leaseErr)
		<-guard.done
		return fmt.Errorf("%s - maintain checker job lease: %w", location, leaseErr)
	}
	if err := ctx.Err(); err != nil {
		guard.cancelFn(err)
		<-guard.done
		return fmt.Errorf("%s - handle checker job cancellation: %w", location, err)
	}
	workflowTimedOut := errors.Is(executeErr, ErrWorkflowDeadline) || !time.Now().Before(guard.workflowDeadline)
	if executeErr != nil && !workflowTimedOut {
		guard.cancelFn(executeErr)
		<-guard.done
		return fmt.Errorf("%s - execute checker workflow: %w", location, executeErr)
	}
	if workflowTimedOut {
		result = domain.CheckerResult{
			Round: guard.round, Status: workflowTimeoutStatus,
			PublicMessage:  "Checker workflow timed out",
			PrivateMessage: "Checker workflow exceeded CHECKER_WORKFLOW_TIMEOUT",
		}
	}

	var completed bool
	var completeErr error
	if workflowTimedOut {
		completeCtx, cancelComplete := guard.recoveryContext(w.options.RecoveryGrace)
		//nolint:contextcheck // guard.ctx is the request context's lease-aware child.
		completed, completeErr = w.repo.Complete(completeCtx, guard.jobID, guard.ownerToken, time.Now(), result)
		cancelComplete()
	} else {
		completed, completeErr = w.repo.Complete(ctx, guard.jobID, guard.ownerToken, time.Now(), result)
	}
	guard.cancelFn(context.Canceled)
	<-guard.done
	if completed {
		w.logger.Info("checker workflow completed", "job_id", guard.jobID, "round", result.Round, "status", result.Status)
		return nil
	}
	closed, stateErr := w.confirmClosed(ctx, guard.jobID)
	if closed {
		return nil
	}
	if completeErr != nil || stateErr != nil {
		return fmt.Errorf("%s - settle checker job completion: %w", location, errors.Join(completeErr, stateErr, ErrLeaseLost))
	}
	return fmt.Errorf("%s - verify checker job completion: %w", location, ErrLeaseLost)
}

func (w *Worker) execute(ctx context.Context, guard *leaseGuard, job domain.CheckerJob, ownerToken string) (domain.CheckerResult, error) {
	const location = "checker.Worker.execute"
	plan := planWorkflow(job)
	if !plan.isValid {
		return plan.failure, nil
	}
	payload := plan.payload
	workflow := plan.workflow

	teamActive, err := w.repo.IsActiveTeam(ctx, payload.Team.ID)
	if err != nil {
		if ctx.Err() != nil {
			return domain.CheckerResult{}, fmt.Errorf("%s - validate scheduled team: %w", location, context.Cause(ctx))
		}
		return domain.CheckerResult{}, fmt.Errorf("%s - validate scheduled team: %w", location, err)
	}
	if !teamActive {
		return failedResult(job.Round, "Scheduled team is no longer active"), nil
	}

	guard.setActionTimeout(time.Duration(payload.Task.CheckerTimeout) * time.Second)

	var verdicts []verdict
	switch workflow {
	case domain.JobActionPutsOnly:
		verdicts, err = w.runPuts(ctx, guard, job, payload, ownerToken)
	case domain.JobActionClassic, domain.JobActionCheckGets:
		check, checkErr := w.runAction(ctx, guard, job, payload, ownerToken, processchecker.ActionCheck, nil)
		if checkErr != nil {
			return domain.CheckerResult{}, fmt.Errorf("%s - run CHECK action: %w", location, checkErr)
		}
		verdicts = append(verdicts, check)
		if check.status == processchecker.StatusUp {
			var branches []verdict
			branches, err = w.runAfterCheck(ctx, guard, job, payload, ownerToken, workflow)
			verdicts = append(verdicts, branches...)
		}
	default:
		return failedResult(job.Round, domain.ErrInvalidJobWorkflow.Error()), nil
	}
	if err != nil {
		return domain.CheckerResult{}, fmt.Errorf("%s - run checker workflow actions: %w", location, err)
	}
	selected := selectVerdict(verdicts)
	if selected == nil {
		return failedResult(job.Round, ErrNoActions.Error()), nil
	}
	public := selected.public
	if workflow == domain.JobActionPutsOnly && selected.action == processchecker.ActionPut && selected.status == processchecker.StatusUp {
		public = "OK"
	}
	return domain.CheckerResult{
		Round: job.Round, Status: statusCode(selected.status), PublicMessage: public,
		PrivateMessage: selected.private, Command: selected.command,
	}, nil
}

type workflowPlan struct {
	payload  domain.JobPayload
	workflow domain.JobAction
	failure  domain.CheckerResult
	isValid  bool
}

func planWorkflow(job domain.CheckerJob) workflowPlan {
	payload, err := decodePayload(job.Payload)
	if err != nil {
		return workflowPlan{failure: failedResult(job.Round, "Invalid checker job payload")}
	}
	if payload.Team.ID == uuid.Nil() || payload.Task.ID == uuid.Nil() || payload.Team.IP == "" || payload.Task.Checker == "" ||
		payload.Task.CheckerTimeout <= 0 {
		return workflowPlan{failure: failedResult(job.Round, "Checker job payload is missing required team or task data")}
	}
	if (job.TeamID != uuid.Nil() && job.TeamID != payload.Team.ID) || (job.TaskID != uuid.Nil() && job.TaskID != payload.Task.ID) {
		return workflowPlan{failure: failedResult(job.Round, "Checker job identity does not match its payload")}
	}
	workflow, err := workflowFor(job.Action, payload.Workflow)
	if err != nil {
		return workflowPlan{failure: failedResult(job.Round, domain.ErrInvalidJobWorkflow.Error())}
	}
	if !validWorkflowActionCount(workflow, payload.Task) {
		return workflowPlan{failure: failedResult(job.Round, "Invalid checker workflow action count")}
	}
	return workflowPlan{payload: payload, workflow: workflow, isValid: true}
}
func (w *Worker) runAfterCheck(ctx context.Context, guard *leaseGuard, job domain.CheckerJob, payload domain.JobPayload, ownerToken string, workflow domain.JobAction) ([]verdict, error) {
	const location = "checker.Worker.runAfterCheck"
	branchCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)
	type branchResult struct {
		verdicts []verdict
		err      error
	}
	count := 0
	results := make(chan branchResult, 2)
	if workflow == domain.JobActionClassic && payload.Task.Puts > 0 {
		count++
		go func() {
			v, err := w.runPuts(branchCtx, guard, job, payload, ownerToken)
			results <- branchResult{verdicts: v, err: err}
		}()
	}
	if payload.Task.Gets > 0 {
		count++
		go func() {
			v, err := w.runGets(branchCtx, guard, job, payload, ownerToken)
			results <- branchResult{verdicts: v, err: err}
		}()
	}
	if count == 0 {
		return nil, nil
	}
	var all []verdict
	var firstErr error
	for range count {
		result := <-results
		if result.err != nil && firstErr == nil {
			firstErr = result.err
			cancel(result.err)
		}
		all = append(all, result.verdicts...)
	}
	if firstErr != nil {
		return nil, fmt.Errorf("%s - run PUT/GET branches: %w", location, firstErr)
	}
	if err := context.Cause(ctx); err != nil {
		return nil, fmt.Errorf("%s - finish PUT/GET branches: %w", location, err)
	}
	// Branch arrival order is nondeterministic; use PUT-first
	// accounting precedence and each PUT's declared index order.
	return orderBranches(all), nil
}

func (w *Worker) runPuts(ctx context.Context, guard *leaseGuard, job domain.CheckerJob, payload domain.JobPayload, ownerToken string) ([]verdict, error) {
	const location = "checker.Worker.runPuts"
	count := int(payload.Task.Puts)
	if count <= 0 {
		return nil, nil
	}
	putCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)
	results := make([]verdict, count)
	errs := make([]error, count)
	workerCount := min(count, cap(w.actions))
	var nextIndex int
	var nextMu sync.Mutex
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-putCtx.Done():
					return
				default:
				}
				nextMu.Lock()
				index := nextIndex
				nextIndex++
				nextMu.Unlock()
				if index >= count {
					return
				}
				flag, err := generateFlag(payload.Team.ID, payload.Task, job.Round)
				if err != nil {
					results[index] = verdict{action: processchecker.ActionPut, status: processchecker.StatusCheckFailed, public: "PUT failed", private: "PUT setup failed: " + err.Error()}
					continue
				}
				v, err := w.runAction(putCtx, guard, job, payload, ownerToken, processchecker.ActionPut, &flag)
				if err != nil {
					errs[index] = err
					cancel(err)
					return
				}
				results[index] = v
			}
		}()
	}
	workers.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("%s - execute PUT actions: %w", location, err)
		}
	}
	return results, nil
}

func (w *Worker) runGets(ctx context.Context, guard *leaseGuard, job domain.CheckerJob, payload domain.JobPayload, ownerToken string) ([]verdict, error) {
	const location = "checker.Worker.runGets"
	lifecycle, err := w.lifecycle.Lifecycle(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s - read lifecycle for GET: %w", location, context.Cause(ctx))
		}
		return []verdict{{action: processchecker.ActionGet, status: processchecker.StatusCheckFailed, public: "GET failed", private: "Could not read flag lifetime"}}, nil
	}
	currentRound, lifetime := job.Round, lifecycle.Config.FlagLifetime
	if currentRound < 1 || lifetime < 1 {
		return []verdict{{action: processchecker.ActionGet, status: processchecker.StatusCheckFailed, public: "GET failed", private: "GET requires a positive current round and flag lifetime"}}, nil
	}
	firstRound := max(int32(1), currentRound-lifetime+1)
	width := int64(currentRound-firstRound) + 1
	verdicts := make([]verdict, 0, payload.Task.Gets)
	previous := verdict{action: processchecker.ActionCheck, status: processchecker.StatusUp}
	for range payload.Task.Gets {
		if previous.status != processchecker.StatusUp {
			break
		}
		flagRound, ok := randomFlagRound(firstRound, width)
		if !ok {
			return append(verdicts, verdict{action: processchecker.ActionGet, status: processchecker.StatusCheckFailed, public: "GET failed", private: "Could not choose a GET flag round"}), nil
		}
		flag, err := w.flags.FindRandomRound(ctx, payload.Team.ID, payload.Task.ID, flagRound)
		if errors.Is(err, domain.ErrFlagNotFound) {
			previous = verdict{
				action: processchecker.ActionGet, status: processchecker.StatusUp,
				private: fmt.Sprintf("No flag from round %d", flagRound),
			}
			verdicts = append(verdicts, previous)
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s - load GET flag: %w", location, context.Cause(ctx))
			}
			return append(verdicts, verdict{action: processchecker.ActionGet, status: processchecker.StatusCheckFailed, public: "GET failed", private: "Could not load flag for GET"}), nil
		}
		previous, err = w.runAction(ctx, guard, job, payload, ownerToken, processchecker.ActionGet, &flag)
		if err != nil {
			return nil, fmt.Errorf("%s - run GET action: %w", location, err)
		}
		verdicts = append(verdicts, previous)
	}
	return verdicts, nil
}

func (w *Worker) runAction(ctx context.Context, guard *leaseGuard, job domain.CheckerJob, payload domain.JobPayload, ownerToken string, action processchecker.Action, flag *domain.Flag) (verdict, error) {
	const location = "checker.Worker.runAction"
	select {
	case w.actions <- struct{}{}:
	case <-ctx.Done():
		return verdict{}, fmt.Errorf("%s - wait for checker action capacity: %w", location, context.Cause(ctx))
	}
	defer func() { <-w.actions }()
	if err := guard.renewNow(); err != nil {
		return verdict{}, fmt.Errorf("%s - renew job ownership before action: %w", location, err)
	}
	if err := guard.checkWorkflowWindow(); err != nil {
		return verdict{}, fmt.Errorf("%s - check workflow deadline: %w", location, err)
	}
	request := processchecker.Request{
		Executable: payload.Task.Checker, Action: action, TeamIP: payload.Team.IP,
		Timeout: time.Duration(payload.Task.CheckerTimeout) * time.Second,
		EnvPath: payload.Task.EnvPath,
	}
	if flag != nil {
		request.PrivateFlagData = flag.PrivateData
		request.Flag = flag.Value
		if flag.VulnerabilityID != nil {
			request.VulnNumber = int(*flag.VulnerabilityID)
		}
	}
	result, err := w.runner.Run(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			return verdict{}, fmt.Errorf("%s - run checker action: %w", location, context.Cause(ctx))
		}
		return verdict{
			action: action, status: processchecker.StatusCheckFailed,
			public: actionName(action) + " failed", private: "Checker process error: " + err.Error(),
		}, nil
	}
	v := verdict{action: action, status: result.Status, public: result.PublicMessage, private: result.PrivateMessage, command: result.Command}
	if !validStatus(v.status) {
		v.status, v.public = processchecker.StatusCheckFailed, "Check failed"
		v.private = fmt.Sprintf("Checker returned unsupported status %d", result.Status)
	}
	if v.status == processchecker.StatusUp && action == processchecker.ActionPut {
		flagData := flagForStorage(*flag, payload.Task, v)
		_, err := w.flags.Create(ctx, flagData, &domain.CheckerJobOwner{JobID: job.ID, ClaimToken: ownerToken})
		if errors.Is(err, domain.ErrCheckerJobClosed) {
			guard.abort(ErrLeaseLost)
			return verdict{}, fmt.Errorf("%s - persist successful PUT flag: %w", location, ErrLeaseLost)
		}
		if err != nil {
			return verdict{}, fmt.Errorf("%s - persist successful PUT flag: %w", location, err)
		}
	}
	return v, nil
}

func (w *Worker) settleClaimFailure(ctx context.Context, jobID uuid.UUID, claimErr error) error {
	const location = "checker.Worker.settleClaimFailure"
	if errors.Is(claimErr, domain.ErrCheckerJobClosed) {
		return nil
	}
	if !errors.Is(claimErr, domain.ErrJobClaimed) && !errors.Is(claimErr, domain.ErrJobNotReady) {
		return fmt.Errorf("%s - claim checker job: %w", location, claimErr)
	}
	if closed, stateErr := w.confirmClosed(ctx, jobID); closed {
		return nil
	} else if stateErr != nil {
		return fmt.Errorf("%s - confirm checker job state: %w", location, errors.Join(claimErr, stateErr))
	}
	return fmt.Errorf("%s - checker job is not claimable: %w", location, claimErr)
}

func (w *Worker) confirmClosed(ctx context.Context, jobID uuid.UUID) (bool, error) {
	const location = "checker.Worker.confirmClosed"
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stateCheckTimeout)
	defer cancel()
	state, err := w.repo.State(checkCtx, jobID)
	if errors.Is(err, domain.ErrCheckerJobClosed) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s - read checker job state: %w", location, err)
	}
	if state.FinishedAt != nil {
		return true, nil
	}
	if state.DeadlineAt != nil && !state.DeadlineAt.After(time.Now()) {
		expired, expireErr := w.repo.Expire(checkCtx, jobID, time.Now())
		if expireErr != nil {
			return false, fmt.Errorf("%s - expire checker job: %w", location, expireErr)
		}
		if expired {
			return true, nil
		}
		state, err = w.repo.State(checkCtx, jobID)
		if errors.Is(err, domain.ErrCheckerJobClosed) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("%s - reread checker job state: %w", location, err)
		}
		return state.FinishedAt != nil, nil
	}
	return false, nil
}

func decodePayload(data []byte) (domain.JobPayload, error) {
	const location = "checker.decodePayload"
	var payload domain.JobPayload
	if len(data) == 0 {
		return payload, fmt.Errorf("%s - payload is empty", location)
	}
	if err := jsonv2.Unmarshal(data, &payload); err != nil {
		return payload, fmt.Errorf("%s - decode job payload: %w", location, err)
	}
	return payload, nil
}

func workflowFor(action domain.JobAction, payloadWorkflow string) (domain.JobAction, error) {
	const location = "checker.workflowFor"
	if action == "" {
		return "", fmt.Errorf("%s - job action is required: %w", location, domain.ErrInvalidJobWorkflow)
	}
	if payloadWorkflow != "" && domain.JobAction(payloadWorkflow) != action {
		return "", fmt.Errorf("%s - payload workflow does not match job action: %w", location, domain.ErrInvalidJobWorkflow)
	}
	switch action {
	case domain.JobActionClassic, domain.JobActionPutsOnly, domain.JobActionCheckGets:
		return action, nil
	default:
		return "", fmt.Errorf("%s - unsupported job action: %w", location, domain.ErrInvalidJobWorkflow)
	}
}

func validWorkflowActionCount(workflow domain.JobAction, task domain.Task) bool {
	if task.Puts < 0 || task.Gets < 0 {
		return false
	}
	var count int64
	switch workflow {
	case domain.JobActionClassic:
		count = int64(task.Puts) + int64(task.Gets)
	case domain.JobActionPutsOnly:
		count = int64(task.Puts)
	case domain.JobActionCheckGets:
		count = int64(task.Gets)
	default:
		return false
	}
	return count <= maxWorkflowActions
}

func generateFlag(teamID uuid.UUID, task domain.Task, round int32) (domain.Flag, error) {
	const location = "checker.generateFlag"
	if task.Name == "" || task.Places <= 0 {
		return domain.Flag{}, fmt.Errorf("%s - task name and positive place count are required for PUT", location)
	}
	r, _ := utf8.DecodeRuneInString(task.Name)
	prefix := unicode.ToUpper(r)
	isLetter := prefix >= 'A' && prefix <= 'Z'
	isDigit := prefix >= '0' && prefix <= '9'
	if !isLetter && !isDigit {
		return domain.Flag{}, fmt.Errorf("%s - task name must start with an ASCII letter or digit", location)
	}
	var randomFlag strings.Builder
	randomFlag.Grow(32)
	randomFlag.WriteRune(prefix)
	for range 30 {
		index, err := randomBelow(int64(len(flagAlphabet)))
		if err != nil {
			return domain.Flag{}, fmt.Errorf("%s - generate random flag: %w", location, err)
		}
		randomFlag.WriteByte(flagAlphabet[int(index)])
	}
	randomFlag.WriteByte('=')
	seed := make([]byte, 20)
	if _, err := io.ReadFull(cryptorand.Reader, seed); err != nil {
		return domain.Flag{}, fmt.Errorf("%s - generate private flag seed: %w", location, err)
	}
	place, err := randomBelow(int64(task.Places))
	if err != nil {
		return domain.Flag{}, fmt.Errorf("%s - choose vulnerability place: %w", location, err)
	}
	// place is less than the positive int32 task.Places bound.
	//nolint:gosec // The selected one-based place cannot exceed task.Places.
	vulnerability := int32(place + 1)
	return domain.Flag{
		Value: randomFlag.String(), TeamID: teamID, TaskID: task.ID, Round: round,
		PrivateData: hex.EncodeToString(seed), VulnerabilityID: &vulnerability,
	}, nil
}

func flagForStorage(flag domain.Flag, task domain.Task, v verdict) domain.Flag {
	switch {
	case hasTag(task.CheckerType, "nfr"):
		flag.PublicData = ""
	case hasTag(task.CheckerType, "pfr"):
		flag.PublicData = v.public
		flag.PrivateData = v.private
	default:
		flag.PublicData = ""
		flag.PrivateData = v.public
	}
	return flag
}

func hasTag(tags, tag string) bool {
	for _, item := range strings.Split(tags, "_") {
		if item == tag {
			return true
		}
	}
	return false
}

func randomBelow(limit int64) (int64, error) {
	const location = "checker.randomBelow"
	if limit <= 0 {
		return 0, fmt.Errorf("%s - random limit must be positive", location)
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(limit))
	if err != nil {
		return 0, fmt.Errorf("%s - generate random integer: %w", location, err)
	}
	return n.Int64(), nil
}

func randomFlagRound(firstRound int32, width int64) (int32, bool) {
	choice, err := randomBelow(width)
	if err != nil {
		return 0, false
	}
	round := int64(firstRound) + choice
	if round < 1 || round > maxCheckerRound {
		return 0, false
	}
	return int32(round), true
}

func newOwnerToken() (string, error) {
	const location = "checker.newOwnerToken"
	var token [32]byte
	if _, err := cryptorand.Read(token[:]); err != nil {
		return "", fmt.Errorf("%s - generate claim token: %w", location, err)
	}
	return hex.EncodeToString(token[:]), nil
}

func validStatus(status processchecker.Status) bool {
	switch status {
	case processchecker.StatusUp, processchecker.StatusCorrupt, processchecker.StatusMumble,
		processchecker.StatusDown, processchecker.StatusCheckFailed:
		return true
	default:
		return false
	}
}

func statusCode(status processchecker.Status) int32 {
	switch status {
	case processchecker.StatusUp:
		return int32(processchecker.StatusUp)
	case processchecker.StatusCorrupt:
		return int32(processchecker.StatusCorrupt)
	case processchecker.StatusMumble:
		return int32(processchecker.StatusMumble)
	case processchecker.StatusDown:
		return int32(processchecker.StatusDown)
	case processchecker.StatusCheckFailed:
		return int32(processchecker.StatusCheckFailed)
	default:
		return checkFailedStatus
	}
}

func actionName(action processchecker.Action) string { return strings.ToUpper(string(action)) }

func failedResult(round int32, private string) domain.CheckerResult {
	return domain.CheckerResult{Round: round, Status: int32(processchecker.StatusCheckFailed), PublicMessage: "Checker failed", PrivateMessage: private}
}

type verdict struct {
	action  processchecker.Action
	status  processchecker.Status
	public  string
	private string
	command string
}

func selectVerdict(verdicts []verdict) *verdict {
	if len(verdicts) == 0 {
		return nil
	}
	for index := range verdicts {
		if verdicts[index].status != processchecker.StatusUp {
			return &verdicts[index]
		}
	}
	return &verdicts[0]
}

func orderBranches(branches []verdict) []verdict {
	puts := make([]verdict, 0, len(branches))
	gets := make([]verdict, 0, len(branches))
	for _, item := range branches {
		switch item.action {
		case processchecker.ActionPut:
			puts = append(puts, item)
		case processchecker.ActionGet:
			gets = append(gets, item)
		case processchecker.ActionCheck:
			// CHECK verdicts are handled before branch ordering.
		}
	}
	return append(puts, gets...)
}

type leaseGuard struct {
	worker           *Worker
	jobID            uuid.UUID
	ownerToken       string
	round            int32
	ctx              context.Context
	cancelFn         context.CancelCauseFunc
	done             chan struct{}
	mu               sync.Mutex
	err              error
	actionTimeout    time.Duration
	workflowDeadline time.Time
	recoveryDeadline time.Time
}

func (g *leaseGuard) run() {
	defer close(g.done)
	ticker := time.NewTicker(g.worker.options.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			if err := g.renewNow(); err != nil {
				return
			}
		}
	}
}

func (g *leaseGuard) renewNow() error {
	const location = "checker.leaseGuard.renewNow"
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.ctx.Err(); err != nil {
		return fmt.Errorf("%s - renew checker job lease: %w", location, context.Cause(g.ctx))
	}
	now := time.Now()
	if !now.Before(g.recoveryDeadline) {
		g.err = fmt.Errorf("%s - workflow recovery grace expired: %w", location, ErrWorkflowRecoveryExpired)
		g.cancelFn(g.err)
		return g.err
	}
	deadline := now.Add(g.actionTimeout + g.worker.options.RecoveryGrace)
	if deadline.After(g.recoveryDeadline) {
		deadline = g.recoveryDeadline
	}
	deadlineSet, err := g.worker.repo.SetDeadline(g.ctx, g.jobID, g.ownerToken, now, deadline)
	if err != nil {
		if g.ctx.Err() != nil {
			return fmt.Errorf("%s - set checker job deadline: %w", location, context.Cause(g.ctx))
		}
		g.err = fmt.Errorf("%s - set checker job deadline: %w", location, err)
		g.cancelFn(g.err)
		return g.err
	}
	if !deadlineSet {
		g.err = fmt.Errorf("%s - set checker job deadline: %w", location, ErrLeaseLost)
		g.cancelFn(g.err)
		return g.err
	}
	leaseDeadline := now.Add(g.worker.options.LeaseDuration)
	if leaseDeadline.After(g.recoveryDeadline) {
		leaseDeadline = g.recoveryDeadline
	}
	owned, err := g.worker.repo.Renew(g.ctx, g.jobID, g.ownerToken, now, leaseDeadline)
	if err != nil {
		if g.ctx.Err() != nil {
			return fmt.Errorf("%s - renew checker job lease: %w", location, context.Cause(g.ctx))
		}
		g.err = fmt.Errorf("%s - renew checker job lease: %w", location, err)
		g.cancelFn(g.err)
		return g.err
	}
	if !owned {
		g.err = fmt.Errorf("%s - renew checker job lease: %w", location, ErrLeaseLost)
		g.cancelFn(g.err)
		return g.err
	}
	return nil
}

func (g *leaseGuard) checkWorkflowWindow() error {
	const location = "checker.leaseGuard.checkWorkflowWindow"
	g.mu.Lock()
	defer g.mu.Unlock()
	if !time.Now().Before(g.workflowDeadline) {
		return fmt.Errorf("%s - workflow deadline exceeded: %w", location, ErrWorkflowDeadline)
	}
	if !time.Now().Before(g.recoveryDeadline) {
		return fmt.Errorf("%s - workflow recovery grace expired: %w", location, ErrWorkflowRecoveryExpired)
	}
	return nil
}

func (g *leaseGuard) abort(err error) {
	g.mu.Lock()
	if g.err == nil {
		g.err = err
	}
	g.mu.Unlock()
	g.cancelFn(err)
}

func (g *leaseGuard) setActionTimeout(timeout time.Duration) {
	g.mu.Lock()
	g.actionTimeout = timeout
	g.mu.Unlock()
}

func (g *leaseGuard) recoveryContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(g.ctx, timeout)
}

func (g *leaseGuard) failure() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err
}
