package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestCreateSnapshotFromMockStdioServer(t *testing.T) {
	t.Parallel()

	serverPath := os.Args[0]

	cmd := exec.CommandContext(context.Background(), serverPath, "-test.run=TestHelperProcessSnapshotServer")
	cmd.Env = append(os.Environ(), "MCPSCOPE_TEST_MOCK_MCP=1")

	snapshot, err := snapshotFromCommand(context.Background(), cmd)
	if err != nil {
		t.Fatalf("snapshotFromCommand returned error: %v", err)
	}

	if snapshot.ServerName != "mock-server" {
		t.Fatalf("server_name = %q", snapshot.ServerName)
	}
	if snapshot.ServerVersion != "1.2.3" {
		t.Fatalf("server_version = %q", snapshot.ServerVersion)
	}
	if len(snapshot.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(snapshot.Tools))
	}
	if snapshot.Tools[0].Name != "alpha" {
		t.Fatalf("first tool = %q", snapshot.Tools[0].Name)
	}
}

func TestResolveSnapshotTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		server      string
		args        []string
		wantURL     string
		wantCommand []string
		wantErr     bool
	}{
		{
			name:        "command args",
			args:        []string{"uv", "run", "server.py"},
			wantCommand: []string{"uv", "run", "server.py"},
		},
		{
			name:    "http server flag",
			server:  "http://127.0.0.1:9000",
			wantURL: "http://127.0.0.1:9000",
		},
		{
			name:    "conflicting inputs",
			server:  "server.exe",
			args:    []string{"node", "server.js"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target, err := resolveSnapshotTarget(tc.server, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if target.url != tc.wantURL {
				t.Fatalf("url = %q, want %q", target.url, tc.wantURL)
			}
			if len(target.command) != len(tc.wantCommand) {
				t.Fatalf("command = %v, want %v", target.command, tc.wantCommand)
			}
			for i := range target.command {
				if target.command[i] != tc.wantCommand[i] {
					t.Fatalf("command = %v, want %v", target.command, tc.wantCommand)
				}
			}
		})
	}
}

func TestWritePrettySnapshot(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := writePrettySnapshot(&buf, snapshotOutput{
		Tools: []snapshotTool{
			{Name: "alpha", Description: "first"},
			{Name: "beta", Description: "second"},
		},
	})
	if err != nil {
		t.Fatalf("writePrettySnapshot returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "alpha") || !strings.Contains(output, "second") {
		t.Fatalf("unexpected pretty output: %q", output)
	}
}

func TestHelperProcessSnapshotServer(t *testing.T) {
	if os.Getenv("MCPSCOPE_TEST_MOCK_MCP") != "1" {
		return
	}

	runMockMCPServer()
	os.Exit(0)
}

func runMockMCPServer() {
	reader := bufio.NewReader(os.Stdin)
	for {
		req, err := readMockRequest(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			panic(err)
		}

		switch req.Method {
		case "initialize":
			writeMockResponse(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"serverInfo": map[string]any{
						"name":    "mock-server",
						"version": "1.2.3",
					},
				},
			})
		case "notifications/initialized":
		case "tools/list":
			writeMockResponse(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "alpha",
							"description": "First tool",
							"inputSchema": map[string]any{"type": "object"},
						},
						{
							"name":        "beta",
							"description": "Second tool",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}},
						},
					},
				},
			})
			return
		default:
			writeMockResponse(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error": map[string]any{
					"message": "unsupported method",
				},
			})
		}
	}
}

type mockRequest struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
}

func readMockRequest(reader *bufio.Reader) (mockRequest, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return mockRequest{}, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		name, value, found := strings.Cut(trimmed, ":")
		if found && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return mockRequest{}, err
			}
			contentLength = parsed
		}
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return mockRequest{}, err
	}

	var req mockRequest
	return req, json.Unmarshal(payload, &req)
}

func writeMockResponse(value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		panic(err)
	}
	if _, err := os.Stdout.Write(payload); err != nil {
		panic(err)
	}
}
