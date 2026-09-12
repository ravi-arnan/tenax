# tenax

**tenax** — A single-binary control plane for Linux servers.

Tenax replaces the fragmented stack of systemd + monitoring + logging + config
management with one agent and one dashboard. It runs on one machine and scales
to a fleet.

```
____________________________
< tenax keeps your server up >
 ----------------------------
        \   ^__^
         \  (oo)\_______
            (__)\       )\/\
                ||----w |
                ||     ||
```

## Status

Alpha. Phases 1-4 complete: single-host agent (probes, metrics, alerting,
API, web UI, TUI), multi-host controller with fleet dashboard, and
declarative service configuration with plan/apply workflow.

- Daemon: `/etc/tenax/tenax.yaml` — [docs/tenax.example.yaml](./docs/tenax.example.yaml)
- Probes: `/etc/tenax/probes.yaml` — [docs/probes.example.yaml](./docs/probes.example.yaml)
- Alerts: `/etc/tenax/alerts.yaml` — [docs/alerts.example.yaml](./docs/alerts.example.yaml)
- Services: `/etc/tenax/services.yaml` — [docs/services.example.yaml](./docs/services.example.yaml)

> Build requires cgo with systemd headers (e.g. `nix-shell -p go systemd.dev pkg-config`).

## HTTP API

The daemon serves on `127.0.0.1:9721` (override with `TENAX_PORT` or
`api.listen` in tenax.yaml):

- `GET /` — **dashboard UI** (embedded HTML, auto-refresh, unit start/stop/restart buttons)

- `GET /metrics` — Prometheus metrics (units, CPU, memory, disk, probes)
- `GET /api/status` — JSON status snapshot
- `GET /api/units[?state=]` — tracked units
- `GET /api/units/{name}` — single unit
- `POST /api/units/{name}/start|stop|restart` — unit actions
- `GET /api/logs?unit=&priority=&limit=` — recent journal entries
- `GET /api/probes`, `GET /api/alerts` — health + rule states
- `GET /api/history/units` — unit state transition timeline
- `GET /api/history/resources` — CPU/memory samples over time
- `GET /healthz` — liveness

## Multi-Host

Run a controller, then point agents at it:

```
tenaxc                          # gRPC :9731, HTTP UI :9730
tenaxc tui                      # fleet terminal dashboard (tab = cycle group)
curl localhost:9730/api/hosts   # fleet as JSON (?group=tag filters)
open localhost:9730/            # fleet dashboard with group links

# on each agent host (tenax.yaml):
# controller:
#   endpoint: ctrl.example.com:9731
#   tags: [prod, web]
```

Agents reconnect with backoff and heartbeat every 10s; hosts go `stale`
after the liveness window (default 2m).

## CLI

```
tenaxctl status              # daemon summary
tenaxctl units [--state=]    # list units
tenaxctl start|stop|restart <unit>
tenaxctl logs [--unit=] [--limit=]
tenaxctl probes
tenaxctl history            # unit events + resource samples
tenaxctl hosts              # fleet hosts via controller

# Phase 4: declarative config
tenaxctl plan                # preview changes (dry run)
tenaxctl apply               # apply desired state
tenaxctl apply --dry-run     # same as plan
```

`TENAX_API` overrides the daemon URL.

## Quick start

```sh
go install github.com/ravi-arnan/tenax/cmd/tenaxd@latest
sudo tenaxd
```

## Architecture

```
tenaxd                  single daemon binary
  ├─ unitmgr            systemd unit CRUD (D-Bus)
  ├─ log reader         native sd-journal tail + ring buffer
  ├─ sysinfo collector  CPU, memory, disk, network
  ├─ probe engine       health checks (HTTP, exec, port)
  ├─ alerting           threshold rules, webhook/log sinks
  ├─ declarative cfg    services.yaml, plan/apply, secrets, templates
  ├─ REST API           local HTTP API + Prometheus /metrics
  └─ TUI                dashboard + full-screen logs (filter, / search, follow)
```

## Roadmap

See [docs/ROADMAP.md](./docs/ROADMAP.md).

## License

MIT