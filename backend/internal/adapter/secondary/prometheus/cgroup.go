package prometheus

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

type workerCgroup struct {
	root   string
	cpu    *prometheus.Desc
	memory *prometheus.Desc
}

func registerWorkerCgroup(registry *prometheus.Registry) {
	const root = "/sys/fs/cgroup"
	if _, err := os.Stat(filepath.Join(root, "cpu.stat")); err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(root, "memory.current")); err != nil {
		return
	}
	registry.MustRegister(&workerCgroup{
		root:   root,
		cpu:    prometheus.NewDesc("forcad_worker_container_cpu_seconds_total", "CPU used by the worker container, including checker subprocesses.", nil, nil),
		memory: prometheus.NewDesc("forcad_worker_container_memory_bytes", "Memory used by the worker container, including checker subprocesses.", nil, nil),
	})
}

func (c *workerCgroup) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpu
	ch <- c.memory
}

func (c *workerCgroup) Collect(ch chan<- prometheus.Metric) {
	if data, err := os.ReadFile(filepath.Join(c.root, "cpu.stat")); err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 || fields[0] != "usage_usec" {
				continue
			}
			if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				ch <- prometheus.MustNewConstMetric(c.cpu, prometheus.CounterValue, float64(value)/1e6)
			}
			break
		}
	}
	if data, err := os.ReadFile(filepath.Join(c.root, "memory.current")); err == nil {
		if value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			ch <- prometheus.MustNewConstMetric(c.memory, prometheus.GaugeValue, float64(value))
		}
	}
}
