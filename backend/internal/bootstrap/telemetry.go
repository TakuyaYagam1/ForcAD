package bootstrap

import (
	"fmt"
	"log/slog"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/websocket"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/prometheus"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase/flags"
)

func newTeamTraffic(cfg config.Config, metrics *prometheus.Metrics, repo *postgres.GameStore, events *websocket.Events, submissions *flags.Service, logger *slog.Logger) (*prometheus.TeamTraffic, error) {
	const location = "bootstrap.newTeamTraffic"
	var teams []config.Team
	if cfg.ConfigPath != "" {
		document, err := config.LoadDocument(cfg.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("%s - read traffic configuration: %w", location, err)
		}
		teams = document.Teams
	}
	traffic, err := prometheus.NewTeamTraffic(metrics.Registry, repo, teams, cfg.TrustedProxyCIDRs, logger)
	if err != nil {
		return nil, fmt.Errorf("%s - initialize team metrics: %w", location, err)
	}
	events.SetObserver(traffic)
	submissions.SetObserver(traffic)
	return traffic, nil
}
