package engine

import (
	"context"
	"errors"
	"sync"

	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/rpc"
)

var kernelMu sync.Mutex

// KernelList reports installed kernels and which binary is active.
func (e *Engine) KernelList() (rpc.KernelList, error) {
	var out rpc.KernelList
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	if e.Kernels != nil {
		ks, err := e.Kernels.List()
		if err != nil {
			return out, err
		}
		out.Current, out.Previous = ks.Current, ks.Previous
		for _, in := range ks.Installed {
			out.Installed = append(out.Installed, rpc.KernelVersion{Version: in.Version, Path: in.Path, Current: in.Current, Previous: in.Previous, Time: in.Time})
		}
	}
	if st.Mihomo.Binary != "" {
		out.Active, out.External = st.Mihomo.Binary, true
	} else if out.Current != "" {
		out.Active = e.Kernels.Binary()
	}
	return out, nil
}

func (e *Engine) kernelManager(ctx context.Context) *kernel.Manager {
	m, _, _ := e.kernelManagerVia(ctx, rpc.ProxyAuto)
	return m
}

// kernelManagerVia is kernelManager with a download proxy choice; stop
// ends a temporary proxy.
func (e *Engine) kernelManagerVia(ctx context.Context, proxy string) (*kernel.Manager, func(), error) {
	m := *e.Kernels
	stop := func() {}
	if m.HTTP == nil {
		st, err := e.Store.Settings()
		if err != nil {
			return &m, stop, err
		}
		cl, _, s, err := e.downloadVia(ctx, st, proxy)
		if err != nil {
			return &m, stop, err
		}
		m.HTTP, stop = cl, s
	} else if proxy == rpc.ProxySubscription {
		// Tests set HTTP; still exercise the temporary proxy's start.
		st, err := e.Store.Settings()
		if err != nil {
			return &m, stop, err
		}
		_, stop, err = e.subscriptionProxy(ctx, st)
		if err != nil {
			return &m, func() {}, err
		}
	}
	return &m, stop, nil
}

func (e *Engine) channel(c string) (string, error) {
	if c != "" {
		return c, nil
	}
	st, err := e.Store.Settings()
	if err != nil {
		return "", err
	}
	return st.KernelChannel, nil
}

// KernelCheck looks up the newest release of a channel.
func (e *Engine) KernelCheck(ctx context.Context, channel string) (rpc.KernelRelease, error) {
	ch, err := e.channel(channel)
	if err != nil {
		return rpc.KernelRelease{}, err
	}
	if ch != kernel.ChannelStable && ch != kernel.ChannelAlpha {
		return rpc.KernelRelease{}, rpc.ErrInvalid("unknown channel %q", ch)
	}
	r, err := e.kernelManager(ctx).Check(ctx, ch)
	if err != nil {
		return rpc.KernelRelease{}, err
	}
	cur, _ := e.Kernels.List()
	return rpc.KernelRelease{
		Channel: r.Channel, Version: r.Version, Tag: r.Tag, Asset: r.Asset, Size: r.Size, Digest: r.Digest,
		Published: r.Published, Current: cur.Current, UpdateAvailable: kernel.Newer(r.Version, cur.Current),
	}, nil
}

// KernelInstall installs the newest release of a channel and makes it
// current. The running service keeps its old binary until it restarts
// (restarting it is the caller's decision).
func (e *Engine) KernelInstall(ctx context.Context, p rpc.KernelInstallParams) (rpc.KernelList, error) {
	if !kernelMu.TryLock() {
		return rpc.KernelList{}, rpc.Errorf(rpc.CodeBusy, "a kernel install is already running")
	}
	defer kernelMu.Unlock()
	ch, err := e.channel(p.Channel)
	if err != nil {
		return rpc.KernelList{}, err
	}
	m, stop, err := e.kernelManagerVia(ctx, p.Proxy)
	defer stop()
	if err != nil {
		return rpc.KernelList{}, err
	}
	m.Repair = p.Repair
	r, err := m.Check(ctx, ch)
	if err != nil {
		return rpc.KernelList{}, err
	}
	if p.Version != "" && p.Version != r.Version {
		return rpc.KernelList{}, rpc.Errorf(rpc.CodeFailed, "the newest %s release is now %s, not %s; check again", ch, r.Version, p.Version)
	}
	if _, err := m.Install(ctx, r); err != nil {
		if !errors.Is(err, kernel.ErrDoesNotRun) || !m.CanFallBack() || p.Version != "" {
			return rpc.KernelList{}, err
		}
		// The plain amd64 build needs x86-64-v3; some CPUs (and some VMs)
		// report it but cannot run it. The "-compatible" build runs on
		// every amd64 CPU.
		e.log().Warn("mihomo did not run; trying the compatible build", "asset", r.Asset, "err", err)
		m.Compatible = true
		if r, err = m.Check(ctx, ch); err != nil {
			return rpc.KernelList{}, err
		}
		if _, err := m.Install(ctx, r); err != nil {
			return rpc.KernelList{}, err
		}
	}
	l, err := e.KernelList()
	e.publish(rpc.EventKernelChanged, l)
	return l, err
}

// KernelRollback makes the previous kernel current again.
func (e *Engine) KernelRollback() (rpc.KernelList, error) {
	if !kernelMu.TryLock() {
		return rpc.KernelList{}, rpc.Errorf(rpc.CodeBusy, "a kernel install is running")
	}
	defer kernelMu.Unlock()
	if _, err := e.Kernels.Rollback(); err != nil {
		return rpc.KernelList{}, err
	}
	l, err := e.KernelList()
	e.publish(rpc.EventKernelChanged, l)
	return l, err
}

// TailscaleInfo reports the Tailscale install and the newest version.
func (e *Engine) TailscaleInfo(ctx context.Context) rpc.TailscaleInfo {
	p := e.TSProbe
	if p == nil {
		p = &kernel.TailscaleProbe{}
	}
	if st, err := e.Store.Settings(); err == nil && p.Binary == "" && st.Tailscale.Binary != "" {
		cp := *p
		cp.Binary = st.Tailscale.Binary
		p = &cp
	}
	info, err := p.Info(ctx)
	out := rpc.TailscaleInfo{Installed: info.Installed, Path: info.Path, Version: info.Version, InstallKind: info.Kind, Latest: info.Latest, UpdateAvailable: info.UpdateAvailable}
	if err != nil {
		out.Error = err.Error()
	}
	return out
}
