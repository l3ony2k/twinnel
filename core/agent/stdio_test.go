package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a goroutine-safe bytes.Buffer (stderr in the tests).
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

const initMsg = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

// TestStdioTolerant serves a real server over the stdio transport and feeds
// it what clients in the wild send: a UTF-8 byte order mark (PowerShell's
// default StreamWriter), leading blank lines and spaces, CRLF, and a line
// that is not JSON. The server answers every real message and keeps going.
func TestStdioTolerant(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var errw syncBuf
	srv := New(Deps{Version: "test"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, NewStdioTransport(inR, outW, &errw)) }()

	lines := bufio.NewScanner(outR)
	next := func() map[string]any {
		t.Helper()
		if !lines.Scan() {
			t.Fatalf("no reply (stderr: %s)", errw.String())
		}
		var m map[string]any
		if err := json.Unmarshal(lines.Bytes(), &m); err != nil {
			t.Fatalf("reply %q: %v", lines.Text(), err)
		}
		return m
	}
	send := func(s string) {
		t.Helper()
		if _, err := io.WriteString(inW, s); err != nil {
			t.Fatal(err)
		}
	}

	send("\ufeff\r\n  \n \t" + initMsg + "\r\n")
	if m := next(); m["id"] != float64(1) || m["result"] == nil {
		t.Fatalf("initialize = %v", m)
	}
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")

	send("this is not json\n")
	m := next()
	if e, _ := m["error"].(map[string]any); e == nil || e["code"] != float64(-32700) {
		t.Fatalf("garbage line = %v", m)
	}
	if !strings.Contains(errw.String(), "not JSON") {
		t.Fatalf("stderr = %q", errw.String())
	}

	send("\ufeff" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\r\n")
	if m := next(); m["id"] != float64(2) || m["result"] == nil {
		t.Fatalf("tools/list after garbage = %v", m)
	}

	inW.Close()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("server did not stop at EOF")
	}
}
