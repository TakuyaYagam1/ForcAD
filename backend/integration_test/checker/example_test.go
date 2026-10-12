package checker_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
)

const (
	exampleFlag       = "ABCDEFGHIJKLMNOPQRSTUVWXYZ12345="
	exampleIdentifier = "0123456789abcdef0123456789abcdef01234567"
)

type exampleState struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Token   string `json:"token"`
	Place   int    `json:"place"`
}

func runExample(t *testing.T, mode, host string, action processchecker.Action, private string, place int) processchecker.Result {
	t.Helper()
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("Python 3 is required for checker examples: %v", err)
	}
	program := exampleProgram(t, mode)
	runner := processchecker.NewRunner(processchecker.Options{DisableMemoryLimit: true})
	result, err := runner.Run(t.Context(), processchecker.Request{
		Executable: program, Action: action, TeamIP: host, PrivateFlagData: private,
		Flag: exampleFlag, VulnNumber: place, EnvPath: filepath.Dir(python), Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Stdout)+string(result.Stderr), exampleFlag) {
		t.Fatal("checker output exposed the flag")
	}
	return result
}

func exampleProgram(t *testing.T, mode string) string {
	t.Helper()
	directory, err := filepath.Abs("../../../checkers/examples/note")
	if err != nil {
		t.Fatal(err)
	}
	if mode == "hackerdom" {
		return filepath.Join(directory, "checker.py")
	}
	if mode != "pfr" {
		t.Fatalf("unknown example mode %q", mode)
	}
	adapted := t.TempDir()
	for _, filename := range []string{"checker.py", "client.py", "protocol.py"} {
		data, err := os.ReadFile(filepath.Join(directory, filename))
		if err != nil {
			t.Fatal(err)
		}
		if filename == "checker.py" {
			if !strings.Contains(string(data), "PUBLIC_HINT = False") {
				t.Fatal("example must default to hackerdom output")
			}
			data = []byte(strings.Replace(string(data), "PUBLIC_HINT = False", "PUBLIC_HINT = True", 1))
		}
		if err := os.WriteFile(filepath.Join(adapted, filename), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(adapted, "checker.py")
}

func requireExampleStatus(t *testing.T, result processchecker.Result, want processchecker.Status) {
	t.Helper()
	if result.Status != want || result.ExitCode != int(want) || result.TimedOut {
		t.Fatalf("status=%d exit=%d timeout=%t, want %d; stdout=%q stderr=%q",
			result.Status, result.ExitCode, result.TimedOut, want, result.Stdout, result.Stderr)
	}
}

func examplePrivateData(t *testing.T, mode string, result processchecker.Result) (string, exampleState) {
	t.Helper()
	raw := strings.TrimSpace(string(result.Stdout))
	if mode == "pfr" {
		raw = strings.TrimSpace(string(result.Stderr))
	} else if len(result.Stderr) != 0 {
		t.Fatal("ordinary successful PUT wrote diagnostic output")
	}
	var state exampleState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("invalid private retrieval data: %v", err)
	}
	if state.Version != 1 || state.ID != exampleIdentifier || len(state.Token) != 32 {
		t.Fatal("invalid private retrieval fields")
	}
	if mode == "pfr" && strings.TrimSpace(string(result.Stdout)) != state.ID {
		t.Fatal("PFR output must contain only the public note ID")
	}
	return raw, state
}

func TestExampleLifecycle(t *testing.T) {
	for _, mode := range []string{"hackerdom", "pfr"} {
		for _, place := range []int{1, 2} {
			t.Run(mode+"/"+strconv.Itoa(place), func(t *testing.T) {
				service, host := newExampleService(t)
				requireExampleStatus(t, runExample(t, mode, host, processchecker.ActionCheck, "", 0), processchecker.StatusUp)
				if notes, calls := service.counts(); notes != 0 || calls != 3 {
					t.Fatalf("CHECK must write, read and delete its smoke note: notes=%d calls=%d", notes, calls)
				}
				put := runExample(t, mode, host, processchecker.ActionPut, exampleIdentifier, place)
				requireExampleStatus(t, put, processchecker.StatusUp)
				private, state := examplePrivateData(t, mode, put)
				if state.Place != place {
					t.Fatal("PUT lost the flag location")
				}
				service.mu.Lock()
				note := service.notes[state.ID]
				service.mu.Unlock()
				if (note.Title == exampleFlag) != (place == 1) || (note.Body == exampleFlag) != (place == 2) {
					t.Fatal("PUT stored the flag in the wrong note field")
				}
				for range 2 {
					requireExampleStatus(t, runExample(t, mode, host, processchecker.ActionGet, private, place), processchecker.StatusUp)
				}
				if notes, calls := service.counts(); notes != 1 || calls != 7 {
					t.Fatalf("PUT must verify storage and GET must preserve it: notes=%d calls=%d", notes, calls)
				}
			})
		}
	}
}
