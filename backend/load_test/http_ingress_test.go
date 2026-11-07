package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

type httpIngressMetadata struct {
	Transport         string `json:"transport"`
	LogicalOrigin     string `json:"logical_origin"`
	Project           string `json:"project,omitempty"`
	ContainerID       string `json:"container_id,omitempty"`
	Service           string `json:"service,omitempty"`
	Index             string `json:"index,omitempty"`
	NginxEndpoint     string `json:"nginx_endpoint,omitempty"`
	PublishedEndpoint string `json:"published_endpoint,omitempty"`
}

type httpIngressRoute struct {
	metadata    httpIngressMetadata
	logicalHost string
}

func prepareHTTPIngress(ctx context.Context, d *deployment, directory string) (*httpIngressRoute, error) {
	const location = "load.prepareHTTPIngress"
	mode, err := parseHTTPIngressTransport(os.Getenv("LOAD_HTTP_INGRESS_TRANSPORT"), runtime.GOOS,
		os.Getenv("DOCKER_HOST"), os.Getenv("DOCKER_CONTEXT"))
	if err != nil {
		return nil, err
	}
	route := &httpIngressRoute{
		metadata: httpIngressMetadata{
			Transport:     mode,
			LogicalOrigin: d.origin,
		},
	}
	if mode == "direct" {
		route, err = resolveHTTPIngress(ctx, d)
		if err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(filepath.Join(directory, "http-ingress.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("%s - create ingress metadata: %w", location, err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(route.metadata); err != nil {
		return nil, fmt.Errorf("%s - save ingress metadata: %w", location, err)
	}
	return route, nil
}

func parseHTTPIngressTransport(
	mode, platform, host, dockerContext string,
) (string, error) {
	const location = "load.parseHTTPIngressTransport"
	switch mode {
	case "", "published":
		return "published", nil
	case "direct":
		if platform != "linux" || host != "unix:///var/run/docker.sock" || dockerContext != "" {
			return "", fmt.Errorf("%s - direct ingress requires Linux, DOCKER_HOST=unix:///var/run/docker.sock and no DOCKER_CONTEXT", location)
		}
		return mode, nil
	default:
		return "", fmt.Errorf("%s - LOAD_HTTP_INGRESS_TRANSPORT must be published or direct", location)
	}
}

func httpIngressOrigin(origin string) (*url.URL, error) {
	const location = "load.httpIngressOrigin"
	parsed, err := url.Parse(origin)
	if err != nil {
		return nil, fmt.Errorf("%s - parse logical origin: %w", location, err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != parsed.Port() ||
		parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil ||
		parsed.Host != net.JoinHostPort("127.0.0.1", parsed.Port()) || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, fmt.Errorf("%s - require the original isolated IPv4 loopback HTTP origin", location)
	}
	return parsed, nil
}

func (r *httpIngressRoute) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	const location = "load.httpIngressRoute.dialContext"
	if network != "tcp" || address != r.logicalHost || r.metadata.Transport != "direct" {
		return nil, fmt.Errorf("%s - unexpected ingress dial target", location)
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, r.metadata.NginxEndpoint)
	if err != nil {
		return nil, fmt.Errorf("%s - dial owned Nginx endpoint: %w", location, err)
	}
	return conn, nil
}

const httpIngressInspectFormat = `{"id":{{json .Id}},"project":{{json (index .Config.Labels "com.docker.compose.project")}},"service":{{json (index .Config.Labels "com.docker.compose.service")}},"index":{{json (index .Config.Labels "com.docker.compose.container-number")}},"running":{{json .State.Running}},"networks":[{{$separator := ""}}{{range .NetworkSettings.Networks}}{{$separator}}{"ipv4":{{json .IPAddress}},"ipv6":{{json .GlobalIPv6Address}}}{{$separator = ","}}{{end}}],"ports":{{json (index .NetworkSettings.Ports "8080/tcp")}}}`

type httpIngressContainer struct {
	ID       string              `json:"id"`
	Project  string              `json:"project"`
	Service  string              `json:"service"`
	Index    string              `json:"index"`
	Running  bool                `json:"running"`
	Networks []poolMetricNetwork `json:"networks"`
	Ports    []httpIngressPort   `json:"ports"`
}

type httpIngressPort struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

func resolveHTTPIngress(ctx context.Context, d *deployment) (*httpIngressRoute, error) {
	const location = "load.resolveHTTPIngress"
	project := os.Getenv("FORCAD_LOAD_PROJECT")
	if !validPoolMetricProject(project) {
		return nil, fmt.Errorf("%s - require an isolated load project", location)
	}
	origin, err := httpIngressOrigin(d.origin)
	if err != nil {
		return nil, err
	}
	rawIDs, err := d.command(ctx, "", "ps", "--status", "running", "-q", "nginx")
	if err != nil {
		return nil, fmt.Errorf("%s - locate owned Nginx container: %w", location, err)
	}
	ids, err := poolMetricContainerIDs(rawIDs, 1)
	if err != nil {
		return nil, fmt.Errorf("%s - require exactly one Nginx container: %w", location, err)
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, "docker", "inspect", "--format", httpIngressInspectFormat, ids[0])
	cmd.WaitDelay = 2 * time.Second
	output := &poolMetricInspectOutput{}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s - inspect Nginx ownership: %w", location, errors.Join(err, bounded.Err()))
	}
	return decodeHTTPIngress(project, ids[0], origin, output.data.Bytes())
}

func decodeHTTPIngress(
	project, id string,
	origin *url.URL,
	data []byte,
) (*httpIngressRoute, error) {
	const location = "load.decodeHTTPIngress"
	if !validPoolMetricProject(project) || origin == nil {
		return nil, fmt.Errorf("%s - require isolated expected ownership and origin", location)
	}
	if _, err := poolMetricContainerIDs([]byte(id), 1); err != nil {
		return nil, fmt.Errorf("%s - validate expected Nginx container ID: %w", location, err)
	}
	if _, err := httpIngressOrigin(origin.String()); err != nil {
		return nil, fmt.Errorf("%s - validate expected logical origin: %w", location, err)
	}
	var container httpIngressContainer
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&container); err != nil {
		return nil, fmt.Errorf("%s - decode projected Nginx ownership: %w", location, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s - unexpected additional Nginx ownership data", location)
	}
	if container.ID != id || container.Project != project || container.Service != "nginx" || container.Index != "1" || !container.Running {
		return nil, fmt.Errorf("%s - unexpected Nginx ownership or running state", location)
	}
	if len(container.Ports) != 1 || container.Ports[0].HostIP != "127.0.0.1" || container.Ports[0].HostPort != origin.Port() {
		return nil, fmt.Errorf("%s - Nginx 8080/tcp must match the original loopback published port", location)
	}
	address, err := poolMetricContainerAddress(container.Networks)
	if err != nil {
		return nil, fmt.Errorf("%s - validate Nginx private address: %w", location, err)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%s - read Nginx private address: %w", location, err)
	}
	return &httpIngressRoute{
		logicalHost: origin.Host,
		metadata: httpIngressMetadata{
			Transport:         "direct",
			LogicalOrigin:     origin.String(),
			Project:           project,
			ContainerID:       id,
			Service:           container.Service,
			Index:             container.Index,
			NginxEndpoint:     net.JoinHostPort(host, "8080"),
			PublishedEndpoint: origin.Host,
		},
	}, nil
}
