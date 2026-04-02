# Demo regeneration

This directory contains a fully scripted demo pipeline for `mcpscope`.

Primary path:

```bash
make demo
```

What it generates:

- `demo/mcpscope-demo.gif`
- `demo/mcpscope-demo.mp4`
- `demo/mcpscope-teaser.gif`

Dependencies:

- `vhs`: `go install github.com/charmbracelet/vhs@latest`
- `ffmpeg`
- `agg`: `cargo install agg`

Notes:

- `demo/mcpscope.tape` is the full recording.
- `demo/teaser.tape` is the short proxy-start teaser.
- `demo/asciinema-fallback.sh` is used if `vhs` is unavailable.
- `demo/bin/mcpscope` is a stable demo shim so the terminal recording stays deterministic.
