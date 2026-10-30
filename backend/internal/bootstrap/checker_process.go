package bootstrap

import (
	promclient "github.com/prometheus/client_golang/prometheus"

	checkerworker "github.com/TakuyaYagam1/ForcAD/internal/worker/checker"
)

func registerCheckerProcessMetrics(registry *promclient.Registry, worker *checkerworker.Worker) {
	registry.MustRegister(
		promclient.NewGaugeFunc(promclient.GaugeOpts{Name: "forcad_checker_processes_unreaped", Help: "Checker processes still awaiting OS reaping after bounded cleanup."}, func() float64 {
			count, _, _ := worker.Unreaped()
			return float64(count)
		}),
		promclient.NewGaugeFunc(promclient.GaugeOpts{Name: "forcad_checker_oldest_unreaped_seconds", Help: "Age of the oldest outstanding checker cleanup failure."}, func() float64 {
			_, age, _ := worker.Unreaped()
			return age.Seconds()
		}),
		promclient.NewCounterFunc(promclient.CounterOpts{Name: "forcad_checker_unreaped_total", Help: "Checker cleanup failures requiring worker replacement."}, func() float64 {
			_, _, count := worker.Unreaped()
			return float64(count)
		}),
		promclient.NewGaugeFunc(promclient.GaugeOpts{Name: "forcad_checker_worker_degraded", Help: "Worker admission is closed after an unreaped checker; restart required."}, func() float64 {
			if worker.Degraded() {
				return 1
			}
			return 0
		}),
	)
}

func checkerFailures(worker *checkerworker.Worker) <-chan error {
	if worker == nil {
		return nil
	}
	return worker.Failures()
}

func checkerFailure(worker *checkerworker.Worker) error {
	if worker != nil && worker.Degraded() {
		return checkerworker.ErrWorkerUnreaped
	}
	return nil
}
