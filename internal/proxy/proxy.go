package proxy

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcpscope/internal/alerting"
	"mcpscope/internal/auditexport"
	"mcpscope/internal/intercept"
	"mcpscope/internal/replay"
	"mcpscope/internal/store"
	"mcpscope/internal/telemetry"
)

type Config struct {
	ServerCommand   []string
	UpstreamURL     string
	ServerName      string
	Version         string
	Workspace       string
	Environment     string
	AuthToken       string
	Port            int
	Transport       string
	Store           store.TraceStore
	Telemetry       *telemetry.Client
	RetentionMaxAge time.Duration
	MaxTraceCount   int
	RedactKeys      []string
	NotifyWebhooks  []string
	SlackWebhooks   []string
	PagerDutyKeys   []string
	AlertingConfig  *alerting.Config
	PublicURL       string
	AlertingEngine  *alerting.Engine
	Runtime         *Runtime
	ServerID        string
	NotifyRetries   int
	NotifyBackoff   time.Duration
	Dashboard       fs.FS
	eventHub        *traceEventHub
	tracker         *traceTracker
	redactor        *payloadRedactor
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
}

type Runtime struct {
	eventHub *traceEventHub
	redactor *payloadRedactor
}

func NewRuntime(redactKeys []string) *Runtime {
	return &Runtime{
		eventHub: newTraceEventHub(),
		redactor: newPayloadRedactor(redactKeys),
	}
}

func prepareConfig(cfg *Config) {
	if cfg.Runtime == nil {
		cfg.Runtime = NewRuntime(cfg.RedactKeys)
	}
	cfg.eventHub = cfg.Runtime.eventHub
	cfg.tracker = newTraceTracker()
	cfg.redactor = cfg.Runtime.redactor
	if strings.TrimSpace(cfg.Workspace) == "" {
		cfg.Workspace = "default"
	}
	if strings.TrimSpace(cfg.Environment) == "" {
		cfg.Environment = "default"
	}
	if strings.TrimSpace(cfg.Version) == "" {
		cfg.Version = "dev"
	}
}

func Run(ctx context.Context, cfg Config) error {
	prepareConfig(&cfg)

	if cfg.AlertingConfig != nil {
		engine, err := alerting.NewEngine(*cfg.AlertingConfig, cfg.Store, alerting.Options{
			Workspace:   cfg.Workspace,
			Environment: cfg.Environment,
			PublicURL:   cfg.PublicURL,
			Logger:      cfg.Stderr,
		})
		if err != nil {
			return err
		}
		cfg.AlertingEngine = engine
		alertCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go engine.Run(alertCtx)
	}

	switch cfg.Transport {
	case "stdio":
		return runStdio(ctx, cfg)
	case "http":
		return runHTTP(ctx, cfg)
	default:
		return fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
}

type traceAPIRecord struct {
	ID           string          `json:"id"`
	TraceID      string          `json:"trace_id"`
	Workspace    string          `json:"workspace"`
	Environment  string          `json:"environment"`
	ServerID     string          `json:"server_id"`
	ServerName   string          `json:"server_name"`
	Method       string          `json:"method"`
	Params       json.RawMessage `json:"params,omitempty"`
	Response     json.RawMessage `json:"response,omitempty"`
	LatencyMs    int64           `json:"latency_ms"`
	IsError      bool            `json:"is_error"`
	ErrorMessage string          `json:"error_message,omitempty"`
	SdkReported  bool            `json:"sdk_reported"`
	CreatedAt    time.Time       `json:"created_at"`
}

type ingestTraceRequest struct {
	Method      string          `json:"method"`
	Params      json.RawMessage `json:"params"`
	Response    json.RawMessage `json:"response"`
	DurationMs  int64           `json:"duration_ms"`
	Error       string          `json:"error"`
	Timestamp   string          `json:"timestamp"`
	TraceID     string          `json:"trace_id,omitempty"`
	Workspace   string          `json:"workspace,omitempty"`
	Environment string          `json:"environment,omitempty"`
	ServerID    string          `json:"server_id,omitempty"`
	ServerName  string          `json:"server_name,omitempty"`
}

type traceListResponse struct {
	Items      []traceAPIRecord `json:"items"`
	Offset     int              `json:"offset"`
	Limit      int              `json:"limit"`
	HasMore    bool             `json:"has_more"`
	NextOffset int              `json:"next_offset"`
}

type latencyStatRecord struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Method     string `json:"method"`
	Count      int    `json:"count"`
	P50Ms      int64  `json:"p50_ms"`
	P95Ms      int64  `json:"p95_ms"`
	P99Ms      int64  `json:"p99_ms"`
}

type errorStatRecord struct {
	ServerID           string     `json:"server_id"`
	Environment        string     `json:"environment"`
	Method             string     `json:"method"`
	Count              int        `json:"count"`
	ErrorCount         int        `json:"error_count"`
	ErrorRatePct       float64    `json:"error_rate_pct"`
	RecentErrorMessage string     `json:"recent_error_message,omitempty"`
	RecentErrorAt      *time.Time `json:"recent_error_at,omitempty"`
}

