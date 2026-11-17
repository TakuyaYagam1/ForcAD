package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const poolMetricBodyLimit = 2 << 20

type poolMetricScrape func(ctx context.Context, replica poolMetricReplica) ([]byte, error)

func newPoolMetricScraper(ctx context.Context, d *deployment, replicas []poolMetricReplica) (poolMetricScrape, func(), error) {
	direct, err := parsePoolMetricTransport(os.Getenv("LOAD_POOL_METRICS_TRANSPORT"), runtime.GOOS,
		os.Getenv("DOCKER_HOST"), os.Getenv("DOCKER_CONTEXT"), os.Getenv("LOAD_MONITORING"))
	if err != nil {
		return nil, nil, err
	}
	if !direct {
		return func(ctx context.Context, replica poolMetricReplica) ([]byte, error) {
				return d.command(ctx, "", "exec", "-T", "--index", replica.index, replica.role, "wget", "-qO-", "http://127.0.0.1:9090/metrics")
			}, func() {
			}, nil
	}
	targets, err := resolvePoolMetricTargets(ctx, d, replicas)
	if err != nil {
		return nil, nil, err
	}
	client := newPoolMetricHTTPClient()
	return func(ctx context.Context, replica poolMetricReplica) ([]byte, error) {
		return fetchPoolMetrics(ctx, client, targets[replica])
	}, client.CloseIdleConnections, nil
}

func parsePoolMetricTransport(
	mode, platform, host, dockerContext, monitoring string,
) (bool, error) {
	const location = "load.parsePoolMetricTransport"
	switch mode {
	case "", "exec":
		return false, nil
	case "direct":
		if platform != "linux" || host != "unix:///var/run/docker.sock" || dockerContext != "" || monitoring != "1" {
			return false, fmt.Errorf("%s - direct metrics require Linux, DOCKER_HOST=unix:///var/run/docker.sock, no DOCKER_CONTEXT and LOAD_MONITORING=1", location)
		}
		return true, nil
	default:
		return false, fmt.Errorf("%s - LOAD_POOL_METRICS_TRANSPORT must be exec or direct", location)
	}
}

// Inspect only ownership labels, running state and addresses. Never request or
// retain a full inspect document, container environment, or command stderr.
const poolMetricInspectFormat = `{"id":{{json .Id}},"project":{{json (index .Config.Labels "com.docker.compose.project")}},"service":{{json (index .Config.Labels "com.docker.compose.service")}},"index":{{json (index .Config.Labels "com.docker.compose.container-number")}},"running":{{json .State.Running}},"networks":[{{$separator := ""}}{{range .NetworkSettings.Networks}}{{$separator}}{"ipv4":{{json .IPAddress}},"ipv6":{{json .GlobalIPv6Address}}}{{$separator = ","}}{{end}}]}`

type poolMetricContainer struct {
	ID       string              `json:"id"`
	Project  string              `json:"project"`
	Service  string              `json:"service"`
	Index    string              `json:"index"`
	Running  bool                `json:"running"`
	Networks []poolMetricNetwork `json:"networks"`
}

type poolMetricNetwork struct {
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

func resolvePoolMetricTargets(ctx context.Context, d *deployment, replicas []poolMetricReplica) (map[poolMetricReplica]string, error) {
	const location = "load.resolvePoolMetricTargets"
	project := os.Getenv("FORCAD_LOAD_PROJECT")
	if !validPoolMetricProject(project) {
		return nil, fmt.Errorf("%s - require an isolated load project", location)
	}
	rawIDs, err := d.command(ctx, "", "ps", "--status", "running", "-q", "api", "receiver")
	if err != nil {
		return nil, fmt.Errorf("%s - locate metrics containers: %w", location, err)
	}
	ids, err := poolMetricContainerIDs(rawIDs, len(replicas))
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, "docker", append([]string{"inspect", "--format", poolMetricInspectFormat}, ids...)...)
	cmd.WaitDelay = 2 * time.Second
	output := &poolMetricInspectOutput{}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s - inspect metrics ownership: %w", location, errors.Join(err, bounded.Err()))
	}
	return decodePoolMetricTargets(project, replicas, ids, output.data.Bytes())
}

