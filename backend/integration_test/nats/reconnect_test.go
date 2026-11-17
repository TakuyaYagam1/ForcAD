package nats_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"
)

func TestConsumerReconnectsAndResumesDelivery(t *testing.T) {
	container := os.Getenv("FORCAD_TEST_NATS_CONTAINER")
	project := os.Getenv("FORCAD_TEST_NATS_PROJECT")
	if container == "" || project == "" {
		t.Skip("isolated NATS container/project not set")
	}
	if !regexp.MustCompile(`^forcad-(integration|deployment)-[a-z0-9-]+$`).MatchString(project) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(container) {
		t.Fatal("refusing restart outside an explicitly identified isolated test project")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	engine, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ID}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if expected := os.Getenv("FORCAD_TEST_NATS_ENGINE_ID"); expected != "" && strings.TrimSpace(string(engine)) != expected {
		t.Fatal("Docker engine differs from test owner")
	}
	data, err := exec.CommandContext(ctx, "docker", "inspect", container).Output()
	if err != nil {
		t.Fatal(err)
	}
	var items []struct {
		ID     string `json:"Id"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err = json.Unmarshal(data, &items); err != nil || len(items) != 1 {
		t.Fatal("cannot verify isolated NATS container")
	}
	item := items[0]
	if item.ID != container || !item.State.Running || item.Config.Labels["com.docker.compose.project"] != project || item.Config.Labels["com.docker.compose.service"] != "nats" {
		t.Fatal("refusing restart of unowned NATS container")
	}
	f := newFixture(t, 1)
	var delivered atomic.Int32
	runBroker(t, f.broker, func(context.Context, uuid.UUID) error {
		delivered.Add(1)
		return nil
	})
	publish(t, f.broker, job(1))
	await(t, func() bool {
		return delivered.Load() == 1
	})
	empty(t, f)
	if err = exec.CommandContext(ctx, "docker", "restart", "--time", "2", container).Run(); err != nil {
		t.Fatalf("restart isolated NATS: %v", err)
	}
	after, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ID}}").Output()
	if err != nil || string(after) != string(engine) {
		t.Fatal("Docker engine changed during restart")
	}
	var lastPublishErr error
	t.Cleanup(func() {
		if t.Failed() && lastPublishErr != nil {
			t.Logf("last publication error after restart: %v", lastPublishErr)
		}
	})
	await(t, func() bool {
		attempt, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		ids, err := f.broker.Publish(attempt, []uuid.UUID{job(2)})
		lastPublishErr = err
		return err == nil && len(ids) == 1
	})
	await(t, func() bool {
		return delivered.Load() >= 2
	})
	empty(t, f)
	await(t, f.broker.Connected)
}
