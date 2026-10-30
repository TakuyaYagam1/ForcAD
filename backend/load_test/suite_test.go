package load_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	if os.Getenv("FORCAD_LOAD_ROOT") == "" {
		t.Skip("use make test-load for the isolated production deployment")
	}
	if raw := os.Getenv("LOAD_TEAM_COUNT"); raw != "" {
		count, parseErr := strconv.Atoi(raw)
		if parseErr != nil || count < 2 || count > 28 {
			t.Fatal("LOAD_TEAM_COUNT must be an integer between 2 and 28")
		}
		teamCount = count
	}
	if raceEnabled {
		t.Fatal("load.TestLoad - performance measurements require a build without -race")
	}
	profile, stages, err := loadProfile()
	if err != nil {
		t.Fatal(err)
	}
	report, directory, err := newReport(profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("report: %s/report.json", directory)
	d, err := openDeployment()
	if err == nil {
		err = runSuite(t.Context(), d, stages, report, directory, t.Logf)
	}
	report.Finished = time.Now().UTC()
	report.CorrectnessPassed = err == nil
	if err != nil {
		report.Error = err.Error()
	}
	if saveErr := report.save(directory); saveErr != nil {
		t.Error(saveErr)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func runSuite(ctx context.Context, d *deployment, stages []stage, report *suiteReport, directory string, logf func(string, ...any)) (runErr error) {
	metadataCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	metadata, err := exec.CommandContext(metadataCtx, "docker", "info", "--format", `{"cpus":{{.NCPU}},"memory_bytes":{{.MemTotal}},"storage_driver":{{json .Driver}},"logging_driver":{{json .LoggingDriver}},"security_options":{{json .SecurityOptions}}}`).Output()
	cancel()
	if err != nil {
		return fmt.Errorf("load.runSuite - inspect Docker host: %w", err)
	}
	report.DockerHost = metadata
	if err := d.start(ctx); err != nil {
		return err
	}
	flags := &catalog{}
	if err := flags.wait(ctx, d); err != nil {
		return err
	}
	before, err := d.number(ctx, `SELECT count(*) FROM TeamTasksLog`)
	if err != nil {
		return err
	}
	watch := observe(ctx, d, flags, directory)
	defer func() {
		var observerErr error
		report.WebSockets, observerErr = watch.stop()
		runErr = errors.Join(runErr, observerErr)
		if report.Profile == "checker" {
			checker, auditErr := collectCheckerReport(ctx, d)
			if auditErr == nil {
				report.Checker = checker
				auditErr = checker.validate()
			}
			runErr = errors.Join(runErr, auditErr)
		}
		if os.Getenv("LOAD_DIAGNOSTICS") == "1" {
			runErr = errors.Join(runErr, saveFlagLog(ctx, d, directory, time.Since(report.Started)))
		}
		after, countErr := d.number(ctx, `SELECT count(*) FROM TeamTasksLog`)
		if countErr == nil {
			report.CheckerResults = after - before
			if runErr == nil && report.CheckerResults < int64(teamCount*3) {
				runErr = fmt.Errorf("load.runSuite - checker rounds stopped progressing")
			}
		} else {
			runErr = errors.Join(runErr, countErr)
		}
		if report.WebSockets.Failures > 0 || report.WebSockets.Connections < int64(teamCount) || report.WebSockets.Messages < int64(teamCount) {
			runErr = errors.Join(runErr, fmt.Errorf("load.runSuite - incomplete or failed WebSocket observations: %+v", report.WebSockets))
		}
	}()
	measurementErr := runStages(ctx, stages, report.Profile != "checker", func(s stage) error {
		return measure(ctx, d, flags, s, report, directory, logf)
	})
	bad, err := d.number(ctx, `SELECT count(*) FROM TeamTasks WHERE status<>101 OR checks<2 OR checks_passed<>checks`)
	if err != nil {
		return errors.Join(measurementErr, err)
	}
	if bad != 0 {
		measurementErr = errors.Join(measurementErr, fmt.Errorf("load.runSuite - checker rounds failed or stopped progressing"))
	}
	failed, err := d.number(ctx, `SELECT count(*) FROM TeamTasksLog WHERE status IN (102,103,104,110)`)
	if err != nil {
		return errors.Join(measurementErr, err)
	}
	if failed != 0 {
		measurementErr = errors.Join(measurementErr, fmt.Errorf("load.runSuite - %d failed checker results in history", failed))
	}
	return measurementErr
}

// Stop increasing pressure after a failed stage, but still observe recovery.
// Its success must not erase an earlier availability or correctness failure.
func runStages(ctx context.Context, stages []stage, withBurst bool, measureStage func(stage) error) error {
	var measurementErr error
	for _, s := range stages[:len(stages)-1] {
		if measurementErr = measureStage(s); measurementErr != nil {
			break
		}
	}
	if measurementErr == nil && withBurst {
		burst := stage{Name: "flag-burst", Duration: 3 * time.Second, ReadRPS: 100, FlagRPS: 400, Overload: true}
		measurementErr = measureStage(burst)
		if !waitContext(ctx, 3*time.Second) {
			return errors.Join(measurementErr, ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(measurementErr, err)
	}
	return errors.Join(measurementErr, measureStage(stages[len(stages)-1]))
}

func measure(ctx context.Context, d *deployment, flags *catalog, s stage, report *suiteReport, directory string, logf func(string, ...any)) error {
	logf("stage=%s path=%s duration=%s reads=%d/s flags=%d/s", s.Name, s.Path, s.Duration, s.ReadRPS, s.FlagRPS*batchSize)
	before, err := d.number(ctx, `SELECT count(*) FROM StolenFlags`)
	if err != nil {
		return err
	}
	checksBefore, err := d.number(ctx, `SELECT count(*) FROM TeamTasksLog`)
	if err != nil {
		return err
	}
	completedBefore := 0
	if report.Profile == "checker" {
		count, err := d.number(ctx, completedCheckerRoundsSQL)
		if err != nil {
			return err
		}
		completedBefore = int(count)
	}
	f := newFarm(d.origin, flags)
	f.allowRateLimit = s.Name == "flag-burst"
	result := stageReport{Name: s.Name, Path: s.Path, Started: time.Now().UTC(), Seconds: s.Duration.Seconds()}
	var workers sync.WaitGroup
	workers.Go(func() {
		result.Reads = attack(ctx, attackOptions{rps: s.ReadRPS, duration: s.Duration, workers: readWorkers, target: readTarget(d.origin, s.Path), validate: validateRead(s.Overload)})
	})
	workers.Go(func() {
		result.Flags = attack(ctx, attackOptions{rps: s.FlagRPS, duration: s.Duration, workers: flagWorkers, target: f.target, validate: f.validate})
	})
	workers.Wait()
	result.Finished = time.Now().UTC()
	result.Accepted = f.accepted
	flagErr := checkAttack("flags", result.Flags, f.allowRateLimit)
	readErr := checkAttack("reads", result.Reads, s.Overload)
	integrityErr := f.reconcile(ctx, d, before)
	result.Committed = f.committed
	checksAfter, progressErr := d.number(ctx, `SELECT count(*) FROM TeamTasksLog`)
	if report.Profile == "checker" {
		checker, auditErr := collectCheckerReport(ctx, d)
		if auditErr == nil {
			report.Checker = checker
			result.CheckerCompletedRounds = checker.completedRounds()
			auditErr = checker.validate()
			if auditErr == nil && result.CheckerCompletedRounds-completedBefore < minimumCompletedRounds(s.Duration, checker.RoundSeconds) {
				auditErr = fmt.Errorf("load.measure - completed checker rounds stopped progressing during %s", s.Name)
			}
		}
		progressErr = errors.Join(progressErr, auditErr)
	} else if progressErr == nil && s.Duration >= 20*time.Second && checksAfter-checksBefore < int64(teamCount*3) {
		progressErr = fmt.Errorf("load.measure - checker rounds stopped progressing during %s", s.Name)
	}
	result.IntegrityPassed = integrityErr == nil
	if f.allowRateLimit && result.Flags.Statuses["429"] == 0 {
		flagErr = errors.Join(flagErr, fmt.Errorf("load.measure - flag burst did not exercise the limiter"))
	}
	report.Stages = append(report.Stages, result)
	logf("reads actual=%.0f/s p99=%.1fms valid=%t SLO=%t; flags p99=%.1fms accepted=%d", result.Reads.AchievedRPS, result.Reads.P99MS, result.Reads.GeneratorValid, result.Reads.SLOPassed, result.Flags.P99MS, result.Accepted)
	return errors.Join(flagErr, readErr, integrityErr, progressErr, report.save(directory))
}
