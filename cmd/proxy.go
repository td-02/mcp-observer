package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mcpscope/internal/alerting"
	"mcpscope/internal/budget"
	"mcpscope/internal/proxy"
	"mcpscope/internal/store"
	"mcpscope/internal/telemetry"
)

func init() {
	rootCmd.AddCommand(newProxyCmd())
}

func newProxyCmd() *cobra.Command {
	var servers []string
	var upstreamURL string
	var port int
	var basePort int
	var transport string
	var dbPath string
	var enableOTEL bool
	var retainFor string
	var maxTraces int
	var redactKeys []string
	var workspace string
	var environment string
	var authToken string
	var alertsConfigPath string
	var publicURL string
	var notifyWebhooks []string
	var slackWebhooks []string
	var pagerDutyKeys []string
	var budgetsConfigPath string

	cmd := &cobra.Command{
		Use:   "proxy [command...]",
		Short: "Launch an MCP server subprocess and proxy JSON-RPC traffic",
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizedTransport, err := validateTransport(transport)
			if err != nil {
				return err
			}

			dbPath = effectiveString(cmd, "db", dbPath, loadedConfig.Proxy.DB)
			if !cmd.Flags().Changed("port") && loadedConfig.Proxy.Port > 0 {
				port = loadedConfig.Proxy.Port
			}
			if !cmd.Flags().Changed("transport") && strings.TrimSpace(loadedConfig.Proxy.Transport) != "" {
				normalizedTransport, err = validateTransport(loadedConfig.Proxy.Transport)
				if err != nil {
					return err
				}
			}
			workspace = effectiveString(cmd, "workspace", workspace, loadedConfig.Workspace)
			environment = effectiveString(cmd, "environment", environment, loadedConfig.Environment)
			authToken = effectiveString(cmd, "auth-token", authToken, loadedConfig.AuthToken)
			retainFor = effectiveString(cmd, "retain-for", retainFor, loadedConfig.Proxy.RetainFor)
			if !cmd.Flags().Changed("max-traces") && loadedConfig.Proxy.MaxTraces > 0 {
				maxTraces = loadedConfig.Proxy.MaxTraces
			}
			if !cmd.Flags().Changed("redact-key") && len(loadedConfig.Proxy.RedactKeys) > 0 {
				redactKeys = loadedConfig.Proxy.RedactKeys
			}
			if !cmd.Flags().Changed("notify-webhook") && len(loadedConfig.Notification.WebhookURLs) > 0 {
				notifyWebhooks = loadedConfig.Notification.WebhookURLs
			}
			if !cmd.Flags().Changed("notify-slack-webhook") && len(loadedConfig.Notification.SlackWebhookURLs) > 0 {
				slackWebhooks = loadedConfig.Notification.SlackWebhookURLs
			}
			if !cmd.Flags().Changed("notify-pagerduty-key") && len(loadedConfig.Notification.PagerDutyRoutingKeys) > 0 {
				pagerDutyKeys = loadedConfig.Notification.PagerDutyRoutingKeys
			}
			if !cmd.Flags().Changed("otel") && loadedConfig.Proxy.EnableOTEL {
				enableOTEL = true
			}
			if err := validatePort(port); err != nil {
				return err
			}
			if err := validatePort(basePort); err != nil {
				return err
			}

			alertsConfig, err := alerting.LoadConfig(alertsConfigPath)
			if err != nil {
				return err
			}
			budgetsConfig, err := budget.LoadConfig(budgetsConfigPath)
			if err != nil {
				return err
			}

			retentionAge, err := parseRetentionDuration(retainFor)
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

			if len(servers) <= 1 {
				targetServer := ""
				if len(servers) == 1 {
					targetServer = servers[0]
				}
				target, err := resolveProxyTarget(targetServer, upstreamURL, normalizedTransport, args)
				if err != nil {
					return err
				}

				return proxy.Run(cmd.Context(), proxy.Config{
					ServerCommand:   target.command,
					UpstreamURL:     target.upstreamURL,
					ServerName:      target.serverName(),
					Version:         buildVersion,
					Port:            port,
					Transport:       normalizedTransport,
					Workspace:       defaultWorkspace(workspace),
					Environment:     defaultEnvironment(environment),
					AuthToken:       strings.TrimSpace(authToken),
					Store:           traceStore,
					Telemetry:       telemetryClient,
					RetentionMaxAge: retentionAge,
					MaxTraceCount:   maxTraces,
					RedactKeys:      normalizeKeys(redactKeys),
					AlertingConfig:  alertsConfig,
					BudgetConfig:    budgetsConfig,
					PublicURL:       strings.TrimSpace(publicURL),
					NotifyWebhooks:  normalizeURLs(notifyWebhooks),
					SlackWebhooks:   normalizeURLs(slackWebhooks),
					PagerDutyKeys:   normalizeURLs(pagerDutyKeys),
					NotifyRetries:   defaultInt(loadedConfig.Notification.RetryMaxAttempts, 3),
					NotifyBackoff:   time.Duration(defaultInt(loadedConfig.Notification.RetryBackoffSeconds, 2)) * time.Second,
					Dashboard:       dashboardFS,
					Stdin:           os.Stdin,
					Stdout:          os.Stdout,
					Stderr:          os.Stderr,
				})
			}

			if len(args) > 0 {
				return errors.New("use repeated --server flags when running multiple MCP servers")
			}
			if strings.TrimSpace(upstreamURL) != "" {
				return errors.New("--upstream-url is not supported with multiple --server values")
			}

			return runMultiServerProxy(cmd.Context(), multiServerOptions{
				Servers:         append([]string(nil), servers...),
				DashboardPort:   port,
				BasePort:        basePort,
				Workspace:       defaultWorkspace(workspace),
				Environment:     defaultEnvironment(environment),
				AuthToken:       strings.TrimSpace(authToken),
				Store:           traceStore,
				RetentionMaxAge: retentionAge,
				MaxTraceCount:   maxTraces,
				RedactKeys:      normalizeKeys(redactKeys),
				AlertingConfig:  alertsConfig,
				BudgetConfig:    budgetsConfig,
				PublicURL:       strings.TrimSpace(publicURL),
				NotifyWebhooks:  normalizeURLs(notifyWebhooks),
				SlackWebhooks:   normalizeURLs(slackWebhooks),
				PagerDutyKeys:   normalizeURLs(pagerDutyKeys),
				NotifyRetries:   defaultInt(loadedConfig.Notification.RetryMaxAttempts, 3),
				NotifyBackoff:   time.Duration(defaultInt(loadedConfig.Notification.RetryBackoffSeconds, 2)) * time.Second,
				Dashboard:       dashboardFS,
				Stderr:          os.Stderr,
			})
		},
	}

	cmd.Flags().StringArrayVar(&servers, "server", nil, "Path to the MCP server binary or HTTP URL. Repeat for multiple servers")
	cmd.Flags().StringVar(&upstreamURL, "upstream-url", "", "HTTP URL for an already-running MCP server. Only valid with --transport http")
	cmd.Flags().IntVar(&port, "port", 4444, "Proxy listen port")
	cmd.Flags().IntVar(&basePort, "base-port", 4444, "Base port for worker proxies. Worker ports start at base-port+1")
	cmd.Flags().StringVar(&transport, "transport", "stdio", "Proxy transport: stdio or http")
	cmd.Flags().StringVar(&dbPath, "db", "mcpscope.db", "SQLite database path for persisted traces")
	cmd.Flags().BoolVar(&enableOTEL, "otel", false, "Enable OpenTelemetry export for intercepted MCP tool calls")
	cmd.Flags().StringVar(&retainFor, "retain-for", "168h", "How long traces should be retained, as a duration. Use 0 to disable age-based retention")
	cmd.Flags().IntVar(&maxTraces, "max-traces", 5000, "Maximum number of traces to retain. Use 0 to disable count-based retention")
	cmd.Flags().StringSliceVar(&redactKeys, "redact-key", []string{"apiKey", "api_key", "authorization", "token", "secret", "password"}, "JSON field names to redact before persistence and logging")
	cmd.Flags().StringVar(&workspace, "workspace", "default", "Logical workspace name for multi-project separation")
	cmd.Flags().StringVar(&environment, "environment", "default", "Logical environment name for traces, alerts, and replay/export operations")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "Bearer token required for dashboard APIs when set")
	cmd.Flags().StringVar(&alertsConfigPath, "alerts-config", "", "Path to a YAML file describing external alert rules")
	cmd.Flags().StringVar(&budgetsConfigPath, "budgets-config", "", "Path to a YAML file describing per-team budgets")
	cmd.Flags().StringVar(&publicURL, "public-url", "", "Public dashboard URL used in alert notifications")
	cmd.Flags().StringSliceVar(&notifyWebhooks, "notify-webhook", nil, "Webhook URL that receives alert state changes. Repeatable")
	cmd.Flags().StringSliceVar(&slackWebhooks, "notify-slack-webhook", nil, "Slack webhook URL that receives alert state changes. Repeatable")
	cmd.Flags().StringSliceVar(&pagerDutyKeys, "notify-pagerduty-key", nil, "PagerDuty routing key that receives alert state changes. Repeatable")

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

func parseRetentionDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("--retain-for must be a valid duration")
	}
	return duration, nil
}

func normalizeKeys(values []string) []string {
	keys := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		keys = append(keys, normalized)
	}
	return keys
}

func normalizeURLs(values []string) []string {
	urls := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		urls = append(urls, normalized)
	}
	return urls
}

func effectiveString(cmd *cobra.Command, flagName, current, fallback string) string {
	if cmd.Flags().Changed(flagName) {
		return current
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return current
}

func defaultEnvironment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	return value
}

func defaultWorkspace(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

type multiServerOptions struct {
	Servers         []string
	DashboardPort   int
	BasePort        int
	Workspace       string
	Environment     string
	AuthToken       string
	Store           store.TraceStore
	RetentionMaxAge time.Duration
	MaxTraceCount   int
	RedactKeys      []string
	AlertingConfig  *alerting.Config
	BudgetConfig    *budget.Config
	PublicURL       string
	NotifyWebhooks  []string
	SlackWebhooks   []string
	PagerDutyKeys   []string
	NotifyRetries   int
	NotifyBackoff   time.Duration
	Dashboard       fs.FS
	Telemetry       *telemetry.Client
	Stderr          io.Writer
}

type workerDeployment struct {
	serverID string
	port     int
	target   proxyTarget
}

func runMultiServerProxy(ctx context.Context, opts multiServerOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if opts.Store == nil {
		return errors.New("trace storage is required")
	}
	if len(opts.Servers) == 0 {
		return errors.New("provide at least one --server")
	}
	if err := validatePort(opts.DashboardPort); err != nil {
		return err
	}
	if err := validatePort(opts.BasePort); err != nil {
		return err
	}

	deployments, err := buildWorkerDeployments(opts.Servers, opts.BasePort, opts.DashboardPort)
	if err != nil {
		return err
	}

	runtime := proxy.NewRuntime(opts.RedactKeys)
	alertEngine, err := buildAlertingEngine(opts, opts.Store)
	if err != nil {
		return err
	}
	if alertEngine != nil {
		alertCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go alertEngine.Run(alertCtx)
	}

	dashboardCfg := proxy.Config{
		Version:         buildVersion,
		Port:            opts.DashboardPort,
		Workspace:       opts.Workspace,
		Environment:     opts.Environment,
		AuthToken:       opts.AuthToken,
		Store:           opts.Store,
		Telemetry:       opts.Telemetry,
		RetentionMaxAge: opts.RetentionMaxAge,
		MaxTraceCount:   opts.MaxTraceCount,
		RedactKeys:      opts.RedactKeys,
		AlertingEngine:  alertEngine,
		BudgetConfig:    opts.BudgetConfig,
		PublicURL:       opts.PublicURL,
		NotifyWebhooks:  opts.NotifyWebhooks,
		SlackWebhooks:   opts.SlackWebhooks,
		PagerDutyKeys:   opts.PagerDutyKeys,
		NotifyRetries:   opts.NotifyRetries,
		NotifyBackoff:   opts.NotifyBackoff,
		Dashboard:       opts.Dashboard,
		Stderr:          opts.Stderr,
		Runtime:         runtime,
	}

	router, err := buildProxyRouter(deployments)
	if err != nil {
		return err
	}

	dashboardServer, dashboardErr, err := proxy.StartHTTPServer(ctx, dashboardCfg, router)
	if err != nil {
		return err
	}
	defer func() {
		if dashboardServer != nil {
			_ = dashboardServer.Close()
		}
	}()

	errCh := make(chan error, len(deployments)+1)
	for _, deployment := range deployments {
		deployment := deployment
		go func() {
			workerCfg := proxy.Config{
				ServerCommand:   deployment.target.command,
				UpstreamURL:     deployment.target.upstreamURL,
				ServerName:      deployment.target.serverName(),
				Version:         buildVersion,
				Port:            deployment.port,
				Transport:       workerTransport(deployment.target),
				Workspace:       opts.Workspace,
				Environment:     opts.Environment,
				AuthToken:       opts.AuthToken,
				Store:           opts.Store,
				Telemetry:       opts.Telemetry,
				RetentionMaxAge: opts.RetentionMaxAge,
				MaxTraceCount:   opts.MaxTraceCount,
				RedactKeys:      opts.RedactKeys,
				BudgetConfig:    opts.BudgetConfig,
				PublicURL:       opts.PublicURL,
				NotifyWebhooks:  opts.NotifyWebhooks,
				SlackWebhooks:   opts.SlackWebhooks,
				PagerDutyKeys:   opts.PagerDutyKeys,
				NotifyRetries:   opts.NotifyRetries,
				NotifyBackoff:   opts.NotifyBackoff,
				Dashboard:       opts.Dashboard,
				Stderr:          opts.Stderr,
				Runtime:         runtime,
				ServerID:        deployment.serverID,
			}
			errCh <- proxy.Run(ctx, workerCfg)
		}()
	}
	go func() {
		errCh <- <-dashboardErr
	}()

	return <-errCh
}

func buildAlertingEngine(opts multiServerOptions, traceStore store.TraceStore) (*alerting.Engine, error) {
	if opts.AlertingConfig == nil {
		return nil, nil
	}

	engine, err := alerting.NewEngine(*opts.AlertingConfig, traceStore, alerting.Options{
		Workspace:   opts.Workspace,
		Environment: opts.Environment,
		PublicURL:   opts.PublicURL,
		Logger:      opts.Stderr,
	})
	if err != nil {
		return nil, err
	}

	return engine, nil
}

func buildWorkerDeployments(servers []string, basePort, dashboardPort int) ([]workerDeployment, error) {
	ports, err := allocateWorkerPorts(basePort, dashboardPort, len(servers))
	if err != nil {
		return nil, err
	}

	deployments := make([]workerDeployment, 0, len(servers))
	seen := make(map[string]int, len(servers))
	for i, raw := range servers {
		target, err := resolveMultiServerTarget(raw)
		if err != nil {
			return nil, err
		}
		serverID := uniqueServerID(serverIDBaseForTarget(raw, target), seen)
		deployments = append(deployments, workerDeployment{
			serverID: serverID,
			port:     ports[i],
			target:   target,
		})
	}

	return deployments, nil
}

func resolveMultiServerTarget(raw string) (proxyTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return proxyTarget{}, errors.New("server value cannot be empty")
	}
	if isHTTPServer(raw) {
		return proxyTarget{upstreamURL: raw}, nil
	}
	return proxyTarget{command: []string{raw}}, nil
}

