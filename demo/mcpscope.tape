Output demo/mcpscope-demo.gif

Set Shell "bash"
Set FontSize 14
Set FontFamily "JetBrains Mono"
Set Theme "Catppuccin Mocha"
Set Width 1200
Set Height 600
Set Padding 24
Set TypingSpeed 40ms
Set PlaybackSpeed 1.0

Sleep 500ms
Type "go install github.com/td-02/mcp-observer@latest"
Sleep 200ms
Enter
Sleep 2s
Type "mcpscope --version"
Enter
Sleep 1s

Sleep 500ms
Type "mcpscope proxy --server ./demo/fake-mcp-server.sh --db /tmp/traces.db &"
Enter
Sleep 11s

Sleep 500ms
Type "# Dashboard live at http://localhost:4444"
Enter
Sleep 300ms
Type "# Tool call feed · P50/P95/P99 histograms · Error rate timeline"
Enter
Sleep 300ms
Type "open http://localhost:4444"
Enter
Sleep 1s

Sleep 300ms
Type "mcpscope snapshot --server ./demo/fake-mcp-server.sh --output /tmp/baseline.json"
Enter
Sleep 1s
Type "mcpscope snapshot --server ./demo/fake-mcp-server-v2.sh --output /tmp/current.json"
Enter
Sleep 1s
Type "mcpscope diff /tmp/baseline.json /tmp/current.json --exit-code"
Enter
Sleep 500ms

Sleep 300ms
Type "OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 mcpscope proxy --server ./demo/fake-mcp-server.sh --otel"
Enter
Sleep 800ms
Sleep 300ms
Ctrl+C
Sleep 200ms
Type "clear"
Enter
Sleep 200ms
Type "echo ''"
Enter
Type "figlet -f slant mcpscope"
Enter
Sleep 300ms
Type "echo 'github.com/td-02/mcp-observer'"
Enter
Sleep 200ms
Type "echo '★  star it if this saved you a production incident'"
Enter
Sleep 3000ms
