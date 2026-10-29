package checker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMemoryLimitMB      = 1024
	DefaultOutputLimitBytes   = 1024 * 1024
	MaxOutputLimitBytes       = 16 * 1024 * 1024
	DefaultTerminateTimeout   = 3 * time.Second
	checkerOutputMessageLimit = 1024
	checkerKillDrainTimeout   = 250 * time.Millisecond
	checkerProcessReapTimeout = 500 * time.Millisecond
	checkerMemoryLimitEnv     = "FORCAD_CHECKER_MEMORY_LIMIT_MB"
	checkerOutputLimitEnv     = "FORCAD_CHECKER_OUTPUT_LIMIT_BYTES"
	checkerOutputLimitMessage = "Checker output limit exceeded"
	checkerOutputLimitPublic  = "Check failed"
	checkerTimeoutPublic      = "Checker timed out"
	checkerCanceledPublic     = "Checker canceled"
	checkerTimeoutPrivate     = "timeout (killed by ForcAD)"
	checkerCanceledPrivate    = "canceled by ForcAD"
	checkerOutputMarker       = " [output truncated]"
)

// Action identifies the checker protocol operation.
type Action string

const (
	ActionCheck Action = "check"
	ActionPut   Action = "put"
	ActionGet   Action = "get"
)

// Status is the exit code returned by a checker.
type Status int

const (
	StatusUp          Status = 101
	StatusCorrupt     Status = 102
	StatusMumble      Status = 103
	StatusDown        Status = 104
	StatusCheckFailed Status = 110
)

// Request contains the checker executable and arguments for one operation.
type Request struct {
	Executable      string
	Action          Action
	TeamIP          string
	PrivateFlagData string
	Flag            string
	VulnNumber      int
	Timeout         time.Duration
	EnvPath         string
}

// Options configures process shutdown, output capture, and Linux memory limits.
type Options struct {
	TerminateTimeout time.Duration
	MaxOutputBytes   int
	// MemoryLimitMB is the checker address-space limit on Linux.
	MemoryLimitMB int
	// DisableMemoryLimit preserves the FORCAD_CHECKER_MEMORY_LIMIT_MB=0 setting.
	DisableMemoryLimit bool
}

// Result contains a checker verdict and bounded process output.
type Result struct {
	Action         Action
	Status         Status
	PublicMessage  string
	PrivateMessage string
	// Command contains a display form with flag and private data arguments redacted.
	Command       string
	Stdout        []byte
	Stderr        []byte
	ExitCode      int
	TimedOut      bool
	Canceled      bool
	Killed        bool
	OutputLimited bool
}

// Runner starts checker executables with bounded output and controlled shutdown.
type Runner struct {
	options Options
}

// DefaultOptions returns the resource limits for checker subprocesses.
func DefaultOptions() Options {
	return Options{
		TerminateTimeout: DefaultTerminateTimeout,
		MaxOutputBytes:   DefaultOutputLimitBytes,
		MemoryLimitMB:    DefaultMemoryLimitMB,
	}
}

// OptionsFromEnv applies the FORCAD_CHECKER_* settings to the defaults.
func OptionsFromEnv(env map[string]string) Options {
	options := DefaultOptions()
	if raw, ok := env[checkerMemoryLimitEnv]; ok {
		value, err := strconv.Atoi(raw)
		if err == nil && value >= 0 {
			if value == 0 {
				options.MemoryLimitMB = 0
				options.DisableMemoryLimit = true
			} else {
				options.MemoryLimitMB = value
			}
		}
	}
	if raw, ok := env[checkerOutputLimitEnv]; ok {
		value, err := strconv.Atoi(raw)
		if err == nil && value >= 1024 && value <= MaxOutputLimitBytes {
			options.MaxOutputBytes = value
		}
	}
	return options
}

