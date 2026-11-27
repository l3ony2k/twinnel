//go:build !windows

package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shortSock returns a socket path short enough for sun_path (104 bytes on
// macOS; t.TempDir() is often longer).
func shortSock(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "twinnelrpc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return filepath.Join(d, "h.sock")
}

func testTransport(t *testing.T) (Transport, string) { return NewUnixTransport(), shortSock(t) }

// The order that made TestUnauthorized flaky, forced: the helper refuses
// the peer and closes before the client writes its request, whose write
// then fails (EPIPE). The call must still report the refusal.
func TestUnauthorizedBeforeRequest(t *testing.T) {
	sock := shortSock(t)
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		c, err := l.Accept()
		if err != nil {
			return
		}
		b, _ := json.Marshal(Response{JSONRPC: Version, ID: json.RawMessage("null"), Error: Errorf(CodeUnauthorized, "not allowed: denied")})
		c.Write(append(b, '\n'))
		c.Close() // at once, as a helper without the linger did
	}()
	raw, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	<-closed
	c := NewClient(&gatedConn{Conn: raw, gate: make(chan struct{})})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Large enough that the write cannot all fit in the socket buffer
	// before the closed peer is noticed.
	err = c.Call(ctx, MethodStatus, map[string]string{"pad": string(make([]byte, 1<<20))}, nil)
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeUnauthorized {
		t.Fatalf("err = %v, want unauthorized", err)
	}
}

// A refused connection stays open for the client's first request: a client
// that writes it only after the refusal arrived writes it without error,
// then reads EOF.
func TestRefusedConnWaitsForRequest(t *testing.T) {
	sock, _ := startServer(t, denyAll{}, func(s *Server) {})
	raw, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(raw)
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil || resp.Error == nil || resp.Error.Code != CodeUnauthorized {
		t.Fatalf("first line %q (%v), want the refusal", line, err)
	}
	if _, err := raw.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"status"}` + "\n")); err != nil {
		t.Fatalf("request after the refusal: %v", err)
	}
	if _, err := r.ReadByte(); err != io.EOF {
		t.Fatalf("after the refusal: %v, want EOF", err)
	}
}

func TestListenStaleAndForeign(t *testing.T) {
	sock := shortSock(t)
	tr := NewUnixTransport()
	// Stale socket (listener gone without unlink).
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	l2, err := tr.Listen(sock)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	fi, _ := os.Stat(sock)
	if fi.Mode().Perm() != 0o660 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	// Live socket: refuse.
	if _, err := tr.Listen(sock); err == nil {
		t.Error("second listener on a live socket succeeded")
	}
	l2.Close()
	// A regular file is never removed.
	os.WriteFile(sock, []byte("keep"), 0o600)
	if _, err := tr.Listen(sock); err == nil {
		t.Error("listened over a regular file")
	}
	if b, _ := os.ReadFile(sock); string(b) != "keep" {
		t.Error("regular file was touched")
	}
}
