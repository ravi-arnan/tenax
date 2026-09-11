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

Pre-alpha. Core agent scaffolded. Builds and runs. No useful features yet.

## Quick start

```sh
go install github.com/ravi-arnan/tenax/cmd/tenaxd@latest
sudo tenaxd
```

## Architecture

```
tenaxd                  single daemon binary
  ├─ unitmgr            systemd unit CRUD (D-Bus)
  ├─ log reader         journald tail + ring buffer
  ├─ sysinfo collector  CPU, memory, disk, network
  ├─ probe engine       health checks (HTTP, exec, port)
  ├─ REST API           local HTTP API
  └─ TUI                bubbletea dashboard (coming soon)
```

## Roadmap

See [docs/ROADMAP.md](./docs/ROADMAP.md).

## License

MIT