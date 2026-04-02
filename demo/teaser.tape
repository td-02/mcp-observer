Output demo/mcpscope-teaser.gif

Set Shell "bash"
Set FontSize 14
Set FontFamily "JetBrains Mono"
Set Theme "Catppuccin Mocha"
Set Width 1200
Set Height 400
Set Padding 24
Set TypingSpeed 40ms

Sleep 500ms
Type "mcpscope proxy --server ./demo/fake-mcp-server.sh --db /tmp/traces.db &"
Enter
Sleep 12s
