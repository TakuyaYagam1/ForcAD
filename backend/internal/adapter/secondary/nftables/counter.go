package nftables

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"
)

const (
	maxCounterOutputBytes = 1 << 20
	maxSourceOutputBytes  = 2 << 20
	nftCommandTimeout     = 3 * time.Second
)

type counterValue struct {
	bytes   uint64
	packets uint64
}

type counterLabel struct {
	teamIP    string
	direction string
}

// CounterLister reads named and per-source counters used by the network exporter.
type CounterLister interface {
	ListCounters(ctx context.Context) ([]byte, error)
	ListSources(ctx context.Context) ([]byte, error)
}

// ExecCounterLister runs the fixed, read-only nft counter query.
type ExecCounterLister struct{}

// ListCounters returns bounded JSON from the monitoring table.
func (ExecCounterLister) ListCounters(ctx context.Context) ([]byte, error) {
	const location = "nftables.ExecCounterLister.ListCounters"
	data, err := runNftQuery(ctx, maxCounterOutputBytes, false)
	if err != nil {
		return nil, fmt.Errorf("%s - list named counters: %w", location, err)
	}
	return data, nil
}

// ListSources returns bounded JSON for the active source-IP counters.
func (ExecCounterLister) ListSources(ctx context.Context) ([]byte, error) {
	const location = "nftables.ExecCounterLister.ListSources"
	data, err := runNftQuery(ctx, maxSourceOutputBytes, true)
	if err != nil {
		return nil, fmt.Errorf("%s - list source counters: %w", location, err)
	}
	return data, nil
}

func runNftQuery(ctx context.Context, maxBytes int, sources bool) ([]byte, error) {
	const location = "nftables.runNftQuery"
	bounded, cancel := context.WithTimeout(ctx, nftCommandTimeout)
	defer cancel()
	output := limitedOutput{
		limit:  maxBytes,
		cancel: cancel,
	}
	command := exec.CommandContext(bounded, "nft", "-j", "list", "counters", "table", "inet", tableName)
	if sources {
		command = exec.CommandContext(bounded, "nft", "-j", "list", "sets", "table", "inet", tableName)
	}
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if output.exceeded {
			return nil, fmt.Errorf("%s - nft output exceeds %d bytes", location, maxBytes)
		}
		if bounded.Err() != nil {
			return nil, fmt.Errorf("%s - nft query canceled or timed out: %w", location, bounded.Err())
		}
		return nil, fmt.Errorf("%s - nft counter query failed", location)
	}
	return output.buffer.Bytes(), nil
}

type limitedOutput struct {
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	exceeded bool
}

func (output *limitedOutput) Write(data []byte) (int, error) {
	const location = "nftables.limitedOutput.Write"
	if len(data) > output.limit-output.buffer.Len() {
		output.exceeded = true
		output.cancel()
		return 0, fmt.Errorf("%s - output limit exceeded", location)
	}
	written, err := output.buffer.Write(data)
	if err != nil {
		return written, fmt.Errorf("%s - buffer command output: %w", location, err)
	}
	return written, nil
}

func parseNftCounters(data []byte, inventory Inventory) (map[counterLabel]counterValue, bool, error) {
	const location = "nftables.parseNftCounters"
	items, err := decodeCounterResponse(data)
	if err != nil {
		return nil, false, fmt.Errorf("%s - decode counter response: %w", location, err)
	}
	fingerprint, err := inventoryFingerprint(inventory)
	if err != nil {
		return nil, false, fmt.Errorf("%s - fingerprint counter inventory: %w", location, err)
	}
	expected := expectedCounters(inventory)
	values := make(map[counterLabel]counterValue, len(expected))
	comment := inventoryComment(fingerprint)
	for _, raw := range items {
		name, value, err := decodeCounterItem(raw, comment)
		if err != nil {
			return nil, false, fmt.Errorf("%s - decode counter item: %w", location, err)
		}
		if name == "" {
			continue
		}
		label, known := expected[name]
		if !known {
			return nil, false, fmt.Errorf("%s - unexpected named counter", location)
		}
		if _, exists := values[label]; exists {
			return nil, false, fmt.Errorf("%s - duplicate named counter", location)
		}
		values[label] = value
	}
	return values, len(values) == len(expected), nil
}

