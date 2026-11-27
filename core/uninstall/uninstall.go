// Package uninstall decides what removing the helper does: what goes, what
// stays, whether mihomo must first be moved back to the service it ran
// under before `duet helper harden`, and when to refuse. The CLI turns the
// plan into steps (and runs them as root); the app shows the same lists in
// its confirm dialog. One rule above all: uninstalling never leaves the
// owner without a network.
package uninstall

import (
	"fmt"

	"github.com/l3ony2k/duet/core/rpc"
)

// Options are the owner's choices.
type Options struct {
	// Purge also deletes duet's data: the state dir (settings,
	// subscriptions, layers, history, logs, takeover and harden backups,
	// kernels) and the secrets (keychain items).
	Purge bool
	// RestoreMihomo moves a hardened mihomo back to the service it
	// replaced (runbook step 8c) before the helper goes: a few seconds
	// offline.
	RestoreMihomo bool
	// KeepMihomo leaves a hardened mihomo running from duet's LaunchDaemon
	// and state dir, unmanaged. Not with Purge (that deletes its files).
	KeepMihomo bool
}

// Kind names one thing the plan removes or keeps.
type Kind string

const (
	HelperService  Kind = "helper-service"  // the helper's LaunchDaemon / unit
	HelperBinary   Kind = "helper-binary"   // the installed helper binary
	HelperSocket   Kind = "helper-socket"   // its socket
	StateDir       Kind = "state-dir"       // settings, subscriptions, layers, history, logs, backups
	Secrets        Kind = "secrets"         // keychain items (Names)
	DuetMihomo     Kind = "duet-mihomo"     // duet's mihomo LaunchDaemon (hardened)
	OriginalMihomo Kind = "original-mihomo" // the service mihomo ran under before duet (adopted)
	Tailscale      Kind = "tailscale"       // tailscaled: never touched
	App            Kind = "app"             // duet.app itself
)

// Item is one line of the plan.
type Item struct {
	Kind  Kind     `json:"kind"`
	Path  string   `json:"path,omitempty"`  // a file, dir or service label
	Names []string `json:"names,omitempty"` // Secrets: the keys
	// Counts of what StateDir holds (shown to the owner).
	Subscriptions int `json:"subscriptions,omitempty"`
	Layers        int `json:"layers,omitempty"`
}

// Restore is the move back from duet's mihomo service to the one it
// replaced, run before the helper is removed.
type Restore struct {
	FromLabel  string `json:"from_label"` // duet's (com.l3ony2k.duet.mihomo)
	ToLabel    string `json:"to_label"`   // the original (com.mihomo.proxy)
	ToPlist    string `json:"to_plist"`
	Controller string `json:"controller"` // waited on: it answers again under ToLabel
}

// Plan is what uninstalling does.
type Plan struct {
	Remove  []Item   `json:"remove"`
	Keep    []Item   `json:"keep"`
	Restore *Restore `json:"restore_mihomo,omitempty"`
	// Known is false when the helper's settings could not be read (it is
	// not running and the caller is not root): the plan then lists only
	// the helper itself, and the run as root checks again.
	Known bool `json:"known"`
}

// Refusal is why a plan cannot run as asked; Code tells the app which
// choice to offer.
type Refusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r *Refusal) Error() string { return r.Message }

// Refusal codes.
const (
	// NeedsRestore: mihomo runs from duet's LaunchDaemon (hardened); pass
	// RestoreMihomo (or KeepMihomo without Purge).
	NeedsRestore = "needs-restore"
	// OwnMihomo: Purge would delete the files of a mihomo that duet
	// installed itself, and there is no earlier service to go back to.
	OwnMihomo = "own-mihomo"
	// Conflict: the options contradict each other.
	Conflict = "conflict"
)

// Facts is what the caller knows about the installed helper; Info is nil
// when unknown.
type Facts struct {
	Info *rpc.HelperInfo
	// HelperService, HelperBinary and Socket are the platform's paths.
	HelperService, HelperBinary, Socket, StateDir string
	// OldPlistExists reports whether a hardened mihomo's original plist is
	// still there (nil: not checked, e.g. not root and not readable).
	OldPlistExists func(path string) bool
	// SecretStore names where secrets live ("keychain" lists items;
	// anything else means they are files in the state dir).
	SecretStore string
}

