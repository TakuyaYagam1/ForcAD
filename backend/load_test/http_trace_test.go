package load_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	httpTraceRequest    = regexp.MustCompile(`"([A-Z]{1,12}) ([^" ]+) HTTP/[0-9.]+"`)
	httpTraceAccess     = regexp.MustCompile(`\[[0-9]{2}/[A-Za-z]{3}/[0-9]{4}:[0-9]{2}:[0-9]{2}:[0-9]{2} [+-][0-9]{4}\] "[A-Z]{1,12} `)
	httpTraceStatus     = regexp.MustCompile(`" ([1-5][0-9]{2}) [0-9]+ `)
	httpTraceTail       = regexp.MustCompile(` rt=([0-9.]+) uct="([^"]*)" uht="([^"]*)" urt="([^"]*)" msec=([0-9]{1,12}\.[0-9]{3}) seq=([0-9]{1,20}|-) upstream="([^"]*)" rid=([a-f0-9]{32}) cid=([0-9]{1,20})$`)
	httpTraceSeverity   = regexp.MustCompile(`\[(error|crit|alert|emerg)\]`)
	httpTraceConnection = regexp.MustCompile(`\*([0-9]{1,20}) `)
	httpTraceTiming     = regexp.MustCompile(`^[0-9., :\-]{1,160}$`)
	httpTraceReplica    = regexp.MustCompile(`-api-([0-9]{1,5})$`)
	httpTraceContainer  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type httpTraceEvent struct {
	Event           string              `json:"event"`
	Time            time.Time           `json:"time"`
	LoggedUTC       *time.Time          `json:"logged_utc,omitempty"`
	NginxUTC        *time.Time          `json:"nginx_utc,omitempty"`
	Route           string              `json:"route,omitempty"`
	Method          string              `json:"method,omitempty"`
	Status          int                 `json:"status,omitempty"`
	Sequence        string              `json:"seq,omitempty"`
	RequestID       string              `json:"request_id,omitempty"`
	Connection      string              `json:"connection,omitempty"`
	Upstream        string              `json:"upstream,omitempty"`
	RequestSeconds  float64             `json:"rt,omitempty"`
	ConnectSeconds  string              `json:"uct,omitempty"`
	HeaderSeconds   string              `json:"uht,omitempty"`
	ResponseSeconds string              `json:"urt,omitempty"`
	Category        string              `json:"category,omitempty"`
	Seen            uint64              `json:"seen,omitempty"`
	Parsed          uint64              `json:"parsed,omitempty"`
	AccessSeen      uint64              `json:"access_seen,omitempty"`
	AccessParsed    uint64              `json:"access_parsed,omitempty"`
	Retained        uint64              `json:"retained,omitempty"`
	Container       string              `json:"container,omitempty"`
	Replicas        map[string][]string `json:"api_replicas,omitempty"`
}

func httpTraceRoute(target string) string {
	path, _, _ := strings.Cut(target, "?")
	if path == "/flags" || path == "/api/events/ws" {
		return path
	}
	for _, route := range readPaths {
		if strings.TrimSuffix(path, "/") == strings.TrimSuffix(route, "/") {
			return route
		}
	}
	return "other"
}

func httpTraceUpstream(raw string) string {
	parts := strings.Split(raw, ", ")
	if len(parts) > 8 {
		return "-"
	}
	for i, part := range parts {
		address, err := netip.ParseAddrPort(part)
		if err != nil {
			return "-"
		}
		parts[i] = address.String()
	}
	return strings.Join(parts, ", ")
}

func httpTraceDuration(raw string) string {
	if httpTraceTiming.MatchString(raw) {
		return raw
	}
	return "-"
}

func httpTraceEpoch(raw string) (time.Time, bool) {
	seconds, milliseconds, ok := strings.Cut(raw, ".")
	if !ok || len(seconds) == 0 || len(seconds) > 12 || len(milliseconds) != 3 {
		return time.Time{}, false
	}
	for _, digit := range seconds + milliseconds {
		if digit < '0' || digit > '9' {
			return time.Time{}, false
		}
	}
	sec, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || sec > 253402300799 {
		// Keep timestamps within the nonnegative Unix/RFC3339 JSON range.
		return time.Time{}, false
	}
	msec, err := strconv.ParseInt(milliseconds, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, msec*int64(time.Millisecond)).UTC(), true
}

