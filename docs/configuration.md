# Configuration

`mcpscope` loads configuration from the first existing file in this order when `--config` is not set:

1. `./mcpscope.yaml`
2. `$HOME/.config/mcpscope/config.yaml`
3. `/etc/mcpscope/config.yaml`

You can always override with `--config /path/to/file.yaml`.

## Schema

```yaml
version: 1
workspace: default
environment: default
authToken: ""
notification:
  webhookUrls: []
  slackWebhookUrls: []
  pagerDutyRoutingKeys: []
  retryMaxAttempts: 3
  retryBackoffSeconds: 2
proxy:
  store: sqlite             # sqlite | postgres
  db: mcpscope.db           # sqlite file path
  dsn: ""                  # required when store=postgres
  port: 4444
  transport: stdio          # stdio | http
  retainFor: 168h
  shutdownTimeout: 30s
  maxTraces: 5000
  redactKeys:
    - apiKey
    - api_key
    - authorization
    - token
    - secret
    - password
  otel: false
```

## Rules and validation

- `version` must be `1`.
- `proxy.port` must be between `1` and `65535`.
- `proxy.transport` must be `stdio` or `http`.
- `proxy.store` must be `sqlite` or `postgres`.
- `proxy.dsn` is required when `proxy.store=postgres`.
- `proxy.retainFor` must be a valid Go duration.
- `proxy.shutdownTimeout` must be a positive Go duration.
- `proxy.maxTraces` must be `0` or greater.

## Flag precedence

CLI flags always override values from the config file.

Examples:

```bash
mcpscope proxy --config ./mcpscope.yaml --port 8080
mcpscope proxy --config ./mcpscope.yaml --log-level debug
```

## Budgets and alerts files

Budget policies and alert rules remain separate YAML files passed by flags:

- `--alerts-config /path/alerts.yaml`
- `--budgets-config /path/budgets.yaml`

If `--budgets-config` is omitted, `mcpscope` also reads `budgets:` from the alerts config file.
