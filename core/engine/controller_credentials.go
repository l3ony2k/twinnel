package engine

import (
	"context"
	"os"

	"github.com/l3ony2k/duet/core/rpc"
)

// ControllerCredentials returns what a client needs to talk to the running
// mihomo controller: the address from settings and the secret mihomo
// loaded (the config file's, as in currentController; the stored one when
// there is no file yet). The secret is never logged.
func (e *Engine) ControllerCredentials() (rpc.ControllerCredentials, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return rpc.ControllerCredentials{}, err
	}
	out := rpc.ControllerCredentials{Controller: st.Mihomo.Controller}
	if b, err := os.ReadFile(st.Mihomo.ConfigFile); err == nil {
		out.Secret, _ = topLevel(b, "secret")
		out.Source = rpc.CredentialsFromConfig
	} else {
		out.Secret = e.controllerSecret()
		out.Source = rpc.CredentialsFromStore
	}
	return out, nil
}

func (e *Engine) registerCredentials(s *rpc.Server) {
	rpc.Typed(s, rpc.MethodControllerCredentials, func(ctx context.Context, _ rpc.Empty) (rpc.ControllerCredentials, error) {
		return e.ControllerCredentials()
	})
}
