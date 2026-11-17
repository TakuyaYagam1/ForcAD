package bootstrap

import (
	"reflect"
	"testing"
)

func TestStartAcceptsWorkerOptionsAndPreservesComposeArguments(t *testing.T) {
	for _, option := range [][]string{{"-w", "3"}, {"--workers", "3"}, {"--workers=3"}, {"-w3"}, {"-w=3"}} {
		args := append([]string{"--fast"}, option...)
		args = append(args, "--scale", "receiver=5", "worker", "receiver")
		got, err := startArguments(args)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"up", "-d", "--build", "--scale", "worker=3", "--scale", "receiver=5", "worker", "receiver"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("start %q = %q, want %q", option, got, want)
		}
	}
}

func TestScaleMapsOldServiceNamesAndOnlyTouchesRequestedServices(t *testing.T) {
	got, err := scaleArguments([]string{"-s", "celery", "3", "--service", "http-receiver", "5"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"up", "-d", "--no-recreate", "--no-deps", "--scale", "worker=3", "--scale", "receiver=5", "worker", "receiver"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scale = %q, want %q", got, want)
	}
	if _, err := scaleArguments([]string{"-s", "worker", "0"}); err != nil {
		t.Fatalf("scale to zero: %v", err)
	}
}

func TestDeploymentOptionsRejectInvalidCountsAndAmbiguousServices(t *testing.T) {
	for _, args := range [][]string{{"-w"}, {"-w", "-1"}, {"--workers=oops"}, {"--workers=1025"}} {
		if _, err := startArguments(args); err == nil {
			t.Fatalf("accepted start options %q", args)
		}
	}
	for _, args := range [][]string{nil, {"-s", "worker"}, {"-s", "worker", "-1"}, {"-s", "worker", "1", "--remove-orphans"}, {"-s", "--help", "1"}, {"-s", "worker; false", "1"}, {"-s", "celery", "1", "-s", "worker", "2"}} {
		if _, err := scaleArguments(args); err == nil {
			t.Fatalf("accepted scale options %q", args)
		}
	}
}

func TestComposeCompatibilityFlagDoesNotConsumePayloadFlags(t *testing.T) {
	_, got, err := controlCommand("/project", []string{"rd", "--fast", "exec", "api", "command", "--fast", "--workers", "9"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--project-root", "/project", "exec", "api", "command", "--fast", "--workers", "9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Compose payload changed: %q", got)
	}
}
