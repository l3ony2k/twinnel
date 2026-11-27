// Package takeover inspects a hand-made mihomo + Tailscale setup and
// produces a TakeoverPlan: which subscription the live config was built from,
// the override layers that turn it back into the live config, and proof (via
// mihomo -t and a semantic diff) that composing them reproduces the live
// behavior. Nothing here changes the system; the helper applies plans later.
package takeover

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/tailscale"
)

// ServiceDef is a service manager entry (a launchd job on macOS).
type ServiceDef struct {
	Label   string   `json:"label"`
	Path    string   `json:"path"` // definition file, e.g. the plist
	Kind    string   `json:"kind"` // "launchd-daemon", "launchd-agent", "systemd", ...
	Program []string `json:"program"`
	User    string   `json:"user,omitempty"`
}

// Probe gives read-only access to the platform. The CLI provides a darwin
// implementation; tests use fakes.
type Probe interface {
	// Services lists service definitions that may run mihomo or tailscaled.
	Services(ctx context.Context) ([]ServiceDef, error)
	// MihomoVersion asks the controller for its version (GET /version).
	MihomoVersion(ctx context.Context, controller, secret string) (string, error)
	// TailscaleStatus returns `tailscale status --json`.
	TailscaleStatus(ctx context.Context) ([]byte, error)
	// TailscaleInterface is the interface the tailnet resolver routes over
	// (e.g. "utun9"), or "" when unknown.
	TailscaleInterface(ctx context.Context) (string, error)
	// TailscaleInstall reports how Tailscale is installed.
	TailscaleInstall() tailscale.Install
	// UserHome is the home dir of the user the setup belongs to.
	UserHome() string
}

// MihomoSetup is the discovered mihomo installation.
type MihomoSetup struct {
	Service        *ServiceDef `json:"service,omitempty"`
	Binary         string      `json:"binary,omitempty"`
	Dir            string      `json:"dir"`    // -d (home dir)
	Config         string      `json:"config"` // -f, or <dir>/config.yaml
	Version        string      `json:"version,omitempty"`
	Running        bool        `json:"running"` // controller answered
	Controller     string      `json:"controller,omitempty"`
	SecretSet      bool        `json:"secret_set"`
	ExternalUI     string      `json:"external_ui,omitempty"`
	ExternalUIName string      `json:"external_ui_name,omitempty"`
	GeoFiles       []string    `json:"geo_files,omitempty"`
	// Profiles are subscription profiles found next to the setup (e.g. a
	// Clash Party profile index); URLs are redacted.
	Profiles []Profile `json:"profiles,omitempty"`
	// Twinnel: this is Twinnel's own setup (its service, or a config it
	// writes), so there is nothing to take over.
	Twinnel bool `json:"twinnel,omitempty"`
}

// Profile is a subscription entry from a GUI client's profile index.
type Profile struct {
	Name   string `json:"name"`
	Source string `json:"source"` // redacted URL
	From   string `json:"from"`   // index file
}

// TailscaleSetup is the discovered Tailscale installation.
type TailscaleSetup struct {
	Install        tailscale.Install `json:"install"`
	Service        *ServiceDef       `json:"service,omitempty"`
	Version        string            `json:"version,omitempty"`
	BackendState   string            `json:"backend_state,omitempty"`
	MagicDNSSuffix string            `json:"magicdns_suffix,omitempty"`
	Interface      string            `json:"interface,omitempty"`
	Peers          int               `json:"peers"`
	status         []byte
}

