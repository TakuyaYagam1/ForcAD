package bootstrap

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/config"
	localoperator "github.com/TakuyaYagam1/ForcAD/internal/operator"
)

func TestWorkerReusesCredentialsAndStartsNoDependencies(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config", "config.yml")
	for _, path := range []string{filepath.Dir(configPath), filepath.Join(root, "deployment", "docker")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(configPath, []byte(operatorProjectConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deployment", "docker", "compose.yml"), []byte("services:\n  worker: {}\n  postgres: {}\n  redis: {}\n  rabbitmq: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := localoperator.Setup(root, localoperator.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	before, err := config.LoadDocument(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST", "")
	var got []string
	runner := func(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
		got = append([]string{name}, args...)
		return nil
	}
	args := []string{"--redis", "cache.example:6380", "--database", "db.example:5433", "--rabbitmq", "broker.example:5673", "-w", "3", "--fast"}
	if err := runWorkerCommand(t.Context(), root, args, io.Discard, runner); err != nil {
		t.Fatal(err)
	}
	want := []string{"docker", "compose", "--project-directory", filepath.Join(root, "deployment", "docker"), "-f", filepath.Join(root, "config", "runtime", "compose.external.yml"), "up", "-d", "--build", "--no-deps", "--scale", "worker=3", "pgbouncer", "worker"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("worker deployment = %q, want %q", got, want)
	}
	after, err := config.LoadDocument(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if *after.Admin != *before.Admin || after.Storages.DB.Password != before.Storages.DB.Password || after.Storages.Redis.Password != before.Storages.Redis.Password || after.Storages.RabbitMQ.Password != before.Storages.RabbitMQ.Password {
		t.Fatal("worker command replaced deployment credentials")
	}
	if after.Storages.DB.Host != "db.example" || after.Storages.Redis.Port != 6380 || after.Storages.RabbitMQ.Port != 5673 {
		t.Fatal("worker command did not apply external addresses")
	}
}

func TestWorkerRejectsIncompleteOptionsBeforeSetup(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{nil, {"--redis", "cache.example"}, {"--redis", "cache.example", "--database", "db.example", "--rabbitmq", "broker.example", "-w", "-1"}} {
		if err := runWorkerCommand(t.Context(), root, args, io.Discard, nil); err == nil {
			t.Fatalf("accepted worker options %q", args)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid worker command changed the project: %v", err)
	}
}

func TestWorkerRefusesToGenerateNewStorageCredentials(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config", "config.yml")
	if err := os.WriteFile(path, []byte(operatorProjectConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--redis", "cache.example", "--database", "db.example", "--rabbitmq", "broker.example"}
	if err := runWorkerCommand(t.Context(), root, args, io.Discard, nil); err == nil {
		t.Fatal("worker accepted a configuration without deployment credentials")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, []byte(operatorProjectConfig)) {
		t.Fatalf("worker changed configuration: %v", err)
	}
}
