import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
export interface Options {
    endpoint?: string;
    token?: string;
    async?: boolean;
}
export declare function wrapServer(server: McpServer, opts: Options): McpServer;
//# sourceMappingURL=index.d.ts.map