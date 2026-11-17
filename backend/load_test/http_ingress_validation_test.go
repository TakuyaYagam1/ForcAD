package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"text/template"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func TestHTTPIngressModeAndOriginGuards(t *testing.T) {
	for _, mode := range []string{"", "published"} {
		if got, err := parseHTTPIngressTransport(mode, "windows", "remote", "remote"); got != "published" || err != nil {
			t.Fatalf("default transport changed: %s %v", got, err)
		}
	}
	if mode, err := parseHTTPIngressTransport("direct", "linux", "unix:///var/run/docker.sock", ""); mode != "direct" || err != nil {
		t.Fatal("valid native direct transport rejected")
	}
	for _, setting := range [][4]string{
		{"bad", "linux", "unix:///var/run/docker.sock", ""},
		{"direct", "windows", "unix:///var/run/docker.sock", ""},
		{"direct", "linux", "", ""},
		{"direct", "linux", "tcp://127.0.0.1:2375", ""},
		{"direct", "linux", "unix:///var/run/docker.sock", "default"},
	} {
		if _, err := parseHTTPIngressTransport(setting[0], setting[1], setting[2], setting[3]); err == nil {
			t.Fatalf("unsafe ingress settings accepted: %v", setting)
		}
	}
	for _, origin := range []string{
		"http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:08080",
		"http://localhost:8080", "http://[::1]:8080", "http://[127.0.0.1]:8080", "http://172.20.0.2:8080",
		"https://127.0.0.1:8080", "http://user:secret@127.0.0.1:8080", "http://127.0.0.1:8080/",
		"http://127.0.0.1:8080?", "http://127.0.0.1:8080?q=1", "http://127.0.0.1:8080#fragment", "%bad",
	} {
		if _, err := httpIngressOrigin(origin); err == nil {
			t.Fatalf("unsafe logical origin accepted: %s", origin)
		}
	}
	t.Setenv("LOAD_HTTP_INGRESS_TRANSPORT", "bad")
	if _, err := prepareHTTPIngress(t.Context(), nil, t.TempDir()); err == nil {
		t.Fatal("unknown mode did not fail before Docker or deployment access")
	}
}

func httpIngressFixture(tb testing.TB) (*url.URL, httpIngressContainer) {
	tb.Helper()
	origin, err := httpIngressOrigin("http://127.0.0.1:45678")
	if err != nil {
		tb.Fatal(err)
	}
	return origin, httpIngressContainer{
		ID:      strings.Repeat("a", 64),
		Project: "forcad-deployment-test",
		Service: "nginx",
		Index:   "1",
		Running: true,
		Networks: []poolMetricNetwork{{
			IPv4: "172.21.0.2",
		}},
		Ports: []httpIngressPort{{
			HostIP:   "127.0.0.1",
			HostPort: "45678",
		}},
	}
}

