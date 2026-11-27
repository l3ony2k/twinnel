package health

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Everything here talks to listeners on 127.0.0.1 only.

func TestNetDialTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := NetDialTCP(ctx, ln.Addr().String()); err != nil {
		t.Fatalf("dial: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	if err := NetDialTCP(ctx, addr); !errors.Is(err, ErrRefused) {
		t.Fatalf("closed port: %v, want ErrRefused", err)
	}
}

// fakeProxy answers CONNECT and pipes to the target, or answers status.
func fakeProxy(t *testing.T, status int) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				if status != http.StatusOK {
					io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
					return
				}
				up, err := net.Dial("tcp", req.Host)
				if err != nil {
					return
				}
				defer up.Close()
				io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
				go io.Copy(up, br)
				io.Copy(c, up)
			}()
		}
	}()
	return ln.Addr().String()
}

func TestNetDialViaProxy(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	target := strings.TrimPrefix(srv.URL, "https://")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := NetDialViaProxy(ctx, fakeProxy(t, 200), target, "example.test"); err != nil {
		t.Fatalf("via proxy: %v", err)
	}
	if err := NetDialViaProxy(ctx, fakeProxy(t, 502), target, "example.test"); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("502: %v", err)
	}
}

func TestEchoMessages(t *testing.T) {
	req := EchoRequest(0x1234, 1)
	if icmpChecksum(req) != 0 {
		t.Fatal("checksum does not verify")
	}
	reply := append([]byte(nil), req...)
	reply[0] = 0
	if !IsEchoReply(reply, 0x1234, 1) || IsEchoReply(req, 0x1234, 1) || IsEchoReply(reply, 0x1234, 2) {
		t.Fatal("echo reply matching")
	}
	withIP := append(append([]byte{0x45}, make([]byte, 19)...), reply...)
	if !IsEchoReply(withIP, 0x1234, 1) {
		t.Fatal("reply behind an IPv4 header")
	}
}
