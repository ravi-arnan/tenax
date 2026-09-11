# Roadmap

## Phase 1: Core Agent (current)

- [x] Project scaffold + Go module
- [x] systemd D-Bus wrapper (list, start, stop, restart units)
- [x] Journald log reader with ring buffer
- [x] Sysinfo collector skeleton
- [x] Agent engine (poll loop, concurrent subsystems)
- [x] Daemon entrypoint with signal handling
- [ ] Full sysinfo implementation (gopsutil or /proc parse)
- [ ] Real journald integration (sd-journal API)
- [ ] TUI: service list + status + log viewer
- [ ] TTI: `tenaxctl` CLI (cobra)
- [ ] Probe engine — HTTP, port, exec health checks
- [ ] Prometheus metrics endpoint
- [ ] Alerting rules (basic: threshold + notification)

## Phase 2: Single-Host Dashboard

- [ ] REST API (chi or stdlib)
- [ ] Web UI (HTMX or Svelte) — service dashboard
- [ ] Config file (YAML: /etc/tenax/tenax.yaml)
- [ ] Log filter + search in TUI
- [ ] Systemd unit history (start/stop/restart timeline)
- [ ] Resource graphs (CPU, memory, disk over time)

## Phase 3: Multi-Host

- [ ] gRPC agent-to-controller protocol
- [ ] Controller daemon (collects from multiple agents)
- [ ] Agent registration + heartbeats
- [ ] Unified TUI across hosts
- [ ] Unified web UI
- [ ] Host groups + tags

## Phase 4: Declarative Config

- [ ] YAML service definition (port, health check, depends_on, env)
- [ ] Apply/diff/plan workflow
- [ ] Secret management (env file, or external store)
- [ ] Template system (per-host variables)

## Phase 5: Plugin System

- [ ] WASM plugin runtime
- [ ] Plugin SDK spec
- [ ] Built-in plugins: backup, cert renewal, DNS
- [ ] Plugin registry