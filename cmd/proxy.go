package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"mcpscope/internal/proxy"
	"mcpscope/internal/store"
	"mcpscope/internal/telemetry"
)

func init() {
	rootCmd.AddCommand(newProxyCmd())
}

func newProxyCmd() *cobra.Command {
	var server string
	var upstreamURL string
	var port int
	var transport string
	var dbPath string
	var enableOTEL bool

	cmd := &cobra.Command{
		Use:   "proxy [command...]",
		Short: "Launch an MCP server subprocess and proxy JSON-RPC traffic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePort(port); err != nil {
				return err
			}

			normalizedTransport, err := validateTransport(transport)
			if err != nil {
				return err
			}

			target, err := resolveProxyTarget(server, upstreamURL, normalizedTransport, args)
			if err != nil {
				return err
			}

			traceStore, err := store.OpenSQLite(cmd.Context(), dbPath)
			if err != nil {
				return err
			}
			defer traceStore.Close()

			telemetryClient, err := telemetry.New(cmd.Context(), enableOTEL)
			if err != nil {
				return err
			}
			defer telemetryClient.Shutdown(cmd.Context())

			return proxy.Run(cmd.Context(), proxy.Config{
				ServerCommand: target.command,
				UpstreamURL:   target.upstreamURL,
				ServerName:    target.serverName(),
				Port:          port,
				Transport:     normalizedTransport,
				Store:         traceStore,
				Telemetry:     telemetryClient,
				Dashboard:     dashboardFS,
				Stdin:         os.Stdin,
				Stdout:        os.Stdout,
				Stderr:        os.Stderr,
			})
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Path to the MCP server binary. Use `-- <command> <args...>` to include arguments")
	cmd.Flags().StringVar(&upstreamURL, "upstream-url", "", "HTTP URL for an already-running MCP server. Only valid with --transport http")
	cmd.Flags().IntVar(&port, "port", 4444, "Proxy listen port")
	cmd.Flags().StringVar(&transport, "transport", "stdio", "Proxy transport: stdio or http")
	cmd.Flags().StringVar(&dbPath, "db", "mcpscope.db", "SQLite database path for persisted traces")
	cmd.Flags().BoolVar(&enableOTEL, "otel", false, "Enable OpenTelemetry export for intercepted MCP tool calls")

	return cmd
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("--port must be between 1 and 65535")
	}

	return nil
}

func validateTransport(transport string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(transport)); normalized {
	case "stdio", "http":
		return normalized, nil
	default:
		return "", fmt.Errorf("--transport must be either stdio or http")
	}
}

type proxyTarget struct {
	command     []string
	upstreamURL string
}

func (t proxyTarget) serverName() string {
	if t.upstreamURL != "" {
		return t.upstreamURL
	}
	if len(t.command) == 0 {
		return ""
	}
	return filepath.Base(t.command[0])
}

func resolveProxyTarget(server, upstreamURL, transport string, args []string) (proxyTarget, error) {
	server = strings.TrimSpace(server)
	upstreamURL = strings.TrimSpace(upstreamURL)

	if len(args) > 0 && server != "" {
		return proxyTarget{}, errors.New("use either --server or a command after `--`, not both")
	}

	if upstreamURL != "" && transport != "http" {
		return proxyTarget{}, errors.New("--upstream-url requires --transport http")
	}

	if upstreamURL == "" && transport == "http" && isHTTPServer(server) {
		upstreamURL = server
		server = ""
	}

	target := proxyTarget{
		command:     commandFromInputs(server, args),
		upstreamURL: upstreamURL,
	}

	switch transport {
	case "stdio":
		if target.upstreamURL != "" {
			return proxyTarget{}, errors.New("--upstream-url is not supported with --transport stdio")
		}
		if len(target.command) == 0 {
			return proxyTarget{}, errors.New("provide --server or a command after `--`")
		}
	case "http":
		if target.upstreamURL == "" && len(target.command) == 0 {
			return proxyTarget{}, errors.New("provide --upstream-url, --server, or a command after `--`")
		}
	}

	return target, nil
}

func commandFromInputs(server string, args []string) []string {
	if len(args) > 0 {
		return append([]string(nil), args...)
	}
	if server == "" {
		return nil
	}
	return []string{server}
}
