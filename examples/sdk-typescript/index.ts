import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { wrapServer } from '@mcpscope/sdk'

const server = wrapServer(new McpServer({ name: 'example', version: '1.0.0' }), {
  endpoint: 'http://localhost:4444',
})

server.registerTool('hello', { description: 'Say hello' }, async () => ({
  content: [{ type: 'text', text: 'Hello from MCP' }],
}))