// OptionsFromEnvironment reads the checker settings from the current process.
func OptionsFromEnvironment() Options {
	env := make(map[string]string, 2)
	for _, key := range []string{checkerMemoryLimitEnv, checkerOutputLimitEnv} {
		if value, ok := os.LookupEnv(key); ok {
			env[key] = value
		}
	}
	return OptionsFromEnv(env)
}

// NewRunner creates a runner. An empty Options value loads settings from the
// environment; use DefaultOptions to request the documented defaults directly.
func NewRunner(options Options) *Runner {
	if options == (Options{}) {
		options = OptionsFromEnvironment()
	}
	if options.TerminateTimeout <= 0 {
		options.TerminateTimeout = DefaultTerminateTimeout
	}
	if options.MaxOutputBytes < 1024 || options.MaxOutputBytes > MaxOutputLimitBytes {
		options.MaxOutputBytes = DefaultOutputLimitBytes
	}
	if options.DisableMemoryLimit {
		options.MemoryLimitMB = 0
	} else if options.MemoryLimitMB <= 0 {
		options.MemoryLimitMB = DefaultMemoryLimitMB
	}
	return &Runner{options: options}
}

// Run executes one checker operation. A context cancellation returns the
// partial Result together with the context error after stopping the process.
func (r *Runner) Run(ctx context.Context, request Request) (Result, error) {
	const location = "checker.Runner.Run"
	result := Result{
		Action:   request.Action,
		Status:   StatusCheckFailed,
		Command:  displayCommand(request),
		ExitCode: -1,
	}
	if ctx == nil {
		return result, fmt.Errorf("%s - checker context must not be nil", location)
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("%s - accept checker request: %w", location, err)
	}
	if request.Executable == "" {
		return result, fmt.Errorf("%s - checker executable is required", location)
	}

	args, err := request.arguments()
	if err != nil {
		return result, fmt.Errorf("%s - prepare checker arguments: %w", location, err)
	}
	env := patchedEnvironment(request.EnvPath)
	executable, err := resolveExecutable(request.Executable, env)
	if err != nil {
		return result, fmt.Errorf("%s - resolve checker executable: %w", location, err)
	}
	executable, args, err = applyMemoryLimit(executable, args, r.options.MemoryLimitMB, request.EnvPath)
	if err != nil {
		return result, fmt.Errorf("%s - apply checker memory limit: %w", location, err)
	}

	process, err := startChecker(executable, args, env, r.options.MaxOutputBytes)
	if err != nil {
		return result, fmt.Errorf("%s - start checker process: %w", location, err)
	}
	waitResult, err := waitForChecker(ctx, request.Timeout, r.options.TerminateTimeout, process, &result)
	if err != nil {
		return result, fmt.Errorf("%s - %w", location, err)
	}
	result, err = finishCheckerResult(request, result, waitResult)
	if err != nil {
		return result, fmt.Errorf("%s - finish checker result: %w", location, err)
	}
	return result, nil
}

type startedChecker struct {
	command     *exec.Cmd
	capture     *outputCapture
	outputLimit chan struct{}
	waited      chan error
}

func startChecker(executable string, args, env []string, outputLimitBytes int) (startedChecker, error) {
	const location = "checker.startChecker"
	//nolint:noctx // This runner handles cancellation through process-group shutdown below.
	cmd := exec.Command(executable, args...)
	cmd.Env = env
	configureProcess(cmd)

	capture := &outputCapture{limit: outputLimitBytes}
	outputLimit := make(chan struct{}, 1)
	cmd.Stdout = captureWriter{capture: capture, stream: outputStdout, limit: outputLimit}
	cmd.Stderr = captureWriter{capture: capture, stream: outputStderr, limit: outputLimit}
	if err := cmd.Start(); err != nil {
		return startedChecker{}, fmt.Errorf("%s - start checker process: %w", location, err)
	}

	waited := make(chan error, 1)
	go func() {
		waited <- cmd.Wait()
	}()
	return startedChecker{command: cmd, capture: capture, outputLimit: outputLimit, waited: waited}, nil
}

