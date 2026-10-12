package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const example = `game:
  round_time: 60
  flag_lifetime: 5
  rounds: 300
  start_time: 2027-01-01 12:00:00
  timezone: Europe/Moscow
teams:
  - ip: 192.0.2.1
    name: Example
tasks:
  - name: example
    checker: example/checker.py
`

func TestDocumentDefaultsAndTimezone(t *testing.T) {
	d, err := DecodeDocument(strings.NewReader(example))
	if err != nil {
		t.Fatal(err)
	}
	start, err := d.Game.StartAt()
	if err != nil {
		t.Fatal(err)
	}
	if start.UTC() != time.Date(2027, 1, 1, 9, 0, 0, 0, time.UTC) {
		t.Fatalf("incorrect timezone: %v", start)
	}
	if *d.Tasks[0].Puts != 1 || *d.Tasks[0].Gets != 1 || d.Tasks[0].Checker != "/checkers/example/checker.py" || *d.Tasks[0].GetPeriod != 60 {
		t.Fatal("incorrect task defaults")
	}
}

func TestLoadDocumentAddsContextAndPreservesFilesystemCause(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "missing.yml")
	_, err := LoadDocument(filename)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadDocument error = %v, want os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), "config.LoadDocument - open configuration file") {
		t.Fatalf("LoadDocument error has no function context: %v", err)
	}
}

func TestDecodeDocumentRedactsMalformedConfiguration(t *testing.T) {
	const secret = "runtime-secret-value"
	input := "admin:\n  password: \"" + secret + "\n"
	_, err := DecodeDocument(strings.NewReader(input))
	if err == nil {
		t.Fatal("accepted malformed configuration")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("configuration parse error exposed a secret: %v", err)
	}
	if !strings.Contains(err.Error(), "config.DecodeDocument - invalid YAML configuration") {
		t.Fatalf("configuration parse error has no function context: %v", err)
	}
}

func TestDocumentRejectsSilentCoercionAndMissingChecker(t *testing.T) {
	for _, replacement := range []string{"rounds: 1.5", "rounds: true", "rounds: '300'", "rounds: 0", "rounds: -1"} {
		if _, err := DecodeDocument(strings.NewReader(strings.Replace(example, "rounds: 300", replacement, 1))); err == nil {
			t.Fatalf("accepted %s", replacement)
		}
	}
	if _, err := DecodeDocument(strings.NewReader(strings.Replace(example, "checker: example/checker.py", "checker: ''", 1))); err == nil {
		t.Fatal("accepted empty checker path")
	}
	if _, err := DecodeDocument(strings.NewReader(example + "---\nextra: document\n")); err == nil {
		t.Fatal("accepted second document")
	}
}

func TestLogoURLs(t *testing.T) {
	for _, value := range []string{"", "/team-logos/example.svg", "https://example.org/logo.png"} {
		if !ValidLogoPath(value) {
			t.Fatalf("rejected %q", value)
		}
	}
	for _, value := range []string{"//example.org/logo", "javascript:alert(1)", "https://user:password@example.org/logo", "/team-logos/%252e%252e/secret", "/x/%0aheader", "/x\\y"} {
		if ValidLogoPath(value) {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestDocumentTeamTokenTypes(t *testing.T) {
	for _, value := range []string{"null", "'0000000000000001'"} {
		input := strings.Replace(example, "name: Example", "name: Example\n    token: "+value, 1)
		d, err := DecodeDocument(strings.NewReader(input))
		if err != nil {
			t.Fatalf("rejected token %s: %v", value, err)
		}
		if value == "null" && d.Teams[0].Token != "" {
			t.Fatal("null token must be generated during initialization")
		}
	}
	for _, value := range []string{"1234567890123456", "true", "''", "'INVALID'"} {
		input := strings.Replace(example, "name: Example", "name: Example\n    token: "+value, 1)
		if _, err := DecodeDocument(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted token %s", value)
		}
	}
}
