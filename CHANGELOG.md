# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

## v1.0.0

- Added production hardening middleware:
  - security headers (`X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`)
  - API rate limiting (token bucket, 120 req/min per IP on `/api/*`)
- Added Prometheus-compatible `/metrics` endpoint with trace counters, proxy duration histogram, and active connection gauge.
- Added graceful shutdown controls with `--shutdown-timeout` and signal-aware command execution.
- Added structured JSON logging baseline using `log/slog` and configurable `--log-level`.
- Added YAML config loading with default discovery order:
  - `./mcpscope.yaml`
  - `$HOME/.config/mcpscope/config.yaml`
  - `/etc/mcpscope/config.yaml`
- Added docs for configuration and operations:
  - `docs/configuration.md`
  - `docs/operations.md`
- Added release-process template: `.github/RELEASE_TEMPLATE.md`.

- Added SDK ingest support with `POST /api/ingest`, `sdk_reported` trace metadata, and thin Go / TypeScript SDKs.
- Added trace search plus `created_after` and `created_before` filtering in the dashboard and trace APIs.
- Added alert rule editing and enable/disable controls in the dashboard.
- Tightened trace timestamp handling so retention and time-range filters behave consistently.

## v0.1.0

- Added a transparent MCP proxy for stdio and HTTP transport with unchanged JSON-RPC forwarding.
- Added JSON-RPC interception with structured stderr logging, trace IDs, timestamps, and request/response parsing.
- Added a SQLite-backed trace store with migrations and queryable trace history.
- Added OpenTelemetry export with OTLP gRPC support and Jaeger example wiring.
- Added Docker and Docker Compose packaging for local proxy deployment and observability demos.
- Added an embedded web dashboard with live traces, latency percentile views, error views, and SSE updates.
- Added the `snapshot` command for capturing MCP tool schemas from stdio or HTTP servers.
- Added the `diff` command for schema comparisons, CI-friendly breaking-change detection, and JSON output.
- Added GitHub Actions CI and an example pull-request schema check workflow.
