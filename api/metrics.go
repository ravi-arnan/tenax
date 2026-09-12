// Package api exposes tenax over HTTP: Prometheus metrics at /metrics,
// a JSON status snapshot at /api/status, and liveness at /healthz.
package api

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/probe"
	"github.com/ravi-arnan/tenax/sysinfo"
)

// Version is the tenax build version reported in build info metrics.
const Version = "0.1.0-dev"

// MetricsSource provides the current state snapshot for metrics collection.
// It is satisfied by *agent.Agent.
type MetricsSource interface {
	Units() []unit.Unit
	SystemInfo() sysinfo.Snapshot
	Probes() []probe.ProbeStatus
}

// metricsCollector turns agent state into Prometheus metric families on
// every scrape.
type metricsCollector struct {
	src MetricsSource

	unitActive   *prometheus.Desc
	unitFailed   *prometheus.Desc
	unitCPU      *prometheus.Desc
	unitMemory   *prometheus.Desc
	cpuUser      *prometheus.Desc
	cpuSystem    *prometheus.Desc
	memUsed      *prometheus.Desc
	memTotal     *prometheus.Desc
	diskUsed     *prometheus.Desc
	probeUp      *prometheus.Desc
	probeStatus  *prometheus.Desc
	probeLatency *prometheus.Desc
}

func newMetricsCollector(src MetricsSource) *metricsCollector {
	labels := []string{"unit"}
	probeLabels := []string{"probe", "type"}

	return &metricsCollector{
		src: src,
		unitActive: prometheus.NewDesc("tenax_unit_active",
			"Unit is active (1) or not (0).", labels, nil),
		unitFailed: prometheus.NewDesc("tenax_unit_failed",
			"Unit is failed (1) or not (0).", labels, nil),
		unitCPU: prometheus.NewDesc("tenax_unit_cpu_seconds",
			"CPU seconds consumed by the unit's main process.", labels, nil),
		unitMemory: prometheus.NewDesc("tenax_unit_memory_bytes",
			"Memory bytes used by the unit's main process.", labels, nil),
		cpuUser: prometheus.NewDesc("tenax_cpu_user_percent",
			"System-wide CPU user percentage.", nil, nil),
		cpuSystem: prometheus.NewDesc("tenax_cpu_system_percent",
			"System-wide CPU system percentage.", nil, nil),
		memUsed: prometheus.NewDesc("tenax_memory_used_bytes",
			"System memory used in bytes.", nil, nil),
		memTotal: prometheus.NewDesc("tenax_memory_total_bytes",
			"System memory total in bytes.", nil, nil),
		diskUsed: prometheus.NewDesc("tenax_disk_used_bytes",
			"Disk bytes used per mountpoint.", []string{"mountpoint", "fstype"}, nil),
		probeUp: prometheus.NewDesc("tenax_probe_up",
			"Probe is healthy (1) or not (0).", probeLabels, nil),
		probeStatus: prometheus.NewDesc("tenax_probe_status_code",
			"Probe status: 0 unknown, 1 healthy, 2 degraded, 3 down.", probeLabels, nil),
		probeLatency: prometheus.NewDesc("tenax_probe_latency_seconds",
			"Duration of the last probe run in seconds.", probeLabels, nil),
	}
}

func (c *metricsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		c.unitActive, c.unitFailed, c.unitCPU, c.unitMemory,
		c.cpuUser, c.cpuSystem, c.memUsed, c.memTotal, c.diskUsed,
		c.probeUp, c.probeStatus, c.probeLatency,
	} {
		ch <- d
	}
}

func (c *metricsCollector) Collect(ch chan<- prometheus.Metric) {
	for _, u := range c.src.Units() {
		active, failed := 0.0, 0.0
		if u.ActiveState == "active" {
			active = 1
		}
		if u.ActiveState == "failed" {
			failed = 1
		}
		ch <- prometheus.MustNewConstMetric(c.unitActive, prometheus.GaugeValue, active, u.Name)
		ch <- prometheus.MustNewConstMetric(c.unitFailed, prometheus.GaugeValue, failed, u.Name)
		if u.CPUUsage > 0 {
			ch <- prometheus.MustNewConstMetric(c.unitCPU, prometheus.CounterValue, u.CPUUsage, u.Name)
		}
		if u.MemoryBytes > 0 {
			ch <- prometheus.MustNewConstMetric(c.unitMemory, prometheus.GaugeValue, float64(u.MemoryBytes), u.Name)
		}
	}

	sys := c.src.SystemInfo()
	ch <- prometheus.MustNewConstMetric(c.cpuUser, prometheus.GaugeValue, sys.CPU.User)
	ch <- prometheus.MustNewConstMetric(c.cpuSystem, prometheus.GaugeValue, sys.CPU.System)
	ch <- prometheus.MustNewConstMetric(c.memUsed, prometheus.GaugeValue, float64(sys.Memory.UsedBytes))
	ch <- prometheus.MustNewConstMetric(c.memTotal, prometheus.GaugeValue, float64(sys.Memory.TotalBytes))
	for _, d := range sys.Disk {
		ch <- prometheus.MustNewConstMetric(c.diskUsed, prometheus.GaugeValue, float64(d.UsedBytes), d.MountPoint, d.FSType)
	}

	for _, p := range c.src.Probes() {
		up, code := 0.0, 0.0
		switch p.Status {
		case probe.StatusHealthy:
			up, code = 1, 1
		case probe.StatusDegraded:
			code = 2
		case probe.StatusDown:
			code = 3
		}
		typ := string(p.Probe.Type)
		ch <- prometheus.MustNewConstMetric(c.probeUp, prometheus.GaugeValue, up, p.Probe.Name, typ)
		ch <- prometheus.MustNewConstMetric(c.probeStatus, prometheus.GaugeValue, code, p.Probe.Name, typ)
		if p.LastResult.Latency > 0 {
			ch <- prometheus.MustNewConstMetric(c.probeLatency, prometheus.GaugeValue, p.LastResult.Latency.Seconds(), p.Probe.Name, typ)
		}
	}
}
