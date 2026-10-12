package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestControlCommandRoutesHostAndContainerCommands(t *testing.T) {
	root := filepath.Join(t.TempDir(), "game directory")
	for _, test := range []struct {
		name    string
		args    []string
		command string
		options []string
	}{
		{"setup", []string{"setup", "--redis", "cache.example:6380"}, "setup", []string{"--project-root", root, "--redis", "cache.example:6380"}},
		{"start", []string{"start", "--scale", "worker=3"}, "compose", []string{"--project-root", root, "up", "-d", "--build", "--scale", "worker=3"}},
		{"logs", []string{"logs", "--tail", "100", "worker"}, "compose", []string{"--project-root", root, "logs", "--tail", "100", "worker"}},
		{"monitoring", []string{"monitoring", "up", "-d", "grafana"}, "compose", []string{"--project-root", root, "-f", filepath.Join(root, "deployment", "docker", "compose.monitoring.yml"), "--profile", "monitoring", "up", "-d", "grafana"}},
		{"status", []string{"status"}, "compose", []string{"--project-root", root, "exec", "-T", "api", "forcad", "status"}},
		{"tokens alias", []string{"print-tokens"}, "compose", []string{"--project-root", root, "exec", "-T", "api", "forcad", "tokens"}},
		{"old tokens name", []string{"print_tokens"}, "compose", []string{"--project-root", root, "exec", "-T", "api", "forcad", "tokens"}},
		{"pause", []string{"pause"}, "compose", []string{"--project-root", root, "exec", "-T", "api", "forcad", "pause"}},
		{"resume", []string{"resume"}, "compose", []string{"--project-root", root, "exec", "-T", "api", "forcad", "resume"}},
		{"fast build", []string{"build", "--fast"}, "compose", []string{"--project-root", root, "build"}},
		{"fast clean", []string{"clean", "--fast", "--confirm"}, "clean", []string{"--project-root", root, "--confirm"}},
		{"round limit", []string{"set-round-limit", "--rounds", "300"}, "compose", []string{"--project-root", root, "exec", "-T", "api", "forcad", "set-round-limit", "--rounds", "300"}},
		{"validate", []string{"validate"}, "validate-config", []string{"--config", filepath.Join(root, "config", "config.yml")}},
		{"custom config", []string{"validate", "--config", "custom.yml"}, "validate-config", []string{"--config", filepath.Join(root, "config", "config.yml"), "--config", "custom.yml"}},
		{"reset confirmation", []string{"reset", "--full", "--confirm"}, "reset", []string{"--project-root", root, "--full", "--confirm"}},
		{"compose passthrough", []string{"rd", "exec", "api", "printf", "a b; $(false)"}, "compose", []string{"--project-root", root, "exec", "api", "printf", "a b; $(false)"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			command, args, err := controlCommand(root, test.args)
			if err != nil {
				t.Fatal(err)
			}
			if command != test.command || !reflect.DeepEqual(args, test.options) {
				t.Fatalf("command = %s %q, want %s %q", command, args, test.command, test.options)
			}
		})
	}
}

func TestControlCommandHelpDoesNotNeedDeployment(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		var output bytes.Buffer
		if err := RunControlCommand(context.Background(), args, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(output.String(), "Usage: ./control.sh COMMAND") {
			t.Fatalf("missing control help: %q", output.String())
		}
	}
}

func TestControlCommandPreservesHelpAndRejectsUnsafeInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunControlCommand(ctx, []string{"start"}, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled command error = %v", err)
	}
	if _, _, err := controlCommand(t.TempDir(), []string{"unsupported"}); err == nil {
		t.Fatal("unknown control command was accepted")
	}
	for _, command := range []string{"build", "clean", "pause", "print_tokens", "reset", "resume", "rd", "scale", "setup", "start", "validate", "worker"} {
		args := []string{"--project-root", t.TempDir(), command, "--help"}
		if err := RunControlCommand(context.Background(), args, io.Discard); !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("%s --help error = %v", command, err)
		}
	}
	if err := RunControlCommand(context.Background(), []string{"--project-root", t.TempDir(), "reset"}, io.Discard); err == nil {
		t.Fatal("reset without confirmation was accepted")
	}
}
