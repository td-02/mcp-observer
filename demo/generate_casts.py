#!/usr/bin/env python3

import json
import sys


def header(width, height):
    return {"version": 2, "width": width, "height": height, "timestamp": 1710000000}


def add_type(events, now, text, step=0.04):
    for ch in text:
      now += step
      events.append([round(now, 3), "o", ch])
    return now


def add_output(events, now, text, delay=0.0):
    now += delay
    events.append([round(now, 3), "o", text])
    return now


def build_full():
    events = []
    now = 0.5
    prompt = "$ "

    now = add_type(events, now, prompt + "go install github.com/td-02/mcp-observer@latest")
    now = add_output(events, now, "\r\n", 0.2)
    now = add_output(events, now, "\r\n", 2.0)

    now = add_type(events, now, prompt + "mcpscope --version")
    now = add_output(events, now, "\r\n", 0.1)
    now = add_output(events, now, "mcpscope version 0.1.0 (go1.22, MIT)\r\n", 0.2)
    now = add_output(events, now, "\r\n", 1.0)

    now = add_type(events, now, prompt + "mcpscope proxy --server ./demo/fake-mcp-server.sh --db /tmp/traces.db")
    now = add_output(events, now, "\r\n", 0.1)
    for line in [
        "  mcpscope proxy v0.1.0\r\n",
        "  ─────────────────────────────────────────\r\n",
        "  Transport    stdio\r\n",
        "  Target       ./demo/fake-mcp-server.sh\r\n",
        "  Dashboard    http://localhost:4444\r\n",
        "  Trace store  /tmp/traces.db\r\n",
        "  OTEL         disabled (set OTEL_EXPORTER_OTLP_ENDPOINT to enable)\r\n",
        "  ─────────────────────────────────────────\r\n",
        "  Proxy ready. Waiting for connections...\r\n",
    ]:
        now = add_output(events, now, line, 0.3)

    now = add_output(events, now, "  12:34:01  →  read_file      args={path:/etc/config}    12ms   ✓\r\n", 1.0)
    now = add_output(events, now, "  12:34:02  →  search_web     args={query:mcpscope}      84ms   ✓\r\n", 1.0)
    now = add_output(events, now, "  \x1b[33m12:34:04  →  run_query      args={sql:SELECT * FROM}   503ms  ⚠  LATENCY SPIKE (P99)\x1b[0m\r\n", 2.0)
    now = add_output(events, now, "  12:34:06  →  write_file     args={path:/tmp/out.txt}   18ms   ✓\r\n", 2.0)
    now = add_output(events, now, "\r\n", 2.0)

    now = add_type(events, now, "# Dashboard live at http://localhost:4444")
    now = add_output(events, now, "\r\n", 0.3)
    now = add_type(events, now, "# Tool call feed · P50/P95/P99 histograms · Error rate timeline")
    now = add_output(events, now, "\r\n", 0.3)
    now = add_type(events, now, prompt + "open http://localhost:4444")
    now = add_output(events, now, "\r\n", 0.1)
    now = add_output(events, now, "# (see demo/dashboard-screenshot.png for the UI)\r\n", 0.2)
    now = add_output(events, now, "\r\n", 1.0)

    now = add_type(events, now, prompt + "mcpscope snapshot --server ./demo/fake-mcp-server.sh --output /tmp/baseline.json")
    now = add_output(events, now, "\r\n", 0.1)
    now = add_output(events, now, "snapshot saved to /tmp/baseline.json\r\n", 0.3)
    now = add_output(events, now, "\r\n", 1.0)

    now = add_type(events, now, prompt + "mcpscope snapshot --server ./demo/fake-mcp-server-v2.sh --output /tmp/current.json")
    now = add_output(events, now, "\r\n", 0.1)
    now = add_output(events, now, "snapshot saved to /tmp/current.json\r\n", 0.3)
    now = add_output(events, now, "\r\n", 1.0)

    now = add_type(events, now, prompt + "mcpscope diff /tmp/baseline.json /tmp/current.json --exit-code")
    now = add_output(events, now, "\r\n", 0.1)
    for line in [
        "\x1b[31m  mcpscope schema diff\r\n",
        "  ─────────────────────────────────────────\r\n",
        "  BREAKING  search_web   param 'limit' removed\r\n",
        "  BREAKING  run_query    return schema changed (string → object)\r\n",
        "  ─────────────────────────────────────────\r\n",
        "  2 breaking changes detected\r\n",
        "  Exit code 1\x1b[0m\r\n",
    ]:
        now = add_output(events, now, line, 0.15)
    now = add_output(events, now, "\r\n", 0.5)

    now = add_type(events, now, prompt + "OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 mcpscope proxy --server ./demo/fake-mcp-server.sh --otel")
    now = add_output(events, now, "\r\n", 0.1)
    now = add_output(events, now, "  OpenTelemetry export → localhost:4317\r\n", 0.3)
    now = add_output(events, now, "  Proxy ready.\r\n", 0.3)
    now = add_output(events, now, "^C\r\n", 0.8)
    now = add_type(events, now, prompt + "clear")
    now = add_output(events, now, "\r\n\x1b[2J\x1b[H", 0.2)
    now = add_type(events, now, prompt + "echo ''")
    now = add_output(events, now, "\r\n\r\n", 0.2)
    now = add_type(events, now, prompt + "figlet -f slant mcpscope")
    now = add_output(events, now, "\r\n", 0.2)
    for line in [
        " __  __  ___ ___ ___  ___  ___ ___ ___\r\n",
        "|  \\/  |/ __| _ \\ __|/ _ \\/ __/ _ \\ _ \\\r\n",
        "| |\\/| | (__|  _/ _|| (_) \\__ \\  __/  _/\r\n",
        "|_|  |_|\\___|_| |___|\\___/|___/\\___|_|\r\n",
    ]:
        now = add_output(events, now, line, 0.05)
    now = add_type(events, now, prompt + "echo 'github.com/td-02/mcp-observer'")
    now = add_output(events, now, "\r\ngithub.com/td-02/mcp-observer\r\n", 0.2)
    now = add_type(events, now, prompt + "echo '★  star it if this saved you a production incident'")
    now = add_output(events, now, "\r\n★  star it if this saved you a production incident\r\n", 0.2)
    now = add_output(events, now, "", 3.0)
    return header(120, 34), events


