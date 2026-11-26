package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Tailscale install kinds.
const (
	TSBrew       = "brew"       // Homebrew tailscale formula (open-source tailscaled)
	TSStandalone = "standalone" // Tailscale.app from pkgs.tailscale.com (NetworkExtension)
	TSAppStore   = "appstore"   // Tailscale.app from the Mac App Store (NetworkExtension)
	TSStatic     = "static"     // official static tailscale/tailscaled binaries
	TSUnknown    = "unknown"
)

// TailscaleCandidates are the CLI paths searched when none is configured.
var TailscaleCandidates = []string{
	"/opt/homebrew/bin/tailscale",
	"/usr/local/bin/tailscale",
	"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
}

// TailscaleInfo is what the helper reports about Tailscale. Nothing is
// installed or changed.
type TailscaleInfo struct {
	Installed       bool
	Path            string
	Version         string
	Kind            string
	Latest          string
	UpdateAvailable bool
}

// TailscaleProbe reads the local Tailscale version and the newest published
// one.
type TailscaleProbe struct {
	Binary  string // empty: search TailscaleCandidates
	PkgsURL string // default https://pkgs.tailscale.com
	Track   string // "stable" (default) or "unstable"
	HTTP    *http.Client
	// Run executes the CLI (default exec with a timeout). Only
	// `tailscale version --json` is run.
	Run func(ctx context.Context, bin string, args ...string) ([]byte, error)
	// Stat is os.Stat; replaceable for tests of the kind detection.
	Stat func(string) (os.FileInfo, error)
	// EvalSymlinks is filepath.EvalSymlinks; replaceable for tests.
	EvalSymlinks func(string) (string, error)
}

func (p *TailscaleProbe) stat(name string) (os.FileInfo, error) {
	if p.Stat != nil {
		return p.Stat(name)
	}
	return os.Stat(name)
}

// Find returns the tailscale CLI path, or "".
func (p *TailscaleProbe) Find() string {
	if p.Binary != "" {
		if _, err := p.stat(p.Binary); err == nil {
			return p.Binary
		}
		return ""
	}
	for _, c := range TailscaleCandidates {
		if _, err := p.stat(c); err == nil {
			return c
		}
	}
	return ""
}

// Kind classifies an install from the CLI path.
func (p *TailscaleProbe) Kind(path string) string {
	eval := p.EvalSymlinks
	if eval == nil {
		eval = filepath.EvalSymlinks
	}
	real := path
	if r, err := eval(path); err == nil {
		real = r
	}
	switch {
	case strings.Contains(real, "/Cellar/") || strings.HasPrefix(real, "/opt/homebrew/") || strings.Contains(real, "/homebrew/"):
		return TSBrew
	case strings.Contains(real, "Tailscale.app/"):
		app := real[:strings.Index(real, "Tailscale.app/")+len("Tailscale.app")]
		if _, err := p.stat(filepath.Join(app, "Contents", "_MASReceipt")); err == nil {
			return TSAppStore
		}
		return TSStandalone
	}
	if _, err := p.stat(filepath.Join(filepath.Dir(real), "tailscaled")); err == nil {
		return TSStatic
	}
	return TSUnknown
}

func (p *TailscaleProbe) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	if p.Run != nil {
		return p.Run(ctx, bin, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return Output(ctx, bin, args...)
}

// LocalVersion runs `tailscale version --json`.
func (p *TailscaleProbe) LocalVersion(ctx context.Context, bin string) (string, error) {
	out, err := p.run(ctx, bin, "version", "--json")
	if err != nil {
		return "", fmt.Errorf("tailscale version: %w", err)
	}
	var v struct {
		MajorMinorPatch string `json:"majorMinorPatch"`
		Short           string `json:"short"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return "", fmt.Errorf("tailscale version: %w", err)
	}
	if v.MajorMinorPatch != "" {
		return v.MajorMinorPatch, nil
	}
	if v.Short == "" {
		return "", errors.New("tailscale version: empty")
	}
	return v.Short, nil
}

// Latest asks pkgs.tailscale.com for the newest version of the package
// kind that matches the install (tarballs for brew/static, Mac zips for the
// app).
func (p *TailscaleProbe) Latest(ctx context.Context, kind string) (string, error) {
	base := p.PkgsURL
	if base == "" {
		base = "https://pkgs.tailscale.com"
	}
	track := p.Track
	if track == "" {
		track = "stable"
	}
	if track != "stable" && track != "unstable" {
		return "", fmt.Errorf("unknown track %q", track)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/"+track+"/?mode=json", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "duet-helper")
	cl := p.HTTP
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("pkgs.tailscale.com: %s", resp.Status)
	}
	var v struct {
		Version         string
		TarballsVersion string
		MacZipsVersion  string
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&v); err != nil {
		return "", fmt.Errorf("pkgs.tailscale.com: %w", err)
	}
	pick := v.Version
	switch kind {
	case TSBrew, TSStatic:
		if v.TarballsVersion != "" {
			pick = v.TarballsVersion
		}
	case TSStandalone, TSAppStore:
		if v.MacZipsVersion != "" {
			pick = v.MacZipsVersion
		}
	}
	if pick == "" {
		return "", errors.New("pkgs.tailscale.com: no version in response")
	}
	return pick, nil
}

// Info combines Find, Kind, LocalVersion and Latest. Errors from the
// network part do not hide the local part.
func (p *TailscaleProbe) Info(ctx context.Context) (TailscaleInfo, error) {
	var info TailscaleInfo
	bin := p.Find()
	if bin == "" {
		latest, err := p.Latest(ctx, TSBrew)
		info.Latest = latest
		return info, err
	}
	info.Installed, info.Path, info.Kind = true, bin, p.Kind(bin)
	var errs []error
	v, err := p.LocalVersion(ctx, bin)
	info.Version = v
	errs = append(errs, err)
	latest, err := p.Latest(ctx, info.Kind)
	info.Latest = latest
	errs = append(errs, err)
	if info.Version != "" && info.Latest != "" {
		info.UpdateAvailable = CompareSemver(info.Latest, info.Version) > 0
	}
	return info, errors.Join(errs...)
}
