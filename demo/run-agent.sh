#!/bin/sh

mode=${1:-full}

sleep 1
printf '  12:34:01  →  read_file      args={path:/etc/config}    12ms   ✓\n'
sleep 1
printf '  12:34:02  →  search_web     args={query:mcpscope}      84ms   ✓\n'

if [ "$mode" = "teaser" ]; then
  sleep 2
  exit 0
fi

sleep 2
printf '  \033[33m12:34:04  →  run_query      args={sql:SELECT * FROM}   503ms  ⚠  LATENCY SPIKE (P99)\033[0m\n'
sleep 2
printf '  12:34:06  →  write_file     args={path:/tmp/out.txt}   18ms   ✓\n'
sleep 2
