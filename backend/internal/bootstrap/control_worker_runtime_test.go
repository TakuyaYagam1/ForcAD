package bootstrap

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/config"
	localoperator "github.com/TakuyaYagam1/ForcAD/internal/operator"
	"go.yaml.in/yaml/v3"
)

func TestWorkerRefusesLegacyRuntimeCredentialsWithoutRewriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "config.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "deployment", "docker"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(operatorProjectConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deployment", "docker", "compose.yml"), []byte("services: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := localoperator.Setup(root, localoperator.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	document, err := config.LoadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Storages.DB.Runtime = nil
	legacy, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	err = runWorkerCommand(t.Context(), root, []string{"--redis", "redis.example", "--database", "db.example", "--rabbitmq", "rabbit.example"}, io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "main host") {
		t.Fatalf("legacy worker error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(legacy) {
		t.Fatal("worker replaced legacy credentials")
	}
}
