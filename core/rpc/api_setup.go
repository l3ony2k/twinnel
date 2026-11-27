package rpc

import "time"

// Methods of a fresh setup (core/setup): the system DNS and the Tailscale
// install. The rest of a setup uses the methods every client has
// (kernel.install, subscription.put, layer.put, compose.apply,
// service.install).
const (
	// MethodDNSStatus reads the DNS of the active network services and
	// whether Twinnel keeps a backup of them.
	MethodDNSStatus = "dns.status"
	// MethodDNSPoint points every active network service at mihomo's DNS
	// (127.0.0.1; on Windows also ::1 for IPv6, see
	// platform/windows/sysdns.go), recording the earlier values first.
	// Refused while mihomo's DNS does not answer.
	MethodDNSPoint = "dns.point"
	// MethodDNSRestore writes the recorded values back.
	MethodDNSRestore = "dns.restore"
	// MethodTailscaleInstall installs the open-source tailscaled as a
	// system service when no Tailscale is installed; an existing install
	// is kept.
	MethodTailscaleInstall = "tailscale.install"
)

// Download proxies of kernel.install and tailscale.install.
const (
	// ProxyAuto: through mihomo's mixed port when mihomo answers, else
	// direct (the default).
	ProxyAuto = ""
	// ProxyDirect: never through a proxy.
	ProxyDirect = "direct"
	// ProxySubscription: through a temporary mihomo (no TUN, no DNS,
	// loopback only) that runs the composed config of the active
	// subscription, for a machine where mihomo does not run yet. It needs
	// a mihomo binary: the managed kernel, or a seed one shipped with the
	// app (<state>/kernels/seed/mihomo).
	ProxySubscription = "subscription"
	// Anything else is a proxy URL: http://, https:// or socks5://.
)

// DNSService is one network service's DNS; no servers means automatic
// (from the network).
type DNSService struct {
	Name    string   `json:"name"`
	Servers []string `json:"servers"`
}

// DNSStatus is the result of the dns.* methods.
type DNSStatus struct {
	Services []DNSService `json:"services"`
	Target   []string     `json:"target"`
	Pointed  bool         `json:"pointed"`
	// Backup is what the services used before dns.point (nil: Twinnel did
	// not change the system DNS, or restored it).
	Backup   []DNSService `json:"backup,omitempty"`
	BackupAt time.Time    `json:"backup_at,omitzero"`
	Changed  []string     `json:"changed,omitempty"`
	// Supported is false where the platform part is missing (Linux for
	// now; macOS and Windows have one): dns.point and dns.restore then
	// fail.
	Supported bool `json:"supported"`
	// Watch is the DNS watchdog (nil: a helper without one).
	Watch *DNSWatch `json:"watch,omitempty"`
}

type TailscaleInstallParams struct {
	// Proxy for the download (Proxy* or a URL).
	Proxy string `json:"proxy,omitempty"`
	// NoStart: install only. The Windows installer starts its service
	// right away; it is stopped again (TailscaleInstallResult.Stopped).
	NoStart bool `json:"no_start,omitempty"`
	// Repair reinstalls an install whose files are damaged (Windows: the
	// official MSI over the old one; the build Twinnel unpacked:
	// unpacked again). An install from elsewhere (Homebrew, the app) is
	// repaired with its own installer.
	Repair bool `json:"repair,omitempty"`
}

// BinaryProblem is an installed program that is not sane (core/binsane):
// Problem is "zeroed", "empty", "bad-header", "bad-signature" (damaged),
// "missing" or "denied".
type BinaryProblem struct {
	Path    string `json:"path"`
	Problem string `json:"problem"`
	Detail  string `json:"detail,omitempty"`
}