func workerTransport(target proxyTarget) string {
	if target.upstreamURL != "" {
		return "http"
	}
	return "stdio"
}

func buildProxyRouter(deployments []workerDeployment) (http.HandlerFunc, error) {
	if len(deployments) == 0 {
		return nil, errors.New("no worker deployments available")
	}

	workers := make(map[string]string, len(deployments))
	defaultURL := ""
	for _, deployment := range deployments {
		workers[deployment.serverID] = fmt.Sprintf("http://127.0.0.1:%d", deployment.port)
		if defaultURL == "" {
			defaultURL = workers[deployment.serverID]
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		serverID := strings.TrimSpace(r.URL.Query().Get("server_id"))
		targetURL := workers[serverID]
		if targetURL == "" && len(workers) == 1 {
			targetURL = defaultURL
		}
		if targetURL == "" {
			http.Error(w, "unknown server_id", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		upstreamURL, err := url.Parse(targetURL)
		if err != nil {
			http.Error(w, "failed to build worker url", http.StatusInternalServerError)
			return
		}
		upstreamURL.Path = r.URL.Path
		upstreamURL.RawQuery = stripQueryParam(r.URL.RawQuery, "server_id")

		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL.String(), bytes.NewReader(body))
		if err != nil {
			http.Error(w, "failed to build upstream request", http.StatusInternalServerError)
			return
		}
		req.Header = r.Header.Clone()
		req.Header.Del("Host")

		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("proxy upstream request: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			return
		}
	}, nil
}

func allocateWorkerPorts(basePort, dashboardPort, count int) ([]int, error) {
	if count <= 0 {
		return nil, errors.New("at least one server is required")
	}

	ports := make([]int, count)
	for i := 0; i < count; i++ {
		port := basePort + i + 1
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("worker port %d is out of range", port)
		}
		if port == dashboardPort {
			return nil, fmt.Errorf("worker port %d conflicts with dashboard port", port)
		}
		ports[i] = port
	}

	return ports, nil
}

func uniqueServerID(base string, seen map[string]int) string {
	base = sanitizeServerID(base)
	if base == "" {
		base = "server"
	}

	count := seen[base]
	seen[base] = count + 1
	if count == 0 {
		return base
	}

	return fmt.Sprintf("%s-%d", base, count+1)
}

func serverIDBaseForTarget(raw string, target proxyTarget) string {
	if target.upstreamURL != "" {
		parsed, err := url.Parse(target.upstreamURL)
		if err == nil {
			host := strings.TrimSpace(parsed.Hostname())
			port := strings.TrimSpace(parsed.Port())
			if host != "" && port != "" {
				return host + "-" + port
			}
			if host != "" {
				return host
			}
		}
		return target.upstreamURL
	}
	if len(target.command) > 0 {
		return filepath.Base(target.command[0])
	}
	return filepath.Base(strings.TrimSpace(raw))
}

func sanitizeServerID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}

	var builder strings.Builder
	lastHyphen := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}

	return strings.Trim(builder.String(), "-")
}

func stripQueryParam(rawQuery, key string) string {
	if strings.TrimSpace(rawQuery) == "" {
		return ""
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	values.Del(key)
	return values.Encode()
}
