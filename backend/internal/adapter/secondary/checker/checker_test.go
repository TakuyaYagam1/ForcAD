package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	fixtureModeEnv      = "FORCAD_CHECKER_FIXTURE_MODE"
	fixtureExitEnv      = "FORCAD_CHECKER_FIXTURE_EXIT"
	fixtureHeartbeatEnv = "FORCAD_CHECKER_FIXTURE_HEARTBEAT"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(fixtureModeEnv); mode != "" {
		os.Exit(runCheckerFixture(mode))
	}
	os.Exit(m.Run())
}

func runCheckerFixture(mode string) int {
	args := os.Args[1:]
	switch mode {
	case "record":
		record := struct {
			Args []string `json:"args"`
			Path string   `json:"path"`
		}{Args: args, Path: environmentValue(os.Environ(), "PATH")}
		if err := json.NewEncoder(os.Stdout).Encode(record); err != nil {
			return 2
		}
		if len(args) > 0 && args[0] == string(ActionPut) {
			_, _ = fmt.Fprint(os.Stdout, strings.Repeat("p", 2048))
			_, _ = fmt.Fprint(os.Stderr, strings.Repeat("s", 2048))
		}
		return int(StatusUp)
	case "large":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("o", 2048))
		_, _ = fmt.Fprint(os.Stderr, strings.Repeat("e", 2048))
		return int(StatusUp)
	case "flood":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("o", 8192))
		_, _ = fmt.Fprint(os.Stderr, strings.Repeat("e", 8192))
		time.Sleep(30 * time.Second)
		return int(StatusUp)
	case "exit":
		code, err := strconv.Atoi(os.Getenv(fixtureExitEnv))
		if err != nil {
			return 2
		}
		return code
	case "sleep":
		fixtureIgnoreTermination()
		time.Sleep(30 * time.Second)
		return int(StatusUp)
	case "spawn", "spawn-parent-term":
		if mode == "spawn" {
			fixtureIgnoreTermination()
		}
		//nolint:noctx // This fixture child must outlive its parent until group teardown.
		child := exec.Command(os.Args[0], "child")
		child.Env = setEnvironmentValue(os.Environ(), fixtureModeEnv, "child")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			return 2
		}
		for {
			writeFixtureHeartbeat(os.Getenv(fixtureHeartbeatEnv) + ".parent")
			time.Sleep(20 * time.Millisecond)
		}
	case "child":
		fixtureIgnoreTermination()
		for {
			writeFixtureHeartbeat(os.Getenv(fixtureHeartbeatEnv) + ".child")
			time.Sleep(20 * time.Millisecond)
		}
	default:
		return 2
	}
}

func writeFixtureHeartbeat(path string) {
	if path == ".parent" || path == ".child" {
		return
	}
	_ = os.WriteFile(path, []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o600)
}

func setEnvironmentValue(env []string, key, value string) []string {
	updated := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !strings.EqualFold(name, key) {
			updated = append(updated, entry)
		}
	}
	return append(updated, key+"="+value)
}

func testRunner(outputLimit int) *Runner {
	options := DefaultOptions()
	options.DisableMemoryLimit = true
	if outputLimit != 0 {
		options.MaxOutputBytes = outputLimit
	}
	return NewRunner(options)
}

func testExecutable(t *testing.T) (string, string) {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Base(path), filepath.Dir(path)
}