// TailscaleInstallResult says what tailscale.install did.
type TailscaleInstallResult struct {
	// Kept: a Tailscale install was already there and nothing was
	// installed. Kind and Path describe it.
	Kept bool   `json:"kept"`
	Kind string `json:"kind,omitempty"` // TailscaleInfo.InstallKind, or "twinnel"
	Path string `json:"path,omitempty"` // the CLI
	// UsesSystemVPN: the kept install is the macOS app (a NetworkExtension
	// that takes the system VPN slot, which conflicts with mihomo's TUN).
	UsesSystemVPN bool   `json:"uses_system_vpn,omitempty"`
	Version       string `json:"version,omitempty"`
	Source        string `json:"source,omitempty"` // pkgs.tailscale.com, homebrew or the Windows MSI
	Via           string `json:"via,omitempty"`    // the download's route: direct, proxy, subscription, url
	Daemon        string `json:"daemon,omitempty"`
	// RestartNeeded: the Windows installer asked for a restart to finish
	// (msiexec exit code 3010); Tailscale may not work fully until then.
	RestartNeeded bool `json:"restart_needed,omitempty"`
	// Service is the tailscaled service install (dry run when the helper
	// is not root).
	Service *ServiceOpResult `json:"service,omitempty"`
	// Stopped: the service was stopped after the install (NoStart).
	Stopped bool `json:"stopped,omitempty"`
	// Repaired: the install was damaged and was reinstalled (Repair).
	Repaired bool `json:"repaired,omitempty"`
	// Damaged are the installed files that are not sane; with an error
	// coded "ts-damaged" unless Repair fixed them.
	Damaged []BinaryProblem `json:"damaged,omitempty"`
}

// MethodTailscaleLogin reads tailscaled's login state and, with Start,
// connects and starts an interactive login (as `tailscale up`). The helper
// does it as root, so it never depends on tailscaled granting LocalAPI
// writes to the user; clients only open AuthURL.
const MethodTailscaleLogin = "tailscale.login"

type TailscaleLoginParams struct {
	// Start connects the node (WantRunning) and, when it then needs a
	// login, starts one. A node that only was stopped just connects.
	Start bool `json:"start,omitempty"`
	// Reauth (with Start) starts a new login even on a running node: the
	// way to renew a node key before it expires.
	Reauth bool `json:"reauth,omitempty"`
}

type TailscaleLoginResult struct {
	BackendState string `json:"backend_state"` // Running, NeedsLogin, Stopped, NoState, ...
	AuthURL      string `json:"auth_url,omitempty"`
	Tailnet      string `json:"tailnet,omitempty"`
	// Started: this call started a login (Start, and not Running yet).
	Started bool `json:"started,omitempty"`
}

// EventDNSWatch: the DNS watchdog restored the system DNS because mihomo
// did not answer, pointed it back, or forgot a restore (data: DNSWatch).
const EventDNSWatch = "dns.watchdog"

// Phases of DNSWatch (core/dnswatch).
const (
	DNSWatchOff      = "off"      // Twinnel does not own the system DNS
	DNSWatchWatching = "watching" // pointed at mihomo, watched
	DNSWatchRestored = "restored" // temporarily back to the earlier DNS
)

// Causes of a restore.
const (
	DNSCauseStopped   = "stopped"    // mihomo's service is not running
	DNSCauseCrashLoop = "crash-loop" // it keeps restarting
	DNSCauseNoAnswer  = "no-answer"  // it runs but its DNS does not answer
)

// DNSWatch is the DNS watchdog's state: once the system DNS points at
// mihomo, a mihomo that dies would cut name resolution for the whole
// machine, so the helper writes the earlier DNS back until mihomo answers
// again (then points it back, leaving alone what the user changed by
// hand). dns.point and dns.restore end a restore: dns.restore is how a
// user keeps the earlier DNS.
type DNSWatch struct {
	Phase string    `json:"phase"`
	Cause string    `json:"cause,omitempty"`
	Since time.Time `json:"since,omitzero"`
	// Services are the network services the watchdog restored.
	Services []string `json:"services,omitempty"`
	// LeftAlone are the services the last point-back skipped because the
	// user changed their DNS by hand meanwhile.
	LeftAlone   []string  `json:"left_alone,omitempty"`
	LastRepoint time.Time `json:"last_repoint,omitzero"`
}

// Restored reports whether the watchdog holds the system DNS restored.
func (w *DNSWatch) Restored() bool { return w != nil && w.Phase == DNSWatchRestored }
