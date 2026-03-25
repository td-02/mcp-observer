[![Build Status](https://github.com/td-02/mcpscope/actions/workflows/ci.yml/badge.svg)](https://github.com/td-02/mcpscope/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go)](https://go.dev/)
[![Latest Release](https://img.shields.io/github/v/release/td-02/mcpscope)](https://github.com/td-02/mcpscope/releases)

# mcpscope

`mcpscope` is a lightweight MCP proxy that sits in front of any MCP server, forwards traffic unchanged, and gives you visibility that most MCP setups lack by default: structured interception logs, SQLite-backed trace persistence, OpenTelemetry export, schema snapshot tooling, and a built-in dashboard for live inspection.

## Features

- Transparent stdio and HTTP MCP proxying with unchanged JSON-RPC forwarding
- Structured interception logs with trace IDs, parsed request/response payloads, and latency metadata
- SQLite trace storage with embedded migrations and queryable APIs
- OpenTelemetry export over OTLP gRPC for Jaeger, collectors, and Grafana pipelines
- Embedded dashboard with live traces, latency percentiles, and error views
- Schema snapshot capture for MCP servers over stdio or HTTP
- Snapshot diffing with breaking-change detection for CI gating
- Docker, Docker Compose, GitHub Actions CI, and release packaging support

## Prerequisites

- Go 1.22 or Docker
- Node.js 20+ if you want to rebuild the dashboard assets locally
- An MCP server binary you want to wrap
- Optional: Jaeger or another OTLP-compatible collector if you want exported spans

## 5-Minute Quickstart

Wrap any MCP server with `mcpscope` in three commands:

```powershell
git clone https://github.com/td-02/mcpscope.git
cd mcpscope
go run . proxy --server "C:\path\to\your-mcp-server.exe"
```

By default this starts the proxy in stdio mode, writes traces into `mcpscope.db`, emits structured interception logs to `stderr`, and opens the built-in dashboard at `http://localhost:4444`.

## Dashboard

When the proxy starts, `mcpscope` serves a built-in dashboard at `http://localhost:4444`. The UI shows the latest stored traces, streams new ones over Server-Sent Events, and includes latency and error views with live filtering by server and time window.

## Architecture

```text
Caller / MCP Client
        |
        v
+------------------------------+
|          mcpscope            |
|------------------------------|
| stdio/http proxy             |
| JSON-RPC interception        |
| stderr structured logs       |
| SQLite trace store           |
| OTEL span export             |
| HTTP API + embedded dashboard|
| SSE event stream             |
+------------------------------+
        |
        v
  Target MCP Server
```

More detail is available in [docs/architecture.md](docs/architecture.md).

## Configuration

| Name | Type | Default | Description |
| --- | --- | --- | --- |
| `--server` | flag | none | Path to the MCP server binary that `mcpscope` launches as a subprocess. |
| `--port` | flag | `4444` | Proxy listen port. Used directly for HTTP transport and reserved for stdio mode compatibility. |
| `--transport` | flag | `stdio` | Proxy transport mode: `stdio` or `http`. |
| `--db` | flag | `mcpscope.db` | SQLite database path used for persisted trace storage. |
| `--otel` | flag | `false` | Enables OpenTelemetry export for intercepted MCP tool calls. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | env var | unset | OTLP gRPC endpoint for span export, for example `localhost:4317` or `jaeger:4317`. If unset, `--otel` falls back to a no-op exporter silently. |

## Docker

Build the container image:

```bash
docker build -t mcpscope:local .
```

Run with Docker Compose:

```bash
export MCP_SERVER_PATH=/absolute/path/to/linux-mcp-server
docker compose up --build
```

Enable Jaeger alongside the proxy:

```bash
export MCP_SERVER_PATH=/absolute/path/to/linux-mcp-server
export OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317
docker compose --profile observability up --build
```

Open Jaeger at `http://localhost:16686`.

## Roadmap

- Per-team budget enforcement
- Replay and mock server support
- Audit log CSV export
- Slack and PagerDuty alerts
- Hosted cloud version

## Contributing

1. Fork the repository and create a feature branch from `main`.
2. Keep changes focused and run `go mod tidy`, `go test ./...`, and `go build ./...` before opening a pull request.
3. Add or update tests whenever you change parsing, persistence, transport, telemetry, or schema tooling behavior.
4. Prefer small, reviewable commits with clear messages.

## License

MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
