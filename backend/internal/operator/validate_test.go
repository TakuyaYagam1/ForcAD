package operator

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFullValidationRequiresSetupAndDoesNotModifyFiles(t *testing.T) {
	root := newProject(t, basicConfig)
	path := filepath.Join(root, "config", "config.yml")
	if err := ValidateConfig(path, false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(path, true); err == nil {
		t.Fatal("full validation accepted missing deployment settings")
	}
	if err := Setup(root, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(path, true); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("validation modified configuration: %v", err)
	}
	bad := bytes.Replace(before, []byte("port: 5432"), []byte("port: 0"), 1)
	if bytes.Equal(before, bad) {
		t.Fatal("fixture did not contain the database port")
	}
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(path, true); err == nil {
		t.Fatal("full validation accepted an invalid database port")
	}
}
