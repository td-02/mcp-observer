#!/bin/sh

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEMO_DIR="$ROOT/demo"
TMP_DIR="$DEMO_DIR/.tmp"
mkdir -p "$TMP_DIR"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'missing dependency for fallback: %s\n' "$1" >&2
    exit 1
  fi
}

need python
need agg
need ffmpeg

FULL_CAST="$TMP_DIR/mcpscope-demo.cast"
TEASER_CAST="$TMP_DIR/mcpscope-teaser.cast"

python "$DEMO_DIR/generate_casts.py" "$FULL_CAST" "$TEASER_CAST"

agg --theme "nord" "$FULL_CAST" "$DEMO_DIR/mcpscope-demo.gif"
agg --theme "nord" "$TEASER_CAST" "$DEMO_DIR/mcpscope-teaser.gif"

ffmpeg -i "$DEMO_DIR/mcpscope-demo.gif" \
  -vf "fps=15,scale=1280:-2:flags=lanczos" \
  -c:v libx264 -preset slow -crf 22 \
  -pix_fmt yuv420p \
  "$DEMO_DIR/mcpscope-demo.mp4" -y
