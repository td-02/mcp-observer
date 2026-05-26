# Operations

## Docker

Build and run:

```bash
docker build -t ghcr.io/td-02/mcpscope:local .
docker run --rm -p 4444:4444 -v $(pwd):/work ghcr.io/td-02/mcpscope:local \
  proxy --server /work/your-mcp-server --db /work/mcpscope.db
```

## Kubernetes example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcpscope
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mcpscope
  template:
    metadata:
      labels:
        app: mcpscope
    spec:
      containers:
        - name: mcpscope
          image: ghcr.io/td-02/mcpscope:v1.0.0
          args:
            - proxy
            - --transport
            - http
            - --upstream-url
            - http://mcp-upstream.default.svc.cluster.local:8080
            - --db
            - /data/mcpscope.db
            - --shutdown-timeout
            - 30s
          ports:
            - containerPort: 4444
          volumeMounts:
            - name: data
              mountPath: /data
      volumes:
        - name: data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: mcpscope
spec:
  selector:
    app: mcpscope
  ports:
    - port: 4444
      targetPort: 4444
```

## SQLite WAL tuning

For write-heavy traffic:

- Keep DB on fast local SSD.
- Use WAL mode (default in modern SQLite workflows).
- Periodically checkpoint WAL files (`wal_checkpoint(TRUNCATE)`) during low traffic.
- Increase `--max-traces` only with corresponding disk sizing.

## Postgres sizing guidance

When using a Postgres-backed build:

- Start with max open pool size near `2 x CPU` on the DB host.
- Keep idle pool around `25-50%` of max open.
- Use statement timeouts and connection lifetime limits.
- Benchmark replay/export workloads separately from live proxy traffic.

## Runtime health and metrics

- Liveness: `GET /healthz`
- Readiness: `GET /readyz`
- Prometheus metrics: `GET /metrics`
- Live stream: `GET /events`

## Graceful shutdown

On `SIGINT`/`SIGTERM`, `mcpscope`:

1. Stops accepting new connections.
2. Waits up to `--shutdown-timeout` for in-flight requests.
3. Flushes SQLite WAL checkpoint.
4. Closes DB connections.
5. Exits cleanly.
