package checker

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

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
	case domain.JobActionClassic, domain.JobActionCheckGets, domain.JobActionCheckOnly:
		check, checkErr := w.runAction(ctx, guard, job, payload, ownerToken, processchecker.ActionCheck, nil)
		if checkErr != nil {
			return domain.CheckerResult{}, fmt.Errorf("%s - run CHECK action: %w", location, checkErr)
		}
		verdicts = append(verdicts, check)
		if check.status == processchecker.StatusUp && workflow != domain.JobActionCheckOnly {
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
	firstPlace, err := randomBelow(int64(payload.Task.Places))
	if err != nil {
		return []verdict{{action: processchecker.ActionPut, status: processchecker.StatusCheckFailed,
			public: "PUT failed", private: "PUT setup failed: " + err.Error()}}, nil
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
				//nolint:gosec // randomBelow checked Places > 0; modulo keeps the value within the positive int32 Places range.
				place := int32((firstPlace+int64(index))%int64(payload.Task.Places) + 1)
				flag, err := generateFlag(payload.Team.ID, payload.Task, job.Round, place)
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
	case domain.JobActionClassic, domain.JobActionPutsOnly, domain.JobActionCheckGets, domain.JobActionCheckOnly:
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
	case domain.JobActionCheckOnly:
		count = 0
	default:
		return false
	}
	return count <= maxWorkflowActions
}