type checkerWait struct {
	ctx                  context.Context
	command              *exec.Cmd
	capture              *outputCapture
	result               *Result
	waited               <-chan error
	timeout              <-chan time.Time
	contextDone          <-chan struct{}
	outputLimit          <-chan struct{}
	terminateTimer       *time.Timer
	terminate            <-chan time.Time
	killDrainTimer       *time.Timer
	killDrain            <-chan time.Time
	reapTimer            *time.Timer
	reap                 <-chan time.Time
	terminationRequested bool
	contextErr           error
	waitErr              error
}

type checkerWaitResult struct {
	waitErr    error
	contextErr error
}

func waitForChecker(ctx context.Context, timeout, terminateTimeout time.Duration, process startedChecker, result *Result) (checkerWaitResult, error) {
	const location = "checker.waitForChecker"
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	state := checkerWait{
		ctx: ctx, command: process.command, capture: process.capture, result: result,
		waited: process.waited, timeout: timeoutTimer.C, contextDone: ctx.Done(), outputLimit: process.outputLimit,
	}
	defer state.stopTimers()
	if timeout <= 0 {
		result.TimedOut = true
		state.timeout = nil
		state.requestTermination(terminateTimeout)
	}
	for {
		done, err := state.waitForEvent(terminateTimeout)
		if err != nil {
			return checkerWaitResult{}, fmt.Errorf("%s - wait for checker process: %w", location, err)
		}
		if done {
			break
		}
	}
	result.Stdout, result.Stderr = process.capture.snapshot()
	result.OutputLimited = result.OutputLimited || process.capture.wasLimited()
	if process.command.ProcessState != nil {
		result.ExitCode = process.command.ProcessState.ExitCode()
	}
	return checkerWaitResult{waitErr: state.waitErr, contextErr: state.contextErr}, nil
}

func (state *checkerWait) waitForEvent(terminateTimeout time.Duration) (bool, error) {
	const location = "checker.waitForEvent"
	select {
	case state.waitErr = <-state.waited:
		return true, nil
	case <-state.timeout:
		state.result.TimedOut = true
		state.timeout = nil
		state.requestTermination(terminateTimeout)
	case <-state.contextDone:
		state.contextDone = nil
		state.contextErr = state.ctx.Err()
		if errors.Is(state.contextErr, context.DeadlineExceeded) {
			state.result.TimedOut = true
		} else {
			state.result.Canceled = true
		}
		state.requestTermination(terminateTimeout)
	case <-state.outputLimit:
		state.outputLimit = nil
		state.result.OutputLimited = true
		state.requestTermination(terminateTimeout)
	case <-state.terminate:
		state.terminate = nil
		state.result.Killed = true
		killProcess(state.command)
		state.killDrainTimer = time.NewTimer(checkerKillDrainTimeout)
		state.killDrain = state.killDrainTimer.C
	case <-state.killDrain:
		state.killDrain = nil
		state.reapTimer = time.NewTimer(checkerProcessReapTimeout)
		state.reap = state.reapTimer.C
	case <-state.reap:
		state.result.Stdout, state.result.Stderr = state.capture.snapshot()
		state.result.OutputLimited = state.result.OutputLimited || state.capture.wasLimited()
		return true, fmt.Errorf("%s - checker process did not exit after SIGKILL", location)
	}
	return false, nil
}

func (state *checkerWait) requestTermination(terminateTimeout time.Duration) {
	if state.terminationRequested {
		return
	}
	state.terminationRequested = true
	terminateProcess(state.command)
	state.terminateTimer = time.NewTimer(terminateTimeout)
	state.terminate = state.terminateTimer.C
}

func (state *checkerWait) stopTimers() {
	if state.terminateTimer != nil {
		state.terminateTimer.Stop()
	}
	if state.killDrainTimer != nil {
		state.killDrainTimer.Stop()
	}
	if state.reapTimer != nil {
		state.reapTimer.Stop()
	}
}

