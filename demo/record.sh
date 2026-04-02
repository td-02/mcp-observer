#!/bin/sh

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEMO_DIR="$ROOT/demo"

install_hint() {
  name=$1
  cmd=$2
  printf 'missing dependency: %s\n' "$name" >&2
  printf 'install with: %s\n' "$cmd" >&2
}

require_cmd() {
  name=$1
  hint=$2
  if ! command -v "$name" >/dev/null 2>&1; then
    install_hint "$name" "$hint"
    return 1
  fi
}

filesize_mb() {
  file=$1
  if [ ! -f "$file" ]; then
    printf 'missing'
    return
  fi
  bytes=$(wc -c <"$file" | tr -d ' ')
  awk "BEGIN { printf \"%.2f MB\", $bytes / 1048576 }"
}

find_mcpscope() {
  if [ -x "$ROOT/mcpscope" ]; then
    printf '%s\n' "$ROOT/mcpscope"
    return
  fi
  if command -v mcpscope >/dev/null 2>&1; then
    command -v mcpscope
    return
  fi
  printf 'building mcpscope binary at repo root...\n'
  (
    cd "$ROOT"
    go build -o mcpscope .
  )
  printf '%s\n' "$ROOT/mcpscope"
}

optimize_gif() {
  file=$1
  if command -v gifsicle >/dev/null 2>&1; then
    tmp="$file.tmp"
    if [ "$file" = "$DEMO_DIR/mcpscope-demo.gif" ]; then
      gifsicle -O3 --no-loopcount "$file" -o "$tmp"
    else
      gifsicle -O3 --loopcount=0 "$file" -o "$tmp"
    fi
    mv "$tmp" "$file"
  fi
}

run_vhs() {
  tape=$1
  if command -v vhs >/dev/null 2>&1; then
    vhs "$tape"
    return 0
  fi
  return 1
}

convert_mp4() {
  ffmpeg -i "$DEMO_DIR/mcpscope-demo.gif" \
    -vf "fps=15,scale=1280:-2:flags=lanczos" \
    -c:v libx264 -preset slow -crf 22 \
    -pix_fmt yuv420p \
    "$DEMO_DIR/mcpscope-demo.mp4" -y
}

require_cmd ffmpeg "brew install ffmpeg   # or sudo apt-get install ffmpeg"

if command -v figlet >/dev/null 2>&1; then
  HAS_FIGLET=1
  REAL_FIGLET=$(command -v figlet)
else
  HAS_FIGLET=0
  REAL_FIGLET=
fi

if ! command -v vhs >/dev/null 2>&1; then
  install_hint "vhs" "go install github.com/charmbracelet/vhs@latest"
  require_cmd agg "cargo install agg"
fi

MCPSCOPE_REAL_BIN=$(find_mcpscope)
export MCPSCOPE_REAL_BIN HAS_FIGLET REAL_FIGLET MCPSCOPE_DEMO_ROOT="$ROOT"

rm -f "$DEMO_DIR/mcpscope-demo.gif" "$DEMO_DIR/mcpscope-demo.mp4" "$DEMO_DIR/mcpscope-teaser.gif"

if ! run_vhs "$DEMO_DIR/mcpscope.tape"; then
  printf 'vhs unavailable, falling back to asciinema pipeline...\n'
  "$DEMO_DIR/asciinema-fallback.sh"
else
  MCPSCOPE_DEMO_MODE=teaser run_vhs "$DEMO_DIR/teaser.tape"
  convert_mp4
  optimize_gif "$DEMO_DIR/mcpscope-demo.gif"
  optimize_gif "$DEMO_DIR/mcpscope-teaser.gif"
fi

printf '[ok] demo/mcpscope-demo.gif     (README embed)\n'
printf '[ok] demo/mcpscope-demo.mp4     (LinkedIn / Twitter native upload)\n'
printf '[ok] demo/mcpscope-teaser.gif   (teaser post)\n'
printf '\nFile sizes:\n'
printf '  mcpscope-demo.gif   -> %s\n' "$(filesize_mb "$DEMO_DIR/mcpscope-demo.gif")"
printf '  mcpscope-demo.mp4   -> %s\n' "$(filesize_mb "$DEMO_DIR/mcpscope-demo.mp4")"
printf '  mcpscope-teaser.gif -> %s\n' "$(filesize_mb "$DEMO_DIR/mcpscope-teaser.gif")"
