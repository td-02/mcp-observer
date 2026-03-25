package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

const mcpProtocolVersion = "2024-11-05"

type snapshotOutput struct {
	Timestamp     string         `json:"timestamp"`
	ServerName    string         `json:"server_name"`
	ServerVersion string         `json:"server_version"`
	Tools         []snapshotTool `json:"tools"`
}

type snapshotTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type initializeResponse struct {
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

type toolsListResponse struct {
	Tools []snapshotTool `json:"tools"`
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponseEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type rpcErrorMessage struct {
	Message string `json:"message"`
}

func init() {
	rootCmd.AddCommand(newSnapshotCmd())
}

func newSnapshotCmd() *cobra.Command {
	var server string
	var outputPath string
	var format string

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Capture an MCP server tool schema snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(server) == "" {
				return errors.New("--server is required")
			}

			snapshot, err := createSnapshot(cmd.Context(), server)
			if err != nil {
				return err
			}

			encoded, err := json.MarshalIndent(snapshot, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal snapshot: %w", err)
			}

			if format != "" && format != "pretty" {
				return fmt.Errorf("--format must be empty or \"pretty\"")
			}

			if format == "pretty" {
				if err := writePrettySnapshot(cmd.ErrOrStderr(), snapshot); err != nil {
					return err
				}
			}

			if strings.TrimSpace(outputPath) == "" {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
				return err
			}

			if err := os.WriteFile(outputPath, append(encoded, '\n'), 0o644); err != nil {
				return fmt.Errorf("write snapshot file: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Path to the MCP server binary or an HTTP URL")
	cmd.Flags().StringVar(&outputPath, "output", "", "Path to write the snapshot JSON file")
	cmd.Flags().StringVar(&format, "format", "", "Optional output format. Use \"pretty\" to print a tool summary to stderr")

	return cmd
}

func createSnapshot(ctx context.Context, server string) (snapshotOutput, error) {
	if isHTTPServer(server) {
		return snapshotFromHTTP(ctx, server)
	}

	return snapshotFromStdio(ctx, server)
}

func snapshotFromStdio(ctx context.Context, server string) (snapshotOutput, error) {
	cmd := exec.CommandContext(ctx, server)
	return snapshotFromCommand(ctx, cmd)
}

func snapshotFromCommand(ctx context.Context, cmd *exec.Cmd) (snapshotOutput, error) {
	serverIn, err := cmd.StdinPipe()
	if err != nil {
		return snapshotOutput{}, fmt.Errorf("create subprocess stdin pipe: %w", err)
	}

	serverOut, err := cmd.StdoutPipe()
	if err != nil {
		return snapshotOutput{}, fmt.Errorf("create subprocess stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return snapshotOutput{}, fmt.Errorf("start subprocess: %w", err)
	}

	client := &stdioSnapshotClient{
		reader: bufio.NewReader(serverOut),
		writer: serverIn,
	}

	snapshot, runErr := runSnapshotFlow(ctx, client)
	closeErr := serverIn.Close()
	waitErr := cmd.Wait()

	if runErr != nil {
		return snapshotOutput{}, runErr
	}
	if closeErr != nil {
		return snapshotOutput{}, fmt.Errorf("close subprocess stdin: %w", closeErr)
	}
	if waitErr != nil {
		return snapshotOutput{}, fmt.Errorf("subprocess exited with error: %w", waitErr)
	}

	return snapshot, nil
}

func snapshotFromHTTP(ctx context.Context, server string) (snapshotOutput, error) {
	client := &httpSnapshotClient{
		baseURL: strings.TrimRight(server, "/"),
		client:  &http.Client{Timeout: 15 * time.Second},
	}

	return runSnapshotFlow(ctx, client)
}

type snapshotTransport interface {
	Call(context.Context, rpcRequest) (rpcResponseEnvelope, error)
	Notify(context.Context, rpcRequest) error
}

func runSnapshotFlow(ctx context.Context, transport snapshotTransport) (snapshotOutput, error) {
	initReq := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "mcpscope",
				"version": "0.1.0",
			},
		},
	}

	initResp, err := transport.Call(ctx, initReq)
	if err != nil {
		return snapshotOutput{}, err
	}

	var initResult initializeResponse
	if err := decodeRPCResult(initResp, &initResult); err != nil {
		return snapshotOutput{}, fmt.Errorf("decode initialize response: %w", err)
	}

	if err := transport.Notify(ctx, rpcRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}); err != nil {
		return snapshotOutput{}, err
	}

	toolsResp, err := transport.Call(ctx, rpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
		Params:  map[string]any{},
	})
	if err != nil {
		return snapshotOutput{}, err
	}

	var toolsResult toolsListResponse
	if err := decodeRPCResult(toolsResp, &toolsResult); err != nil {
		return snapshotOutput{}, fmt.Errorf("decode tools/list response: %w", err)
	}

	return snapshotOutput{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		ServerName:    initResult.ServerInfo.Name,
		ServerVersion: initResult.ServerInfo.Version,
		Tools:         toolsResult.Tools,
	}, nil
}

