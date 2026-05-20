import type { CallToolResult } from '@modelcontextprotocol/sdk/types.js'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'

export interface Options {
  endpoint?: string
  token?: string
  async?: boolean
}

type ToolHandler = (args: unknown, context?: unknown) => Promise<CallToolResult> | CallToolResult
type AnyRegistrar = (...args: any[]) => unknown

type TracePayload = {
  method: string
  params?: unknown
  response?: unknown
  duration_ms: number
  error?: string
  timestamp: string
  server_name?: string
  workspace?: string
  environment?: string
}

const DEFAULT_ENDPOINT = 'http://localhost:4444'

export function wrapServer(server: McpServer, opts: Options): McpServer {
  const endpoint = normalizeEndpoint(opts.endpoint)
  const asyncMode = opts.async !== false
  const token = opts.token?.trim() ?? ''
  const serverName = inferServerName(server)

  const registerTool = (server as unknown as { registerTool?: AnyRegistrar }).registerTool
  if (registerTool) {
    ;(server as unknown as { registerTool: AnyRegistrar }).registerTool = function wrapRegisteredTool(
      this: unknown,
      ...args: any[]
    ) {
      if (args.length === 0) {
        return registerTool.call(this)
      }
      const handler = args[args.length - 1]
      const name = String(args[0] ?? 'tool')
      if (typeof handler !== 'function') {
        return registerTool.call(this, ...args)
      }
      return registerTool.call(this, ...args.slice(0, -1), wrapHandler(name, handler, serverName, endpoint, token, asyncMode))
    }
  }

  const legacyTool = (server as unknown as { tool?: AnyRegistrar }).tool
  if (legacyTool) {
    ;(server as unknown as { tool: AnyRegistrar }).tool = function wrapLegacyTool(this: unknown, ...args: any[]) {
      if (args.length === 0) {
        return legacyTool.call(this)
      }
      const handler = args[args.length - 1]
      const name = String(args[0] ?? 'tool')
      if (typeof handler !== 'function') {
        return legacyTool.call(this, ...args)
      }
      return legacyTool.call(this, ...args.slice(0, -1), wrapHandler(name, handler, serverName, endpoint, token, asyncMode))
    }
  }

  return server
}

function wrapHandler(
  method: string,
  handler: ToolHandler,
  serverName: string,
  endpoint: string,
  token: string,
  asyncMode: boolean,
): ToolHandler {
  return async function tracedHandler(this: unknown, args: unknown, context?: unknown): Promise<CallToolResult> {
    const started = Date.now()
    try {
      const response = await handler.call(this, args, context)
      await reportTrace(
        {
          method,
          params: args ?? {},
          response,
          duration_ms: Math.max(Date.now() - started, 0),
          timestamp: new Date().toISOString(),
          server_name: serverName,
          workspace: readEnv('MCPSCOPE_WORKSPACE'),
          environment: readEnv('MCPSCOPE_ENVIRONMENT'),
          error: extractError(response),
        },
        endpoint,
        token,
        asyncMode,
      )
      return response
    } catch (error) {
      await reportTrace(
        {
          method,
          params: args ?? {},
          response: serializeError(error),
          duration_ms: Math.max(Date.now() - started, 0),
          timestamp: new Date().toISOString(),
          server_name: serverName,
          workspace: readEnv('MCPSCOPE_WORKSPACE'),
          environment: readEnv('MCPSCOPE_ENVIRONMENT'),
          error: errorMessage(error),
        },
        endpoint,
        token,
        asyncMode,
      )
      throw error
    }
  }
}

async function reportTrace(payload: TracePayload, endpoint: string, token: string, asyncMode: boolean) {
  const send = async () => {
    try {
      await fetch(`${endpoint.replace(/\/+$/, '')}/api/ingest`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify(payload),
      })
    } catch {
      // Fire-and-forget by default. Observability must not break the server path.
    }
  }

  if (asyncMode) {
    void send()
    return
  }

  await send()
}

function normalizeEndpoint(endpoint?: string) {
  const trimmed = endpoint?.trim()
  return trimmed && trimmed.length > 0 ? trimmed : DEFAULT_ENDPOINT
}

function inferServerName(server: McpServer) {
  const candidate = (server as unknown as { serverInfo?: { name?: string }; name?: string; server?: { serverInfo?: { name?: string } } })
  return candidate.serverInfo?.name?.trim() || candidate.name?.trim() || candidate.server?.serverInfo?.name?.trim() || 'sdk'
}

function extractError(response: unknown) {
  if (!response || typeof response !== 'object') {
    return ''
  }

  const maybe = response as { isError?: boolean; content?: Array<{ type?: string; text?: string }> }
  if (!maybe.isError) {
    return ''
  }

  return maybe.content?.find((item) => item.type === 'text' && item.text)?.text?.trim() ?? 'tool returned an error'
}

function serializeError(error: unknown) {
  return { error: errorMessage(error) }
}

function errorMessage(error: unknown) {
  if (error instanceof Error) {
    return error.message
  }
  if (typeof error === 'string') {
    return error
  }
  return 'tool invocation failed'
}

function readEnv(name: string) {
  const value = process.env[name]
  return value && value.trim() !== '' ? value.trim() : undefined
}
