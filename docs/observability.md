# Observability

`mcpscope` can export intercepted MCP calls as OpenTelemetry spans over OTLP gRPC. That lets you send traces into Jaeger directly, or into Grafana via an OTLP collector pipeline.

## Jaeger

Set the endpoint and enable OTEL export:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
./mcpscope proxy --server ./your-mcp-server --otel
```

If you use the provided compose example, Jaeger is available at `http://localhost:16686`.

## Grafana

Grafana typically consumes OTLP data through Grafana Alloy, the OpenTelemetry Collector, or Tempo. A common setup is:

1. `mcpscope` exports OTLP spans to a collector.
2. The collector forwards spans to Jaeger and/or Tempo.
3. Grafana visualizes traces and dashboards from Tempo and Prometheus-backed metrics.

Example environment variable:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
```

## Sample Grafana dashboard JSON

Use this as a starting point for a custom dashboard panel layout:

```json
{
  "title": "mcpscope Overview",
  "timezone": "browser",
  "panels": [
    {
      "type": "timeseries",
      "title": "Proxy Request Latency",
      "targets": [
        {
          "expr": "histogram_quantile(0.95, sum(rate(mcpscope_latency_bucket[5m])) by (le, method))",
          "legendFormat": "{{method}} p95"
        }
      ]
    },
    {
      "type": "table",
      "title": "Recent Error Methods",
      "targets": [
        {
          "expr": "sum(rate(mcpscope_errors_total[5m])) by (method)",
          "format": "table"
        }
      ]
    }
  ],
  "schemaVersion": 39,
  "version": 1
}
```

## Recommended flow

- Use Jaeger for request-by-request trace inspection.
- Use Grafana for aggregated operational views and alerting.
- Keep OTEL export behind `--otel` so local proxy sessions stay lightweight unless tracing is required.
