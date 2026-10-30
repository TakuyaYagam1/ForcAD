package checker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (w *Worker) runAction(ctx context.Context, guard *leaseGuard, job domain.CheckerJob, payload domain.JobPayload, ownerToken string, action processchecker.Action, flag *domain.Flag) (verdict, error) {
	const location = "checker.Worker.runAction"
	if err := w.beginAction(ctx); err != nil {
		return verdict{}, fmt.Errorf("%s - wait for checker action capacity: %w", location, err)
	}
	var pendingReap <-chan struct{}
	defer func() { w.endAction(pendingReap) }()
	//nolint:contextcheck // Lease writes use the guard's bounded recovery context, beyond the action deadline.
	if err := guard.renewNow(); err != nil {
		return verdict{}, fmt.Errorf("%s - renew job ownership before action: %w", location, err)
	}
	if err := guard.checkWorkflowWindow(); err != nil {
		return verdict{}, fmt.Errorf("%s - check workflow deadline: %w", location, err)
	}
	if w.Degraded() {
		return verdict{}, fmt.Errorf("%s - worker degraded during lease maintenance: %w", location, ErrWorkerUnreaped)
	}
	request := checkerRequest(payload, action, flag)
	ctx = processchecker.WithDiagnosticHandler(ctx, func(diagnostic processchecker.ProcessDiagnostic) {
		w.logger.Warn("checker process diagnostic", "job_id", job.ID, "round", job.Round,
			"checker_service", payload.Task.Name, "team", payload.Team.Name, "action", string(action),
			"diagnostic", diagnostic)
	})
	started := time.Now()
	result, err := w.runner.Run(ctx, request)
	pendingReap = result.PendingReap
	finished := time.Now()
	guard.appendCheckerLog(checkerActionLog(job, action, flag, started, finished, result, err))
	if pendingReap != nil {
		return verdict{}, fmt.Errorf("%s - contain unreaped checker: %w", location, ErrWorkerUnreaped)
	}
	if err != nil {
		return checkerActionFailure(ctx, location, action, err)
	}
	return w.acceptCheckerResult(ctx, guard, job, payload, ownerToken, action, flag, result)
}

func checkerRequest(payload domain.JobPayload, action processchecker.Action, flag *domain.Flag) processchecker.Request {
	request := processchecker.Request{
		Executable: payload.Task.Checker, Action: action, TeamIP: payload.Team.IP,
		Timeout: time.Duration(payload.Task.CheckerTimeout) * time.Second,
		EnvPath: payload.Task.EnvPath,
	}
	if flag == nil {
		return request
	}
	request.PrivateFlagData = flag.PrivateData
	request.Flag = flag.Value
	if flag.VulnerabilityID != nil {
		request.VulnNumber = int(*flag.VulnerabilityID)
	}
	return request
}

func checkerActionLog(
	job domain.CheckerJob,
	action processchecker.Action,
	flag *domain.Flag,
	started, finished time.Time,
	result processchecker.Result,
	runErr error,
) domain.CheckerLog {
	// Keep an explicit runner shutdown cause even if its cleanup subsequently
	// encounters a context deadline. Fall back to the error for other runners.
	timedOut := result.TimedOut || (!result.OutputLimited && !result.Canceled &&
		(errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, ErrWorkflowDeadline)))
	status := result.Status
	publicMessage := result.PublicMessage
	if !validStatus(status) {
		status = processchecker.StatusCheckFailed
	}
	if timedOut {
		status = processchecker.StatusDown
		if publicMessage == "" || runErr != nil {
			publicMessage = actionName(action) + " timed out"
			if errors.Is(runErr, ErrWorkflowDeadline) {
				publicMessage = "Checker workflow timed out"
			}
		}
	} else if runErr != nil {
		status = processchecker.StatusCheckFailed
		publicMessage = actionName(action) + " failed"
	}
	if flag != nil {
		publicMessage = redactCheckerSecrets(publicMessage, flag.Value, flag.PrivateData)
	}
	duration := finished.Sub(started)
	if duration < 0 {
		duration = 0
	}
	startedAt, finishedAt := started.UTC(), finished.UTC()
	if finishedAt.Before(startedAt) {
		finishedAt = startedAt
	}
	log := domain.CheckerLog{
		Round: job.Round, Action: string(action), Stage: string(job.Action),
		StartedAt: startedAt, FinishedAt: finishedAt, DurationMS: duration.Milliseconds(),
		TimedOut: timedOut, Status: statusCode(status), PublicMessage: publicMessage,
	}
	return log.PublicView()
}

func checkerActionFailure(ctx context.Context, location string, action processchecker.Action, err error) (verdict, error) {
	if ctx.Err() != nil {
		return verdict{}, fmt.Errorf("%s - run checker action: %w", location, context.Cause(ctx))
	}
	return verdict{
		action: action, status: processchecker.StatusCheckFailed,
		public: actionName(action) + " failed", private: "Checker process error: " + err.Error(),
	}, nil
}

func (w *Worker) acceptCheckerResult(
	ctx context.Context,
	guard *leaseGuard,
	job domain.CheckerJob,
	payload domain.JobPayload,
	ownerToken string,
	action processchecker.Action,
	flag *domain.Flag,
	result processchecker.Result,
) (verdict, error) {
	v := verdict{action: action, status: result.Status, public: result.PublicMessage, private: result.PrivateMessage, command: result.Command}
	if !validStatus(v.status) {
		v.status, v.public = processchecker.StatusCheckFailed, "Check failed"
		v.private = fmt.Sprintf("Checker returned unsupported status %d", result.Status)
	}
	if v.status != processchecker.StatusUp || action != processchecker.ActionPut {
		return v, nil
	}
	return w.persistPutFlag(ctx, guard, job, payload.Task, ownerToken, flag, v)
}

func (w *Worker) persistPutFlag(
	ctx context.Context,
	guard *leaseGuard,
	job domain.CheckerJob,
	task domain.Task,
	ownerToken string,
	flag *domain.Flag,
	v verdict,
) (verdict, error) {
	const location = "checker.Worker.runAction"
	flagData := flagForStorage(*flag, task, v)
	_, err := w.flags.Create(ctx, flagData, &domain.CheckerJobOwner{JobID: job.ID, ClaimToken: ownerToken})
	if errors.Is(err, domain.ErrCheckerJobClosed) {
		guard.abort(ErrLeaseLost)
		return verdict{}, fmt.Errorf("%s - persist successful PUT flag: %w", location, ErrLeaseLost)
	}
	if err != nil {
		return verdict{}, fmt.Errorf("%s - persist successful PUT flag: %w", location, err)
	}
	return v, nil
}

func redactCheckerSecrets(message string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}
