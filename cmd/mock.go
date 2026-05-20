package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mcpscope/internal/replay"
	"mcpscope/internal/store"
)

func init() {
	rootCmd.AddCommand(newMockCmd())
}

func newMockCmd() *cobra.Command {
	var dbPath string
	var port int

	cmd := &cobra.Command{
		Use:   "mock",
		Short: "Serve recorded MCP responses from the trace database",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath = effectiveString(cmd, "db", dbPath, loadedConfig.Proxy.DB)

			traceStore, err := store.OpenSQLite(cmd.Context(), dbPath)
			if err != nil {
				return err
			}
			defer traceStore.Close()

			listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				return fmt.Errorf("listen on port %d: %w", port, err)
			}
			defer listener.Close()

			server := &http.Server{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					handleMockRequest(w, r, traceStore)
				}),
			}

			go func() {
				<-cmd.Context().Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdownCtx)
			}()

			if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dbPath, "db", "mcpscope.db", "SQLite database path for persisted traces")
	cmd.Flags().IntVar(&port, "port", 5555, "Port to listen on")

	return cmd
}

func handleMockRequest(w http.ResponseWriter, r *http.Request, traceStore store.TraceStore) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respondMockError(w, "invalid request")
		return
	}
	if strings.TrimSpace(request.Method) == "" {
		respondMockError(w, "missing method")
		return
	}

	traces, err := traceStore.Query(r.Context(), store.QueryFilter{Method: request.Method})
	if err != nil {
		respondMockError(w, "trace lookup failed")
		return
	}

	trace, ok := replay.MatchTraceByParams(traces, request.Method, request.Params)
	if !ok {
		respondMockError(w, "no recorded response")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]any{
		"jsonrpc": "2.0",
		"id":      request.ID,
	}
	if trace.IsError {
		response["error"] = json.RawMessage(trace.ResponsePayload)
	} else {
		response["result"] = json.RawMessage(trace.ResponsePayload)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func respondMockError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]any{"error": message}
	_ = json.NewEncoder(w).Encode(response)
}

func mockMatchParamsHash(method string, params json.RawMessage) string {
	_ = method
	return replay.ParamsHash(params)
}

func mockMatchTrace(traces []store.Trace, method string, params json.RawMessage) (store.Trace, bool) {
	return replay.MatchTraceByParams(traces, method, params)
}

func parsePort(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > 65535 {
		return 0, fmt.Errorf("invalid port %q", raw)
	}
	return parsed, nil
}
