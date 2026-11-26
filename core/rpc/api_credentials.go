package rpc

import (
	"fmt"
	"log/slog"
)

// MethodControllerCredentials returns the address and secret of the mihomo
// external controller the helper manages. Only authorized peers reach the
// helper (root, the admin group, the helper's own uid), and any of them can
// already reconfigure mihomo through it, so handing out the secret does
// not widen access. The app uses it instead of a secret typed into its
// settings, which goes stale after controller.secure.
const MethodControllerCredentials = "controller.credentials"

// Where ControllerCredentials.Secret came from.
const (
	CredentialsFromConfig = "config" // the config file mihomo loaded
	CredentialsFromStore  = "store"  // the secret store (no config file yet)
)

// ControllerCredentials is the result of controller.credentials. Never log
// it: String and LogValue hide the secret.
type ControllerCredentials struct {
	Controller string `json:"controller"`
	Secret     string `json:"secret,omitempty"`
	Source     string `json:"source,omitempty"`
}

func (c ControllerCredentials) String() string {
	return fmt.Sprintf("{controller:%s secret:%s source:%s}", c.Controller, redactedSecret(c.Secret), c.Source)
}

// GoString keeps %#v from printing the secret too.
func (c ControllerCredentials) GoString() string { return c.String() }

func (c ControllerCredentials) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("controller", c.Controller),
		slog.String("secret", redactedSecret(c.Secret)),
		slog.String("source", c.Source),
	)
}

func redactedSecret(s string) string {
	if s == "" {
		return "(none)"
	}
	return "(set)"
}
