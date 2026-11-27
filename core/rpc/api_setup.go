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
	// (127.0.0.1), recording the earlier values first. Refused while
	// mihomo's DNS does not answer.
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
	// now): dns.point and dns.restore then fail.
	Supported bool `json:"supported"`
}

type TailscaleInstallParams struct {
	// Proxy for the download (Proxy* or a URL).
	Proxy string `json:"proxy,omitempty"`
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
	Source        string `json:"source,omitempty"` // pkgs.tailscale.com or homebrew
	Via           string `json:"via,omitempty"`    // the download's route: direct, proxy, subscription, url
	Daemon        string `json:"daemon,omitempty"`
	// Service is the tailscaled service install (dry run when the helper
	// is not root).
	Service *ServiceOpResult `json:"service,omitempty"`
}