type statusResponse struct {
	Service string          `json:"service"`
	Status  string          `json:"status"`
	Ready   bool            `json:"ready"`
	Checks  map[string]bool `json:"checks,omitempty"`
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type traceEventHub struct {
	mu          sync.RWMutex
	subscribers map[chan traceAPIRecord]struct{}
}

type pendingTrace struct {
	id         string
	traceID    string
	serverID   string
	server     string
	method     string
	params     json.RawMessage
	paramsHash string
	createdAt  time.Time
}

type traceTracker struct {
	mu      sync.Mutex
	pending map[string]pendingTrace
}

func newTraceTracker() *traceTracker {
	return &traceTracker{pending: make(map[string]pendingTrace)}
}

func (t *traceTracker) Record(serverID, serverName string, event intercept.Event) (traceAPIRecord, bool) {
	if t == nil {
		return traceRecordFromEvent(intercept.NewUUID(), serverID, serverName, event), true
	}

	now := time.Unix(0, event.ReceivedAtUnixN).UTC()
	t.mu.Lock()
	defer t.mu.Unlock()

	t.evictStaleLocked(now)

	messageID := intercept.MessageIDKey(event.ID)
	if event.Direction == "client_to_server" && event.Method != "" && messageID != "" {
		t.pending[messageID] = pendingTrace{
			id:         intercept.NewUUID(),
			traceID:    event.TraceID,
			serverID:   serverID,
			server:     serverName,
			method:     event.Method,
			params:     cloneRawMessage(event.Params),
			paramsHash: event.ParamsHash,
			createdAt:  time.Unix(0, event.ReceivedAtUnixN).UTC(),
		}
		return traceAPIRecord{}, false
	}

	if event.Direction == "server_to_client" && messageID != "" {
		if request, ok := t.pending[messageID]; ok {
			delete(t.pending, messageID)
			return traceAPIRecord{
				ID:           request.id,
				TraceID:      request.traceID,
				ServerID:     request.serverID,
				ServerName:   request.server,
				Method:       request.method,
				Params:       cloneRawMessage(request.params),
				Response:     cloneRawMessage(selectResponsePayload(event)),
				LatencyMs:    maxDurationMs(request.createdAt, time.Unix(0, event.SentAtUnixN).UTC()),
				IsError:      event.IsError,
				ErrorMessage: event.ErrorMessage,
				CreatedAt:    request.createdAt,
			}, true
		}
	}

	return traceRecordFromEvent(intercept.NewUUID(), serverID, serverName, event), true
}

func (t *traceTracker) evictStaleLocked(now time.Time) {
	cutoff := now.Add(-15 * time.Minute)
	for key, trace := range t.pending {
		if trace.createdAt.Before(cutoff) {
			delete(t.pending, key)
		}
	}
}

func newTraceEventHub() *traceEventHub {
	return &traceEventHub{subscribers: make(map[chan traceAPIRecord]struct{})}
}

func (h *traceEventHub) Subscribe() (chan traceAPIRecord, func()) {
	ch := make(chan traceAPIRecord, 32)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		h.mu.Unlock()
		close(ch)
	}
}

func (h *traceEventHub) Publish(record traceAPIRecord) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subscribers {
		select {
		case ch <- record:
		default:
		}
	}
}

func runStdio(ctx context.Context, cfg Config) error {
	server, serverErr, err := StartHTTPServer(ctx, cfg, nil)
	if err != nil {
		return err
	}

	if len(cfg.ServerCommand) == 0 {
		shutdownHTTPServer(server)
		return errors.New("missing stdio server command")
	}

	cmd := exec.CommandContext(ctx, cfg.ServerCommand[0], cfg.ServerCommand[1:]...)
	cmd.Stderr = cfg.Stderr
	cmd.Env = append(os.Environ(), fmt.Sprintf("MCPSCOPE_PROXY_PORT=%d", cfg.Port))

	serverIn, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create subprocess stdin pipe: %w", err)
	}

	serverOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create subprocess stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		shutdownHTTPServer(server)
		return fmt.Errorf("start subprocess: %w", err)
	}

	copyErr := make(chan error, 2)

	go func() {
		copyErr <- forwardStdio(ctx, cfg, cfg.Stdin, serverIn, "client_to_server")
	}()

	go func() {
		copyErr <- forwardStdio(ctx, cfg, serverOut, cfg.Stdout, "server_to_client")
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		if err := <-copyErr; err != nil && !errors.Is(err, io.EOF) && firstErr == nil {
			firstErr = err
		}
	}

	waitErr := cmd.Wait()
	shutdownHTTPServer(server)
	if err := <-serverErr; err != nil {
		return err
	}

	if firstErr != nil {
		return firstErr
	}

	if waitErr != nil {
		return fmt.Errorf("subprocess exited with error: %w", waitErr)
	}

	return nil
}

func forwardStdio(ctx context.Context, cfg Config, src io.Reader, dst io.Writer, direction string) error {
	reader := bufio.NewReader(src)
	writeCloser, canClose := dst.(io.WriteCloser)

	for {
		receivedAt := time.Now()
		frame, err := readFrame(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if canClose {
					return writeCloser.Close()
				}
				return nil
			}
			return err
		}

		if _, err := dst.Write(frame.header); err != nil {
			return fmt.Errorf("write frame header: %w", err)
		}
		if _, err := dst.Write(frame.payload); err != nil {
			return fmt.Errorf("write frame payload: %w", err)
		}
		if flusher, ok := dst.(interface{ Flush() error }); ok {
			if err := flusher.Flush(); err != nil {
				return fmt.Errorf("flush frame: %w", err)
			}
		}

		sentAt := time.Now()
		captureAndPersistBestEffort(ctx, cfg, "stdio", direction, receivedAt, sentAt, frame.payload)
	}
}

