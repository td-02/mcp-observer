package proxy

import (
	"bufio"
	"bytes"
	"context"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcpscope/internal/intercept"
	"mcpscope/internal/store"
	"mcpscope/internal/telemetry"
)

type Config struct {
	Server     string
	ServerName string
	Port       int
	Transport  string
	Store      store.TraceStore
	Telemetry  *telemetry.Client
	Dashboard  fs.FS
	eventHub   *traceEventHub
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
}

func Run(ctx context.Context, cfg Config) error {
	cfg.eventHub = newTraceEventHub()

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
	ServerName   string          `json:"server_name"`
	Method       string          `json:"method"`
	Params       json.RawMessage `json:"params,omitempty"`
	Response     json.RawMessage `json:"response,omitempty"`
	LatencyMs    int64           `json:"latency_ms"`
	IsError      bool            `json:"is_error"`
	ErrorMessage string          `json:"error_message,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type latencyStatRecord struct {
	ServerName string `json:"server_name"`
	Method     string `json:"method"`
	Count      int    `json:"count"`
	P50Ms      int64  `json:"p50_ms"`
	P95Ms      int64  `json:"p95_ms"`
	P99Ms      int64  `json:"p99_ms"`
}

type errorStatRecord struct {
	Method             string     `json:"method"`
	Count              int        `json:"count"`
	ErrorCount         int        `json:"error_count"`
	ErrorRatePct       float64    `json:"error_rate_pct"`
	RecentErrorMessage string     `json:"recent_error_message,omitempty"`
	RecentErrorAt      *time.Time `json:"recent_error_at,omitempty"`
}

type traceEventHub struct {
	mu          sync.RWMutex
	subscribers map[chan traceAPIRecord]struct{}
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
	server, serverErr, err := startHTTPServer(ctx, cfg, nil)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, cfg.Server)
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
		if err := captureAndPersist(ctx, cfg, "stdio", direction, receivedAt, sentAt, frame.payload); err != nil {
			return err
		}
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

	cmd := exec.CommandContext(ctx, cfg.Server)
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

	targetBaseURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", upstreamPort))
	if err != nil {
		return fmt.Errorf("build upstream url: %w", err)
	}

	var mu sync.Mutex
	client := &http.Client{}
	server, serverErr, err := startHTTPServer(ctx, cfg, func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		requestReceivedAt := time.Now()

		upstreamURL := *targetBaseURL
		upstreamURL.Path = r.URL.Path
		upstreamURL.RawQuery = r.URL.RawQuery

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
		if err := captureAndPersist(r.Context(), cfg, "http", "client_to_server", requestReceivedAt, requestSentAt, requestBody); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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
		_ = captureAndPersist(r.Context(), cfg, "http", "server_to_client", responseReceivedAt, responseSentAt, responseBody)
	})
	if err != nil {
		return err
	}

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

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

	if err := intercept.EmitLog(cfg.Stderr, event); err != nil {
		return err
	}

	if cfg.Telemetry != nil {
		cfg.Telemetry.RecordCall(ctx, cfg.ServerName, event)
	}

	recordID := intercept.NewUUID()
	record := traceRecordFromEvent(recordID, cfg.ServerName, event)
	if cfg.eventHub != nil {
		cfg.eventHub.Publish(record)
	}

	if cfg.Store == nil {
		return nil
	}

	if err := cfg.Store.Insert(ctx, store.Trace{
		ID:              recordID,
		TraceID:         event.TraceID,
		ServerName:      cfg.ServerName,
		Method:          event.Method,
		ParamsHash:      event.ParamsHash,
		ParamsPayload:   rawMessageString(event.Params),
		ResponseHash:    event.ResponseHash,
		ResponsePayload: rawMessageString(selectResponsePayload(event)),
		LatencyMs:       event.LatencyMs,
		IsError:         event.IsError,
		ErrorMessage:    event.ErrorMessage,
		CreatedAt:       time.Unix(0, event.ReceivedAtUnixN).UTC(),
	}); err != nil {
		return err
	}

	return nil
}

func validateUpstreamPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("derived upstream port %d is out of range", port)
	}
	return nil
}

func startHTTPServer(ctx context.Context, cfg Config, proxyPostHandler http.HandlerFunc) (*http.Server, <-chan error, error) {
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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/traces":
			handleTraceList(w, r, cfg)
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
}

func handleTraceList(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")
	if cfg.Store == nil {
		_ = json.NewEncoder(w).Encode([]traceAPIRecord{})
		return
	}

	serverName := strings.TrimSpace(r.URL.Query().Get("server"))
	traces, err := cfg.Store.Query(r.Context(), store.QueryFilter{
		ServerName: serverName,
		Limit:      200,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	records := make([]traceAPIRecord, 0, len(traces))
	for _, trace := range traces {
		records = append(records, traceRecordFromStored(trace))
	}

	_ = json.NewEncoder(w).Encode(records)
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

	serverName := strings.TrimSpace(r.URL.Query().Get("server"))
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
			if serverName != "" && record.ServerName != serverName {
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

func handleLatencyStats(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")

	traces, err := queryWindowedTraces(r, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	type key struct {
		server string
		method string
	}

	grouped := make(map[key][]int64)
	for _, trace := range traces {
		if trace.Method == "" {
			continue
		}
		k := key{server: trace.ServerName, method: trace.Method}
		grouped[k] = append(grouped[k], trace.LatencyMs)
	}

	records := make([]latencyStatRecord, 0, len(grouped))
	for k, values := range grouped {
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		records = append(records, latencyStatRecord{
			ServerName: k.server,
			Method:     k.method,
			Count:      len(values),
			P50Ms:      percentile(values, 0.50),
			P95Ms:      percentile(values, 0.95),
			P99Ms:      percentile(values, 0.99),
		})
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].ServerName == records[j].ServerName {
			return records[i].Method < records[j].Method
		}
		return records[i].ServerName < records[j].ServerName
	})

	_ = json.NewEncoder(w).Encode(records)
}

func handleErrorStats(w http.ResponseWriter, r *http.Request, cfg Config) {
	w.Header().Set("Content-Type", "application/json")

	traces, err := queryWindowedTraces(r, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	grouped := make(map[string]*errorStatRecord)
	for _, trace := range traces {
		if trace.Method == "" {
			continue
		}

		record := grouped[trace.Method]
		if record == nil {
			record = &errorStatRecord{Method: trace.Method}
			grouped[trace.Method] = record
		}

		record.Count++
		if trace.IsError {
			record.ErrorCount++
			if record.RecentErrorAt == nil || trace.CreatedAt.After(*record.RecentErrorAt) {
				ts := trace.CreatedAt
				record.RecentErrorAt = &ts
				record.RecentErrorMessage = trace.ErrorMessage
			}
		}
	}

	records := make([]errorStatRecord, 0, len(grouped))
	for _, record := range grouped {
		if record.Count > 0 {
			record.ErrorRatePct = float64(record.ErrorCount) * 100 / float64(record.Count)
		}
		records = append(records, *record)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].ErrorRatePct == records[j].ErrorRatePct {
			return records[i].Method < records[j].Method
		}
		return records[i].ErrorRatePct > records[j].ErrorRatePct
	})

	_ = json.NewEncoder(w).Encode(records)
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

func traceRecordFromEvent(id, serverName string, event intercept.Event) traceAPIRecord {
	return traceAPIRecord{
		ID:           id,
		TraceID:      event.TraceID,
		ServerName:   serverName,
		Method:       event.Method,
		Params:       cloneRawMessage(event.Params),
		Response:     cloneRawMessage(selectResponsePayload(event)),
		LatencyMs:    event.LatencyMs,
		IsError:      event.IsError,
		ErrorMessage: event.ErrorMessage,
		CreatedAt:    time.Unix(0, event.ReceivedAtUnixN).UTC(),
	}
}

func traceRecordFromStored(trace store.Trace) traceAPIRecord {
	return traceAPIRecord{
		ID:           trace.ID,
		TraceID:      trace.TraceID,
		ServerName:   trace.ServerName,
		Method:       trace.Method,
		Params:       asRawJSON(trace.ParamsPayload),
		Response:     asRawJSON(trace.ResponsePayload),
		LatencyMs:    trace.LatencyMs,
		IsError:      trace.IsError,
		ErrorMessage: trace.ErrorMessage,
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

func queryWindowedTraces(r *http.Request, cfg Config) ([]store.Trace, error) {
	if cfg.Store == nil {
		return []store.Trace{}, nil
	}

	window, err := parseWindow(r.URL.Query().Get("window"))
	if err != nil {
		return nil, err
	}

	serverName := strings.TrimSpace(r.URL.Query().Get("server"))
	start := time.Now().Add(-window)

	return cfg.Store.Query(r.Context(), store.QueryFilter{
		ServerName:   serverName,
		CreatedAfter: &start,
	})
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
