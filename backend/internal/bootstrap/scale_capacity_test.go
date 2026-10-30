package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"text/template"
)

func scaleComposeFixture(t *testing.T, workerPool int) string {
	t.Helper()
	services := make(map[string]any)
	for role, pool := range map[string]int{"api": 16, "receiver": 32, "worker": workerPool, "ticker": 8} {
		services[role] = map[string]any{"environment": map[string]string{
			"FORCAD_" + strings.ToUpper(role) + "_DATABASE_MAX_CONNS": fmt.Sprint(pool),
			"DATABASE_PASSWORD": "must-not-appear-in-scale-output",
		}}
	}
	data, err := json.Marshal(map[string]any{"services": services})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func scaleInspectFixture(workerPool int) string {
	var lines []string
	for role, count := range map[string]int{"api": 4, "receiver": 2, "worker": 2, "ticker": 1} {
		pool := map[string]int{"api": 16, "receiver": 32, "worker": workerPool, "ticker": 8}[role]
		for range count {
			lines = append(lines, fmt.Sprintf("%s true service FORCAD_ROLE=%s FORCAD_%s_DATABASE_MAX_CONNS=%d", role, role, strings.ToUpper(role), pool))
		}
	}
	return strings.Join(lines, "\n")
}

func TestCapacityInspectTemplateIncludesOnlyBudgetEnvironment(t *testing.T) {
	format, err := template.New("inspect").Funcs(template.FuncMap{"split": strings.Split}).Parse(applicationCapacityTemplate)
	if err != nil {
		t.Fatal(err)
	}
	container := map[string]any{
		"Config": map[string]any{"Labels": map[string]string{"com.docker.compose.service": "worker", "com.docker.compose.oneoff": "False"},
			"Env": []string{"FORCAD_ROLE=worker", "FORCAD_WORKER_DATABASE_MAX_CONNS=32", "DATABASE_PASSWORD=hidden", "ADMIN_PASSWORD=hidden"}},
		"State": map[string]any{"Running": true},
	}
	var out bytes.Buffer
	if err := format.Execute(&out, container); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "worker true service FORCAD_ROLE=worker FORCAD_WORKER_DATABASE_MAX_CONNS=32" {
		t.Fatal("inspect template exposed unexpected fields")
	}
	roles, err := configuredScalePools([]byte(scaleComposeFixture(t, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if err := addDeployedScalePools(roles, out.Bytes()); err != nil {
		t.Fatal(err)
	}
	if roles["worker"].pool != 32 || roles["worker"].replicas != 1 {
		t.Fatal("deployed pool and replica were not included")
	}
	if err := addDeployedScalePools(roles, []byte("worker true oneoff FORCAD_ROLE=worker")); err == nil {
		t.Fatal("one-off process silently replaced by requested replica count")
	}
}

func TestCapacityAWKReadsOnlyNumericDeployedCaps(t *testing.T) {
	awk, err := exec.LookPath("awk")
	if err != nil {
		t.Skip("awk is not available")
	}
	var configText strings.Builder
	configText.WriteString("[databases]\n")
	for _, role := range []string{"api", "receiver", "worker", "ticker"} {
		fmt.Fprintf(&configText, "forcad_%s = host='private-host' password='hidden' max_db_client_connections=328\n", role)
	}
	configText.WriteString("[pgbouncer]\nmax_client_conn = 1024\nauth_file = /private/users.txt\n")
	command := exec.CommandContext(t.Context(), awk, poolerCapacityAWK)
	command.Stdin = strings.NewReader(configText.String())
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	caps, err := parsePoolerCapacity(data)
	if err != nil || caps["worker"] != 328 || caps["global"] != 1024 {
		t.Fatalf("budget extraction failed: %v", err)
	}
	if strings.Contains(string(data), "hidden") || strings.Contains(string(data), "private") {
		t.Fatal("budget extraction exposed unrelated config")
	}
}

func TestControlScaleChecksDeployedBudgetsBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name                                                   string
		count, currentPool, proposedPool, workerCap, globalCap int
		wantError                                              string
	}{
		{"stale pooler budget", 20, 16, 16, 256, 1024, "worker needs 328 clients"},
		{"deployed budget permits twenty", 20, 16, 16, 328, 1024, ""},
		{"older container has larger pool", 10, 64, 16, 328, 1024, "worker needs 648 clients"},
		{"new containers have larger pool", 20, 16, 32, 328, 1024, "worker needs 648 clients"},
		{"aggregate budget", 20, 16, 16, 328, 400, "application pools need 488 clients"},
		{"scale to zero", 0, 16, 16, 256, 1024, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TEST", "")
			root := t.TempDir()
			_, args, err := controlCommand(root, []string{"scale", "-s", "worker", fmt.Sprint(test.count)})
			if err != nil {
				t.Fatal(err)
			}
			mutations, reads := 0, 0
			runner := func(ctx context.Context, name string, command []string, _ []string, _ io.Reader, out, _ io.Writer) error {
				if ctx.Err() != nil || name != "docker" {
					t.Fatal("invalid preflight runner context or executable")
				}
				joined := strings.Join(command, " ")
				switch {
				case strings.Contains(joined, "exec -T pgbouncer awk"):
					reads++
					_, err = fmt.Fprintf(out, "api 256\nreceiver 256\nworker %d\nticker 256\nglobal %d\n", test.workerCap, test.globalCap)
				case strings.Contains(joined, "config --format json"):
					reads++
					_, err = io.WriteString(out, scaleComposeFixture(t, test.proposedPool))
				case strings.Contains(joined, "ps --all --quiet"):
					reads++
					_, err = io.WriteString(out, "abcdef123456\n")
				case command[0] == "inspect":
					reads++
					_, err = io.WriteString(out, scaleInspectFixture(test.currentPool))
				case strings.Contains(joined, "up -d --no-recreate --no-deps --scale"):
					mutations++
				default:
					t.Fatalf("unexpected command %v", command)
				}
				return err
			}
			var output bytes.Buffer
			err = runOperatorCommand(t.Context(), "compose", args, &output, runner, nil)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || mutations != 0 {
					t.Fatalf("unsafe scale result=%v mutations=%d", err, mutations)
				}
			} else if err != nil || mutations != 1 {
				t.Fatalf("permitted scale result=%v mutations=%d", err, mutations)
			}
			if reads != 4 || output.Len() != 0 || (err != nil && strings.Contains(err.Error(), "must-not-appear")) {
				t.Fatal("capacity reads were skipped or private config escaped")
			}
		})
	}
}

