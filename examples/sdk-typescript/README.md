# SDK TypeScript Example

Minimal MCP server wrapping usage for reporting tool calls to mcpscope.

```ts
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { wrapServer } from '@mcpscope/sdk'
wrapServer(new McpServer({ name: 'example', version: '1.0.0' }), { endpoint: 'http://localhost:4444' })
```
