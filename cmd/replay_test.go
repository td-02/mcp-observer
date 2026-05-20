package cmd

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseIgnoreFields(t *testing.T) {
	t.Parallel()

	fields := parseIgnoreFields(" $.timestamp , $.request_id,, ")
	if len(fields) != 2 || fields[0] != "$.timestamp" || fields[1] != "$.request_id" {
		t.Fatalf("unexpected parsed fields: %#v", fields)
	}
}

func TestReadReplayFrame(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("Content-Length: 11\r\n\r\n{\"ok\":true}"))
	payload, err := readReplayFrame(reader)
	if err != nil {
		t.Fatalf("readReplayFrame returned error: %v", err)
	}
	if string(payload) != "{\"ok\":true}" {
		t.Fatalf("unexpected payload: %q", string(payload))
	}
}
