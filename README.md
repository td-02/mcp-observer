# mcpscope

![mcpscope demo](demo/mcpscope-demo.gif)

Open source MCP observability: proxy traffic, inspect traces, replay calls, diff schemas, and alert on failures.

## What it does

- Proxies stdio and HTTP MCP traffic
- Captures request/response traces with latency and error state
- Serves a local dashboard for traces, latency, errors, and alerts, including trace search, time-range filtering, and alert rule editing
- Stores traces in SQLite with retention controls
- Supports workspace and environment scoping
- Enforces per-team budgets from a YAML config file
- Accepts SDK-reported traces through `POST /api/ingest`
- Ships thin SDKs for direct embedding in Go and TypeScript servers
- Evaluates built-in alert rules and delivers to webhook, Slack, or PagerDuty
- Exports traces for replay and CI checks
- Snapshots and diffs MCP schemas

## Architecture

```mermaid
flowchart LR
  Client["MCP client / agent"] --> Dashboard["mcpscope dashboard + API"]
  Dashboard --> WorkerA["Worker proxy :4445"]
  Dashboard --> WorkerB["Worker proxy :4446"]
  Dashboard --> WorkerC["Worker proxy :4447"]
  WorkerA --> ServerA["MCP server A"]
  WorkerB --> ServerB["MCP server B"]
  WorkerC --> ServerC["MCP server C"]
  Dashboard --> DB[("SQLite traces.db")]
  WorkerA --> DB
  WorkerB --> DB
  WorkerC --> DB
```

## Quick start

```bash
# 1) Download the archive for your platform:
# https://github.com/td-02/mcp-observer/releases/latest

# 2) Run a local MCP server through mcpscope:
mcpscope proxy --server ./your-mcp-server --db ./mcpscope.db
```

Open [http://localhost:4444](http://localhost:4444).

For commands with arguments:

```bash
mcpscope proxy -- uv run server.py
```

For an existing HTTP MCP server:

```bash
mcpscope proxy --transport http --upstream-url http://127.0.0.1:8080
```

For source builds:

```bash
make build
make test
```

The dashboard trace view supports text search plus `created_after` and `created_before` filtering, and the Alerts tab lets you edit, enable, disable, or delete rules in place.
The Budgets tab shows live usage by team and window.

SDKs for direct embedding:

- Go: `github.com/td-02/mcp-observer/sdk/go/mcpscope`
- TypeScript: `@mcpscope/sdk`

See [`examples/sdk-go`](examples/sdk-go/) and [`examples/sdk-typescript`](examples/sdk-typescript/) for minimal usage.

## Common flows

Run with config:

```bash
mcpscope proxy --config ./mcpscope.yaml -- uv run server.py
```

Export traces:

```bash
mcpscope export --config ./mcpscope.example.json --output traces.json --limit 200
```

Replay traces:

```bash
mcpscope replay --input traces.json -- uv run server.py
```

Fail CI on replay errors or latency regressions:

```bash
mcpscope replay --input traces.json --fail-on-error --max-latency-ms 500 -- uv run server.py
```

Check schema compatibility:

```bash
mcpscope snapshot --server ./your-mcp-server --output baseline.json
mcpscope diff baseline.json current.json --exit-code
```

Reset a budget window during an incident:

```bash
mcpscope budget reset --team team-alpha --window hour
```

## Config

Full schema: [docs/configuration.md](docs/configuration.md)

Default config search order:

1. `./mcpscope.yaml`
2. `$HOME/.config/mcpscope/config.yaml`
3. `/etc/mcpscope/config.yaml`

Key fields:

- `version`: current config schema version, `1`
- `workspace`: logical project boundary
- `environment`: logical environment like `prod` or `staging`
- `authToken`: bearer token for dashboard API access
- `notification.webhookUrls`: generic webhooks
- `notification.slackWebhookUrls`: Slack incoming webhooks
- `notification.pagerDutyRoutingKeys`: PagerDuty routing keys
- `proxy.db`: SQLite path
- `proxy.transport`: `stdio` or `http`
- `proxy.retainFor`: trace retention duration
- `proxy.maxTraces`: trace cap
- `--budgets-config`: YAML file with per-team call and token limits
- `--shutdown-timeout`: graceful shutdown timeout, default `30s`
- `--log-level`: `debug|info|warn|error`, default `info`

CLI flags override config values.

## Verified

Verified in this repo with:

- `go test ./cmd ./internal/...`
- `npm exec tsc -b` in [`dashboard/`](dashboard/)
- fresh `mcpscope.exe` build
- regenerated demo GIF from the current binary and dashboard

Stress-tested locally (HTTP upstream + proxy):

- Proxy forwarding path (`POST /`, JSON-RPC), `1200` requests, `60` workers:
  - `1200/1200` successful, `0` errors
  - `~689.65 req/s`, `85.19ms` average, `224ms` max latency
- API limiter path (`GET /api/traces`), `300` requests, `40` workers:
  - `120` successful, `180` rate-limited (`429`), `0` other errors

Clean heavy benchmark (tuned HTTP connection reuse):

- Proxy forwarding path (`POST /`, JSON-RPC), `50000` requests, `200` workers:
  - `50000/50000` successful, `0` errors
  - `~1403.52 req/s`
  - latency: `141.80ms` avg, `138ms` p50, `217ms` p95, `284ms` p99, `808ms` max
- API limiter path (`GET /api/traces`), `5000` requests, `120` workers:
  - `120` successful, `4880` rate-limited (`429`), `0` other errors

## Notes

- The dashboard served by the Go binary comes from [`dashboard/dist`](dashboard/dist), which is checked in and embedded at build time.
- `budgets-config` is separate from the main config file so you can rotate budget policy without changing proxy runtime settings.
- `/metrics` exposes Prometheus-compatible metrics.
- Rebuilding the Vite dashboard bundle currently needs Node `20.19+` or `22.12+`.
- The HTTP server exposes `/healthz` for liveness and `/readyz` for readiness.
- CI includes a smoke workflow that builds the packaged binary, starts a mock upstream, and exercises the dashboard and trace APIs end to end.

## Contributing

### Regenerating the demo

The demo GIF is fully automated. After making changes to the CLI output or adding features:

```bash
make demo
```

This requires `vhs` and `ffmpeg`. See `demo/README.md` for details.

## License

MIT