// Discovery is everything Discover found.
type Discovery struct {
	Mihomo    *MihomoSetup    `json:"mihomo,omitempty"`
	Tailscale *TailscaleSetup `json:"tailscale,omitempty"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// DiscoverOptions override what Discover would find.
type DiscoverOptions struct {
	ConfigPath string // live config; default from the service's -d/-f
	MihomoBin  string // default the service's binary
	// Own is Twinnel's own setup beyond DefaultOwn (the helper's state
	// dir and the config it writes): a setup it matches is marked
	// MihomoSetup.Twinnel.
	Own Own
}

// Discover finds the mihomo service, its files, and Tailscale. Read-only.
func Discover(ctx context.Context, p Probe, opt DiscoverOptions) (*Discovery, error) {
	d := &Discovery{}
	svcs, err := p.Services(ctx)
	if err != nil {
		d.Warnings = append(d.Warnings, "services: "+err.Error())
	}
	m := &MihomoSetup{Service: MihomoService(svcs)}
	for i := range svcs {
		s := svcs[i]
		if len(s.Program) > 0 && strings.Contains(strings.ToLower(filepath.Base(s.Program[0])), "tailscaled") {
			if d.Tailscale == nil {
				d.Tailscale = &TailscaleSetup{}
			}
			d.Tailscale.Service = &s
		}
	}
	m.Binary, m.Dir, m.Config = MihomoFiles(m.Service, p.UserHome())
	if opt.ConfigPath != "" {
		m.Config = opt.ConfigPath
		if m.Service == nil {
			m.Dir = filepath.Dir(opt.ConfigPath)
		}
	}
	if opt.MihomoBin != "" {
		m.Binary = opt.MihomoBin
	}
	if m.Service == nil {
		d.Warnings = append(d.Warnings, "no mihomo service found; using "+m.Config)
	}
	m.Twinnel = DefaultOwn().With(opt.Own).Has(m)
	if err := m.readConfig(); err != nil {
		return nil, err
	}
	secret := m.secret()
	if m.Controller != "" {
		if v, err := p.MihomoVersion(ctx, m.Controller, secret); err == nil {
			m.Version, m.Running = v, true
		} else {
			d.Warnings = append(d.Warnings, "mihomo controller: "+err.Error())
		}
	}
	m.GeoFiles = geoFiles(m.Dir)
	m.Profiles = findProfiles(m.Dir)
	d.Mihomo = m

	ts := d.Tailscale
	if ts == nil {
		ts = &TailscaleSetup{}
	}
	ts.Install = p.TailscaleInstall()
	if st, err := p.TailscaleStatus(ctx); err == nil {
		ts.status = st
		var s struct {
			Version        string
			BackendState   string
			MagicDNSSuffix string
			Peer           map[string]json.RawMessage
		}
		if json.Unmarshal(st, &s) == nil {
			ts.Version, ts.BackendState = s.Version, s.BackendState
			ts.MagicDNSSuffix = strings.Trim(s.MagicDNSSuffix, ".")
			ts.Peers = len(s.Peer)
		}
	} else {
		d.Warnings = append(d.Warnings, "tailscale status: "+err.Error())
	}
	if iface, err := p.TailscaleInterface(ctx); err == nil {
		ts.Interface = iface
	}
	if ts.Install.Kind != tailscale.InstallUnknown || ts.status != nil || ts.Service != nil {
		d.Tailscale = ts
	}
	return d, nil
}

// MihomoService picks the service that runs mihomo (or a Clash core):
// the first whose program is named so. Nil when there is none.
func MihomoService(svcs []ServiceDef) *ServiceDef {
	for i := range svcs {
		if len(svcs[i].Program) == 0 {
			continue
		}
		base := strings.ToLower(filepath.Base(svcs[i].Program[0]))
		if strings.Contains(base, "mihomo") || strings.Contains(base, "clash") {
			s := svcs[i]
			return &s
		}
	}
	return nil
}

// MihomoFiles is the binary, home dir and config file of the mihomo that
// svc runs (its -d/-f), as a takeover plan reads them. With no service,
// or no -d, the dir is ~/.config/mihomo; the file defaults to
// <dir>/config.yaml.
func MihomoFiles(svc *ServiceDef, home string) (bin, dir, config string) {
	if svc != nil && len(svc.Program) > 0 {
		bin = svc.Program[0]
		dir, config = ConfigFromArgs(svc.Program)
	}
	if dir == "" {
		dir = filepath.Join(home, ".config", "mihomo")
	}
	if config == "" {
		config = filepath.Join(dir, "config.yaml")
	}
	return bin, dir, config
}

// ConfigFromArgs returns mihomo's home dir (-d) and config file (-f) from its
// command line. The file defaults to <dir>/config.yaml.
func ConfigFromArgs(args []string) (dir, file string) {
	for i := 1; i < len(args); i++ {
		a := strings.TrimLeft(args[i], "-")
		val := ""
		if k, v, ok := strings.Cut(a, "="); ok {
			a, val = k, v
		} else if i+1 < len(args) {
			val = args[i+1]
		}
		switch a {
		case "f":
			file = val
		case "d":
			dir = val
		}
	}
	if file != "" && !filepath.IsAbs(file) && dir != "" {
		file = filepath.Join(dir, file)
	}
	if file == "" && dir != "" {
		file = filepath.Join(dir, "config.yaml")
	}
	if dir == "" && file != "" {
		dir = filepath.Dir(file)
	}
	return dir, file
}

// readConfig reads the few settings discovery needs. The secret is not kept.
func (m *MihomoSetup) readConfig() error {
	b, err := config.ReadConfined(m.Config)
	if err != nil {
		return fmt.Errorf("live config: %w", err)
	}
	var c struct {
		Controller     string `yaml:"external-controller"`
		Secret         string `yaml:"secret"`
		ExternalUI     string `yaml:"external-ui"`
		ExternalUIName string `yaml:"external-ui-name"`
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return fmt.Errorf("live config %s: %w", m.Config, err)
	}
	m.Controller, m.SecretSet = c.Controller, c.Secret != ""
	m.ExternalUI, m.ExternalUIName = c.ExternalUI, c.ExternalUIName
	if m.ExternalUI != "" && !filepath.IsAbs(m.ExternalUI) {
		m.ExternalUI = filepath.Join(m.Dir, m.ExternalUI)
	}
	return nil
}

// secret re-reads the controller secret for the version request only.
func (m *MihomoSetup) secret() string {
	b, err := config.ReadConfined(m.Config)
	if err != nil {
		return ""
	}
	var c struct {
		Secret string `yaml:"secret"`
	}
	_ = yaml.Unmarshal(b, &c)
	return c.Secret
}

func geoFiles(dir string) []string {
	var out []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".mmdb", ".metadb", ".dat":
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// findProfiles reads GUI profile indexes (Clash Party "index.json"-style
// lists with a "source"/"url") next to the setup. Only redacted URLs leave.
func findProfiles(dir string) []Profile {
	var out []Profile
	for _, f := range searchDirs(dir) {
		p := filepath.Join(f, "index.json")
		b, err := config.ReadConfined(p)
		if err != nil {
			continue
		}
		var items []map[string]any
		if json.Unmarshal(b, &items) != nil {
			continue
		}
		for _, it := range items {
			src, _ := it["source"].(string)
			if src == "" {
				src, _ = it["url"].(string)
			}
			if !strings.Contains(src, "://") {
				continue
			}
			name, _ := it["name"].(string)
			out = append(out, Profile{Name: name, Source: redactURL(src), From: p})
		}
	}
	return out
}

// searchDirs are where base candidates and profile indexes are looked for:
// the config dir, its parent, and backup-like dirs next to them. Symlinked
// subdirs are not followed (DirEntry.IsDir is false for a symlink), so a
// "backup" link to /etc is not searched as root.
func searchDirs(confDir string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			seen[d] = true
			out = append(out, d)
		}
	}
	add(confDir)
	parent := filepath.Dir(confDir)
	add(parent)
	for _, root := range []string{confDir, parent} {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			if e.IsDir() && (strings.Contains(n, "backup") || strings.Contains(n, "bak") ||
				strings.Contains(n, "profile") || strings.Contains(n, "sub")) {
				add(filepath.Join(root, e.Name()))
			}
		}
	}
	return out
}
