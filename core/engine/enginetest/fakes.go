// Package enginetest has fakes of mihomo for tests and development runs of
// the helper: an HTTP controller that "loads" config files, and a shell
// script that answers `mihomo -v` and `mihomo -t`. Nothing here touches a
// real mihomo.
package enginetest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Markers that steer the fakes from inside a config.
const (
	// MarkInvalid makes the fake `mihomo -t` reject the config.
	MarkInvalid = "duet-test-invalid"
	// MarkBreakController makes the fake controller stop answering after it
	// loads the config.
	MarkBreakController = "duet-test-break-controller"
	// MarkReloadFail makes PUT /configs fail.
	MarkReloadFail = "duet-test-reload-fail"
)

// Controller is a fake mihomo external-controller.
type Controller struct {
	mu         sync.Mutex
	Secret     string
	KernelVer  string
	Down       bool     // /version fails
	Reloads    []string // paths passed to PUT /configs
	Forces     []bool   // the force flag of each reload
	Loaded     []byte   // content of the last loaded file
	BadAuth    int
	VersionHit int
}

// NewController returns a running fake with version v1.19.34.
func NewController(secret string) *Controller {
	return &Controller{Secret: secret, KernelVer: "v1.19.34"}
}

func (c *Controller) authOK(r *http.Request) bool {
	if c.Secret == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+c.Secret
}

// ServeHTTP implements the subset of the API the helper uses.
func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.authOK(r) {
		c.BadAuth++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/version":
		c.VersionHit++
		if c.Down {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"meta": true, "version": c.KernelVer})
	case r.Method == http.MethodPut && r.URL.Path == "/configs":
		force := r.URL.Query().Get("force") == "true"
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
			http.Error(w, `{"message":"Body invalid"}`, http.StatusBadRequest)
			return
		}
		b, err := os.ReadFile(body.Path)
		if err != nil {
			http.Error(w, `{"message":"read config failed"}`, http.StatusBadRequest)
			return
		}
		c.Reloads = append(c.Reloads, body.Path)
		c.Forces = append(c.Forces, force)
		if bytes.Contains(b, []byte(MarkReloadFail)) {
			http.Error(w, `{"message":"parse config error"}`, http.StatusBadRequest)
			return
		}
		c.Loaded = b
		c.Down = bytes.Contains(b, []byte(MarkBreakController))
		// Like mihomo, a hot reload does not change the controller's
		// secret (hub.applyRoute runs only at start); see Restart.
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func topSecret(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "secret: "); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// Restart emulates a restart of mihomo: the secret of the last loaded
// config takes effect.
func (c *Controller) Restart() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Loaded != nil {
		c.Secret = topSecret(c.Loaded)
	}
}

// Start emulates a fresh mihomo process that loaded cfg: its secret takes
// effect and the controller answers.
func (c *Controller) Start(cfg []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Loaded = append([]byte(nil), cfg...)
	c.Secret = topSecret(cfg)
	c.Down = false
}

// ForceFlags returns the force flag of each reload so far.
func (c *Controller) ForceFlags() []bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]bool(nil), c.Forces...)
}

// State returns a copy of the counters (safe for concurrent use).
func (c *Controller) State() (reloads []string, loaded []byte, down bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.Reloads...), append([]byte(nil), c.Loaded...), c.Down
}

// SetDown makes /version fail or succeed.
func (c *Controller) SetDown(d bool) {
	c.mu.Lock()
	c.Down = d
	c.mu.Unlock()
}

// FakeMihomoScript is a /bin/sh stand-in for the mihomo binary.
const FakeMihomoScript = `#!/bin/sh
# Fake mihomo for duet tests. Answers -v and -t only.
case "$1" in
-v)
  echo "Mihomo Meta v1.19.34 darwin arm64 with go1.27.4 (fake)"
  exit 0 ;;
-t)
  shift
  while [ $# -gt 0 ]; do
    case "$1" in -f) f="$2"; shift ;; -d) d="$2"; shift ;; esac
    shift
  done
  [ -n "$f" ] && [ -f "$f" ] || { echo 'level=fatal msg="no config file"'; exit 1; }
  if grep -q '` + MarkInvalid + `' "$f"; then
    echo 'time="2026-11-26T00:00:00Z" level=error msg="proxy group[0]: '\''missing-node'\'' not found"'
    echo "configuration file $f test failed"
    exit 1
  fi
  echo "configuration file $f test is successful"
  exit 0 ;;
esac
echo "fake mihomo: unsupported arguments: $*" >&2
exit 2
`

// WriteFakeMihomo writes FakeMihomoScript to dir/mihomo and returns its path.
func WriteFakeMihomo(dir string) (string, error) {
	p := filepath.Join(dir, "mihomo")
	return p, os.WriteFile(p, []byte(FakeMihomoScript), 0o755)
}