func decodeCounterResponse(data []byte) ([]json.RawMessage, error) {
	const location = "nftables.decodeCounterResponse"
	if len(data) > maxCounterOutputBytes {
		return nil, fmt.Errorf("%s - nft output exceeds 1 MiB", location)
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return nil, fmt.Errorf("%s - invalid or duplicate nft JSON fields", location)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var response struct {
		Nftables []json.RawMessage `json:"nftables"`
	}
	if err := decoder.Decode(&response); err != nil || response.Nftables == nil {
		return nil, fmt.Errorf("%s - invalid nft counter response", location)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s - nft response must contain one JSON value", location)
	}
	return response.Nftables, nil
}

func decodeCounterItem(raw json.RawMessage, comment string) (string, counterValue, error) {
	const location = "nftables.decodeCounterItem"
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil || len(item) != 1 {
		return "", counterValue{}, fmt.Errorf("%s - malformed nft response item", location)
	}
	if _, ok := item["metainfo"]; ok {
		return "", counterValue{}, nil
	}
	rawCounter, ok := item["counter"]
	if !ok {
		return "", counterValue{}, fmt.Errorf("%s - unexpected nft response item", location)
	}
	name, value, err := decodeNamedCounter(rawCounter, comment)
	if err != nil {
		return "", counterValue{}, fmt.Errorf("%s - decode named counter: %w", location, err)
	}
	return name, value, nil
}

func decodeNamedCounter(raw json.RawMessage, comment string) (string, counterValue, error) {
	const location = "nftables.decodeNamedCounter"
	var counter struct {
		Family  string          `json:"family"`
		Table   string          `json:"table"`
		Name    string          `json:"name"`
		Handle  json.RawMessage `json:"handle,omitempty"`
		Comment string          `json:"comment"`
		Packets json.Number     `json:"packets"`
		Bytes   json.Number     `json:"bytes"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&counter); err != nil {
		return "", counterValue{}, fmt.Errorf("%s - malformed named counter", location)
	}
	if counter.Family != "inet" || counter.Table != tableName || counter.Name == "" {
		return "", counterValue{}, fmt.Errorf("%s - counter does not belong to the monitoring table", location)
	}
	packetCount, err := parseCounterNumber(counter.Packets)
	if err != nil {
		return "", counterValue{}, fmt.Errorf("%s - invalid packet count", location)
	}
	byteCount, err := parseCounterNumber(counter.Bytes)
	if err != nil {
		return "", counterValue{}, fmt.Errorf("%s - invalid byte count", location)
	}
	if counter.Comment != comment {
		return "", counterValue{}, fmt.Errorf("%s - counter inventory fingerprint does not match", location)
	}
	return counter.Name, counterValue{
		bytes:   byteCount,
		packets: packetCount,
	}, nil
}

func expectedCounters(inventory Inventory) map[string]counterLabel {
	result := make(map[string]counterLabel, len(inventory.Teams)*2)
	for _, team := range inventory.Teams {
		result[counterName(team.TeamIP, "rx")] = counterLabel{
			teamIP:    team.TeamIP,
			direction: "rx",
		}
		result[counterName(team.TeamIP, "tx")] = counterLabel{
			teamIP:    team.TeamIP,
			direction: "tx",
		}
	}
	return result
}

func parseCounterNumber(value json.Number) (uint64, error) {
	const location = "nftables.parseCounterNumber"
	count, err := strconv.ParseUint(value.String(), 10, 64)
	if err != nil {
		return count, fmt.Errorf("%s - parse unsigned counter: %w", location, err)
	}
	return count, nil
}
