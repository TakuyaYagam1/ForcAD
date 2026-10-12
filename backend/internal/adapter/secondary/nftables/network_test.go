package nftables

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
)

func testInventory() Inventory {
	return Inventory{Teams: []InventoryTeam{
		{TeamIP: "192.0.2.10", Networks: []string{"192.0.2.0/24"}},
		{TeamIP: "2001:db8::10", Networks: []string{"2001:db8:1::/64"}},
	}}
}

func singleTeamInventory() Inventory {
	return Inventory{Teams: []InventoryTeam{{TeamIP: "192.0.2.10", Networks: []string{"192.0.2.10/32"}}}}
}

func TestInventoryFromDocumentUsesTrafficNetworks(t *testing.T) {
	document := config.Document{Teams: []config.Team{
		{IP: "192.0.2.10"},
		{IP: "2001:db8::10", TrafficCIDRs: []string{"2001:db8:1::/64"}},
	}}
	inventory, err := InventoryFromDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	want := []InventoryTeam{
		{TeamIP: "192.0.2.10", Networks: []string{"192.0.2.10/32"}},
		{TeamIP: "2001:db8::10", Networks: []string{"2001:db8:1::/64"}},
	}
	if len(inventory.Teams) != len(want) {
		t.Fatalf("teams = %#v, want %#v", inventory.Teams, want)
	}
	for index := range want {
		if inventory.Teams[index].TeamIP != want[index].TeamIP || strings.Join(inventory.Teams[index].Networks, ",") != strings.Join(want[index].Networks, ",") {
			t.Fatalf("team %d = %#v, want %#v", index, inventory.Teams[index], want[index])
		}
	}
}

