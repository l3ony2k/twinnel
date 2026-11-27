package version

import (
	"context"
	"sync"
	"time"

	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
)

// Report is every component's version (`twinnel version --json`).
type Report struct {
	CLI       Entry `json:"cli"`
	Helper    Entry `json:"helper"`
	Mihomo    Entry `json:"mihomo"`
	Tailscale Entry `json:"tailscale"`
	// HelperBinaries are the helper's two copies on disk (nil when neither
	// is found): the one shipped beside the CLI and the installed one the
	// service runs.
	HelperBinaries *HelperBinaries `json:"helper_binaries,omitempty"`
	// ServiceHost is Windows' service host (twinnel-svchost.exe), the
	// copy of the helper that runs mihomo as a service; nil elsewhere.
	// Detail: its path.
	ServiceHost *Entry `json:"service_host,omitempty"`
}

// HelperBinaries compares the helper binary shipped with Twinnel with the
// installed one. On Windows they are %LOCALAPPDATA%\Programs\Twinnel\bin
// and C:\Program Files\Twinnel; `twinnel helper upgrade` (设置 → 后台助手 →
// 更新) copies the first over the second.
type HelperBinaries struct {
	Bundled   Entry  `json:"bundled"`           // Detail: its path
	Installed Entry  `json:"installed"`         // Detail: its path
	Warning   string `json:"warning,omitempty"` // they, or the running helper, differ
}

// HelperDrift says what is out of step between the bundled helper binary,
// the installed one and the running helper ("" when nothing is, or when
// a version is unknown).
func HelperDrift(bundled, installed, running string) string {
	if bundled != "" && installed != "" && bundled != installed {
		cmp, ok := Compare(bundled, installed)
		switch {
		case ok && cmp > 0:
			return "the installed helper (" + installed + ") is older than the one shipped with this Twinnel (" + bundled + "): run `twinnel helper upgrade` (or Twinnel → 设置 → 后台助手 → 更新)"
		case ok && cmp < 0:
			return "the installed helper (" + installed + ") is newer than the one shipped with this Twinnel (" + bundled + "): update Twinnel"
		case !ok:
			return "the installed helper (" + installed + ") is not the one shipped with this Twinnel (" + bundled + "): run `twinnel helper upgrade`"
		}
	}
	if running != "" && installed != "" && running != installed {
		return "the running helper (" + running + ") is not the installed binary (" + installed + "): run `twinnel helper upgrade` to restart it"
	}
	return ""
}

// ServiceHostDrift says when the service host is out of step with the
// helper ("" when it is not, or a version is unknown): its version
// differs from the installed helper's, or the helper reports it stale
// (mihomo runs an older host).
func ServiceHostDrift(host, helper string, stale bool) string {
	if host == "" {
		return ""
	}
	if stale || helper != "" && host != helper {
		return "the service host that runs mihomo (" + host + ") is not up to date with the helper: run `twinnel helper upgrade` (or Twinnel → 设置 → 后台助手 → 更新)"
	}
	return ""
}

// Entry is one component. Version is empty when it could not be read.
type Entry struct {
	Version string `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"` // where it was read, build info
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"` // out of step with another component
}

// Sources read the components; each may be nil (then it reads as not
// reachable). All of them only read.
type Sources struct {
	Socket     string // the helper's, for Detail
	Controller string // mihomo's, for Detail
	Helper     func(ctx context.Context) (rpc.Status, error)
	Mihomo     func(ctx context.Context) (mihomo.Version, error)
	Tailscale  func(ctx context.Context) (string, error)
}

var errNoSource = sourceError("not available")

type sourceError string

func (e sourceError) Error() string { return string(e) }

// Collect asks every component at once, each for at most timeout; none of
// them is required. self is the calling program's own entry (the CLI).
func Collect(ctx context.Context, timeout time.Duration, self Entry, src Sources) Report {
	rep := Report{CLI: self}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		wg      sync.WaitGroup
		hst     rpc.Status
		herr    = error(errNoSource)
		mv      mihomo.Version
		merr    = error(errNoSource)
		tsv     string
		tserr   = error(errNoSource)
		hasHelp bool
	)
	if src.Helper != nil {
		wg.Add(1)
		go func() { defer wg.Done(); hst, herr = src.Helper(ctx); hasHelp = herr == nil }()
	}
	if src.Mihomo != nil {
		wg.Add(1)
		go func() { defer wg.Done(); mv, merr = src.Mihomo(ctx) }()
	}
	if src.Tailscale != nil {
		wg.Add(1)
		go func() { defer wg.Done(); tsv, tserr = src.Tailscale(ctx) }()
	}
	wg.Wait()

	if hasHelp {
		rep.Helper = Entry{Version: hst.HelperVersion, Detail: src.Socket}
		if h := hst.ServiceHost; h != nil {
			rep.ServiceHost = &Entry{Version: h.Version, Detail: h.Path, Error: h.Error,
				Warning: ServiceHostDrift(h.Version, "", h.Stale)}
		}
	} else {
		rep.Helper = Entry{Detail: src.Socket, Error: herr.Error()}
	}

	switch {
	case merr == nil && mv.Version != "":
		d := "controller " + src.Controller
		if mv.Meta {
			d = "Meta, " + d
		}
		rep.Mihomo = Entry{Version: mv.Version, Detail: d}
	case hasHelp && hst.Mihomo.Version != "":
		// The helper may reach a controller this user cannot (secret).
		rep.Mihomo = Entry{Version: hst.Mihomo.Version, Detail: "via the helper, controller " + hst.Mihomo.Controller}
	case merr != nil:
		rep.Mihomo = Entry{Detail: "controller " + src.Controller, Error: merr.Error()}
	default:
		rep.Mihomo = Entry{Detail: "controller " + src.Controller, Error: "empty /version answer"}
	}
	if hasHelp && hst.Kernel.Active != "" {
		k := "binary " + hst.Kernel.Active
		if hst.Kernel.Current != "" && !hst.Kernel.External {
			k = "managed kernel " + hst.Kernel.Current
		}
		rep.Mihomo.Detail = joinDetail(rep.Mihomo.Detail, k)
	}
	// Another program's mihomo on the controller address: the version
	// that answered is its, not Twinnel's.
	if hasHelp && hst.Mihomo.Owner.IsForeign() && rep.Mihomo.Version != "" {
		o := hst.Mihomo.Owner
		rep.Mihomo.Warning = o.Addr + " is held by " + o.Who() + "'s mihomo: this is its version, not Twinnel's"
	}

	switch {
	case tserr == nil && tsv != "":
		rep.Tailscale = Entry{Version: tsv, Detail: "tailscaled"}
	case tserr != nil:
		rep.Tailscale = Entry{Error: tserr.Error()}
	default:
		rep.Tailscale = Entry{Error: "tailscaled did not report a version"}
	}
	return rep
}

func joinDetail(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
