package bootstrap

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunNetworkConfigCommandEmitsOnlySanitizedInventory(t *testing.T) {
	const secret = "0123456789abcdef"
	configuration := "game:\n  round_time: 60\n  flag_lifetime: 5\n  start_time: 2027-01-01 12:00:00\n  timezone: UTC\ntasks:\n  - name: example\n    checker: example/checker.py\nteams:\n  - ip: 192.0.2.10\n    name: Private Team Name\n    token: \"" + secret + "\"\n    traffic_cidrs:\n      - 192.0.2.0/24\n"
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := RunNetworkConfigCommand(context.Background(), []string{"--config", configPath}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"team_ip": "192.0.2.10"`) || !strings.Contains(output.String(), `"192.0.2.0/24"`) {
		t.Fatalf("sanitized inventory is missing team network data:\n%s", output.String())
	}
	if strings.Contains(output.String(), secret) || strings.Contains(output.String(), "Private Team Name") || strings.Contains(output.String(), "token") {
		t.Fatalf("sanitized inventory exposed team configuration fields:\n%s", output.String())
	}
}

func TestRunNetworkRulesCommandOnlyRendersOwnedTable(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "inventory.json")
	const inventory = `{"teams":[{"team_ip":"192.0.2.10","networks":["192.0.2.10/32"]}]}`
	if err := os.WriteFile(configPath, []byte(inventory), 0o600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := RunNetworkRulesCommand(context.Background(), []string{"--config", configPath}, &output); err != nil {
		t.Fatal(err)
	}
	rules := output.String()
	if !strings.HasPrefix(rules, "create table inet forcad_monitoring\n") || strings.Contains(rules, "add table inet forcad_monitoring") {
		t.Fatalf("rules do not use create-table refusal semantics:\n%s", rules)
	}
}

func TestNetworkMetricsServerShutsDownOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	status := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serveNetworkMetricsWithListener(ctx, newTestListener(), http.NotFoundHandler(), statusWriter{status: status})
	}()
	select {
	case message := <-status:
		if !strings.Contains(message, "Serving network metrics on 127.0.0.1:") {
			t.Fatalf("unexpected server status %q", message)
		}
	case err := <-done:
		t.Fatalf("metrics command exited before serving: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("metrics command did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("graceful shutdown failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("metrics command did not stop after cancellation")
	}
}

type statusWriter struct {
	status chan<- string
}

func (writer statusWriter) Write(data []byte) (int, error) {
	writer.status <- string(data)
	return len(data), nil
}

type testListener struct {
	closed chan struct{}
	once   sync.Once
}

func newTestListener() *testListener {
	return &testListener{closed: make(chan struct{})}
}

func (listener *testListener) Accept() (net.Conn, error) {
	<-listener.closed
	return nil, net.ErrClosed
}

func (listener *testListener) Close() error {
	listener.once.Do(func() { close(listener.closed) })
	return nil
}

func (listener *testListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9109}
}
