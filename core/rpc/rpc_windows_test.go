//go:build windows

package rpc

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

var pipeSeq atomic.Int64

func testTransport(t *testing.T) (Transport, string) {
	return NewPipeTransport(), fmt.Sprintf(`\\.\pipe\twinnel-test-%d-%d`, os.Getpid(), pipeSeq.Add(1))
}

// The pipe identifies the client: the test process's own SID, which the
// default descriptor allows.
func TestPipePeerAndSecondListener(t *testing.T) {
	tr, addr := testTransport(t)
	l, err := tr.Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err := tr.Listen(addr); err == nil {
		t.Fatal("second listener on a live pipe succeeded")
	}
	self, err := currentSID()
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan Peer, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			got <- Peer{}
			return
		}
		defer c.Close()
		p, _ := tr.PeerOf(c)
		got <- p
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := tr.Dial(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := <-got
	if p.SID != self || p.PID != os.Getpid() || p.UID != -1 {
		t.Fatalf("peer %+v, want SID %s pid %d", p, self, os.Getpid())
	}
	if err := (&SIDAuthorizer{SelfSID: self}).Authorize(p); err != nil {
		t.Fatal(err)
	}
}
