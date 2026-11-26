package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

// StatusFileEnv names a saved `tailscale status --json` that replaces the
// LocalAPI (DefaultAPI). Development only: screenshot runs set it to a
// synthetic fixture (cmd/app/testdata/shots-tailscale-status.json) so no
// real account, tailnet or device name ends up in a shot. It is never set
// in a release; the app says so in its log when it is.
const StatusFileEnv = "DUET_TAILSCALE_STATUS_FILE"

// ErrStatusFile is returned by a FileAPI's writes: the fixture is read-only.
var ErrStatusFile = errors.New("tailscale: reading a status file (" + StatusFileEnv + "); changes are not possible")

// DefaultAPI is the LocalAPI on the default socket, or FileAPI when
// StatusFileEnv is set.
func DefaultAPI() API {
	if path := os.Getenv(StatusFileEnv); path != "" {
		return FileAPI(path)
	}
	return LocalAPI(nil)
}

// FileAPI answers from a saved `tailscale status --json` at path, read on
// every Status call. Prefs follow the status (running, MagicDNS on, the
// exit node it names); writes fail with ErrStatusFile; the IPN bus never
// sends anything.
func FileAPI(path string) API { return fileAPI{path} }

type fileAPI struct{ path string }

func (f fileAPI) Status(context.Context) (*ipnstate.Status, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		return nil, err
	}
	var st ipnstate.Status
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", f.path, err)
	}
	return &st, nil
}

func (f fileAPI) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	st, err := f.Status(ctx)
	if err != nil {
		return nil, err
	}
	p := &ipn.Prefs{WantRunning: st.BackendState == ipn.Running.String(), CorpDNS: true}
	if st.ExitNodeStatus != nil {
		p.ExitNodeID = st.ExitNodeStatus.ID
	}
	return p, nil
}

func (fileAPI) EditPrefs(context.Context, *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	return nil, ErrStatusFile
}

func (fileAPI) WatchIPNBus(ctx context.Context, _ ipn.NotifyWatchOpt) (Watcher, error) {
	return idleWatcher{ctx}, nil
}

// idleWatcher blocks until its context ends: a file sends no changes.
type idleWatcher struct{ ctx context.Context }

func (w idleWatcher) Next() (ipn.Notify, error) {
	<-w.ctx.Done()
	return ipn.Notify{}, w.ctx.Err()
}

func (idleWatcher) Close() error { return nil }
