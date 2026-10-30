package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestStartReplicaEnvironment(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{"long", []string{"start", "--workers", "20"}, []string{"FORCAD_WORKER_REPLICAS=20"}},
		{"equals", []string{"start", "--workers=20"}, []string{"FORCAD_WORKER_REPLICAS=20"}},
		{"short", []string{"up", "-w20"}, []string{"FORCAD_WORKER_REPLICAS=20"}},
		{"short separate", []string{"start", "-w", "20"}, []string{"FORCAD_WORKER_REPLICAS=20"}},
		{"short equals", []string{"start", "-w=20"}, []string{"FORCAD_WORKER_REPLICAS=20"}},
		{"scale", []string{"start", "--scale", "worker=20"}, []string{"FORCAD_WORKER_REPLICAS=20"}},
		{"multiple roles", []string{"start", "--scale=api=6", "--scale=receiver=3", "-w20", "--scale=ticker=0"}, []string{"FORCAD_API_REPLICAS=6", "FORCAD_RECEIVER_REPLICAS=3", "FORCAD_WORKER_REPLICAS=20", "FORCAD_TICKER_REPLICAS=0"}},
		{"last wins", []string{"start", "-w2", "--scale=worker=20"}, []string{"FORCAD_WORKER_REPLICAS=20"}},
		{"zero", []string{"start", "-w0"}, []string{"FORCAD_WORKER_REPLICAS=0"}},
		{"configured defaults", []string{"start"}, nil},
		{"other service", []string{"start", "--scale=nginx=2"}, nil},
		{"terminator", []string{"start", "--", "--scale=worker=20"}, nil},
		{"dependencies enabled", []string{"start", "-w20", "--no-deps=false"}, []string{"FORCAD_WORKER_REPLICAS=20"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, args, err := controlCommand(t.TempDir(), test.args)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			runner := func(_ context.Context, _ string, _ []string, environment []string, _ io.Reader, _, _ io.Writer) error {
				calls++
				if !reflect.DeepEqual(environment, test.want) {
					t.Fatalf("child overrides = %v, want %v", environment, test.want)
				}
				return nil
			}
			if err := runStartCommand(t.Context(), args, io.Discard, runner, nil); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("start issued %d commands, want 1", calls)
			}
		})
	}
}

func TestScaledStartRejectsExcludedPoolerAndInvalidCounts(t *testing.T) {
	for _, options := range [][]string{
		{"-w20", "--no-deps", "worker"},
		{"-w20", "--no-deps=true", "worker"},
		{"--scale=worker=-1"}, {"--scale=worker=1025"},
		{"--scale", "worker"}, {"--scale"},
	} {
		t.Run(strings.Join(options, " "), func(t *testing.T) {
			_, args, err := controlCommand(t.TempDir(), append([]string{"start"}, options...))
			if err != nil {
				t.Fatal(err)
			}
			runner := func(context.Context, string, []string, []string, io.Reader, io.Writer, io.Writer) error {
				t.Fatal("invalid start invoked Docker")
				return nil
			}
			if err := runStartCommand(t.Context(), args, io.Discard, runner, nil); err == nil {
				t.Fatal("unsafe start accepted")
			}
		})
	}
}

func TestStartCannotChangeAnUnrecreatedPoolerBudget(t *testing.T) {
	_, args, err := controlCommand(t.TempDir(), []string{"start", "--workers=20", "--no-recreate=true"})
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	runner := func(_ context.Context, _ string, command, environment []string, _ io.Reader, out, _ io.Writer) error {
		if !reflect.DeepEqual(environment, []string{"FORCAD_WORKER_REPLICAS=20"}) {
			t.Fatalf("preflight lost child overrides: %v", environment)
		}
		joined := strings.Join(command, " ")
		var data string
		switch {
		case strings.Contains(joined, "exec -T pgbouncer awk"):
			data = "api 256\nreceiver 256\nworker 256\nticker 256\nglobal 1024\n"
		case strings.Contains(joined, "config --format json"):
			data = scaleComposeFixture(t, 16)
		case strings.Contains(joined, "ps --all --quiet"):
		default:
			t.Fatalf("undersized running pooler allowed mutation: %v", command)
		}
		reads++
		_, err := io.WriteString(out, data)
		return err
	}
	err = runStartCommand(t.Context(), args, io.Discard, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "328") || reads != 3 {
		t.Fatalf("preflight result = %v, reads = %d", err, reads)
	}
}

func TestCapacityOptionsRespectEqualsAndTerminator(t *testing.T) {
	for _, test := range []struct {
		args []string
		want map[string]int
	}{
		{[]string{"up", "--no-recreate=true", "--scale=worker=20"}, map[string]int{"worker": 20}},
		{[]string{"up", "--no-recreate", "--no-recreate=false", "--scale=worker=20"}, nil},
		{[]string{"up", "--", "--no-recreate", "--scale=worker=20"}, nil},
		{[]string{"up", "--no-recreate", "--", "--scale=worker=20"}, map[string]int{}},
		{[]string{"exec", "api", "--no-recreate", "--scale=worker=20"}, nil},
	} {
		got, err := capacityScaleTargets(test.args)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("targets(%v) = %v, %v; want %v", test.args, got, err, test.want)
		}
	}
}

func TestOperatorChildEnvironment(t *testing.T) {
	const marker = "FORCAD_TEST_OPERATOR_ENV_CHILD"
	if os.Getenv(marker) == "1" {
		fmt.Printf("worker=%s cap=%s\n", os.Getenv("FORCAD_WORKER_REPLICAS"), os.Getenv("PGBOUNCER_WORKER_CLIENTS"))
		return
	}
	t.Setenv("FORCAD_WORKER_REPLICAS", "2")
	t.Setenv("PGBOUNCER_WORKER_CLIENTS", "64")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runner := withReplicaEnvironment(execOperatorCommand, map[string]int{"worker": 20})
	err = runner(t.Context(), executable, []string{"-test.run=^TestOperatorChildEnvironment$"}, []string{marker + "=1"}, nil, &output, &output)
	if err != nil || !strings.Contains(output.String(), "worker=20 cap=64") {
		t.Fatalf("child environment = %q, %v", output.String(), err)
	}
	if os.Getenv("FORCAD_WORKER_REPLICAS") != "2" || os.Getenv("PGBOUNCER_WORKER_CLIENTS") != "64" {
		t.Fatal("start changed parent environment")
	}
}