def build_teaser():
    events = []
    now = 0.5
    prompt = "$ "
    now = add_type(events, now, prompt + "mcpscope proxy --server ./demo/fake-mcp-server.sh --db /tmp/traces.db")
    now = add_output(events, now, "\r\n", 0.1)
    for line in [
        "  mcpscope proxy v0.1.0\r\n",
        "  ─────────────────────────────────────────\r\n",
        "  Transport    stdio\r\n",
        "  Target       ./demo/fake-mcp-server.sh\r\n",
        "  Dashboard    http://localhost:4444\r\n",
        "  Trace store  /tmp/traces.db\r\n",
        "  OTEL         disabled (set OTEL_EXPORTER_OTLP_ENDPOINT to enable)\r\n",
        "  ─────────────────────────────────────────\r\n",
        "  Proxy ready. Waiting for connections...\r\n",
    ]:
        now = add_output(events, now, line, 0.3)
    now = add_output(events, now, "  12:34:01  →  read_file      args={path:/etc/config}    12ms   ✓\r\n", 1.0)
    now = add_output(events, now, "  12:34:02  →  search_web     args={query:mcpscope}      84ms   ✓\r\n", 1.0)
    now = add_output(events, now, "", 2.0)
    return header(120, 22), events


def write_cast(path, hdr, events):
    with open(path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(json.dumps(hdr) + "\n")
        for event in events:
            fh.write(json.dumps(event, ensure_ascii=False) + "\n")


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: generate_casts.py FULL_CAST TEASER_CAST")
    full_header, full_events = build_full()
    teaser_header, teaser_events = build_teaser()
    write_cast(sys.argv[1], full_header, full_events)
    write_cast(sys.argv[2], teaser_header, teaser_events)


if __name__ == "__main__":
    main()