func TestOptionsFromEnvPreservesCheckerSettings(t *testing.T) {
	defaults := OptionsFromEnv(nil)
	if defaults.MemoryLimitMB != DefaultMemoryLimitMB || defaults.MaxOutputBytes != DefaultOutputLimitBytes {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	configured := OptionsFromEnv(map[string]string{
		checkerMemoryLimitEnv: "256",
		checkerOutputLimitEnv: "4096",
	})
	if configured.MemoryLimitMB != 256 || configured.MaxOutputBytes != 4096 {
		t.Fatalf("settings were not applied: %+v", configured)
	}

	disabled := OptionsFromEnv(map[string]string{checkerMemoryLimitEnv: "0"})
	if !disabled.DisableMemoryLimit || disabled.MemoryLimitMB != 0 {
		t.Fatalf("zero memory limit should disable the cap: %+v", disabled)
	}

	invalid := OptionsFromEnv(map[string]string{
		checkerMemoryLimitEnv: "invalid",
		checkerOutputLimitEnv: "17",
	})
	if invalid.MemoryLimitMB != DefaultMemoryLimitMB || invalid.MaxOutputBytes != DefaultOutputLimitBytes {
		t.Fatalf("invalid settings should use defaults: %+v", invalid)
	}
}

func TestRunnerPreservesProtocolArgumentsAndPutOutput(t *testing.T) {
	executable, executableDir := testExecutable(t)
	t.Setenv(fixtureModeEnv, "record")

	tests := []struct {
		name   string
		action Action
		want   []string
	}{
		{name: "check", action: ActionCheck, want: []string{"check", "192.0.2.1"}},
		{name: "put", action: ActionPut, want: []string{"put", "192.0.2.1", "private value", "FLAG{secret}", "17"}},
		{name: "get", action: ActionGet, want: []string{"get", "192.0.2.1", "private value", "FLAG{secret}", "17"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := Request{
				Executable:      executable,
				Action:          test.action,
				TeamIP:          "192.0.2.1",
				PrivateFlagData: "private value",
				Flag:            "FLAG{secret}",
				VulnNumber:      17,
				Timeout:         2 * time.Second,
				EnvPath:         executableDir,
			}
			result, err := testRunner(0).Run(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusUp {
				t.Fatalf("status = %d, want %d", result.Status, StatusUp)
			}

			var record struct {
				Args []string `json:"args"`
				Path string   `json:"path"`
			}
			line, _, _ := strings.Cut(string(result.Stdout), "\n")
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("decode fixture record: %v; output=%q", err, result.Stdout)
			}
			if strings.Join(record.Args, "\x00") != strings.Join(test.want, "\x00") {
				t.Fatalf("arguments = %#v, want %#v", record.Args, test.want)
			}
			if !strings.HasPrefix(record.Path, executableDir+string(os.PathListSeparator)) {
				t.Fatalf("PATH = %q, want prefix %q", record.Path, executableDir+string(os.PathListSeparator))
			}
			if strings.Contains(result.Command, "private value") || strings.Contains(result.Command, "FLAG{secret}") {
				t.Fatalf("display command exposed flag arguments: %q", result.Command)
			}
			if test.action == ActionPut {
				if len(result.Stdout) <= checkerOutputMessageLimit || len(result.Stderr) <= checkerOutputMessageLimit {
					t.Fatalf("PUT output was shortened: stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
				}
				if result.PublicMessage != strings.TrimSpace(string(result.Stdout)) || result.PrivateMessage != strings.TrimSpace(string(result.Stderr)) {
					t.Fatalf("PUT output was not preserved in the verdict")
				}
			}
		})
	}
}

func TestRunnerBoundsCombinedOutputAndReturnsCheckFailed(t *testing.T) {
	executable, executableDir := testExecutable(t)
	t.Setenv(fixtureModeEnv, "flood")

	result, err := testRunner(1024).Run(context.Background(), Request{
		Executable: executable,
		Action:     ActionCheck,
		TeamIP:     "192.0.2.1",
		Timeout:    5 * time.Second,
		EnvPath:    executableDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OutputLimited || result.Status != StatusCheckFailed {
		t.Fatalf("unexpected output-limit result: %+v", result)
	}
	if len(result.Stdout)+len(result.Stderr) > 1024 {
		t.Fatalf("captured output exceeds combined limit: stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
	}
	if result.PublicMessage != checkerOutputLimitPublic || result.PrivateMessage != checkerOutputLimitMessage {
		t.Fatalf("unexpected output-limit messages: public=%q private=%q", result.PublicMessage, result.PrivateMessage)
	}
}

func TestRunnerShortensOrdinaryMessages(t *testing.T) {
	executable, executableDir := testExecutable(t)
	t.Setenv(fixtureModeEnv, "large")

	result, err := testRunner(0).Run(context.Background(), Request{
		Executable: executable,
		Action:     ActionCheck,
		TeamIP:     "192.0.2.1",
		Timeout:    2 * time.Second,
		EnvPath:    executableDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PublicMessage) != checkerOutputMessageLimit+len(checkerOutputMarker) || !strings.HasSuffix(result.PublicMessage, checkerOutputMarker) {
		t.Fatalf("stdout message was not shortened: length=%d suffix=%q", len(result.PublicMessage), result.PublicMessage[len(result.PublicMessage)-len(checkerOutputMarker):])
	}
	if len(result.PrivateMessage) != checkerOutputMessageLimit+len(checkerOutputMarker) || !strings.HasSuffix(result.PrivateMessage, checkerOutputMarker) {
		t.Fatalf("stderr message was not shortened: length=%d", len(result.PrivateMessage))
	}
}

func TestRunnerMapsExitStatusesAndUnknownCodes(t *testing.T) {
	executable, executableDir := testExecutable(t)
	t.Setenv(fixtureModeEnv, "exit")

	for _, code := range []int{101, 102, 103, 104, 110, 99} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Setenv(fixtureExitEnv, strconv.Itoa(code))
			result, err := testRunner(0).Run(context.Background(), Request{
				Executable: executable,
				Action:     ActionCheck,
				TeamIP:     "192.0.2.1",
				Timeout:    2 * time.Second,
				EnvPath:    executableDir,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := StatusCheckFailed
			if code == 101 || code == 102 || code == 103 || code == 104 || code == 110 {
				want = Status(code)
			}
			if result.Status != want {
				t.Fatalf("status = %d, want %d", result.Status, want)
			}
			if code == 99 && !strings.Contains(result.PrivateMessage, "99 is not a valid TaskStatus") {
				t.Fatalf("unexpected invalid-code message: %q", result.PrivateMessage)
			}
		})
	}
}

func TestRunnerTimeoutAndContextCancellation(t *testing.T) {
	executable, executableDir := testExecutable(t)
	t.Setenv(fixtureModeEnv, "sleep")
	runner := testRunner(0)
	runner.options.TerminateTimeout = 50 * time.Millisecond

	result, err := runner.Run(context.Background(), Request{
		Executable: executable,
		Action:     ActionGet,
		TeamIP:     "192.0.2.1",
		Timeout:    50 * time.Millisecond,
		EnvPath:    executableDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || result.Status != StatusDown || result.PublicMessage != checkerTimeoutPublic {
		t.Fatalf("unexpected timeout result: %+v", result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	timer := time.AfterFunc(50*time.Millisecond, cancel)
	t.Cleanup(func() { timer.Stop() })
	result, err = runner.Run(ctx, Request{
		Executable: executable,
		Action:     ActionGet,
		TeamIP:     "192.0.2.1",
		Timeout:    5 * time.Second,
		EnvPath:    executableDir,
	})
	if !errors.Is(err, context.Canceled) || !result.Canceled || result.Status != StatusDown || !strings.HasPrefix(err.Error(), "checker.Runner.Run - ") {
		t.Fatalf("unexpected cancellation result: result=%+v err=%v", result, err)
	}

	deadlineCtx, cancelDeadline := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancelDeadline)
	result, err = runner.Run(deadlineCtx, Request{
		Executable: executable,
		Action:     ActionGet,
		TeamIP:     "192.0.2.1",
		Timeout:    5 * time.Second,
		EnvPath:    executableDir,
	})
	if !errors.Is(err, context.DeadlineExceeded) || !result.TimedOut || result.Status != StatusDown || !strings.HasPrefix(err.Error(), "checker.Runner.Run - ") {
		t.Fatalf("unexpected context-deadline result: result=%+v err=%v", result, err)
	}
}

func TestRunnerZeroTimeoutIsImmediate(t *testing.T) {
	executable, executableDir := testExecutable(t)
	t.Setenv(fixtureModeEnv, "record")

	result, err := testRunner(0).Run(context.Background(), Request{
		Executable: executable,
		Action:     ActionCheck,
		TeamIP:     "192.0.2.1",
		EnvPath:    executableDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || result.Status != StatusDown || result.PublicMessage != checkerTimeoutPublic {
		t.Fatalf("zero timeout did not expire immediately: %+v", result)
	}
}

func TestRunnerStartErrorDoesNotExposeFlagArguments(t *testing.T) {
	result, err := testRunner(0).Run(context.Background(), Request{
		Executable:      filepath.Join(t.TempDir(), "missing-checker"),
		Action:          ActionPut,
		TeamIP:          "192.0.2.1",
		PrivateFlagData: "private sentinel",
		Flag:            "FLAG{sentinel}",
		VulnNumber:      3,
		Timeout:         time.Second,
	})
	if err == nil || !strings.HasPrefix(err.Error(), "checker.Runner.Run - ") {
		t.Fatalf("start error lacks location context: %v", err)
	}
	if strings.Contains(err.Error(), "private sentinel") || strings.Contains(err.Error(), "FLAG{sentinel}") || strings.Contains(result.Command, "FLAG{sentinel}") {
		t.Fatalf("start error exposed flag arguments: result=%+v err=%v", result, err)
	}
}

func TestRunnerRejectsUnknownAction(t *testing.T) {
	_, err := testRunner(0).Run(context.Background(), Request{
		Executable: "/bin/true",
		Action:     Action("list"),
		Timeout:    time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported checker action") || !strings.HasPrefix(err.Error(), "checker.Runner.Run - ") {
		t.Fatalf("unexpected error: %v", err)
	}
}
