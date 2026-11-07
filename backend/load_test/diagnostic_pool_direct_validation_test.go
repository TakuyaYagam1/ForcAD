package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"text/template"
	"time"
)

func TestPoolMetricTransportGuards(t *testing.T) {
	for _, mode := range []string{"", "exec"} {
		direct, err := parsePoolMetricTransport(mode, "windows", "remote", "remote", "0")
		if direct || err != nil {
			t.Fatalf("exec mode rejected: direct=%t err=%v", direct, err)
		}
	}
	direct, err := parsePoolMetricTransport("direct", "linux", "unix:///var/run/docker.sock", "", "1")
	if !direct || err != nil {
		t.Fatalf("valid direct mode rejected: direct=%t err=%v", direct, err)
	}
	for _, settings := range [][5]string{
		{"unknown", "linux", "unix:///var/run/docker.sock", "", "1"},
		{"direct", "windows", "unix:///var/run/docker.sock", "", "1"},
		{"direct", "linux", "", "", "1"},
		{"direct", "linux", "tcp://127.0.0.1:2375", "", "1"},
		{"direct", "linux", "unix:///another.sock", "", "1"},
		{"direct", "linux", "unix:///var/run/docker.sock", "default", "1"},
		{"direct", "linux", "unix:///var/run/docker.sock", "", ""},
	} {
		if enabled, err := parsePoolMetricTransport(settings[0], settings[1], settings[2], settings[3], settings[4]); enabled || err == nil {
			t.Fatalf("unsafe settings accepted: %v", settings)
		}
	}
	t.Setenv("LOAD_POOL_METRICS_TRANSPORT", "unknown")
	if scrape, cleanup, err := newPoolMetricScraper(t.Context(), nil, nil); scrape != nil || cleanup != nil || err == nil {
		t.Fatal("invalid mode did not fail before Docker resolution")
	}
}

func poolMetricOwnershipFixture() ([]poolMetricReplica, []string, []poolMetricContainer) {
	replicas := []poolMetricReplica{{"receiver", "1"}, {"api", "1"}}
	ids := []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	containers := []poolMetricContainer{
		{
			ID:      ids[0],
			Project: "forcad-deployment-test",
			Service: "receiver",
			Index:   "1",
			Running: true,
			Networks: []poolMetricNetwork{{
				IPv4: "172.21.0.2",
			}},
		},
		{
			ID:      ids[1],
			Project: "forcad-deployment-test",
			Service: "api",
			Index:   "1",
			Running: true,
			Networks: []poolMetricNetwork{{
				IPv4: "172.21.0.3",
			}},
		},
	}
	return replicas, ids, containers
}

func encodePoolMetricOwnership(tb testing.TB, containers []poolMetricContainer) []byte {
	tb.Helper()
	var output bytes.Buffer
	for _, container := range containers {
		if err := json.NewEncoder(&output).Encode(container); err != nil {
			tb.Fatal(err)
		}
	}
	return output.Bytes()
}

func TestPoolMetricOwnedTargets(t *testing.T) {
	replicas, ids, containers := poolMetricOwnershipFixture()
	targets, err := decodePoolMetricTargets("forcad-deployment-test", replicas, ids, encodePoolMetricOwnership(t, containers))
	if err != nil || len(targets) != 2 || targets[replicas[0]] != "172.21.0.2:9090" || targets[replicas[1]] != "172.21.0.3:9090" {
		t.Fatalf("valid targets rejected: %v, %v", targets, err)
	}
	for _, mutate := range []func(c []poolMetricContainer){
		func(c []poolMetricContainer) {
			c[0].Project = "production"
		},
		func(c []poolMetricContainer) {
			c[0].Service = "worker"
		},
		func(c []poolMetricContainer) {
			c[0].Index = "2"
		},
		func(c []poolMetricContainer) {
			c[0].Index = "01"
		},
		func(c []poolMetricContainer) {
			c[0].Running = false
		},
		func(c []poolMetricContainer) {
			c[0].ID = strings.Repeat("c", 64)
		},
		func(c []poolMetricContainer) {
			c[1].ID = c[0].ID
		},
		func(c []poolMetricContainer) {
			c[1].Service = c[0].Service
		},
		func(c []poolMetricContainer) {
			c[1].Networks[0].IPv4 = c[0].Networks[0].IPv4
		},
	} {
		replicas, ids, containers := poolMetricOwnershipFixture()
		mutate(containers)
		if targets, err := decodePoolMetricTargets("forcad-deployment-test", replicas, ids, encodePoolMetricOwnership(t, containers)); err == nil || targets != nil {
			t.Fatalf("unsafe targets admitted: %v, %v", targets, err)
		}
	}
	for _, data := range [][]byte{[]byte("not json"), encodePoolMetricOwnership(t, containers[:1]), encodePoolMetricOwnership(t, append(containers, containers[0])), append(encodePoolMetricOwnership(t, containers), []byte("trailing")...)} {
		if _, err := decodePoolMetricTargets("forcad-deployment-test", replicas, ids, data); err == nil {
			t.Fatal("missing, extra or malformed ownership output admitted")
		}
	}
}