func finishCheckerResult(request Request, result Result, wait checkerWaitResult) (Result, error) {
	const location = "checker.finishCheckerResult"
	if wait.waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(wait.waitErr, &exitErr) && !result.TimedOut && !result.Canceled && !result.OutputLimited {
			return result, fmt.Errorf("%s - wait for checker process: %w", location, wait.waitErr)
		}
	}
	switch {
	case result.TimedOut:
		result.Status = StatusDown
		result.PublicMessage = checkerTimeoutPublic
		result.PrivateMessage = strings.ToUpper(string(request.Action)) + " " + checkerTimeoutPrivate
	case result.Canceled:
		result.Status = StatusDown
		result.PublicMessage = checkerCanceledPublic
		result.PrivateMessage = strings.ToUpper(string(request.Action)) + " " + checkerCanceledPrivate
	case result.OutputLimited:
		result.Status = StatusCheckFailed
		result.PublicMessage = checkerOutputLimitPublic
		result.PrivateMessage = checkerOutputLimitMessage
	default:
		setCheckerExitResult(request, &result)
	}
	if wait.contextErr != nil {
		return result, fmt.Errorf("%s - checker process was interrupted: %w", location, wait.contextErr)
	}
	return result, nil
}

func setCheckerExitResult(request Request, result *Result) {
	status, err := statusFromExitCode(result.ExitCode)
	if err != nil {
		result.Status = StatusCheckFailed
		result.PublicMessage = checkerOutputLimitPublic
		result.PrivateMessage = fmt.Sprintf(
			"Check failed with ValueError: %s\nReturn code: %d\nStdout: %s\nStderr: %s",
			fmt.Sprintf("%d is not a valid TaskStatus", result.ExitCode),
			result.ExitCode,
			shortOutput(result.Stdout),
			shortOutput(result.Stderr),
		)
		return
	}
	result.Status = status
	if request.Action == ActionPut && result.Status == StatusUp {
		result.PublicMessage = strings.TrimSpace(decodeOutput(result.Stdout))
		result.PrivateMessage = strings.TrimSpace(decodeOutput(result.Stderr))
	} else {
		result.PublicMessage = strings.TrimSpace(shortOutput(result.Stdout))
		result.PrivateMessage = strings.TrimSpace(shortOutput(result.Stderr))
	}
}

func (request Request) arguments() ([]string, error) {
	const location = "checker.Request.arguments"
	switch request.Action {
	case ActionCheck:
		return []string{string(ActionCheck), request.TeamIP}, nil
	case ActionPut, ActionGet:
		return []string{
			string(request.Action),
			request.TeamIP,
			request.PrivateFlagData,
			request.Flag,
			strconv.Itoa(request.VulnNumber),
		}, nil
	default:
		return nil, fmt.Errorf("%s - unsupported checker action %q", location, request.Action)
	}
}

func displayCommand(request Request) string {
	parts := []string{request.Executable, string(request.Action), request.TeamIP}
	if request.Action == ActionPut || request.Action == ActionGet {
		parts = append(parts, "<private_flag_data>", "<flag>", strconv.Itoa(request.VulnNumber))
	}
	for i := range parts {
		parts[i] = strconv.Quote(parts[i])
	}
	return strings.Join(parts, " ")
}

func statusFromExitCode(code int) (Status, error) {
	const location = "checker.statusFromExitCode"
	status := Status(code)
	switch status {
	case StatusUp, StatusCorrupt, StatusMumble, StatusDown, StatusCheckFailed:
		return status, nil
	default:
		return StatusCheckFailed, fmt.Errorf("%s - %d is not a valid TaskStatus", location, code)
	}
}

func decodeOutput(output []byte) string {
	return strings.ToValidUTF8(string(output), "\uFFFD")
}

func shortOutput(output []byte) string {
	if len(output) <= checkerOutputMessageLimit {
		return decodeOutput(output)
	}
	return decodeOutput(output[:checkerOutputMessageLimit]) + checkerOutputMarker
}

