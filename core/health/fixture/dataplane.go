package fixture

import (
	"context"
	"errors"

	"github.com/l3ony2k/twinnel/core/health"
)

var _ health.DataplaneEnv = (*Env)(nil)

// errNoAnswer stands in for a handshake past its deadline.
var errNoAnswer = timeout{}

type timeout struct{}

func (timeout) Error() string   { return "i/o timeout" }
func (timeout) Timeout() bool   { return true }
func (timeout) Temporary() bool { return true }

// DialTCP answers from the fixture: never the network.
func (e *Env) DialTCP(context.Context, string) error {
	if e.M.TUNTCPDead {
		return errNoAnswer
	}
	return nil
}

func (e *Env) DialViaProxy(context.Context, string, string, string) error {
	if e.M.Runtime == nil {
		return errors.New("fixture: mihomo controller not reachable")
	}
	return nil
}

func (e *Env) Ping(context.Context, string) error { return nil }