func validPoolMetricProject(project string) bool {
	const prefix = "forcad-deployment-"
	return len(project) > len(prefix) && len(project) <= 128 && strings.HasPrefix(project, prefix) &&
		strings.IndexFunc(project, func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-'
		}) == -1
}

func poolMetricContainerIDs(data []byte, expected int) ([]string, error) {
	const location = "load.poolMetricContainerIDs"
	ids := strings.Fields(string(data))
	if expected < 1 || expected > 32 || len(ids) != expected {
		return nil, fmt.Errorf("%s - metrics container count does not match configured replicas", location)
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if len(id) != 64 || strings.IndexFunc(id, func(r rune) bool {
			return (r < '0' || r > '9') && (r < 'a' || r > 'f')
		}) != -1 || seen[id] {
			return nil, fmt.Errorf("%s - require unique full metrics container IDs", location)
		}
		seen[id] = true
	}
	return ids, nil
}

func decodePoolMetricTargets(
	project string,
	replicas []poolMetricReplica,
	ids []string,
	data []byte,
) (map[poolMetricReplica]string, error) {
	const location = "load.decodePoolMetricTargets"
	if !validPoolMetricProject(project) || len(ids) != len(replicas) {
		return nil, fmt.Errorf("%s - invalid expected metrics ownership", location)
	}
	wantIDs := make(map[string]bool, len(ids))
	for _, id := range ids {
		wantIDs[id] = true
	}
	wanted := make(map[poolMetricReplica]bool, len(replicas))
	for _, replica := range replicas {
		wanted[replica] = true
	}
	targets := make(map[poolMetricReplica]string, len(replicas))
	addresses := make(map[string]bool, len(replicas))
	decoder := json.NewDecoder(bytes.NewReader(data))
	for range replicas {
		var container poolMetricContainer
		if err := decoder.Decode(&container); err != nil {
			return nil, fmt.Errorf("%s - decode metrics ownership: %w", location, err)
		}
		replica := poolMetricReplica{container.Service, container.Index}
		if container.Project != project || !container.Running || !wantIDs[container.ID] || !wanted[replica] || targets[replica] != "" {
			return nil, fmt.Errorf("%s - unexpected metrics container ownership or state", location)
		}
		address, err := poolMetricContainerAddress(container.Networks)
		if err != nil {
			return nil, err
		}
		if addresses[address] {
			return nil, fmt.Errorf("%s - metrics containers must have distinct addresses", location)
		}
		delete(wantIDs, container.ID)
		addresses[address] = true
		targets[replica] = address
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s - unexpected extra metrics ownership data", location)
	}
	return targets, nil
}

func poolMetricContainerAddress(networks []poolMetricNetwork) (string, error) {
	const location = "load.poolMetricContainerAddress"
	if len(networks) != 1 || networks[0].IPv6 != "" {
		return "", fmt.Errorf("%s - metrics container must have one IPv4 address", location)
	}
	address, err := netip.ParseAddr(networks[0].IPv4)
	if err != nil || !address.Is4() || !address.IsPrivate() {
		return "", fmt.Errorf("%s - metrics container must have a private IPv4 address", location)
	}
	return net.JoinHostPort(address.String(), "9090"), nil
}

type poolMetricInspectOutput struct {
	data bytes.Buffer
}

func (o *poolMetricInspectOutput) Write(data []byte) (int, error) {
	const location = "load.poolMetricInspectOutput.Write"
	if len(data) > (64<<10)-o.data.Len() {
		return 0, fmt.Errorf("%s - projected metrics inspect exceeds 64 KiB", location)
	}
	return o.data.Write(data)
}

func newPoolMetricHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   1,
			IdleConnTimeout:       30 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			DisableCompression:    true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func fetchPoolMetrics(ctx context.Context, client *http.Client, address string) ([]byte, error) {
	const location = "load.fetchPoolMetrics"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/metrics", nil)
	if err != nil {
		return nil, fmt.Errorf("%s - create metrics request: %w", location, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s - read metrics: %w", location, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s - metrics returned HTTP %d", location, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, poolMetricBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("%s - read metrics body: %w", location, err)
	}
	if len(data) > poolMetricBodyLimit {
		return nil, fmt.Errorf("%s - metrics body exceeds 2 MiB", location)
	}
	return data, nil
}
