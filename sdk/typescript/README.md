# @mcpscope/sdk

Thin SDK for reporting MCP tool calls to a running mcpscope instance.

```ts
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { wrapServer } from '@mcpscope/sdk'
wrapServer(new McpServer({ name: 'demo', version: '1.0.0' }), { endpoint: 'http://localhost:4444' })
```
