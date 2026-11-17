package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFixAndCheckAgain(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.go")
	source := []byte("package p\nfunc f(){use();use()}\n")
	if err := os.WriteFile(path, source, 0o640); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	if status := run([]string{"--check", directory}, &out, &diagnostics); status != 1 {
		t.Fatalf("check status=%d diagnostics=%s", status, &diagnostics)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, source) {
		t.Fatal("check mode rewrote the input")
	}
	if status := run([]string{"--fix", directory}, &out, &diagnostics); status != 0 {
		t.Fatalf("fix status=%d diagnostics=%s", status, &diagnostics)
	}
	out.Reset()
	if status := run([]string{"--check", directory}, &out, &diagnostics); status != 0 || out.Len() != 0 {
		t.Fatalf("check after fix status=%d output=%s diagnostics=%s", status, &out, &diagnostics)
	}
	got, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(got), "func f() {\n\tuse()\n\tuse()\n}") {
		t.Fatalf("fix did not expand the function: %s, %v", got, err)
	}
}

func TestInvalidModeAndMalformedFileReturnErrors(t *testing.T) {
	var out, diagnostics bytes.Buffer
	if status := run([]string{"--fix", "--check"}, &out, &diagnostics); status != 2 {
		t.Fatalf("conflicting modes status=%d", status)
	}
	path := filepath.Join(t.TempDir(), "broken.go")
	source := []byte("package p\nfunc broken( {")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	if status := run([]string{"--fix", path}, &out, &diagnostics); status != 2 {
		t.Fatalf("malformed file status=%d", status)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, source) {
		t.Fatal("malformed input was changed")
	}
}

func TestDiscoveryIsSortedDeduplicatedAndSkipsCaches(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"b.go", "nested/a.go", ".cache/ignored.go", "vendor/ignored.go", "node_modules/ignored.go", "load_test/reports/test-assets/ignored.go", "notes.txt"} {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package p"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := goFiles([]string{directory, filepath.Join(directory, "b.go")})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != filepath.Join(directory, "b.go") || files[1] != filepath.Join(directory, "nested/a.go") {
		t.Fatalf("unexpected discovered files: %v", files)
	}
}

func TestDiscoverySkipsDockerVolumes(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"main.go", "docker_volumes/postgres/data/ignored.go"} {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package p"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := goFiles([]string{directory})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(directory, "main.go")}
	if len(files) != len(want) || files[0] != want[0] {
		t.Fatalf("unexpected discovered files: got %v, want %v", files, want)
	}
}
func TestDiscoveryDoesNotFollowSymlinks(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package p;func f(){use()}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "linked.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, err := goFiles([]string{directory})
	if err != nil || len(files) != 0 {
		t.Fatalf("directory discovery followed a symlink: %v, %v", files, err)
	}
	if _, err := goFiles([]string{link}); err == nil {
		t.Fatal("explicit symlink was accepted")
	}
}
