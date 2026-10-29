package prometheus

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

// CheckerMetrics tracks completed checker work with bounded label values.
type CheckerMetrics struct {
	active           prometheus.Gauge
	actionsActive    prometheus.Gauge
	workflows        *prometheus.CounterVec
	serviceWorkflows *prometheus.CounterVec
	workflowTimeouts prometheus.Counter
	actions          *prometheus.CounterVec
	serviceActions   *prometheus.CounterVec
	actionTimeouts   *prometheus.CounterVec
	serviceTimeouts  *prometheus.CounterVec
	duration         *prometheus.HistogramVec
	serviceDuration  *prometheus.HistogramVec
	lastCompleted    *prometheus.GaugeVec
	lastActionStatus *prometheus.GaugeVec
	lastActionTime   *prometheus.GaugeVec
}

func newCheckerMetrics(registry *prometheus.Registry) *CheckerMetrics {
	m := &CheckerMetrics{
		active: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "forcad_checker_workflows_active", Help: "Checker workflows currently running in this worker.",
		}),
		actionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "forcad_checker_actions_active", Help: "Checker action slots currently occupied in this worker.",
		}),
		workflows: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "forcad_checker_workflows_total", Help: "Checker workflows durably completed by status.",
		}, []string{"status"}),
		serviceWorkflows: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "forcad_checker_service_workflows_total", Help: "Durably completed checker workflows by service and numeric status.",
		}, []string{"service", "status_code"}),
		workflowTimeouts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "forcad_checker_workflow_timeouts_total", Help: "Durably completed checker workflows that exceeded their deadline.",
		}),
		actions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "forcad_checker_actions_total", Help: "Checker actions in durably completed workflows.",
		}, []string{"action", "status"}),
		serviceActions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "forcad_checker_service_actions_total", Help: "Checker actions in durably completed workflows by service, team and numeric status.",
		}, []string{"service", "team", "action", "status_code"}),
		actionTimeouts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "forcad_checker_action_timeouts_total", Help: "Timed out checker actions in durably completed workflows.",
		}, []string{"action"}),
		serviceTimeouts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "forcad_checker_service_action_timeouts_total", Help: "Timed out checker actions by service and action in durably completed workflows.",
		}, []string{"service", "action"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "forcad_checker_action_duration_seconds", Help: "Duration of checker actions in durably completed workflows.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120},
		}, []string{"action"}),
		serviceDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "forcad_checker_service_action_duration_seconds", Help: "Checker action duration by service and action in durably completed workflows.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120},
		}, []string{"service", "action"}),
		lastCompleted: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "forcad_checker_service_last_completed_timestamp_seconds", Help: "Unix time of the most recent durably completed checker workflow by service.",
		}, []string{"service"}),
		lastActionStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "forcad_checker_service_action_last_status_code", Help: "Most recent durably completed checker action status by service, team and action.",
		}, []string{"service", "team", "action"}),
		lastActionTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "forcad_checker_service_action_last_completed_timestamp_seconds", Help: "Unix time of the most recent durably completed checker action by service, team and action.",
		}, []string{"service", "team", "action"}),
	}
	registry.MustRegister(m.active, m.actionsActive, m.workflows, m.serviceWorkflows, m.workflowTimeouts, m.actions, m.serviceActions, m.actionTimeouts, m.serviceTimeouts, m.duration, m.serviceDuration, m.lastCompleted, m.lastActionStatus, m.lastActionTime)
	return m
}

func (m *CheckerMetrics) WorkflowStarted() { m.active.Inc() }
func (m *CheckerMetrics) WorkflowEnded()   { m.active.Dec() }
func (m *CheckerMetrics) ActionStarted()   { m.actionsActive.Inc() }
func (m *CheckerMetrics) ActionEnded()     { m.actionsActive.Dec() }

func (m *CheckerMetrics) ObserveCompleted(service, team string, result domain.CheckerResult, timedOut bool) {
	if service == "" {
		service = "unknown"
	}
	if team == "" {
		team = "unknown"
	}
	m.workflows.WithLabelValues(checkerStatus(result.Status)).Inc()
	m.serviceWorkflows.WithLabelValues(service, checkerStatusCode(result.Status)).Inc()
	m.lastCompleted.WithLabelValues(service).SetToCurrentTime()
	if timedOut {
		m.workflowTimeouts.Inc()
	}
	for _, log := range result.Logs {
		action := checkerAction(log.Action)
		m.actions.WithLabelValues(action, checkerStatus(log.Status)).Inc()
		m.serviceActions.WithLabelValues(service, team, action, checkerStatusCode(log.Status)).Inc()
		m.lastActionStatus.WithLabelValues(service, team, action).Set(float64(log.Status))
		m.lastActionTime.WithLabelValues(service, team, action).SetToCurrentTime()
		if log.TimedOut {
			m.actionTimeouts.WithLabelValues(action).Inc()
			m.serviceTimeouts.WithLabelValues(service, action).Inc()
		}
		m.duration.WithLabelValues(action).Observe(float64(max(0, log.DurationMS)) / 1000)
		m.serviceDuration.WithLabelValues(service, action).Observe(float64(max(0, log.DurationMS)) / 1000)
	}
}

func checkerStatusCode(status int32) string {
	switch status {
	case 101:
		return "101"
	case 102:
		return "102"
	case 103:
		return "103"
	case 104:
		return "104"
	case 110:
		return "110"
	default:
		return "other"
	}
}

func checkerStatus(status int32) string {
	switch status {
	case 101:
		return "up"
	case 102:
		return "corrupt"
	case 103:
		return "mumble"
	case 104:
		return "down"
	default:
		return "check_failed"
	}
}

func checkerAction(action string) string {
	switch action {
	case "check", "put", "get":
		return action
	default:
		return "unknown"
	}
}

func registerCheckerJobs(registry *prometheus.Registry, pool *pgxpool.Pool) {
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "forcad_checker_jobs_open", Help: "Checker jobs without a durable result, including queued and running jobs.",
	}, func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var count int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM CheckerJobs WHERE finished_at IS NULL").Scan(&count); err != nil {
			return math.NaN()
		}
		return float64(count)
	}))
}
