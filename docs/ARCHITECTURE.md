# Tenax Architecture

## Vision

Tenax is a single-binary control plane for Linux homelab servers.
It replaces the fragmented stack of systemd + monitoring + config
management with one agent and one dashboard. Runs on one machine;
scales to a fleet.

## Principles

- **Single binary, zero deps.** Statically linked Go binary.
  No Python, no Node, no DB to install.
- **Systemd-native.** Wraps systemd, does not replace it.
  Reads unit state, journal, and resource control through the
  existing interfaces.
- **Local-first, multi-host-optional.** Full TUI on the host.
  Web UI and multi-agent via gRPC, optional, add later.
- **Everything programmable.** REST API from day one so every
  feature is scriptable.

## Architecture Overview

```
+------------------+
|   tenaxctl CLI   |  (future: user-facing CLI)
+--------+---------+
         |
+--------+---------+       +------------------+
|   tenaxd daemon  +<----->+   gRPC API       |
|                  |       |   (multi-host)    |
+--------+---------+       +------------------+
         |
+--------+---------+
|   Core Engine    |
|  +-------------+ |
|  | Service Mgr | |  systemd unit CRUD + status
|  +-------------+ |
|  | Probe Engine| |  health checks (HTTP, exec, port, metric)
|  +-------------+ |
|  | Log Engine  | |  journald tail + filter + cache
|  +-------------+ |
|  | Sysinfo     | |  CPU, mem, disk, net, cgroups
|  +-------------+ |
+--------+---------+
         |
+--------+---------+
|   TUI (bubbletea) |  local dashboard (future: plugin panels)
+------------------+
```

## Data Model

```go
// Unit is a simplified view of a systemd unit.
type Unit struct {
    Name        string
    Description string
    ActiveState string // active, inactive, failed, activating
    SubState    string // running, exited, dead, etc.
    LoadState   string // loaded, not-found, error
    PID         int
    CPUUsage    float64
    MemoryBytes uint64
    Uptime      time.Duration
}

// Probe checks the health of a service.
type Probe struct {
    Name     string
    Type     ProbeType // http, exec, port, tcp
    Target   string    // URL, command, host:port
    Interval time.Duration
    Timeout  time.Duration
    Retries  int
}

// ProbeResult is the outcome of a single probe run.
type ProbeResult struct {
    ProbeName string
    Success   bool
    Latency   time.Duration
    Error     string
    Timestamp time.Time
}

// LogEntry is a single journald log line.
type LogEntry struct {
    Timestamp   time.Time
    Unit        string
    Message     string
    Priority    int    // 0-7 syslog
    PID         int
    Source      string // e.g. "systemd", "kernel", "app"
}
```

## Directory Layout

```
cmd/tenaxd/        main() — daemon entrypoint
agent/             core engine: Run(), Stop(), unit manager
agent/units.go     systemd unit CRUD
agent/probes.go    probe engine
agent/logs.go      log tailer
agent/sysinfo.go   system metrics
api/               REST + gRPC handlers
pkg/unit/          systemd D-Bus wrapper
pkg/health/        probe types (HTTP, exec, port, TCP)
pkg/xdg/           XDG paths, config file discovery
service/           sdnotify, pidfile, socket activation
probe/             probe runner implementations
log/               journald reader + ring buffer
sysinfo/           CPU/mem/disk/net collectors
docs/              ARCHITECTURE.md, ROADMAP.md
web/               (future) static web UI
```

## Phase Plan

Phase 1 (now):  Core agent — unit manager, log tail, sysinfo, TUI
Phase 2:         Probe engine — health checks + alerting
Phase 3:         REST API + web UI
Phase 4:         Multi-host — gRPC agent-to-controller
Phase 5:         Declarative config — YAML service definitions
Phase 6:         Plugin system — WASM plugins + SDK