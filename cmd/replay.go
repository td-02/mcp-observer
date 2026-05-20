package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mcpscope/internal/replay"
	"mcpscope/internal/store"
)

func init() {
	rootCmd.AddCommand(newReplayCmd())
}

func newReplayCmd() *cobra.Command {
	var dbPath string
	var traceID string
	var server string
	var all bool
	var ignoreFields string

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay recorded traces against a server and compare responses",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath = effectiveString(cmd, "db", dbPath, loadedConfig.Proxy.DB)

			traceStore, err := store.OpenSQLite(cmd.Context(), dbPath)
			if err != nil {
				return err
			}
			defer traceStore.Close()

			ignore := parseIgnoreFields(ignoreFields)
			switch {
			case all:
				traces, err := replay.LoadAllTraces(cmd.Context(), traceStore)
				if err != nil {
					return err
				}
				return replayAll(cmd, server, args, traces, ignore)
			case strings.TrimSpace(traceID) == "":
				return fmt.Errorf("--id is required unless --all is set")
			default:
				trace, err := replay.LoadTraceByID(cmd.Context(), traceStore, traceID)
				if err != nil {
					return err
				}
				return replayOne(cmd, server, args, trace, ignore)
			}
		},
	}

	cmd.Flags().StringVar(&dbPath, "db", "mcpscope.db", "SQLite database path for persisted traces")
	cmd.Flags().StringVar(&traceID, "id", "", "Trace ID to replay")
	cmd.Flags().StringVar(&server, "server", "", "Target MCP server command or HTTP URL")
	cmd.Flags().BoolVar(&all, "all", false, "Replay every trace in the database")
	cmd.Flags().StringVar(&ignoreFields, "ignore-fields", "", "Comma-separated JSON paths to ignore when comparing responses")

	return cmd
}

func replayOne(cmd *cobra.Command, server string, args []string, trace store.Trace, ignoreFields []string) error {
	body, latencyMs, err := invokeReplayTarget(cmd.Context(), server, args, trace)
	if err != nil {
		return err
	}

	match, diff, err := replay.CompareResponses([]byte(trace.ResponsePayload), body, ignoreFields)
	if err != nil {
		return err
	}

	if match {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "PASS  %s  (%dms)\n", trace.TraceID, latencyMs); err != nil {
			return err
		}
		return nil
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "FAIL  %s  response mismatch (%dms)\n", trace.TraceID, latencyMs); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "original vs new:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), diff); err != nil {
		return err
	}
	return exitCodeError{code: 1, err: errors.New("response mismatch")}
}

func replayAll(cmd *cobra.Command, server string, args []string, traces []store.Trace, ignoreFields []string) error {
	failures := 0
	for _, trace := range traces {
		body, latencyMs, err := invokeReplayTarget(cmd.Context(), server, args, trace)
		if err != nil {
			failures++
			if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "FAIL  %s  %v\n", trace.TraceID, err); writeErr != nil {
				return writeErr
			}
			continue
		}

		match, _, err := replay.CompareResponses([]byte(trace.ResponsePayload), body, ignoreFields)
		if err != nil {
			failures++
			if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "FAIL  %s  %v\n", trace.TraceID, err); writeErr != nil {
				return writeErr
			}
			continue
		}

		if match {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "PASS  %s  (%dms)\n", trace.TraceID, latencyMs); err != nil {
				return err
			}
			continue
		}

		failures++
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "FAIL  %s  response mismatch (%dms)\n", trace.TraceID, latencyMs); err != nil {
			return err
		}
	}

	if failures > 0 {
		return exitCodeError{code: failures, err: fmt.Errorf("%d replay failures", failures)}
	}
	return nil
}

func invokeReplayTarget(ctx context.Context, server string, args []string, trace store.Trace) ([]byte, int64, error) {
	if isHTTPServer(server) {
		return replay.InvokeHTTP(ctx, server, trace)
	}
	command := commandFromInputs(strings.TrimSpace(server), args)
	if len(command) == 0 {
		return nil, 0, fmt.Errorf("provide --server or a command after `--`")
	}
	return invokeReplayStdio(ctx, command, trace)
}

func invokeReplayStdio(ctx context.Context, command []string, trace store.Trace) ([]byte, int64, error) {
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, 0, err
	}

	started := time.Now()
	payload, err := replay.BuildJSONRPCRequest(trace, trace.TraceID)
	if err != nil {
		return nil, 0, err
	}
	reader := bufio.NewReader(stdout)
	if _, err := fmt.Fprintf(stdin, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return nil, 0, err
	}
	if _, err := stdin.Write(payload); err != nil {
		return nil, 0, err
	}
	response, err := readReplayFrame(reader)
	if err != nil {
		return nil, 0, err
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		return nil, 0, err
	}
	return response, time.Since(started).Milliseconds(), nil
}

func readReplayFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		name, value, found := strings.Cut(trimmed, ":")
		if found && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	payload := make([]byte, contentLength)
	_, err := io.ReadFull(reader, payload)
	return payload, err
}

func parseIgnoreFields(raw string) []string {
	parts := strings.Split(raw, ",")
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		fields = append(fields, value)
	}
	return fields
}