func encodeHTTPIngress(tb testing.TB, container httpIngressContainer) []byte {
	tb.Helper()
	data, err := json.Marshal(container)
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

func TestHTTPIngressRequiresExactOwnedNginxAndPublishedPort(t *testing.T) {
	origin, container := httpIngressFixture(t)
	route, err := decodeHTTPIngress(container.Project, container.ID, origin, encodeHTTPIngress(t, container))
	if err != nil || route.metadata.NginxEndpoint != "172.21.0.2:8080" || route.logicalHost != origin.Host || route.metadata.PublishedEndpoint != origin.Host {
		t.Fatalf("valid owned route rejected: %+v %v", route, err)
	}
	for _, mutate := range []func(c *httpIngressContainer){
		func(c *httpIngressContainer) {
			c.Project = "production"
		},
		func(c *httpIngressContainer) {
			c.ID = strings.Repeat("b", 64)
		},
		func(c *httpIngressContainer) {
			c.Service = "api"
		},
		func(c *httpIngressContainer) {
			c.Index = "2"
		},
		func(c *httpIngressContainer) {
			c.Index = "01"
		},
		func(c *httpIngressContainer) {
			c.Running = false
		},
		func(c *httpIngressContainer) {
			c.Ports = nil
		},
		func(c *httpIngressContainer) {
			c.Ports = append(c.Ports, c.Ports[0])
		},
		func(c *httpIngressContainer) {
			c.Ports[0].HostIP = "0.0.0.0"
		},
		func(c *httpIngressContainer) {
			c.Ports[0].HostPort = "45679"
		},
		func(c *httpIngressContainer) {
			c.Networks = nil
		},
		func(c *httpIngressContainer) {
			c.Networks = append(c.Networks, c.Networks[0])
		},
		func(c *httpIngressContainer) {
			c.Networks[0].IPv4 = "8.8.8.8"
		},
		func(c *httpIngressContainer) {
			c.Networks[0].IPv4 = "127.0.0.1"
		},
		func(c *httpIngressContainer) {
			c.Networks[0].IPv6 = "fd00::1"
		},
	} {
		origin, bad := httpIngressFixture(t)
		mutate(&bad)
		if _, err := decodeHTTPIngress(container.Project, container.ID, origin, encodeHTTPIngress(t, bad)); err == nil {
			t.Fatalf("unsafe Nginx route admitted: %+v", bad)
		}
	}
	for _, data := range [][]byte{[]byte("bad"), []byte("null"), append(encodeHTTPIngress(t, container), encodeHTTPIngress(t, container)...)} {
		if _, err := decodeHTTPIngress(container.Project, container.ID, origin, data); err == nil {
			t.Fatal("malformed or extra Nginx inspect admitted")
		}
	}
}

func TestHTTPIngressInspectTemplateAndMetadataContainNoEnvironment(t *testing.T) {
	origin, container := httpIngressFixture(t)
	fixture := map[string]any{
		"Id": container.ID,
		"Config": map[string]any{
			"Env": []string{"SECRET=must-not-appear"},
			"Labels": map[string]string{
				"com.docker.compose.project":          container.Project,
				"com.docker.compose.service":          "nginx",
				"com.docker.compose.container-number": "1",
			},
		},
		"State": map[string]bool{
			"Running": true,
		},
		"NetworkSettings": map[string]any{
			"Networks": map[string]any{
				"default": map[string]string{
					"IPAddress":         "172.21.0.2",
					"GlobalIPv6Address": "",
				},
			},
			"Ports": map[string]any{
				"8080/tcp": container.Ports,
			},
		},
	}
	inspect := template.Must(template.New("inspect").Funcs(template.FuncMap{
		"json": func(value any) (string, error) {
			data, err := json.Marshal(value)
			return string(data), err
		},
	}).Parse(httpIngressInspectFormat))
	var output bytes.Buffer
	if err := inspect.Execute(&output, fixture); err != nil {
		t.Fatal(err)
	}
	route, err := decodeHTTPIngress(container.Project, container.ID, origin, output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(route.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String()+string(metadata), "SECRET") || strings.Contains(output.String()+string(metadata), "must-not-appear") {
		t.Fatal("projected Nginx ownership leaked environment")
	}
	t.Setenv("LOAD_HTTP_INGRESS_TRANSPORT", "published")
	directory := t.TempDir()
	if _, err := prepareHTTPIngress(t.Context(), &deployment{
		origin: origin.String(),
	}, directory); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "http-ingress.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved httpIngressMetadata
	if err := json.Unmarshal(data, &saved); err != nil || saved.Transport != "published" || saved.LogicalOrigin != origin.String() || saved.ContainerID != "" {
		t.Fatalf("published metadata changed or invented ownership: %+v %v", saved, err)
	}
}

func TestHTTPIngressDirectDialPreservesHTTPRequests(t *testing.T) {
	const logicalOrigin = "http://127.0.0.1:45678"
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			var received atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || r.Method != method || r.Host != "127.0.0.1:45678" || r.URL.RequestURI() != "/flags?load_request=7" ||
					r.Header.Get("Authorization") != "test-token" || r.Header.Get("Origin") != logicalOrigin || string(body) != `["test-flag"]` {
					t.Error("direct dial changed the logical HTTP request")
				}
				received.Add(1)
				_, _ = io.WriteString(w, "{}")
			}))
			defer server.Close()
			route := &httpIngressRoute{
				logicalHost: "127.0.0.1:45678",
				metadata: httpIngressMetadata{
					Transport:     "direct",
					LogicalOrigin: logicalOrigin,
					NginxEndpoint: server.Listener.Addr().String(),
				},
			}
			report := attack(t.Context(), attackOptions{
				ingress:  route,
				rps:      20,
				duration: 100 * time.Millisecond,
				workers:  2,
				target: vegeta.NewStaticTargeter(vegeta.Target{
					Method: method,
					URL:    logicalOrigin + "/flags?load_request=7",
					Body:   []byte(`["test-flag"]`),
					Header: http.Header{
						"Authorization": {"test-token"},
						"Origin":        {logicalOrigin},
					},
				}),
				validate: validateRead(false),
			})
			if received.Load() == 0 || report.Failures != 0 || report.HTTPIngressTransport != "direct" {
				t.Fatalf("direct HTTP attack failed: received=%d report=%+v", received.Load(), report)
			}
			for _, target := range [][2]string{{"udp", route.logicalHost}, {"tcp4", route.logicalHost}, {"tcp", "127.0.0.1:45679"}, {"tcp", "example.test:80"}} {
				if conn, err := route.dialContext(t.Context(), target[0], target[1]); conn != nil || err == nil {
					t.Fatal("direct dial accepted an unexpected logical target")
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := route.dialContext(ctx, "tcp", route.logicalHost); !errors.Is(err, context.Canceled) {
				t.Fatalf("direct dial lost cancellation: %v", err)
			}
		})
	}
}

func TestHTTPIngressDirectKeepsRedirectsDisabled(t *testing.T) {
	var followed atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		_, _ = io.WriteString(w, "{}")
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer server.Close()
	route := &httpIngressRoute{
		logicalHost: "127.0.0.1:45678",
		metadata: httpIngressMetadata{
			Transport:     "direct",
			NginxEndpoint: server.Listener.Addr().String(),
		},
	}
	report := attack(t.Context(), attackOptions{
		ingress:  route,
		rps:      20,
		duration: 100 * time.Millisecond,
		workers:  2,
		target: vegeta.NewStaticTargeter(vegeta.Target{
			Method: http.MethodGet,
			URL:    "http://127.0.0.1:45678/redirect",
		}),
		validate: func(*vegeta.Result) error {
			return nil
		},
	})
	if followed.Load() != 0 || report.Statuses["302"] == 0 || report.TransportErrorsTotal != 0 {
		t.Fatalf("direct transport changed redirect policy: followed=%d report=%+v", followed.Load(), report)
	}
}