type outputStream uint8

const (
	outputStdout outputStream = iota
	outputStderr
)

type outputCapture struct {
	mu      sync.Mutex
	limit   int
	stdout  []byte
	stderr  []byte
	limited bool
}

func (capture *outputCapture) append(stream outputStream, output []byte) bool {
	capture.mu.Lock()
	defer capture.mu.Unlock()

	remaining := capture.limit - len(capture.stdout) - len(capture.stderr)
	if remaining <= 0 {
		if len(output) != 0 {
			capture.limited = true
			return true
		}
		return false
	}
	if len(output) > remaining {
		output = output[:remaining]
		capture.limited = true
	}
	if stream == outputStdout {
		capture.stdout = append(capture.stdout, output...)
	} else {
		capture.stderr = append(capture.stderr, output...)
	}
	return capture.limited && len(output) == remaining
}

func (capture *outputCapture) snapshot() ([]byte, []byte) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]byte(nil), capture.stdout...), append([]byte(nil), capture.stderr...)
}

func (capture *outputCapture) wasLimited() bool {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.limited
}

type captureWriter struct {
	capture *outputCapture
	stream  outputStream
	limit   chan<- struct{}
}

func (writer captureWriter) Write(output []byte) (int, error) {
	if writer.capture.append(writer.stream, output) {
		select {
		case writer.limit <- struct{}{}:
		default:
		}
	}
	return len(output), nil
}

func patchedEnvironment(envPath string) []string {
	env := os.Environ()
	if envPath == "" {
		return env
	}

	pathKey := "PATH"
	pathValue := ""
	pathFound := false
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && environmentKeyMatches(key, pathKey) {
			pathKey = key
			pathValue = value
			pathFound = true
			break
		}
	}
	if pathFound {
		pathValue = envPath + string(os.PathListSeparator) + pathValue
	} else {
		pathValue = envPath
	}

	for i, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && environmentKeyMatches(key, pathKey) {
			env[i] = pathKey + "=" + pathValue
			return env
		}
	}
	if !pathFound {
		pathKey = "PATH"
	}
	return append(env, pathKey+"="+pathValue)
}

func resolveExecutable(executable string, env []string) (string, error) {
	const location = "checker.resolveExecutable"
	if filepath.IsAbs(executable) || filepath.VolumeName(executable) != "" || strings.ContainsRune(executable, os.PathSeparator) ||
		(runtime.GOOS == "windows" && strings.ContainsRune(executable, '/')) {
		return executable, nil
	}
	pathValue := environmentValue(env, "PATH")
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" {
			directory = "."
		}
		for _, candidate := range executableCandidates(directory, executable, env) {
			info, err := os.Stat(candidate)
			if err == nil && info.Mode().IsRegular() && isExecutable(info.Mode()) {
				absolute, err := filepath.Abs(candidate)
				if err == nil {
					return absolute, nil
				}
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("%s - checker executable %q not found in PATH", location, executable)
}

func executableCandidates(directory, executable string, env []string) []string {
	candidates := []string{filepath.Join(directory, executable)}
	if runtime.GOOS != "windows" || filepath.Ext(executable) != "" {
		return candidates
	}
	pathExt := environmentValue(env, "PATHEXT")
	if pathExt == "" {
		pathExt = ".COM;.EXE;.BAT;.CMD"
	}
	for extension := range strings.SplitSeq(pathExt, ";") {
		if extension != "" {
			candidates = append(candidates, filepath.Join(directory, executable+extension))
		}
	}
	return candidates
}

func environmentValue(env []string, key string) string {
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && environmentKeyMatches(name, key) {
			return value
		}
	}
	return ""
}

func environmentKeyMatches(name, key string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(name, key)
	}
	return name == key
}

func isExecutable(mode os.FileMode) bool {
	return runtime.GOOS == "windows" || mode.Perm()&0111 != 0
}

var _ io.Writer = captureWriter{}