// Make builds the plan, or a *Refusal.
func Make(f Facts, o Options) (Plan, error) {
	if o.RestoreMihomo && o.KeepMihomo {
		return Plan{}, &Refusal{Code: Conflict, Message: "choose either restoring mihomo's original service or keeping duet's, not both"}
	}
	if o.Purge && o.KeepMihomo {
		return Plan{}, &Refusal{Code: Conflict, Message: "--keep-mihomo cannot be combined with --purge: purging deletes the files that mihomo runs from"}
	}
	p := Plan{Known: f.Info != nil}
	p.Remove = append(p.Remove,
		Item{Kind: HelperService, Path: f.HelperService},
		Item{Kind: HelperBinary, Path: f.HelperBinary},
		Item{Kind: HelperSocket, Path: f.Socket},
	)
	info := f.Info
	stateDir := f.StateDir
	if info != nil && info.StateDir != "" {
		stateDir = info.StateDir
	}
	if info != nil {
		m := info.Mihomo
		switch h := info.Hardened; {
		case h != nil && !o.RestoreMihomo && !o.KeepMihomo:
			return Plan{}, &Refusal{Code: NeedsRestore, Message: fmt.Sprintf(
				"mihomo runs from duet's LaunchDaemon %s (since `duet helper harden`); removing the helper would leave it unmanaged. "+
					"Pass --restore-mihomo to move it back to %s first (a few seconds offline), or --keep-mihomo to leave it running as it is (not with --purge)",
				m.Label, h.OldLabel)}
		case h != nil && o.RestoreMihomo:
			if f.OldPlistExists != nil && !f.OldPlistExists(h.OldPlist) {
				return Plan{}, &Refusal{Code: NeedsRestore, Message: fmt.Sprintf(
					"cannot move mihomo back: %s, the service it ran under before harden, is gone. Restore that plist first (runbook step 8c)", h.OldPlist)}
			}
			p.Restore = &Restore{FromLabel: m.Label, ToLabel: h.OldLabel, ToPlist: h.OldPlist, Controller: m.Controller}
			p.Remove = append(p.Remove, Item{Kind: DuetMihomo, Path: m.Label})
			p.Keep = append(p.Keep, Item{Kind: OriginalMihomo, Path: h.OldLabel})
		case h != nil: // KeepMihomo, no purge
			p.Keep = append(p.Keep, Item{Kind: DuetMihomo, Path: m.Label})
		case m.Adopted:
			p.Keep = append(p.Keep, Item{Kind: OriginalMihomo, Path: m.Label})
		case m.Installed && o.Purge:
			return Plan{}, &Refusal{Code: OwnMihomo, Message: fmt.Sprintf(
				"mihomo runs from duet's data (service %s) and has no earlier service to go back to: purging would cut the network. "+
					"Remove that service first (`duet service uninstall mihomo`), or uninstall without --purge", m.Label)}
		case m.Installed:
			p.Keep = append(p.Keep, Item{Kind: DuetMihomo, Path: m.Label})
		}
	}
	data := Item{Kind: StateDir, Path: stateDir}
	if info != nil {
		data.Subscriptions, data.Layers = info.Subscriptions, info.Layers
	}
	var secrets *Item
	if f.SecretStore == "keychain" && (info == nil || len(info.SecretKeys) > 0) {
		secrets = &Item{Kind: Secrets}
		if info != nil {
			secrets.Names = info.SecretKeys
		}
	}
	if o.Purge {
		p.Remove = append(p.Remove, data)
		if secrets != nil {
			p.Remove = append(p.Remove, *secrets)
		}
	} else {
		p.Keep = append(p.Keep, data)
		if secrets != nil {
			p.Keep = append(p.Keep, *secrets)
		}
	}
	p.Keep = append(p.Keep, Item{Kind: Tailscale}, Item{Kind: App})
	return p, nil
}

// Removes reports whether the plan removes an item of kind k.
func (p Plan) Removes(k Kind) bool {
	for _, it := range p.Remove {
		if it.Kind == k {
			return true
		}
	}
	return false
}
