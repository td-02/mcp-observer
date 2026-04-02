#!/bin/sh

tool_list() {
  cat <<'EOF'
{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"read_file","description":"Read a file from disk","inputSchema":{"type":"object","properties":{"path":{"type":"string","description":"Absolute file path"}},"required":["path"]},"outputSchema":{"type":"string"}},{"name":"search_web","description":"Search the web and return results","inputSchema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]},"outputSchema":{"type":"array","items":{"type":"string"}}},{"name":"run_query","description":"Run a database query","inputSchema":{"type":"object","properties":{"sql":{"type":"string"}},"required":["sql"]},"outputSchema":{"type":"object","properties":{"rows":{"type":"array"}}}},{"name":"write_file","description":"Write data to a file","inputSchema":{"type":"object","properties":{"path":{"type":"string"},"contents":{"type":"string"}},"required":["path","contents"]},"outputSchema":{"type":"boolean"}}]}}
EOF
}

call_result() {
  id="$1"
  payload="$2"
  printf '{"jsonrpc":"2.0","id":%s,"result":%s}\n' "$id" "$payload"
}

extract_id() {
  printf '%s\n' "$1" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -n 1
}

while IFS= read -r line; do
  [ -z "$line" ] && continue
  id=$(extract_id "$line")
  [ -n "$id" ] || id=1

  case "$line" in
    *'"method":"tools/list"'*|*'"method": "tools/list"'*)
      tool_list
      ;;
    *'"name":"read_file"'*|*'"name": "read_file"'*)
      sleep 0.1
      call_result "$id" '{"content":"mock file contents","latency_ms":12}'
      ;;
    *'"name":"search_web"'*|*'"name": "search_web"'*)
      sleep 0.07
      call_result "$id" '{"results":["mcpscope demo"],"latency_ms":84}'
      ;;
    *'"name":"run_query"'*|*'"name": "run_query"'*)
      sleep 0.45
      call_result "$id" '{"rows":["demo row"],"latency_ms":503}'
      ;;
    *'"name":"write_file"'*|*'"name": "write_file"'*)
      sleep 0.15
      call_result "$id" '{"written":true,"latency_ms":18}'
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