func TestPoolMetricAddressAndIDGuards(t *testing.T) {
	for _, networks := range [][]poolMetricNetwork{
		nil,
		{{
			IPv4: "172.21.0.2",
		}, {
			IPv4: "172.22.0.2",
		}},
		{{
			IPv4: "172.21.0.2",
			IPv6: "fd00::1",
		}},
		{{
			IPv4: "",
		}},
		{{
			IPv4: "127.0.0.1",
		}},
		{{
			IPv4: "0.0.0.0",
		}},
		{{
			IPv4: "169.254.1.1",
		}},
		{{
			IPv4: "8.8.8.8",
		}},
		{{
			IPv4: "fd00::1",
		}},
		{{
			IPv4: "::ffff:172.21.0.2",
		}},
		{{
			IPv4: "172.21.0.2:9090",
		}},
	} {
		if address, err := poolMetricContainerAddress(networks); address != "" || err == nil {
			t.Fatalf("ambiguous/nonprivate address accepted: %v", networks)
		}
	}
	for _, project := range []string{"production", "forcad-deployment-", "forcad-deployment-Test", "forcad-deployment-../../production"} {
		if validPoolMetricProject(project) {
			t.Fatalf("unsafe project accepted: %s", project)
		}
	}
	id := strings.Repeat("a", 64)
	if ids, err := poolMetricContainerIDs([]byte(id+"\n"), 1); err != nil || len(ids) != 1 {
		t.Fatalf("full ID rejected: %v", err)
	}
	for _, raw := range []string{"", "short", strings.Repeat("z", 64), id + "\n" + id} {
		if _, err := poolMetricContainerIDs([]byte(raw), 1); err == nil {
			t.Fatal("invalid ID or count accepted")
		}
	}
	if _, err := poolMetricContainerIDs([]byte(id+"\n"+id), 2); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
}

func TestPoolMetricProjectedInspectOmitsSecretsAndBoundsOutput(t *testing.T) {
	replicas, ids, _ := poolMetricOwnershipFixture()
	var output bytes.Buffer
	inspect := template.Must(template.New("inspect").Funcs(template.FuncMap{
		"json": func(value any) (string, error) {
			data, err := json.Marshal(value)
			return string(data), err
		},
	}).Parse(poolMetricInspectFormat))
	for index, replica := range replicas {
		fixture := map[string]any{
			"Id": ids[index],
			"Config": map[string]any{
				"Env": []string{"SECRET=must-not-appear"},
				"Labels": map[string]string{
					"com.docker.compose.project":          "forcad-deployment-test",
					"com.docker.compose.service":          replica.role,
					"com.docker.compose.container-number": replica.index,
				},
			},
			"State": map[string]bool{
				"Running": true,
			},
			"NetworkSettings": map[string]any{
				"Networks": map[string]any{
					"default": map[string]string{
						"IPAddress":         []string{"172.21.0.2", "172.21.0.3"}[index],
						"GlobalIPv6Address": "",
					},
				},
			},
		}
		if err := inspect.Execute(&output, fixture); err != nil {
			t.Fatal(err)
		}
		output.WriteByte('\n')
	}
	if strings.Contains(output.String(), "SECRET") || strings.Contains(output.String(), "must-not-appear") {
		t.Fatal("projected inspect retained container environment")
	}
	if _, err := decodePoolMetricTargets("forcad-deployment-test", replicas, ids, output.Bytes()); err != nil {
		t.Fatalf("projected Docker template cannot be decoded: %v", err)
	}
	bounded := &poolMetricInspectOutput{}
	if _, err := io.Copy(bounded, strings.NewReader(strings.Repeat("x", (64<<10)+1))); err == nil || bounded.data.Len() > 64<<10 {
		t.Fatal("projected inspect exceeded its memory bound")
	}
}

