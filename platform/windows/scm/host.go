package scm

import (
	"fmt"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/platform/windows/winpath"
)

// The service host (twinnel-svchost.exe) is the third Twinnel binary on a
// Windows machine, beside the CLI and the helper: a copy of the helper
// that runs mihomo.exe as a Windows service. It is kept apart from the
// helper so removing or replacing the helper never touches a running
// service, which also means installing a new helper does not update it.
// Host and its RefreshPlan bring it up to date with the helper: on every
// helper upgrade (twinnel helper upgrade) and whenever the helper starts.
//
// A running service keeps its host's file locked: Windows refuses to
// overwrite or delete it but lets it be renamed. So a refresh moves the
// file aside (twinnel-svchost.old-<n>.exe) and copies the new one in
// place, and the running service carries on with the old copy until it
// next restarts; the copy is deleted once nothing runs it. The service is
// restarted at once only when its host is an older revision
// (svchost.Revision), a change that must reach it now.

// Host is the service host as a refresh sees it (ReadHost reads it).
type Host struct {
	Path    string `json:"path"`              // winpath.SvcHostBin()
	Present bool   `json:"present"`           //
	Version string `json:"version,omitempty"` // its --version ("" unknown)
	// Same is true when it has the bytes of the helper it is refreshed
	// from.
	Same bool `json:"same"`
	// Services are Twinnel's services that run under the host.
	Services []HostedService `json:"services,omitempty"`
	// Aside are copies moved aside by earlier refreshes.
	Aside []string `json:"aside,omitempty"`
}

// HostedService is a service that runs under the host.
type HostedService struct {
	Label   string `json:"label"`
	Running bool   `json:"running"`
	// Image is the host executable the running service was started from
	// (an aside copy after a refresh; "" when unknown).
	Image string `json:"image,omitempty"`
	// Revision is that executable's svchost revision (0: from before
	// revisions, or unknown).
	Revision int `json:"revision"`
}

// Restarts are the running services a refresh restarts: those whose
// host is older than revision.
func (h Host) Restarts(revision int) []string {
	var out []string
	for _, s := range h.Services {
		if s.Running && s.Revision < revision {
			out = append(out, s.Label)
		}
	}
	return out
}

// Stale reports whether the host needs a refresh: its file differs from
// the helper's, or a service runs an older revision. A host that is not
// installed (no hosted service yet) is never stale: installing the
// mihomo service copies it.
func (h Host) Stale(revision int) bool {
	return h.Present && (!h.Same || len(h.Restarts(revision)) > 0)
}

// AsidePrefix and AsideSuffix name the copies a refresh moves aside. The
// name keeps .exe: Windows runs no program with another extension.
const (
	AsidePrefix = "twinnel-svchost.old-"
	AsideSuffix = ".exe"
)

// AsidePath is where a refresh at now moves the host's file.
func AsidePath(host string, now time.Time) string {
	return winpath.Join(winpath.Dir(host), fmt.Sprintf("%s%d%s", AsidePrefix, now.UnixNano(), AsideSuffix))
}

// IsAside reports whether a file name is one AsidePath makes.
func IsAside(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, AsidePrefix) && strings.HasSuffix(n, AsideSuffix)
}

// InUse reports whether a running service was started from path (an
// aside copy that cannot be deleted yet).
func (h Host) InUse(path string) bool {
	for _, s := range h.Services {
		if s.Running && strings.EqualFold(s.Image, path) {
			return true
		}
	}
	return false
}

// Idle are the aside copies no running service uses: TidyPlan deletes
// them. A copy whose user is unknown (no right to read the process's
// image) counts as idle; Windows refuses to delete it while it runs.
func (h Host) Idle() []string {
	var out []string
	for _, p := range h.Aside {
		if !h.InUse(p) {
			out = append(out, p)
		}
	}
	return out
}

// TidyPlan deletes the aside copies no running service uses any more (a
// service that restarted runs the host's own file). Empty when none.
func (h Host) TidyPlan() []service.Step {
	var steps []service.Step
	for _, p := range h.Idle() {
		steps = append(steps, service.Step{Kind: service.StepRemove, Path: p, IgnoreError: true})
	}
	return steps
}

// RefreshPlan brings the host up to date with src (the helper binary it
// is a copy of): the services on an older revision are stopped, the file
// is moved aside and replaced when it differs, those services start
// again, and copies left aside are deleted (ignoring the ones a running
// service still uses). Empty when the host is not installed; only the
// clean-up when it is current.
func (h Host) RefreshPlan(src string, revision int, now time.Time) []service.Step {
	if !h.Present {
		return nil
	}
	m := &Manager{}
	restart := h.Restarts(revision)
	var steps []service.Step
	for _, l := range restart {
		steps = append(steps, m.stopSteps(l, false)...)
	}
	aside := h.Aside
	if !h.Same {
		p := AsidePath(h.Path, now)
		steps = append(steps,
			service.Step{Kind: service.StepRename, Src: h.Path, Path: p},
			service.Step{Kind: service.StepCopy, Src: src, Path: h.Path, Mode: 0o755},
		)
		aside = append(aside[:len(aside):len(aside)], p)
	}
	for _, l := range restart {
		steps = append(steps, startAgain(l))
	}
	for _, p := range aside {
		steps = append(steps, service.Step{Kind: service.StepRemove, Path: p, IgnoreError: true})
	}
	return steps
}