func TestRenderRulesSupportsBothFamiliesAndRefusesRepeatedApply(t *testing.T) {
	inventory := testInventory()
	first, err := RenderRules(inventory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderRules(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("rendered rules changed between identical calls")
	}
	if !strings.HasPrefix(first, "create table inet forcad_monitoring\n") || strings.Contains(first, "add table inet forcad_monitoring") {
		t.Fatalf("rules do not refuse an existing table:\n%s", first)
	}
	for _, expected := range []string{
		"type filter hook forward priority -10; policy accept;",
		"ip daddr 192.0.2.0/24 counter name " + counterName("192.0.2.10", "rx"),
		"ip saddr 192.0.2.0/24 counter name " + counterName("192.0.2.10", "tx"),
		"ip6 daddr 2001:db8:1::/64 counter name " + counterName("2001:db8::10", "rx"),
		"ip6 saddr 2001:db8:1::/64 counter name " + counterName("2001:db8::10", "tx"),
	} {
		if !strings.Contains(first, expected) {
			t.Errorf("rules do not contain %q", expected)
		}
	}
	for _, line := range strings.Split(first, "\n") {
		if strings.HasPrefix(line, "add rule ") && (strings.Contains(line, " accept") || strings.Contains(line, " drop")) {
			t.Errorf("rule has a terminal verdict: %q", line)
		}
	}
}

func TestCounterNamesAndFingerprintDoNotDependOnInventoryOrder(t *testing.T) {
	inventory := testInventory()
	reordered := Inventory{Teams: []InventoryTeam{inventory.Teams[1], inventory.Teams[0]}}
	firstFingerprint, err := inventoryFingerprint(inventory)
	if err != nil {
		t.Fatal(err)
	}
	reorderedFingerprint, err := inventoryFingerprint(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != reorderedFingerprint {
		t.Fatalf("fingerprint changed after reorder: %q != %q", firstFingerprint, reorderedFingerprint)
	}
	firstRules, err := RenderRules(inventory)
	if err != nil {
		t.Fatal(err)
	}
	reorderedRules, err := RenderRules(reordered)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		counterName("192.0.2.10", "rx"), counterName("192.0.2.10", "tx"),
		counterName("2001:db8::10", "rx"), counterName("2001:db8::10", "tx"),
	} {
		if !strings.Contains(firstRules, name) || !strings.Contains(reorderedRules, name) {
			t.Errorf("stable counter name %q missing after reorder", name)
		}
	}
	changed := testInventory()
	changed.Teams[0].Networks[0] = "198.51.100.0/24"
	changedFingerprint, err := inventoryFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedFingerprint == firstFingerprint {
		t.Fatal("fingerprint did not change with the inventory")
	}
}

func TestReadInventoryValidatesSanitizedJSONIndependently(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{name: "noncanonical prefix", json: `{"teams":[{"team_ip":"192.0.2.1","networks":["192.0.2.1/24"]}]}`},
		{name: "overlapping networks", json: `{"teams":[{"team_ip":"192.0.2.1","networks":["192.0.2.0/24"]},{"team_ip":"192.0.2.2","networks":["192.0.2.0/25"]}]}`},
		{name: "mapped IPv6", json: `{"teams":[{"team_ip":"::ffff:192.0.2.1","networks":["192.0.2.1/32"]}]}`},
		{name: "duplicate field", json: `{"teams":[],"teams":[]}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "inventory.json")
			if err := os.WriteFile(path, []byte(test.json), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadInventory(path); err == nil {
				t.Fatal("accepted invalid sanitized inventory")
			}
		})
	}
}

func TestParseNftCountersMapsStableNamesAndFingerprint(t *testing.T) {
	inventory := testInventory()
	fixtures := []counterFixture{
		{teamIP: "192.0.2.10", direction: "rx", packets: 11, bytes: 110},
		{teamIP: "192.0.2.10", direction: "tx", packets: 12, bytes: 120},
		{teamIP: "2001:db8::10", direction: "rx", packets: 21, bytes: 210},
		{teamIP: "2001:db8::10", direction: "tx", packets: 22, bytes: 220},
	}
	data := counterResponse(t, inventory, fixtures)
	values, complete, err := parseNftCounters(data, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if !complete || len(values) != len(fixtures) {
		t.Fatalf("complete = %v, counter count = %d", complete, len(values))
	}
	for _, fixture := range fixtures {
		label := counterLabel{teamIP: fixture.teamIP, direction: fixture.direction}
		if got := values[label]; got.bytes != fixture.bytes || got.packets != fixture.packets {
			t.Errorf("counter %v = %#v, want packets=%d bytes=%d", label, got, fixture.packets, fixture.bytes)
		}
	}
}

func TestParseNftCountersReportsMissingAndRejectsMalformedData(t *testing.T) {
	inventory := singleTeamInventory()
	rx := counterFixture{teamIP: "192.0.2.10", direction: "rx", packets: 7, bytes: 70}
	data := counterResponse(t, inventory, []counterFixture{rx})
	values, complete, err := parseNftCounters(data, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if complete || len(values) != 1 || values[counterLabel{teamIP: rx.teamIP, direction: "rx"}].bytes != rx.bytes {
		t.Fatalf("missing-counter result = %#v, complete=%v", values, complete)
	}

	duplicate := counterResponse(t, inventory, []counterFixture{rx, rx})
	if _, _, err := parseNftCounters(duplicate, inventory); err == nil {
		t.Fatal("accepted duplicate counter")
	}

	wrongInventory := singleTeamInventory()
	wrongInventory.Teams[0].Networks[0] = "198.51.100.10/32"
	if _, _, err := parseNftCounters(data, wrongInventory); err == nil {
		t.Fatal("accepted counters from a different inventory fingerprint")
	}

	malformedNumber := strings.Replace(string(data), `"packets":7`, `"packets":7.5`, 1)
	if _, _, err := parseNftCounters([]byte(malformedNumber), inventory); err == nil {
		t.Fatal("accepted a fractional packet count")
	}
	if _, _, err := parseNftCounters([]byte(`{"nftables":[{"counter":{},"counter":{}}]}`), inventory); err == nil {
		t.Fatal("accepted duplicate JSON fields")
	}
}

func TestMetricsHandlerExportsMappedCountersAndMissingStatus(t *testing.T) {
	inventory := singleTeamInventory()
	rx := counterFixture{teamIP: "192.0.2.10", direction: "rx", packets: 7, bytes: 70}
	tx := counterFixture{teamIP: "192.0.2.10", direction: "tx", packets: 9, bytes: 90}

	goodHandler, err := NewMetricsHandler(context.Background(), inventory, counterListerFunc(func(context.Context) ([]byte, error) {
		return counterResponse(t, inventory, []counterFixture{rx, tx}), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	good := scrapeHandler(goodHandler)
	for _, expected := range []string{
		"forcad_network_scrape_success 1",
		`forcad_team_network_receive_bytes_total{team_ip="192.0.2.10"} 70`,
		`forcad_team_network_transmit_bytes_total{team_ip="192.0.2.10"} 90`,
		`forcad_team_network_receive_packets_total{team_ip="192.0.2.10"} 7`,
		`forcad_team_network_transmit_packets_total{team_ip="192.0.2.10"} 9`,
	} {
		if !strings.Contains(good, expected) {
			t.Errorf("metrics do not contain %q:\n%s", expected, good)
		}
	}

	missingHandler, err := NewMetricsHandler(context.Background(), inventory, counterListerFunc(func(context.Context) ([]byte, error) {
		return counterResponse(t, inventory, []counterFixture{rx}), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	missing := scrapeHandler(missingHandler)
	if !strings.Contains(missing, "forcad_network_scrape_success 0") || !strings.Contains(missing, "forcad_team_network_receive_bytes_total") || strings.Contains(missing, "forcad_team_network_transmit_bytes_total{") {
		t.Fatalf("missing counter was not reported as incomplete with absent series:\n%s", missing)
	}

	errorHandler, err := NewMetricsHandler(context.Background(), inventory, counterListerFunc(func(context.Context) ([]byte, error) {
		return nil, errors.New("fixture query failure")
	}))
	if err != nil {
		t.Fatal(err)
	}
	failed := scrapeHandler(errorHandler)
	if !strings.Contains(failed, "forcad_network_scrape_success 0") || strings.Contains(failed, "forcad_team_network_receive_bytes_total{") {
		t.Fatalf("failed scrape emitted counter series:\n%s", failed)
	}
}

func TestMetricsHandlerHonorsCancellationAndBoundsConcurrentScrapes(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	lister := counterListerFunc(func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return counterResponse(t, singleTeamInventory(), []counterFixture{{teamIP: "192.0.2.10", direction: "rx", packets: 1, bytes: 1}, {teamIP: "192.0.2.10", direction: "tx", packets: 1, bytes: 1}}), nil
		}
	})
	handler, err := NewMetricsHandler(context.Background(), singleTeamInventory(), lister)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_ = scrapeHandler(handler)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first scrape did not start")
	}
	second := scrapeHandler(handler)
	if !strings.Contains(second, "forcad_network_scrape_success 0") {
		t.Fatalf("concurrent scrape was not rejected as unavailable:\n%s", second)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent requests started %d nft queries, want 1", got)
	}
	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first scrape did not finish")
	}

	cancelable, cancel := context.WithCancel(context.Background())
	cancelStarted := make(chan struct{})
	cancelHandler, err := NewMetricsHandler(cancelable, singleTeamInventory(), counterListerFunc(func(ctx context.Context) ([]byte, error) {
		close(cancelStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/metrics", nil)
	responseDone := make(chan string, 1)
	go func() {
		recorder := httptest.NewRecorder()
		cancelHandler.ServeHTTP(recorder, request)
		responseDone <- recorder.Body.String()
	}()
	select {
	case <-cancelStarted:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("cancelable scrape did not start")
	}
	cancel()
	select {
	case output := <-responseDone:
		if !strings.Contains(output, "forcad_network_scrape_success 0") {
			t.Fatalf("canceled scrape status is missing:\n%s", output)
		}
	case <-time.After(time.Second):
		t.Fatal("scrape did not stop after cancellation")
	}
}

type counterFixture struct {
	teamIP    string
	direction string
	packets   uint64
	bytes     uint64
}

func counterResponse(t *testing.T, inventory Inventory, fixtures []counterFixture) []byte {
	t.Helper()
	fingerprint, err := inventoryFingerprint(inventory)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]map[string]any, 0, len(fixtures)+1)
	items = append(items, map[string]any{"metainfo": map[string]any{}})
	for _, fixture := range fixtures {
		items = append(items, map[string]any{"counter": map[string]any{
			"family": "inet", "table": tableName,
			"name":    counterName(fixture.teamIP, fixture.direction),
			"packets": fixture.packets, "bytes": fixture.bytes,
			"comment": inventoryComment(fingerprint),
		}})
	}
	data, err := json.Marshal(map[string]any{"nftables": items})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func scrapeHandler(handler http.Handler) string {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	return recorder.Body.String()
}

type counterListerFunc func(context.Context) ([]byte, error)

func (function counterListerFunc) ListCounters(ctx context.Context) ([]byte, error) {
	return function(ctx)
}

func TestParseNftCountersRejectsOversizedPayload(t *testing.T) {
	data := fmt.Appendf(nil, `{"nftables":[%q]}`, strings.Repeat("x", maxCounterOutputBytes))
	if _, _, err := parseNftCounters(data, singleTeamInventory()); err == nil {
		t.Fatal("accepted oversized nft output")
	}
}