func TestPoolMetricDirectOutputMatchesExecBodyAndWhitelist(t *testing.T) {
	body := "# HELP secret not-retained\ngo_goroutines 5\nforcad_database_connections_in_use 16\nunrelated_secret 123\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.String() != "/metrics" {
			t.Error("unexpected metrics request")
		}
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	client := newPoolMetricHTTPClient()
	defer client.CloseIdleConnections()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("direct metrics may not use a proxy")
	}
	direct, err := fetchPoolMetrics(t.Context(), client, strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var directOutput, execOutput bytes.Buffer
	now := time.Date(2026, 11, 6, 12, 0, 0, 123, time.UTC)
	for _, sample := range []struct {
		output io.Writer
		body   []byte
	}{{&directOutput, direct}, {&execOutput, []byte(body)}} {
		if err := writePoolMetricSample(sample.output, now, poolMetricReplica{"api", "1"}, sample.body); err != nil {
			t.Fatal(err)
		}
	}
	want := "2026-11-06T12:00:00.000000123Z api-1\ngo_goroutines 5\nforcad_database_connections_in_use 16\n"
	if directOutput.String() != want || directOutput.String() != execOutput.String() {
		t.Fatalf("transport changed output: direct=%q exec=%q", directOutput.String(), execOutput.String())
	}
}

type poolMetricSampleOutput struct {
	data   bytes.Buffer
	writes int
	err    error
}

func (o *poolMetricSampleOutput) Write(data []byte) (int, error) {
	o.writes++
	if o.err != nil {
		return 0, o.err
	}
	return o.data.Write(data)
}

func TestPoolMetricSampleBuffersWritesAndFlushesEveryReplica(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 123, time.FixedZone("sample", 3*60*60))
	metric := "forcad_database_connections_in_use{role=\"api\",pool=\"main\"} 16\n"
	body := []byte("# HELP ignored\n" + strings.Repeat(metric+"unrelated_secret 123\n", 1000))
	output := &poolMetricSampleOutput{}
	want := ""
	for _, replica := range []poolMetricReplica{{"api", "1"}, {"receiver", "2"}} {
		beforeWrites := output.writes
		sample := "2026-11-06T09:00:00.000000123Z " + replica.role + "-" + replica.index + "\n" + strings.Repeat(metric, 1000)
		if err := writePoolMetricSample(output, now, replica, body); err != nil {
			t.Fatal(err)
		}
		want += sample
		if output.data.String() != want {
			t.Fatal("replica sample was changed or not completely flushed before return")
		}
		if got, maximum := output.writes-beforeWrites, (len(sample)+(16<<10)-1)/(16<<10); got < 2 || got > maximum {
			t.Fatalf("sample was not written in bounded chunks: got %d writes, maximum %d", got, maximum)
		}
	}
}

func TestPoolMetricSamplePropagatesFinalFlushError(t *testing.T) {
	output := &poolMetricSampleOutput{
		err: io.ErrClosedPipe,
	}
	err := writePoolMetricSample(output, time.Now(), poolMetricReplica{"api", "1"}, []byte("go_goroutines 5\n"))
	if !errors.Is(err, io.ErrClosedPipe) || !strings.Contains(err.Error(), "load.writePoolMetricSample - flush sample:") {
		t.Fatalf("final flush error was not preserved: %v", err)
	}
	if output.writes != 1 || output.data.Len() != 0 {
		t.Fatalf("unexpected failed flush behavior: writes=%d bytes=%d", output.writes, output.data.Len())
	}
}

func TestPoolMetricDirectRejectsHTTPFailuresOversizeAndRedirects(t *testing.T) {
	var redirected atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	for _, mode := range []string{"non200", "oversize", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "non200":
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, "sensitive-response-body")
				case "oversize":
					_, _ = io.WriteString(w, strings.Repeat("x", poolMetricBodyLimit+1))
				case "redirect":
					http.Redirect(w, r, destination.URL, http.StatusFound)
				}
			}))
			defer server.Close()
			client := newPoolMetricHTTPClient()
			defer client.CloseIdleConnections()
			if data, err := fetchPoolMetrics(t.Context(), client, strings.TrimPrefix(server.URL, "http://")); data != nil || err == nil || strings.Contains(err.Error(), "sensitive-response-body") {
				t.Fatalf("invalid HTTP response admitted or leaked: len=%d err=%v", len(data), err)
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("direct metrics followed a redirect")
	}
}

func TestPoolMetricDirectCancellationStopsBodyRead(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := newPoolMetricHTTPClient()
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := fetchPoolMetrics(ctx, client, strings.TrimPrefix(server.URL, "http://"))
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("metrics request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation chain lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("metrics body read ignored cancellation")
	}
}