func parseHTTPTrace(line string) (httpTraceEvent, bool, bool) {
	event := httpTraceEvent{
		Time: time.Now().UTC(),
	}
	if len(line) > 16<<10 {
		return event, false, false
	}
	if timestamp, _, ok := strings.Cut(line, " "); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			parsed = parsed.UTC()
			event.LoggedUTC = &parsed
		}
	}
	if request := httpTraceRequest.FindStringSubmatch(line); request != nil {
		event.Method, event.Route = request[1], httpTraceRoute(request[2])
		switch event.Method {
		case "GET", "PUT", "POST", "DELETE", "HEAD", "OPTIONS", "PATCH":
		default:
			event.Method = "OTHER"
		}
	}
	if tail := httpTraceTail.FindStringSubmatch(line); tail != nil {
		status := httpTraceStatus.FindStringSubmatch(line)
		seconds, err := strconv.ParseFloat(tail[1], 64)
		nginxUTC, validEpoch := httpTraceEpoch(tail[5])
		if status == nil || event.Method == "" || err != nil || !validEpoch {
			return event, false, false
		}
		event.Event = "access"
		event.Status, _ = strconv.Atoi(status[1])
		event.RequestSeconds = seconds
		// Nginx's cached log-phase clock is separate from Docker log receipt.
		// Millisecond precision does not imply an independently exact completion time.
		event.NginxUTC = &nginxUTC
		event.ConnectSeconds, event.HeaderSeconds, event.ResponseSeconds = httpTraceDuration(tail[2]), httpTraceDuration(tail[3]), httpTraceDuration(tail[4])
		event.Sequence, event.Upstream, event.RequestID, event.Connection = tail[6], httpTraceUpstream(tail[7]), tail[8], tail[9]
		return event, true, seconds >= 0.5 || event.Status == 499 || event.Status >= 500
	}
	if httpTraceSeverity.MatchString(line) {
		event.Event, event.Category = "error", "nginx_error"
		for _, category := range []string{"upstream timed out", "connect() failed", "upstream prematurely closed"} {
			if strings.Contains(line, category) {
				event.Category = strings.ReplaceAll(category, " ", "_")
				break
			}
		}
		if connection := httpTraceConnection.FindStringSubmatch(line); connection != nil {
			event.Connection = connection[1]
		}
		return event, true, true
	}
	return event, false, false
}

func streamHTTPTrace(ctx context.Context, cmd *exec.Cmd, encoder *json.Encoder) error {
	const location = "load.streamHTTPTrace"
	stop := httpTraceEvent{
		Event: "stop",
	}
	err := collectDiagnosticLines(ctx, cmd, func(line string) error {
		stop.Seen++
		access := httpTraceAccess.MatchString(line)
		if access {
			stop.AccessSeen++
		}
		event, parsed, retain := parseHTTPTrace(line)
		if parsed {
			stop.Parsed++
			if access && event.Event == "access" {
				stop.AccessParsed++
			}
		}
		if retain {
			stop.Retained++
			return encoder.Encode(event)
		}
		return nil
	})
	stop.Time = time.Now().UTC()
	stop.Category = "canceled"
	if ctx.Err() == nil {
		stop.Category = "unexpected_eof"
		if err != nil {
			stop.Category = "stream_error"
		}
		// Command errors may contain raw stderr. Persist only fixed categories.
		err = fmt.Errorf("%s - %s", location, stop.Category)
	} else {
		switch {
		case stop.AccessParsed == 0:
			stop.Category = "missing_access"
			err = fmt.Errorf("%s - capture contained no parsed access events", location)
		case stop.AccessSeen != stop.AccessParsed:
			stop.Category = "unparsed_access"
			err = fmt.Errorf("%s - capture contained %d unparsed access candidates", location, stop.AccessSeen-stop.AccessParsed)
		default:
			err = nil
		}
	}
	return errors.Join(err, encoder.Encode(stop))
}

func httpTraceReplicas(ctx context.Context, d *deployment) (map[string][]string, error) {
	const location = "load.httpTraceReplicas"
	ids, err := d.command(ctx, "", "ps", "-q", "api")
	if err != nil {
		return nil, fmt.Errorf("%s - locate owned API replicas", location)
	}
	args := []string{"inspect", "--format", `{{.Name}}{{range .NetworkSettings.Networks}} {{.IPAddress}}{{end}}`}
	for _, id := range strings.Fields(string(ids)) {
		if !httpTraceContainer.MatchString(id) {
			return nil, fmt.Errorf("%s - invalid owned container ID", location)
		}
		args = append(args, id)
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(bounded, "docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s - inspect owned API addresses", location)
	}
	replicas := make(map[string][]string)
	for line := range strings.SplitSeq(string(output), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		name := httpTraceReplica.FindStringSubmatch(parts[0])
		if name == nil {
			return nil, fmt.Errorf("%s - unexpected API replica name", location)
		}
		for _, raw := range parts[1:] {
			address, err := netip.ParseAddr(raw)
			if err != nil {
				return nil, fmt.Errorf("%s - invalid API address", location)
			}
			replicas["api-"+name[1]] = append(replicas["api-"+name[1]], address.String())
		}
	}
	return replicas, nil
}

func (o *observers) traceHTTP(ctx context.Context, d *deployment, directory string) {
	const location = "load.observers.traceHTTP"
	file, err := os.OpenFile(filepath.Join(directory, "http-trace.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		o.fail(fmt.Errorf("%s - create trace: %w", location, err))
		return
	}
	defer file.Close()
	started := time.Now().UTC()
	ids, err := d.command(ctx, "", "ps", "-q", "nginx")
	container := strings.TrimSpace(string(ids))
	if err != nil || !httpTraceContainer.MatchString(container) {
		if ctx.Err() == nil {
			o.fail(fmt.Errorf("%s - require one owned Nginx container", location))
		}
		return
	}
	replicas, err := httpTraceReplicas(ctx, d)
	if err != nil {
		if ctx.Err() == nil {
			o.fail(err)
		}
		return
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(httpTraceEvent{
		Event:     "start",
		Time:      started,
		Container: container,
		Replicas:  replicas,
	}); err != nil {
		o.fail(fmt.Errorf("%s - record trace source: %w", location, err))
		return
	}
	cmd := exec.CommandContext(ctx, "docker", "logs", "--follow", "--timestamps", "--since", started.Format(time.RFC3339Nano), container)
	if err := streamHTTPTrace(ctx, cmd, encoder); err != nil {
		o.fail(err)
	}
}