func TestScaleFailsClosedWhenDeployedCapacityCannotBeRead(t *testing.T) {
	for _, response := range []string{"", "api 256\nworker 256\nglobal 1024", "api secret\n", "api 256\napi 256\n"} {
		if _, err := parsePoolerCapacity([]byte(response)); err == nil {
			t.Fatalf("accepted invalid budget %q", response)
		}
	}
	failure := errors.New("pooler unavailable")
	calls := 0
	err := checkScaleCapacity(t.Context(), []string{"docker", "compose"}, []string{"up", "--no-recreate", "--scale", "worker=20"},
		func(context.Context, string, []string, []string, io.Reader, io.Writer, io.Writer) error {
			calls++
			return failure
		})
	if !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("preflight=%v calls=%d", err, calls)
	}
}

func TestDeclarativeStartDoesNotRequireAnExistingPooler(t *testing.T) {
	t.Setenv("FORCAD_WORKER_REPLICAS", "20")
	called := false
	if err := checkScaleCapacity(t.Context(), []string{"docker", "compose"}, []string{"up", "-d", "--build"},
		func(context.Context, string, []string, []string, io.Reader, io.Writer, io.Writer) error {
			called = true
			return nil
		}); err != nil || called {
		t.Fatalf("declarative start unexpectedly required live capacity: %v", err)
	}
}

func TestScaleWorkerOnlyDeploymentAccountsForAbsentAndOrphanRoles(t *testing.T) {
	for _, orphan := range []bool{false, true} {
		t.Run(fmt.Sprint(orphan), func(t *testing.T) {
			runner := func(_ context.Context, _ string, args []string, _ []string, _ io.Reader, out, _ io.Writer) error {
				joined := strings.Join(args, " ")
				var data string
				switch {
				case strings.Contains(joined, "exec -T pgbouncer awk"):
					data = "api 256\nreceiver 256\nworker 328\nticker 256\nglobal 352\n"
				case strings.Contains(joined, "config --format json"):
					data = `{"services":{"worker":{"environment":{"FORCAD_WORKER_DATABASE_MAX_CONNS":"16"}},"pgbouncer":{}}}`
				case strings.Contains(joined, "ps --all --quiet"):
					if len(args) != 4 {
						t.Fatal("worker-only inventory requested services absent from Compose")
					}
					data = "abcdef123456\n"
				case args[0] == "inspect":
					data = "\nworker true service FORCAD_ROLE=worker\n\n"
					if orphan {
						data += "api true service FORCAD_ROLE=api\n"
					}
				default:
					t.Fatalf("unexpected capacity command %v", args)
				}
				_, err := io.WriteString(out, data)
				return err
			}
			err := checkScaleCapacity(t.Context(), []string{"docker", "compose"}, []string{"up", "--no-recreate", "--scale", "worker=20"}, runner)
			if !orphan && err != nil {
				t.Fatalf("worker-only scale rejected: %v", err)
			}
			if orphan && (err == nil || !strings.Contains(err.Error(), "need 368 clients")) {
				t.Fatalf("orphan application omitted from budget: %v", err)
			}
		})
	}
}
