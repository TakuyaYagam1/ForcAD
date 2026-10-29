package prometheus

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestCheckerMetricsRecordCompletedActions(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := newCheckerMetrics(registry)
	metrics.WorkflowStarted()
	metrics.ActionStarted()
	metrics.ObserveCompleted("sentinel", "team one", domain.CheckerResult{
		Status: 104,
		Logs:   []domain.CheckerLog{{Action: "check", Status: 104, TimedOut: true, DurationMS: 1500}},
	}, true)
	metrics.ActionEnded()
	metrics.WorkflowEnded()

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		labels map[string]string
		want   float64
	}{
		{name: "forcad_checker_workflows_active", want: 0},
		{name: "forcad_checker_actions_active", want: 0},
		{name: "forcad_checker_workflows_total", labels: map[string]string{"status": "down"}, want: 1},
		{name: "forcad_checker_service_workflows_total", labels: map[string]string{"service": "sentinel", "status_code": "104"}, want: 1},
		{name: "forcad_checker_workflow_timeouts_total", want: 1},
		{name: "forcad_checker_actions_total", labels: map[string]string{"action": "check", "status": "down"}, want: 1},
		{name: "forcad_checker_service_actions_total", labels: map[string]string{"service": "sentinel", "team": "team one", "action": "check", "status_code": "104"}, want: 1},
		{name: "forcad_checker_action_timeouts_total", labels: map[string]string{"action": "check"}, want: 1},
		{name: "forcad_checker_service_action_timeouts_total", labels: map[string]string{"service": "sentinel", "action": "check"}, want: 1},
		{name: "forcad_checker_action_duration_seconds", labels: map[string]string{"action": "check"}, want: 1},
		{name: "forcad_checker_service_action_duration_seconds", labels: map[string]string{"service": "sentinel", "action": "check"}, want: 1},
		{name: "forcad_checker_service_action_last_status_code", labels: map[string]string{"service": "sentinel", "team": "team one", "action": "check"}, want: 104},
	} {
		t.Run(test.name, func(t *testing.T) {
			metric := findCheckerMetric(families, test.name, test.labels)
			if metric == nil {
				t.Fatalf("missing metric %s with labels %v", test.name, test.labels)
			}
			var got float64
			switch {
			case metric.Counter != nil:
				got = metric.Counter.GetValue()
			case metric.Gauge != nil:
				got = metric.Gauge.GetValue()
			case metric.Histogram != nil:
				got = float64(metric.Histogram.GetSampleCount())
			}
			if got != test.want {
				t.Fatalf("metric value = %g, want %g", got, test.want)
			}
		})
	}
}

func findCheckerMetric(families []*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			matched := len(metric.Label) == len(labels)
			for _, label := range metric.Label {
				if labels[label.GetName()] != label.GetValue() {
					matched = false
				}
			}
			if matched {
				return metric
			}
		}
	}
	return nil
}