type frame struct {
	header  []byte
	payload []byte
}

func readFrame(reader *bufio.Reader) (frame, error) {
	var header bytes.Buffer
	contentLength := -1

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && header.Len() == 0 {
				return frame{}, io.EOF
			}
			return frame{}, fmt.Errorf("read frame header: %w", err)
		}

		header.Write(line)
		trimmed := strings.TrimRight(string(line), "\r\n")
		if trimmed == "" {
			break
		}

		name, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}

		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return frame{}, fmt.Errorf("parse content length: %w", err)
			}
			contentLength = parsed
		}
	}

	if contentLength < 0 {
		return frame{}, fmt.Errorf("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return frame{}, fmt.Errorf("read frame payload: %w", err)
	}

	return frame{
		header:  header.Bytes(),
		payload: payload,
	}, nil
}

func runHTTP(ctx context.Context, cfg Config) error {
	upstreamPort := cfg.Port + 1
	if err := validateUpstreamPort(upstreamPort); err != nil {
		return err
	}

	targetURL := strings.TrimSpace(cfg.UpstreamURL)
	var cmd *exec.Cmd
	var waitErr <-chan error

	if targetURL == "" {
		if len(cfg.ServerCommand) == 0 {
			return errors.New("missing http server command")
		}

		cmd = exec.CommandContext(ctx, cfg.ServerCommand[0], cfg.ServerCommand[1:]...)
		cmd.Stdout = cfg.Stderr
		cmd.Stderr = cfg.Stderr
		cmd.Env = append(
			os.Environ(),
			fmt.Sprintf("PORT=%d", upstreamPort),
			fmt.Sprintf("MCPSCOPE_PROXY_PORT=%d", cfg.Port),
			fmt.Sprintf("MCPSCOPE_UPSTREAM_PORT=%d", upstreamPort),
		)

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start subprocess: %w", err)
		}

		targetURL = fmt.Sprintf("http://127.0.0.1:%d", upstreamPort)
		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()
		waitErr = waitCh
	}

	targetBaseURL, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("build upstream url: %w", err)
	}

	var mu sync.Mutex
	client := &http.Client{}
	server, serverErr, err := StartHTTPServer(ctx, cfg, func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		requestReceivedAt := time.Now()

		upstreamURL := *targetBaseURL
		upstreamURL.Path = r.URL.Path
		upstreamURL.RawQuery = stripQueryParam(r.URL.RawQuery, "server_id")

		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL.String(), bytes.NewReader(requestBody))
		if err != nil {
			http.Error(w, "failed to build upstream request", http.StatusInternalServerError)
			return
		}

		req.Header = r.Header.Clone()
		req.Header.Del("Host")

		mu.Lock()
		resp, err := client.Do(req)
		mu.Unlock()
		requestSentAt := time.Now()
		captureAndPersistBestEffort(r.Context(), cfg, "http", "client_to_server", requestReceivedAt, requestSentAt, requestBody)
		if err != nil {
			http.Error(w, fmt.Sprintf("proxy upstream request: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		responseReceivedAt := time.Now()
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "failed to read upstream response", http.StatusBadGateway)
			return
		}

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		w.WriteHeader(resp.StatusCode)
		if _, err := w.Write(responseBody); err != nil {
			return
		}

		responseSentAt := time.Now()
		captureAndPersistBestEffort(r.Context(), cfg, "http", "server_to_client", responseReceivedAt, responseSentAt, responseBody)
	})
	if err != nil {
		return err
	}

	if waitErr == nil {
		waitCh := make(chan error, 1)
		go func() {
			<-ctx.Done()
			waitCh <- nil
		}()
		waitErr = waitCh
	}

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("http proxy server failed: %w", err)
		}
		if err := <-waitErr; err != nil {
			return fmt.Errorf("subprocess exited with error: %w", err)
		}
		return nil
	case err := <-waitErr:
		shutdownHTTPServer(server)
		if err != nil {
			return fmt.Errorf("subprocess exited with error: %w", err)
		}
		if err := <-serverErr; err != nil {
			return fmt.Errorf("http proxy server failed: %w", err)
		}
		return nil
	}
}

func captureAndPersist(ctx context.Context, cfg Config, transport, direction string, receivedAt, sentAt time.Time, payload []byte) error {
	event := intercept.Capture(transport, direction, receivedAt, sentAt, payload)
	event = cfg.redactor.Event(event)

	if err := intercept.EmitLog(stderrWriter(cfg), event); err != nil {
		return err
	}

	record, persist := cfg.tracker.Record(cfg.ServerID, cfg.ServerName, event)
	if !persist {
		return nil
	}
	record.Workspace = cfg.Workspace
	record.Environment = cfg.Environment
	if record.ServerID == "" {
		record.ServerID = cfg.ServerID
	}
	if record.ServerID == "" {
		record.ServerID = serverIDFromName(cfg.ServerName)
	}

	if cfg.Telemetry != nil {
		cfg.Telemetry.RecordCall(ctx, record.ServerName, eventFromRecord(record))
	}
	return persistTraceRecord(ctx, cfg, record)
}

func captureAndPersistBestEffort(ctx context.Context, cfg Config, transport, direction string, receivedAt, sentAt time.Time, payload []byte) {
	if err := captureAndPersist(ctx, cfg, transport, direction, receivedAt, sentAt, payload); err != nil {
		logProxyWarning(cfg.Stderr, "trace capture failed", err)
	}
}

