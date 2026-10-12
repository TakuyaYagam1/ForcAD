package application_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/bootstrap"
)

const applicationConfigEnv = "FORCAD_TEST_APPLICATION_CONFIG"

// The child uses the same entrypoint and dependency assembly as cmd/forcad.
// Reusing the test executable also preserves race instrumentation in children.
func TestApplicationProcess(t *testing.T) {
	encoded := os.Getenv(applicationConfigEnv)
	if encoded == "" {
		t.Skip("application subprocess entrypoint")
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	logger, err := bootstrap.NewLogger(os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	if err := bootstrap.RunApplication(cfg, logger); err != nil {
		t.Fatal(err)
	}
}

type applicationProcess struct {
	URL        string
	controlURL string
	cmd        *exec.Cmd
	stop       context.CancelFunc
	done       chan struct{}
	err        error
}

func startApplication(t *testing.T, values map[string]string) *applicationProcess {
	t.Helper()
	if values["FORCAD_CONFIG"] == "" {
		filename, err := filepath.Abs("../fixtures/config.yml")
		if err != nil {
			t.Fatal(err)
		}
		values["FORCAD_CONFIG"] = filename
	}
	values["HTTP_ADDR"] = localAddress(t)
	values["METRICS_ADDR"] = localAddress(t)
	values["CONTROL_TOKEN"] = strings.Repeat("d", 32)
	values["SHUTDOWN_TIMEOUT"] = "5s"
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "application.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	processCtx, stopProcess := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processCtx, executable, "-test.run=^TestApplicationProcess$")
	cmd.Env = append(os.Environ(), applicationConfigEnv+"="+string(encoded))
	cmd.Stdout, cmd.Stderr = log, log
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		stopProcess()
		t.Fatal(err)
	}
	app := &applicationProcess{
		URL: "http://" + values["HTTP_ADDR"], controlURL: "http://" + values["METRICS_ADDR"],
		cmd: cmd, stop: stopProcess, done: make(chan struct{}),
	}
	go func() {
		app.err = cmd.Wait()
		close(app.done)
	}()
	t.Cleanup(func() {
		select {
		case <-app.done:
		default:
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-app.done:
			case <-time.After(8 * time.Second):
				_ = cmd.Process.Kill()
				<-app.done
				t.Error("application did not stop gracefully")
			}
		}
		if app.err != nil {
			t.Errorf("application exited: %v", app.err)
		}
		if t.Failed() {
			contents, _ := os.ReadFile(logPath)
			t.Logf("application output:\n%s", contents)
		}
		app.stop()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.controlURL+"/readyz", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return app
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("application did not become ready")
		case <-app.done:
			t.Fatalf("application stopped before readiness: %v", app.err)
		case <-tick.C:
		}
	}
}

func localAddress(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func (app *applicationProcess) drain(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, app.controlURL+"/drain", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("d", 32))
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("drain: HTTP %d", response.StatusCode)
	}
	select {
	case <-app.done:
		if app.err != nil {
			t.Fatalf("drained application: %v", app.err)
		}
	case <-ctx.Done():
		t.Fatal("drain did not stop the application")
	}
}
