package checker

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (w *Worker) runAction(
	ctx context.Context,
	guard *leaseGuard,
	job domain.CheckerJob,
	payload domain.JobPayload,
	ownerToken string,
	action processchecker.Action,
	flag *domain.Flag,
) (verdict, error) {
	const location = "checker.Worker.runAction"
	if err := w.beginAction(ctx); err != nil {
		return verdict{}, fmt.Errorf("%s - wait for checker action capacity: %w", location, err)
	}
	var pendingReap <-chan struct{}
	defer func() {
		w.endAction(pendingReap)
	}()
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
	log := checkerActionLog(job, action, flag, started, finished, result, err)
	guard.appendCheckerLog(log)
	if pendingReap != nil {
		return verdict{}, fmt.Errorf("%s - contain unreaped checker: %w", location, ErrWorkerUnreaped)
	}
	if err != nil {
		return checkerActionFailure(ctx, location, action, err)
	}
	return w.acceptCheckerResult(ctx, guard, job, payload, ownerToken, action, flag, result, log.PublicMessage)
}

func checkerRequest(payload domain.JobPayload, action processchecker.Action, flag *domain.Flag) processchecker.Request {
	request := processchecker.Request{
		Executable: payload.Task.Checker,
		Action:     action,
		TeamIP:     payload.Team.IP,
		Timeout:    time.Duration(payload.Task.CheckerTimeout) * time.Second,
		EnvPath:    payload.Task.EnvPath,
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
		publicMessage = "Check failed"
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
	if action != processchecker.ActionPut || status != processchecker.StatusUp {
		publicMessage = redactCheckerSecrets(publicMessage, flag)
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
		Round:         job.Round,
		Action:        string(action),
		Stage:         string(job.Action),
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		DurationMS:    duration.Milliseconds(),
		TimedOut:      timedOut,
		Status:        statusCode(status),
		PublicMessage: publicMessage,
	}
	return log.PublicView()
}

func checkerActionFailure(
	ctx context.Context,
	location string,
	action processchecker.Action,
	err error,
) (verdict, error) {
	if ctx.Err() != nil {
		return verdict{}, fmt.Errorf("%s - run checker action: %w", location, context.Cause(ctx))
	}
	return verdict{
		action:  action,
		status:  processchecker.StatusCheckFailed,
		public:  actionName(action) + " failed",
		private: "Checker process error: " + err.Error(),
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
	publicMessage string,
) (verdict, error) {
	const location = "checker.Worker.acceptCheckerResult"
	v := verdict{
		action:  action,
		status:  result.Status,
		public:  publicMessage,
		private: result.PrivateMessage,
		command: result.Command,
	}
	if !validStatus(v.status) {
		v.status = processchecker.StatusCheckFailed
		v.private = fmt.Sprintf("Checker returned unsupported status %d", result.Status)
	}
	if v.status != processchecker.StatusUp || action != processchecker.ActionPut {
		return v, nil
	}
	// Successful PUT stdout belongs to the checker storage protocol, never
	// to the scoreboard. Only flagForStorage receives the unredacted output.
	stored := flagForStorage(*flag, payload.Task, result.PublicMessage, result.PrivateMessage)
	if err := w.persistPutFlag(ctx, guard, job, ownerToken, stored); err != nil {
		return verdict{}, fmt.Errorf("%s - persist checker flag: %w", location, err)
	}
	v.public = "OK"
	return v, nil
}

func (w *Worker) persistPutFlag(
	ctx context.Context,
	guard *leaseGuard,
	job domain.CheckerJob,
	ownerToken string,
	flag domain.Flag,
) error {
	const location = "checker.Worker.persistPutFlag"
	_, err := w.flags.Create(ctx, flag, &domain.CheckerJobOwner{
		JobID:      job.ID,
		ClaimToken: ownerToken,
	})
	if errors.Is(err, domain.ErrCheckerJobClosed) {
		guard.abort(ErrLeaseLost)
		return fmt.Errorf("%s - persist successful PUT flag: %w", location, ErrLeaseLost)
	}
	if err != nil {
		return fmt.Errorf("%s - persist successful PUT flag: %w", location, err)
	}
	return nil
}

var checkerFlagPattern = regexp.MustCompile(`[A-Z0-9]{31}=`)

func redactCheckerSecrets(message string, flag *domain.Flag) string {
	if flag != nil {
		longer, shorter := flag.PrivateData, flag.Value
		if len(longer) < len(shorter) {
			longer, shorter = shorter, longer
		}
		for _, secret := range [...]string{longer, shorter} {
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
	}
	if strings.Contains(message, "=") && checkerFlagPattern.MatchString(message) {
		return checkerFlagPattern.ReplaceAllString(message, "[redacted]")
	}
	return message
}