func logProxyWarning(stderr io.Writer, message string, err error) {
	if stderr == nil || err == nil {
		return
	}
	_, _ = fmt.Fprintf(stderr, "mcpscope: %s: %v\n", message, err)
}

func stderrWriter(cfg Config) io.Writer {
	if cfg.Stderr != nil {
		return cfg.Stderr
	}
	return io.Discard
}

func validateUpstreamPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("derived upstream port %d is out of range", port)
	}
	return nil
}

func StartHTTPServer(ctx context.Context, cfg Config, proxyPostHandler http.HandlerFunc) (*http.Server, <-chan error, error) {
	prepareConfig(&cfg)
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return nil, nil, fmt.Errorf("listen on port %d: %w", cfg.Port, err)
	}

	server := &http.Server{
		Handler: newHTTPHandler(cfg, proxyPostHandler),
	}

	serverErr := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownHTTPServer(server)
	}()

	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("http server failed: %w", err)
			return
		}
		serverErr <- nil
	}()

	return server, serverErr, nil
}

func shutdownHTTPServer(server *http.Server) {
	if server == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func newHTTPHandler(cfg Config, proxyPostHandler http.HandlerFunc) http.Handler {
	fileServer := http.FileServer(http.FS(cfg.Dashboard))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/healthz":
			handleHealthz(w, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			handleReadyz(w, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/traces":
			handleTraceList(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/export":
			handleAuditExport(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/export/traces":
			handleTraceExport(w, r, cfg)
		case r.Method == http.MethodPost && r.URL.Path == "/api/ingest":
			handleIngestTrace(w, r, cfg)
		case r.Method == http.MethodPost && r.URL.Path == "/api/replay":
			handleReplay(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/alerts/rules":
			handleAlertRuleList(w, r, cfg)
		case r.Method == http.MethodPost && r.URL.Path == "/api/alerts/rules":
			handleAlertRuleUpsert(w, r, cfg)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/alerts/rules":
			handleAlertRuleDelete(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/alerts/evaluations":
			handleAlertEvaluations(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/alerts/events":
			handleAlertEvents(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/alert-rules":
			handleConfiguredAlertRules(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/stats/latency":
			handleLatencyStats(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/api/stats/errors":
			handleErrorStats(w, r, cfg)
		case r.Method == http.MethodGet && r.URL.Path == "/events":
			handleEvents(w, r, cfg)
		case r.Method == http.MethodPost && proxyPostHandler != nil:
			proxyPostHandler(w, r)
		case r.Method == http.MethodGet || r.Method == http.MethodHead:
			serveDashboardAsset(w, r, cfg.Dashboard, fileServer)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return requireAuth(cfg, handler)
}

func handleHealthz(w http.ResponseWriter, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Version: cfg.Version,
	})
}

func handleReadyz(w http.ResponseWriter, cfg Config) {
	ready := isReady(cfg)
	status := "ok"
	code := http.StatusOK
	if !ready {
		status = "not_ready"
		code = http.StatusServiceUnavailable
	}

	writeStatus(w, code, statusResponse{
		Service: "mcpscope",
		Status:  status,
		Ready:   ready,
		Checks:  readinessChecks(cfg),
	})
}

func writeStatus(w http.ResponseWriter, statusCode int, value statusResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func isReady(cfg Config) bool {
	return cfg.Dashboard != nil && cfg.Store != nil
}

func readinessChecks(cfg Config) map[string]bool {
	return map[string]bool{
		"dashboard_embedded": cfg.Dashboard != nil,
		"trace_store":        cfg.Store != nil,
	}
}

func handleTraceList(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		_ = json.NewEncoder(w).Encode(traceListResponse{Items: []traceAPIRecord{}})
		return
	}

	filter, limit, offset, err := parseTraceQuery(r, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	traces, err := cfg.Store.Query(r.Context(), store.QueryFilter{
		Workspace:     filter.Workspace,
		Environment:   filter.Environment,
		ServerID:      filter.ServerID,
		ServerName:    filter.ServerName,
		Method:        filter.Method,
		Search:        filter.Search,
		IsError:       filter.IsError,
		CreatedAfter:  filter.CreatedAfter,
		CreatedBefore: filter.CreatedBefore,
		Limit:         limit + 1,
		Offset:        offset,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	hasMore := len(traces) > limit
	if hasMore {
		traces = traces[:limit]
	}

	records := make([]traceAPIRecord, 0, len(traces))
	for _, trace := range traces {
		records = append(records, traceRecordFromStored(trace))
	}

	_ = json.NewEncoder(w).Encode(traceListResponse{
		Items:      records,
		Offset:     offset,
		Limit:      limit,
		HasMore:    hasMore,
		NextOffset: offset + len(records),
	})
}

func handleEvents(w http.ResponseWriter, r *http.Request, cfg Config) {
	if cfg.eventHub == nil {
		http.Error(w, "event stream unavailable", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	filter, _, _, err := parseTraceQuery(r, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ch, unsubscribe := cfg.eventHub.Subscribe()
	defer unsubscribe()

	for {
		select {
		case <-r.Context().Done():
			return
		case record, ok := <-ch:
			if !ok {
				return
			}
			if filter.ServerID != "" && record.ServerID != filter.ServerID {
				continue
			}
			if filter.ServerName != "" && record.ServerName != filter.ServerName {
				continue
			}
			if filter.Workspace != "" && record.Workspace != filter.Workspace {
				continue
			}
			if filter.Environment != "" && record.Environment != filter.Environment {
				continue
			}
			if filter.Method != "" && record.Method != filter.Method {
				continue
			}
			if filter.IsError != nil && record.IsError != *filter.IsError {
				continue
			}
			if filter.Search != "" && !traceRecordMatchesSearch(record, filter.Search) {
				continue
			}
			if filter.CreatedAfter != nil && record.CreatedAt.Before(filter.CreatedAfter.UTC()) {
				continue
			}
			if filter.CreatedBefore != nil && record.CreatedAt.After(filter.CreatedBefore.UTC()) {
				continue
			}
			payload, err := json.Marshal(record)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func handleAlertRuleList(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		_ = json.NewEncoder(w).Encode([]store.AlertRule{})
		return
	}

	rules, err := cfg.Store.ListAlertRules(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	workspace := workspaceFromRequest(r, cfg)
	environment := environmentFromRequest(r, cfg)
	filtered := make([]store.AlertRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Workspace == workspace && rule.Environment == environment {
			filtered = append(filtered, rule)
		}
	}

	_ = json.NewEncoder(w).Encode(filtered)
}

func handleAlertRuleUpsert(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		http.Error(w, "alert storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var rule store.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "invalid alert rule payload", http.StatusBadRequest)
		return
	}
	if err := validateAlertRule(rule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if rule.ID == "" {
		rule.ID = intercept.NewUUID()
		rule.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(rule.Environment) == "" {
		rule.Environment = environmentFromRequest(r, cfg)
	}
	if strings.TrimSpace(rule.Workspace) == "" {
		rule.Workspace = workspaceFromRequest(r, cfg)
	}
	rule.UpdatedAt = time.Now().UTC()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = rule.UpdatedAt
	}

	saved, err := cfg.Store.UpsertAlertRule(r.Context(), rule)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(saved)
}

func handleAlertRuleDelete(w http.ResponseWriter, r *http.Request, cfg Config) {
	if cfg.Store == nil {
		http.Error(w, "alert storage unavailable", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "missing alert rule id", http.StatusBadRequest)
		return
	}
	if err := cfg.Store.DeleteAlertRule(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleAlertEvaluations(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		_ = json.NewEncoder(w).Encode([]alertEvaluation{})
		return
	}

	rules, err := cfg.Store.ListAlertRules(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	workspace := workspaceFromRequest(r, cfg)
	environment := environmentFromRequest(r, cfg)
	filtered := make([]store.AlertRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Workspace == workspace && rule.Environment == environment {
			filtered = append(filtered, rule)
		}
	}
	evaluations, err := evaluateAlertRules(r.Context(), cfg.Store, time.Now().UTC(), filtered)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(evaluations)
}

func handleAlertEvents(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		_ = json.NewEncoder(w).Encode([]store.AlertEvent{})
		return
	}

	events, err := cfg.Store.ListAlertEvents(r.Context(), workspaceFromRequest(r, cfg), environmentFromRequest(r, cfg), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(events)
}

func handleConfiguredAlertRules(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.AlertingEngine == nil {
		_ = json.NewEncoder(w).Encode([]alerting.RuleStatus{})
		return
	}
	_ = json.NewEncoder(w).Encode(cfg.AlertingEngine.Snapshot())
}

func handleLatencyStats(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		_ = json.NewEncoder(w).Encode([]latencyStatRecord{})
		return
	}

	filter, err := queryWindowFilter(r, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	stats, err := cfg.Store.QueryLatencyStats(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	records := make([]latencyStatRecord, 0, len(stats))
	for _, stat := range stats {
		records = append(records, latencyStatRecord{
			ServerID:   stat.ServerID,
			ServerName: stat.ServerName,
			Method:     stat.Method,
			Count:      stat.Count,
			P50Ms:      stat.P50Ms,
			P95Ms:      stat.P95Ms,
			P99Ms:      stat.P99Ms,
		})
	}

	_ = json.NewEncoder(w).Encode(records)
}

func handleErrorStats(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		_ = json.NewEncoder(w).Encode([]errorStatRecord{})
		return
	}

	filter, err := queryWindowFilter(r, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	stats, err := cfg.Store.QueryErrorStats(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	records := make([]errorStatRecord, 0, len(stats))
	for _, stat := range stats {
		records = append(records, errorStatRecord{
			ServerID:           stat.ServerID,
			Environment:        stat.Environment,
			Method:             stat.Method,
			Count:              stat.Count,
			ErrorCount:         stat.ErrorCount,
			ErrorRatePct:       stat.ErrorRatePct,
			RecentErrorMessage: stat.RecentErrorMessage,
			RecentErrorAt:      stat.RecentErrorAt,
		})
	}

	_ = json.NewEncoder(w).Encode(records)
}

func handleAuditExport(w http.ResponseWriter, r *http.Request, cfg Config) {
	if cfg.Store == nil {
		http.Error(w, "trace storage unavailable", http.StatusServiceUnavailable)
		return
	}

	queryStore, ok := cfg.Store.(interface {
		QueryRows(context.Context, store.QueryFilter) (*sql.Rows, error)
	})
	if !ok {
		http.Error(w, "streaming export unavailable", http.StatusNotImplemented)
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = auditexport.FormatJSON
	}

	filter, err := auditexport.BuildQueryFilter(auditexport.FilterInput{
		Workspace:   workspaceFromRequest(r, cfg),
		Environment: environmentFromRequest(r, cfg),
		Method:      r.URL.Query().Get("method"),
		Status:      r.URL.Query().Get("status"),
		From:        r.URL.Query().Get("from"),
		To:          r.URL.Query().Get("to"),
	}, time.Now().UTC())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rows, err := queryStore.QueryRows(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	filename := "audit.ndjson"
	contentType := "application/x-ndjson; charset=utf-8"
	if format == auditexport.FormatCSV {
		filename = "audit.csv"
		contentType = "text/csv; charset=utf-8"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if err := auditexport.StreamRows(rows, format, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleTraceExport(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		_ = json.NewEncoder(w).Encode([]store.Trace{})
		return
	}

	filter, limit, offset, err := parseTraceQuery(r, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	traces, err := cfg.Store.Query(r.Context(), store.QueryFilter{
		TraceID:       filter.TraceID,
		Search:        filter.Search,
		Workspace:     filter.Workspace,
		Environment:   filter.Environment,
		ServerID:      filter.ServerID,
		ServerName:    filter.ServerName,
		Method:        filter.Method,
		IsError:       filter.IsError,
		CreatedAfter:  filter.CreatedAfter,
		CreatedBefore: filter.CreatedBefore,
		Limit:         limit,
		Offset:        offset,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(traces)
}

func handleReplay(w http.ResponseWriter, r *http.Request, cfg Config) {
	if cfg.Store == nil {
		http.Error(w, "trace storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		TraceID string `json:"trace_id"`
		Server  string `json:"server"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid replay payload", http.StatusBadRequest)
		return
	}
	traceID := strings.TrimSpace(input.TraceID)
	if traceID == "" {
		http.Error(w, "missing trace_id", http.StatusBadRequest)
		return
	}

	trace, err := replay.LoadTraceByID(r.Context(), cfg.Store, traceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	target := strings.TrimSpace(input.Server)
	if target == "" {
		target = strings.TrimSpace(trace.ServerName)
	}
	if target == "" {
		http.Error(w, "missing server", http.StatusBadRequest)
		return
	}
	parsedTarget, err := url.Parse(target)
	if err != nil || (parsedTarget.Scheme != "http" && parsedTarget.Scheme != "https") {
		http.Error(w, "replay endpoint expects an HTTP server URL", http.StatusBadRequest)
		return
	}

	responseBody, latencyMs, err := replay.InvokeHTTP(r.Context(), target, trace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	match, diff, err := replay.CompareResponses([]byte(trace.ResponsePayload), responseBody, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"trace_id":   trace.TraceID,
		"server":     target,
		"match":      match,
		"latency_ms": latencyMs,
		"diff":       diff,
		"status":     "done",
	})
	_, _ = fmt.Fprintf(w, "event: replay\n")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

func serveDashboardAsset(w http.ResponseWriter, r *http.Request, static fs.FS, fileServer http.Handler) {
	if static == nil {
		http.NotFound(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	if _, err := fs.Stat(static, path); err == nil {
		fileServer.ServeHTTP(w, r)
		return
	}

	index, err := fs.ReadFile(static, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}

func traceRecordFromEvent(id, serverID, serverName string, event intercept.Event) traceAPIRecord {
	return traceAPIRecord{
		ID:           id,
		TraceID:      event.TraceID,
		Workspace:    "",
		Environment:  "",
		ServerID:     serverID,
		ServerName:   serverName,
		Method:       event.Method,
		Params:       cloneRawMessage(event.Params),
		Response:     cloneRawMessage(selectResponsePayload(event)),
		LatencyMs:    event.LatencyMs,
		IsError:      event.IsError,
		ErrorMessage: event.ErrorMessage,
		SdkReported:  false,
		CreatedAt:    time.Unix(0, event.ReceivedAtUnixN).UTC(),
	}
}

func traceRecordFromStored(trace store.Trace) traceAPIRecord {
	return traceAPIRecord{
		ID:           trace.ID,
		TraceID:      trace.TraceID,
		Workspace:    trace.Workspace,
		Environment:  trace.Environment,
		ServerID:     trace.ServerID,
		ServerName:   trace.ServerName,
		Method:       trace.Method,
		Params:       asRawJSON(trace.ParamsPayload),
		Response:     asRawJSON(trace.ResponsePayload),
		LatencyMs:    trace.LatencyMs,
		IsError:      trace.IsError,
		ErrorMessage: trace.ErrorMessage,
		SdkReported:  trace.SdkReported,
		CreatedAt:    trace.CreatedAt,
	}
}

func selectResponsePayload(event intercept.Event) json.RawMessage {
	if len(event.Result) > 0 {
		return event.Result
	}
	return event.Error
}

func rawMessageString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

func asRawJSON(value string) json.RawMessage {
	if value == "" {
		return nil
	}
	return json.RawMessage(value)
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

func serverIDFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	parsed, err := url.Parse(name)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		host := strings.TrimSpace(parsed.Hostname())
		port := strings.TrimSpace(parsed.Port())
		if host != "" && port != "" {
			return sanitizeServerID(host + "-" + port)
		}
		if host != "" {
			return sanitizeServerID(host)
		}
	}

	return sanitizeServerID(filepath.Base(name))
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

	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "server"
	}
	return result
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

func hashRawMessage(raw json.RawMessage) string {
	return intercept.HashRaw(raw)
}

func eventFromRecord(record traceAPIRecord) intercept.Event {
	return intercept.Event{
		TraceID:         record.TraceID,
		ReceivedAtUnixN: record.CreatedAt.UnixNano(),
		SentAtUnixN:     record.CreatedAt.Add(time.Duration(record.LatencyMs) * time.Millisecond).UnixNano(),
		LatencyMs:       record.LatencyMs,
		Method:          record.Method,
		IsError:         record.IsError,
		ErrorMessage:    record.ErrorMessage,
	}
}

func handleIngestTrace(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		http.Error(w, "trace storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var input ingestTraceRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid ingest payload", http.StatusBadRequest)
		return
	}

	record, err := traceRecordFromIngestRequest(r, cfg, input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := persistTraceRecord(r.Context(), cfg, record); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(record)
}

func traceRecordFromIngestRequest(r *http.Request, cfg Config, input ingestTraceRequest) (traceAPIRecord, error) {
	method := strings.TrimSpace(input.Method)
	if method == "" {
		return traceAPIRecord{}, fmt.Errorf("method is required")
	}

	timestamp := time.Now().UTC()
	if raw := strings.TrimSpace(input.Timestamp); raw != "" {
		parsed, err := parseQueryTime(raw)
		if err != nil {
			return traceAPIRecord{}, fmt.Errorf("timestamp must be a valid timestamp")
		}
		timestamp = parsed.UTC()
	}

	record := traceAPIRecord{
		ID:           strings.TrimSpace(input.TraceID),
		TraceID:      strings.TrimSpace(input.TraceID),
		Workspace:    strings.TrimSpace(input.Workspace),
		Environment:  strings.TrimSpace(input.Environment),
		ServerID:     strings.TrimSpace(input.ServerID),
		ServerName:   strings.TrimSpace(input.ServerName),
		Method:       method,
		Params:       cloneRawMessage(input.Params),
		Response:     cloneRawMessage(input.Response),
		LatencyMs:    maxInt64(input.DurationMs, 0),
		IsError:      strings.TrimSpace(input.Error) != "",
		ErrorMessage: strings.TrimSpace(input.Error),
		SdkReported:  true,
		CreatedAt:    timestamp,
	}

	if record.Workspace == "" {
		record.Workspace = strings.TrimSpace(r.Header.Get("X-Mcpscope-Workspace"))
	}
	if record.Workspace == "" {
		record.Workspace = workspaceFromRequest(r, cfg)
	}
	if record.Environment == "" {
		record.Environment = strings.TrimSpace(r.Header.Get("X-Mcpscope-Environment"))
	}
	if record.Environment == "" {
		record.Environment = environmentFromRequest(r, cfg)
	}
	if record.ServerName == "" {
		record.ServerName = strings.TrimSpace(r.Header.Get("X-Mcpscope-Server-Name"))
	}
	if record.ServerID == "" {
		record.ServerID = strings.TrimSpace(r.Header.Get("X-Mcpscope-Server-Id"))
	}
	if record.ServerID == "" {
		record.ServerID = cfg.ServerID
	}
	if record.ServerID == "" {
		record.ServerID = serverIDFromName(record.ServerName)
	}
	if record.ServerName == "" {
		record.ServerName = cfg.ServerName
	}
	if record.ServerName == "" {
		record.ServerName = "sdk"
	}
	if record.ID == "" {
		record.ID = intercept.NewUUID()
	}
	if record.TraceID == "" {
		record.TraceID = record.ID
	}

	return record, nil
}

func persistTraceRecord(ctx context.Context, cfg Config, record traceAPIRecord) error {
	if cfg.eventHub != nil {
		cfg.eventHub.Publish(record)
	}

	if cfg.Store == nil {
		return nil
	}

	if err := cfg.Store.Insert(ctx, store.Trace{
		ID:              record.ID,
		TraceID:         record.TraceID,
		Workspace:       record.Workspace,
		Environment:     record.Environment,
		ServerID:        record.ServerID,
		ServerName:      record.ServerName,
		Method:          record.Method,
		ParamsHash:      hashRawMessage(record.Params),
		ParamsPayload:   rawMessageString(record.Params),
		ResponseHash:    hashRawMessage(record.Response),
		ResponsePayload: rawMessageString(record.Response),
		LatencyMs:       record.LatencyMs,
		IsError:         record.IsError,
		ErrorMessage:    record.ErrorMessage,
		SdkReported:     record.SdkReported,
		CreatedAt:       record.CreatedAt.UTC(),
	}); err != nil {
		return err
	}

	if cfg.RetentionMaxAge > 0 {
		if err := cfg.Store.DeleteOlderThan(ctx, record.CreatedAt.Add(-cfg.RetentionMaxAge)); err != nil {
			return err
		}
	}
	if cfg.MaxTraceCount > 0 {
		if err := cfg.Store.TrimToCount(ctx, cfg.MaxTraceCount); err != nil {
			return err
		}
	}

	if err := processAlertEvaluations(ctx, cfg); err != nil {
		return err
	}

	return nil
}

func maxDurationMs(start, end time.Time) int64 {
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func queryWindowFilter(r *http.Request, cfg Config) (store.QueryFilter, error) {
	window, err := parseWindow(r.URL.Query().Get("window"))
	if err != nil {
		return store.QueryFilter{}, err
	}

	environment := environmentFromRequest(r, cfg)
	serverID := strings.TrimSpace(r.URL.Query().Get("server_id"))
	serverName := strings.TrimSpace(r.URL.Query().Get("server"))
	method := strings.TrimSpace(r.URL.Query().Get("method"))
	start := time.Now().Add(-window)

	return store.QueryFilter{
		Workspace:    workspaceFromRequest(r, cfg),
		Environment:  environment,
		ServerID:     serverID,
		ServerName:   serverName,
		Method:       method,
		CreatedAfter: &start,
	}, nil
}

func parseTraceQuery(r *http.Request, cfg Config) (store.QueryFilter, int, int, error) {
	query := r.URL.Query()
	limit := 50
	offset := 0
	if raw := strings.TrimSpace(query.Get("page")); raw != "" || strings.TrimSpace(query.Get("per_page")) != "" {
		page := 1
		if raw := strings.TrimSpace(query.Get("page")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 {
				return store.QueryFilter{}, 0, 0, fmt.Errorf("page must be 1 or greater")
			}
			page = parsed
		}
		if raw := strings.TrimSpace(query.Get("per_page")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				return store.QueryFilter{}, 0, 0, fmt.Errorf("per_page must be between 1 and 200")
			}
			limit = parsed
		}
		offset = (page - 1) * limit
	} else {
		if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				return store.QueryFilter{}, 0, 0, fmt.Errorf("limit must be between 1 and 200")
			}
			limit = parsed
		}

		if raw := strings.TrimSpace(query.Get("offset")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				return store.QueryFilter{}, 0, 0, fmt.Errorf("offset must be 0 or greater")
			}
			offset = parsed
		}
	}

	filter := store.QueryFilter{
		TraceID:     strings.TrimSpace(query.Get("trace_id")),
		Search:      strings.TrimSpace(query.Get("search")),
		Workspace:   workspaceFromRequest(r, cfg),
		Environment: environmentFromRequest(r, cfg),
		ServerID:    strings.TrimSpace(query.Get("server_id")),
		ServerName:  strings.TrimSpace(query.Get("server")),
		Method:      strings.TrimSpace(query.Get("method")),
	}
	if raw := strings.TrimSpace(query.Get("created_after")); raw != "" {
		parsed, err := parseQueryTime(raw)
		if err != nil {
			return store.QueryFilter{}, 0, 0, fmt.Errorf("created_after must be a valid timestamp")
		}
		filter.CreatedAfter = &parsed
	}
	if raw := strings.TrimSpace(query.Get("created_before")); raw != "" {
		parsed, err := parseQueryTime(raw)
		if err != nil {
			return store.QueryFilter{}, 0, 0, fmt.Errorf("created_before must be a valid timestamp")
		}
		filter.CreatedBefore = &parsed
	}
	if filter.CreatedAfter != nil && filter.CreatedBefore != nil && filter.CreatedAfter.After(*filter.CreatedBefore) {
		return store.QueryFilter{}, 0, 0, fmt.Errorf("created_after must be earlier than or equal to created_before")
	}
	switch strings.TrimSpace(query.Get("status")) {
	case "":
	case "error":
		value := true
		filter.IsError = &value
	case "success":
		value := false
		filter.IsError = &value
	default:
		return store.QueryFilter{}, 0, 0, fmt.Errorf("status must be empty, success, or error")
	}

	return filter, limit, offset, nil
}

func parseQueryTime(raw string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time")
}

func traceRecordMatchesSearch(record traceAPIRecord, search string) bool {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return true
	}

	fields := []string{
		record.ID,
		record.TraceID,
		record.Workspace,
		record.Environment,
		record.ServerID,
		record.ServerName,
		record.Method,
		record.ErrorMessage,
		string(record.Params),
		string(record.Response),
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), search) {
			return true
		}
	}
	return false
}

func environmentFromRequest(r *http.Request, cfg Config) string {
	environment := strings.TrimSpace(r.URL.Query().Get("environment"))
	if environment == "" {
		environment = cfg.Environment
	}
	if environment == "" {
		return "default"
	}
	return environment
}

func workspaceFromRequest(r *http.Request, cfg Config) string {
	workspace := strings.TrimSpace(r.URL.Query().Get("workspace"))
	if workspace == "" {
		workspace = cfg.Workspace
	}
	if workspace == "" {
		return "default"
	}
	return workspace
}

func requireAuth(cfg Config, next http.Handler) http.Handler {
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/events" {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("Authorization") != "Bearer "+cfg.AuthToken && r.URL.Query().Get("token") != cfg.AuthToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func validateAlertRule(rule store.AlertRule) error {
	if strings.TrimSpace(rule.Name) == "" {
		return fmt.Errorf("alert rule name is required")
	}
	switch rule.RuleType {
	case "error_rate", "latency_p95":
	default:
		return fmt.Errorf("rule_type must be error_rate or latency_p95")
	}
	if rule.Threshold < 0 {
		return fmt.Errorf("threshold must be non-negative")
	}
	if rule.WindowMinutes < 1 {
		return fmt.Errorf("window_minutes must be at least 1")
	}
	return nil
}

func parseWindow(raw string) (time.Duration, error) {
	switch strings.TrimSpace(raw) {
	case "", "5m":
		return 5 * time.Minute, nil
	case "30m":
		return 30 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid window %q", raw)
	}
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}

	index := int(math.Ceil(p*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