func decodeRPCResult(resp rpcResponseEnvelope, out any) error {
	if len(resp.Error) > 0 && string(resp.Error) != "null" {
		var rpcErr rpcErrorMessage
		if err := json.Unmarshal(resp.Error, &rpcErr); err == nil && rpcErr.Message != "" {
			return errors.New(rpcErr.Message)
		}
		return fmt.Errorf("rpc error: %s", string(resp.Error))
	}

	if len(resp.Result) == 0 || string(resp.Result) == "null" {
		return errors.New("missing rpc result")
	}

	if err := json.Unmarshal(resp.Result, out); err != nil {
		return err
	}

	return nil
}

type stdioSnapshotClient struct {
	reader *bufio.Reader
	writer io.Writer
}

func (c *stdioSnapshotClient) Call(ctx context.Context, req rpcRequest) (rpcResponseEnvelope, error) {
	if err := c.write(req); err != nil {
		return rpcResponseEnvelope{}, err
	}

	return c.readResponse(ctx)
}

func (c *stdioSnapshotClient) Notify(ctx context.Context, req rpcRequest) error {
	return c.write(req)
}

func (c *stdioSnapshotClient) write(req rpcRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(c.writer, header); err != nil {
		return fmt.Errorf("write request header: %w", err)
	}
	if _, err := c.writer.Write(payload); err != nil {
		return fmt.Errorf("write request payload: %w", err)
	}

	return nil
}

func (c *stdioSnapshotClient) readResponse(ctx context.Context) (rpcResponseEnvelope, error) {
	type result struct {
		resp rpcResponseEnvelope
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		resp, err := readFramedResponse(c.reader)
		ch <- result{resp: resp, err: err}
	}()

	select {
	case <-ctx.Done():
		return rpcResponseEnvelope{}, ctx.Err()
	case res := <-ch:
		return res.resp, res.err
	}
}

func readFramedResponse(reader *bufio.Reader) (rpcResponseEnvelope, error) {
	contentLength := -1

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return rpcResponseEnvelope{}, fmt.Errorf("read response header: %w", err)
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}

		name, value, found := strings.Cut(trimmed, ":")
		if found && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return rpcResponseEnvelope{}, fmt.Errorf("parse content length: %w", err)
			}
		}
	}

	if contentLength < 0 {
		return rpcResponseEnvelope{}, errors.New("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return rpcResponseEnvelope{}, fmt.Errorf("read response payload: %w", err)
	}

	var resp rpcResponseEnvelope
	if err := json.Unmarshal(payload, &resp); err != nil {
		return rpcResponseEnvelope{}, fmt.Errorf("decode response payload: %w", err)
	}

	return resp, nil
}

type httpSnapshotClient struct {
	baseURL string
	client  *http.Client
}

func (c *httpSnapshotClient) Call(ctx context.Context, req rpcRequest) (rpcResponseEnvelope, error) {
	resp, err := c.do(ctx, req)
	if err != nil {
		return rpcResponseEnvelope{}, err
	}
	return resp, nil
}

func (c *httpSnapshotClient) Notify(ctx context.Context, req rpcRequest) error {
	_, err := c.do(ctx, req)
	return err
}

func (c *httpSnapshotClient) do(ctx context.Context, req rpcRequest) (rpcResponseEnvelope, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return rpcResponseEnvelope{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return rpcResponseEnvelope{}, fmt.Errorf("build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return rpcResponseEnvelope{}, fmt.Errorf("send http request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		body, _ := io.ReadAll(httpResp.Body)
		return rpcResponseEnvelope{}, fmt.Errorf("http error %d: %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	var resp rpcResponseEnvelope
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return rpcResponseEnvelope{}, fmt.Errorf("decode http response: %w", err)
	}

	return resp, nil
}

func isHTTPServer(server string) bool {
	parsed, err := url.Parse(server)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func writePrettySnapshot(w io.Writer, snapshot snapshotOutput) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "TOOL\tDESCRIPTION\n"); err != nil {
		return err
	}
	for _, tool := range snapshot.Tools {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", tool.Name, tool.Description); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func defaultSnapshotOutputPath(server string) string {
	base := filepath.Base(server)
	return fmt.Sprintf("%s-snapshot.json", strings.TrimSuffix(base, filepath.Ext(base)))
}
