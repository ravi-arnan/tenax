# Roadmap

## Phase 1: Core Agent (done)

- [x] Project scaffold + Go module
- [x] systemd D-Bus wrapper (list, start, stop, restart units)
- [x] Journald log reader with ring buffer
- [x] Sysinfo collector skeleton
- [x] Agent engine (poll loop, concurrent subsystems)
- [x] Daemon entrypoint with signal handling
- [x] Full sysinfo implementation (/proc parse)
- [x] Real journald integration (sd-journal API, native cgo)
- [x] TUI: service list + status + log viewer + probes panel
- [x] CLI: `tenaxctl` (cobra)
- [x] Probe engine — HTTP, port, exec health checks
- [x] Prometheus metrics endpoint
- [x] Alerting rules (basic: threshold + notification)

## Phase 2: Single-Host Dashboard (done)

- [x] REST API (stdlib, pattern routing) — units, logs, probes, alerts, actions
- [x] Web UI — embedded server-rendered HTML dashboard (zero deps, auto-refresh, unit actions)
- [x] Config file (YAML: /etc/tenax/tenax.yaml)
- [x] Log filter + search in TUI (unit filter, severity cycling, / search, follow)
- [x] Systemd unit history (start/stop/restart timeline) — API + tenaxctl history
- [x] Resource graphs (CPU, memory over time) — samples API + TUI sparkline

## Phase 3: Multi-Host (done)

- [x] gRPC agent-to-controller protocol (proto/tenax.proto, client-streaming snapshots)
- [x] Controller daemon `tenaxc` (registry, gRPC :9731, HTTP :9730, fleet page)
- [x] Agent registration + heartbeats (uplink with reconnect backoff, liveness sweep)
- [x] Unified TUI across hosts — `tenaxc tui` fleet dashboard (group cycling)
- [x] Fleet web UI (host list + status page + group filter)
- [x] Host groups + tags (from agent config, filterable in API/HTML/TUI)

## Phase 4: Declarative Config (next)

- [ ] YAML service definition (port, health check, depends_on, env)
- [ ] Apply/diff/plan workflow
- [ ] Secret management (env file, or external store)
- [ ] Template system (per-host variables)

## Phase 5: Plugin System

- [ ] WASM plugin runtime
- [ ] Plugin SDK spec
- [ ] Built-in plugins: backup, cert renewal, DNS
- [ ] Plugin registry