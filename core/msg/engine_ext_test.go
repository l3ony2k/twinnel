package msg_test

import (
	"testing"

	"github.com/l3ony2k/twinnel/core/engine"
	"github.com/l3ony2k/twinnel/core/msg"
)

// The engine's own texts (an external test: the engine imports msg).
func TestEngineLoginInSession(t *testing.T) {
	if m := msg.Parse(engine.ErrLoginInSession); m.Code != "ts-login-session" {
		t.Fatalf("%+v", m)
	}
}
