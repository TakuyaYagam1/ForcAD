package nftables

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	scrapeTimeout      = 5 * time.Second
	maxScrapesInFlight = 1
)

// NewMetricsHandler creates a bounded handler for the network counters.
func NewMetricsHandler(ctx context.Context, inventory Inventory, lister CounterLister) (http.Handler, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nftables.NewMetricsHandler - parent context is required")
	}
	if lister == nil {
		return nil, fmt.Errorf("nftables.NewMetricsHandler - counter lister is required")
	}
	if err := validateInventory(inventory); err != nil {
		return nil, err
	}
	return &metricsHandler{
		parent:    ctx,
		inventory: inventory,
		lister:    lister,
		inFlight:  make(chan struct{}, maxScrapesInFlight),
	}, nil
}

type metricsHandler struct {
	parent    context.Context
	inventory Inventory
	lister    CounterLister
	inFlight  chan struct{}
}

type teamSample struct {
	teamIP      string
	receive     counterValue
	transmit    counterValue
	hasReceive  bool
	hasTransmit bool
}

type scrapeSnapshot struct {
	success  bool
	duration float64
	teams    []teamSample
}

func (handler *metricsHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	started := time.Now()
	select {
	case handler.inFlight <- struct{}{}:
		defer func() { <-handler.inFlight }()
		handler.writeMetrics(writer, request, handler.scrape(request.Context(), started))
	default:
		handler.writeMetrics(writer, request, scrapeSnapshot{duration: time.Since(started).Seconds()})
	}
}

func (handler *metricsHandler) scrape(requestContext context.Context, started time.Time) scrapeSnapshot {
	snapshot := scrapeSnapshot{teams: make([]teamSample, len(handler.inventory.Teams))}
	for index, team := range handler.inventory.Teams {
		snapshot.teams[index].teamIP = team.TeamIP
	}
	ctx, cancel := context.WithTimeout(requestContext, scrapeTimeout)
	stopParent := context.AfterFunc(handler.parent, cancel) //nolint:contextcheck // Both the server lifetime and request context must cancel this scrape.
	defer func() {
		stopParent()
		cancel()
	}()

	data, err := handler.lister.ListCounters(ctx)
	if err == nil {
		var values map[counterLabel]counterValue
		values, snapshot.success, err = parseNftCounters(data, handler.inventory)
		if err == nil {
			for index := range snapshot.teams {
				teamIP := snapshot.teams[index].teamIP
				if value, ok := values[counterLabel{teamIP: teamIP, direction: "rx"}]; ok {
					snapshot.teams[index].receive = value
					snapshot.teams[index].hasReceive = true
				}
				if value, ok := values[counterLabel{teamIP: teamIP, direction: "tx"}]; ok {
					snapshot.teams[index].transmit = value
					snapshot.teams[index].hasTransmit = true
				}
			}
		}
	}
	snapshot.duration = time.Since(started).Seconds()
	return snapshot
}

func (handler *metricsHandler) writeMetrics(writer http.ResponseWriter, request *http.Request, snapshot scrapeSnapshot) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(newSnapshotCollector(snapshot)); err != nil {
		http.Error(writer, "could not register network metrics", http.StatusInternalServerError)
		return
	}
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(writer, request)
}

type snapshotCollector struct {
	snapshot        scrapeSnapshot
	success         *prometheus.Desc
	duration        *prometheus.Desc
	receiveBytes    *prometheus.Desc
	transmitBytes   *prometheus.Desc
	receivePackets  *prometheus.Desc
	transmitPackets *prometheus.Desc
}

func newSnapshotCollector(snapshot scrapeSnapshot) *snapshotCollector {
	return &snapshotCollector{
		snapshot:        snapshot,
		success:         prometheus.NewDesc("forcad_network_scrape_success", "Whether the latest network counter scrape completed.", nil, nil),
		duration:        prometheus.NewDesc("forcad_network_scrape_duration_seconds", "Time spent scraping network counters.", nil, nil),
		receiveBytes:    prometheus.NewDesc("forcad_team_network_receive_bytes_total", "Bytes received by a team.", []string{"team_ip"}, nil),
		transmitBytes:   prometheus.NewDesc("forcad_team_network_transmit_bytes_total", "Bytes transmitted by a team.", []string{"team_ip"}, nil),
		receivePackets:  prometheus.NewDesc("forcad_team_network_receive_packets_total", "Packets received by a team.", []string{"team_ip"}, nil),
		transmitPackets: prometheus.NewDesc("forcad_team_network_transmit_packets_total", "Packets transmitted by a team.", []string{"team_ip"}, nil),
	}
}

func (collector *snapshotCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- collector.success
	ch <- collector.duration
	ch <- collector.receiveBytes
	ch <- collector.transmitBytes
	ch <- collector.receivePackets
	ch <- collector.transmitPackets
}

func (collector *snapshotCollector) Collect(ch chan<- prometheus.Metric) {
	success := 0.0
	if collector.snapshot.success {
		success = 1
	}
	ch <- prometheus.MustNewConstMetric(collector.success, prometheus.GaugeValue, success)
	ch <- prometheus.MustNewConstMetric(collector.duration, prometheus.GaugeValue, collector.snapshot.duration)
	for _, team := range collector.snapshot.teams {
		if team.hasReceive {
			ch <- prometheus.MustNewConstMetric(collector.receiveBytes, prometheus.CounterValue, float64(team.receive.bytes), team.teamIP)
			ch <- prometheus.MustNewConstMetric(collector.receivePackets, prometheus.CounterValue, float64(team.receive.packets), team.teamIP)
		}
		if team.hasTransmit {
			ch <- prometheus.MustNewConstMetric(collector.transmitBytes, prometheus.CounterValue, float64(team.transmit.bytes), team.teamIP)
			ch <- prometheus.MustNewConstMetric(collector.transmitPackets, prometheus.CounterValue, float64(team.transmit.packets), team.teamIP)
		}
	}
}
