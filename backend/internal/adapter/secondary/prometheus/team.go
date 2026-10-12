package prometheus

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/wahrwelt-kit/go-httpkit/httputil"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

const (
	unknownTeam         = "__unknown__"
	teamRefreshInterval = 15 * time.Second
	teamRefreshTimeout  = 3 * time.Second
)

type TeamReader interface {
	Teams(ctx context.Context, active bool) ([]domain.Team, error)
}

type trafficTeam struct {
	id, name, ip string
	networks     []netip.Prefix
}

type teamInventory struct{ teams []trafficTeam }

// TeamTraffic keeps attribution off the request's database path. The inventory
// contains public identity only; failed refreshes retain the last good snapshot.
type TeamTraffic struct {
	reader          TeamReader
	logger          *slog.Logger
	networks        map[string][]netip.Prefix
	trusted         []*net.IPNet
	inventory       atomic.Pointer[teamInventory]
	refreshed       atomic.Int64
	info, freshness *prometheus.Desc
	requests        *prometheus.CounterVec
	duration        *prometheus.HistogramVec
	flags           *prometheus.CounterVec
	sockets         *prometheus.GaugeVec
	upgrades        *prometheus.CounterVec
}

func NewTeamTraffic(registry prometheus.Registerer, reader TeamReader, teams []config.Team, trusted []string, logger *slog.Logger) (*TeamTraffic, error) {
	const location = "prometheus.NewTeamTraffic"
	nets, err := httputil.ParseTrustedProxyCIDRs(trusted)
	if err != nil {
		return nil, fmt.Errorf("%s - parse trusted proxies: %w", location, err)
	}
	t := &TeamTraffic{reader: reader, logger: logger, trusted: nets, networks: make(map[string][]netip.Prefix)}
	for _, team := range teams {
		address, err := netip.ParseAddr(team.IP)
		if err != nil {
			return nil, fmt.Errorf("%s - parse team IP: %w", location, err)
		}
		prefixes, err := team.TrafficNetworks()
		if err != nil {
			return nil, fmt.Errorf("%s - parse traffic networks: %w", location, err)
		}
		t.networks[address.Unmap().String()] = prefixes
	}
	t.inventory.Store(&teamInventory{})
	t.info = prometheus.NewDesc("forcad_team_info", "Configured team identity for traffic dashboards.", []string{"team_id", "team_name", "team_ip"}, nil)
	t.freshness = prometheus.NewDesc("forcad_team_inventory_refresh_timestamp_seconds", "Last successful team inventory refresh.", nil, nil)
	t.requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "forcad_team_http_requests_total", Help: "Completed REST requests by attributed team, including rejected requests."}, []string{"team_id", "route", "status"})
	t.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "forcad_team_http_request_duration_seconds", Help: "REST request duration, excluding long-lived WebSocket streams.", Buckets: prometheus.DefBuckets}, []string{"team_id", "route"})
	t.flags = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "forcad_team_flags_total", Help: "Decoded submitted flags by verified team and outcome; accepted means committed, not guaranteed delivered."}, []string{"team_id", "verdict"})
	t.sockets = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "forcad_team_websocket_connections", Help: "Open WebSocket connections by source network."}, []string{"team_id"})
	t.upgrades = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "forcad_team_websocket_upgrades_total", Help: "Successful WebSocket upgrades by source network."}, []string{"team_id"})
	registry.MustRegister(t, t.requests, t.duration, t.flags, t.sockets, t.upgrades)
	return t, nil
}

func (t *TeamTraffic) Describe(ch chan<- *prometheus.Desc) { ch <- t.info; ch <- t.freshness }
func (t *TeamTraffic) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(t.info, prometheus.GaugeValue, 1, unknownTeam, "Unknown", "")
	for _, team := range t.inventory.Load().teams {
		ch <- prometheus.MustNewConstMetric(t.info, prometheus.GaugeValue, 1, team.id, team.name, team.ip)
	}
	ch <- prometheus.MustNewConstMetric(t.freshness, prometheus.GaugeValue, float64(t.refreshed.Load()))
}

func (t *TeamTraffic) Refresh(ctx context.Context) error {
	const location = "prometheus.TeamTraffic.Refresh"
	bounded, cancel := context.WithTimeout(ctx, teamRefreshTimeout)
	defer cancel()
	teams, err := t.reader.Teams(bounded, false)
	if err != nil {
		return fmt.Errorf("%s - read team inventory: %w", location, err)
	}
	inventory := &teamInventory{teams: make([]trafficTeam, 0, len(teams))}
	for _, team := range teams {
		address, err := netip.ParseAddr(team.IP)
		if err != nil {
			return fmt.Errorf("%s - parse stored team IP: %w", location, err)
		}
		ip := address.Unmap().String()
		prefixes := t.networks[ip]
		if len(prefixes) == 0 {
			prefixes, err = (config.Team{IP: team.IP}).TrafficNetworks()
			if err != nil {
				return fmt.Errorf("%s - parse stored team address: %w", location, err)
			}
		}
		inventory.teams = append(inventory.teams, trafficTeam{id: team.ID.String(), name: team.Name, ip: ip, networks: prefixes})
	}
	t.inventory.Store(inventory)
	t.refreshed.Store(time.Now().Unix())
	return nil
}

func (t *TeamTraffic) Run(ctx context.Context) {
	ticker := time.NewTicker(teamRefreshInterval)
	defer ticker.Stop()
	for {
		if err := t.Refresh(ctx); err != nil && ctx.Err() == nil {
			t.logger.Warn("team metrics inventory refresh failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (t *TeamTraffic) source(address string) string {
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return unknownTeam
	}
	ip = ip.Unmap()
	found := unknownTeam
	for _, team := range t.inventory.Load().teams {
		for _, network := range team.networks {
			if network.Contains(ip) {
				if found != unknownTeam && found != team.id {
					return unknownTeam
				}
				found = team.id
			}
		}
	}
	return found
}

func (*TeamTraffic) ResolveTeam(ctx context.Context, team uuid.UUID) {
	if scope, ok := ctx.Value(trafficKey{}).(*requestTraffic); ok {
		scope.team.Store(team.String())
	}
}

func (t *TeamTraffic) ObserveFlags(team uuid.UUID, verdict string, count int) {
	id := team.String()
	if team == uuid.Nil() {
		id = unknownTeam
	}
	t.flags.WithLabelValues(id, verdict).Add(float64(count))
}

func (t *TeamTraffic) WebSocketOpened(ctx context.Context) func() {
	id := unknownTeam
	if scope, ok := ctx.Value(trafficKey{}).(*requestTraffic); ok {
		scope.upgraded.Store(true)
		id = scope.team.Load().(string)
	}
	gauge := t.sockets.WithLabelValues(id)
	gauge.Inc()
	t.upgrades.WithLabelValues(id).Inc()
	return gauge.Dec
}
