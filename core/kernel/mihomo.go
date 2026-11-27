// Package kernel manages the mihomo kernel binaries the helper runs, and
// reports Tailscale versions.
//
// Layout under Manager.Dir (normally <state>/kernels/mihomo):
//
//	<version>/mihomo     one dir per installed version
//	current -> <version> symlink; the service runs current/mihomo
//	previous -> <version>
//
// Switching rewrites the symlink with a rename, so the path the service
// uses always points at a complete binary.
package kernel

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Channels.
const (
	ChannelStable = "stable"
	ChannelAlpha  = "alpha"
)

const (
	defaultAPI  = "https://api.github.com"
	defaultRepo = "MetaCubeX/mihomo"
	alphaTag    = "Prerelease-Alpha"
	binName     = "mihomo"
	maxDownload = 256 << 20
)

// Release is a downloadable mihomo build for this platform.
type Release struct {
	Channel   string
	Tag       string
	Version   string // "v1.19.34" or "alpha-1a2b3c4"
	Asset     string
	URL       string
	Size      int64
	Digest    string // "sha256:<hex>" as published by GitHub, may be empty
	Published time.Time
}

// Installed is one installed version.
type Installed struct {
	Version  string
	Path     string // the binary
	Current  bool
	Previous bool
	Time     time.Time
}

// State is the list of installed kernels.
type State struct {
	Current   string
	Previous  string
	Installed []Installed
}

// Manager installs and switches mihomo kernels.
type Manager struct {
	Dir    string
	API    string // GitHub API base, default https://api.github.com
	Repo   string // default MetaCubeX/mihomo
	HTTP   *http.Client
	GOOS   string // default runtime.GOOS
	GOARCH string // default runtime.GOARCH
	// Probe runs the new binary and returns its version; the install fails
	// if it errors. Default: ProbeVersion (`mihomo -v`).
	Probe func(ctx context.Context, bin string) (string, error)
	// AllowUnverified installs assets without a published digest. Off by
	// default: every current MetaCubeX release asset has a sha256 digest.
	AllowUnverified bool
	// Keep is how many versions stay installed (default 3; current and
	// previous are always kept).
	Keep int
}

func (m *Manager) api() string {
	if m.API != "" {
		return strings.TrimRight(m.API, "/")
	}
	return defaultAPI
}

func (m *Manager) repo() string {
	if m.Repo != "" {
		return m.Repo
	}
	return defaultRepo
}

func (m *Manager) client() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (m *Manager) goos() string {
	if m.GOOS != "" {
		return m.GOOS
	}
	return runtime.GOOS
}

func (m *Manager) goarch() string {
	if m.GOARCH != "" {
		return m.GOARCH
	}
	return runtime.GOARCH
}

var versionRE = regexp.MustCompile(`^(v\d+\.\d+\.\d+(?:[.-][0-9A-Za-z.]+)?|alpha-[0-9a-f]{7,40})$`)

// ValidVersion reports whether v is a version string this package creates.
// Versions become directory names.
func ValidVersion(v string) bool { return versionRE.MatchString(v) }

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// Check looks up the newest release of channel for this platform.
func (m *Manager) Check(ctx context.Context, channel string) (Release, error) {
	var path string
	switch channel {
	case "", ChannelStable:
		channel = ChannelStable
		path = "/repos/" + m.repo() + "/releases/latest"
	case ChannelAlpha:
		path = "/repos/" + m.repo() + "/releases/tags/" + alphaTag
	default:
		return Release{}, fmt.Errorf("unknown channel %q", channel)
	}
	return m.release(ctx, channel, path)
}

// CheckVersion looks up the stable release tagged version (e.g. v1.19.34)
// for this platform: used to install the version that already runs.
func (m *Manager) CheckVersion(ctx context.Context, version string) (Release, error) {
	if !ValidVersion(version) || !strings.HasPrefix(version, "v") {
		return Release{}, fmt.Errorf("invalid release version %q", version)
	}
	r, err := m.release(ctx, ChannelStable, "/repos/"+m.repo()+"/releases/tags/"+version)
	if err != nil {
		return Release{}, err
	}
	if r.Version != version {
		return Release{}, fmt.Errorf("release %s has an asset for %s, not %s", r.Tag, r.Version, version)
	}
	return r, nil
}

// Has reports whether version is installed.
func (m *Manager) Has(version string) bool {
	if !ValidVersion(version) {
		return false
	}
	fi, err := os.Stat(filepath.Join(m.Dir, version, binName))
	return err == nil && fi.Mode().IsRegular()
}

func (m *Manager) release(ctx context.Context, channel, path string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.api()+path, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "twinnel-helper")
	resp, err := m.client().Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub %s: %s", path, resp.Status)
	}
	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&gr); err != nil {
		return Release{}, fmt.Errorf("GitHub release: %w", err)
	}
	re := regexp.MustCompile(`^mihomo-` + regexp.QuoteMeta(m.goos()) + `-` + regexp.QuoteMeta(m.goarch()) +
		`-(v\d+\.\d+\.\d+|alpha-[0-9a-f]{7,40})\.gz$`)
	for _, a := range gr.Assets {
		mm := re.FindStringSubmatch(a.Name)
		if mm == nil {
			continue
		}
		if err := m.checkAssetURL(a.URL); err != nil {
			return Release{}, err
		}
		return Release{
			Channel: channel, Tag: gr.TagName, Version: mm[1], Asset: a.Name, URL: a.URL,
			Size: a.Size, Digest: a.Digest, Published: gr.PublishedAt,
		}, nil
	}
	return Release{}, fmt.Errorf("release %s has no asset for %s/%s", gr.TagName, m.goos(), m.goarch())
}

// checkAssetURL only lets downloads come from GitHub (or, in tests, from the
// configured API host).
func (m *Manager) checkAssetURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if m.api() == defaultAPI {
		if u.Scheme == "https" && u.Host == "github.com" {
			return nil
		}
	} else if api, _ := url.Parse(m.api()); api != nil && u.Scheme == api.Scheme && u.Host == api.Host {
		return nil
	}
	return fmt.Errorf("refusing asset URL outside the release host: %s", raw)
}

// Install downloads r, verifies its digest, unpacks it, probes the binary
// and makes it current (the old current becomes previous). If anything
// fails, current is unchanged.
func (m *Manager) Install(ctx context.Context, r Release) (State, error) {
	if !ValidVersion(r.Version) {
		return State{}, fmt.Errorf("invalid version %q", r.Version)
	}
	if err := m.checkAssetURL(r.URL); err != nil {
		return State{}, err
	}
	algo, want, _ := strings.Cut(r.Digest, ":")
	if r.Digest == "" || algo != "sha256" || len(want) != 64 {
		if !m.AllowUnverified {
			return State{}, fmt.Errorf("%s has no sha256 digest; refusing to install", r.Asset)
		}
		want = ""
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return State{}, err
	}
	tmp, err := os.MkdirTemp(m.Dir, ".install-")
	if err != nil {
		return State{}, err
	}
	defer os.RemoveAll(tmp)

	gz := filepath.Join(tmp, "download.gz")
	if err := m.download(ctx, r.URL, gz, want); err != nil {
		return State{}, err
	}
	bin := filepath.Join(tmp, binName)
	if err := gunzip(gz, bin); err != nil {
		return State{}, err
	}
	os.Remove(gz)
	probe := m.Probe
	if probe == nil {
		probe = ProbeVersion
	}
	got, err := probe(ctx, bin)
	if err != nil {
		return State{}, fmt.Errorf("new binary does not run: %w", err)
	}
	if got != "" && got != r.Version {
		return State{}, fmt.Errorf("new binary reports version %q, expected %q", got, r.Version)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return State{}, err
	}
	dst := filepath.Join(m.Dir, r.Version)
	if _, err := os.Lstat(dst); err == nil {
		// Reinstall of the same version: move the old dir aside first, but
		// never the one current points at while it is still current.
		cur, _ := m.link("current")
		if cur == r.Version {
			return m.List()
		}
		if err := os.RemoveAll(dst); err != nil {
			return State{}, err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		return State{}, err
	}
	if err := m.Switch(r.Version); err != nil {
		return State{}, err
	}
	m.prune()
	return m.List()
}

func (m *Manager) download(ctx context.Context, rawURL, dst, wantHex string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "twinnel-helper")
	cl := *m.client()
	// Redirects (GitHub → its CDN) are followed, but only over https, or
	// within the test host.
	cl.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "https" && r.URL.Host != via[0].URL.Host {
			return fmt.Errorf("refusing redirect to %s", r.URL.Redacted())
		}
		return nil
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", rawURL, resp.Status)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxDownload+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n > maxDownload {
		return errors.New("download too large")
	}
	if wantHex != "" {
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantHex) {
			return fmt.Errorf("checksum mismatch: got sha256:%s, want sha256:%s", got, wantHex)
		}
	}
	return nil
}

func gunzip(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	zr, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(zr, maxDownload))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	return os.Chmod(dst, 0o755)
}

var probeRE = regexp.MustCompile(`Mihomo Meta (\S+)`)

// ProbeVersion runs `bin -v` and parses "Mihomo Meta <version> ...".
func ProbeVersion(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := CombinedOutput(ctx, bin, "-v")
	if err != nil {
		return "", fmt.Errorf("%s -v: %w: %s", bin, err, strings.TrimSpace(string(out)))
	}
	mm := probeRE.FindStringSubmatch(string(out))
	if mm == nil {
		return "", fmt.Errorf("%s -v: unexpected output %q", bin, strings.TrimSpace(string(out)))
	}
	return mm[1], nil
}

// Binary is the path the service runs: <Dir>/current/mihomo.
func (m *Manager) Binary() string { return filepath.Join(m.Dir, "current", binName) }

func (m *Manager) link(name string) (string, error) {
	t, err := os.Readlink(filepath.Join(m.Dir, name))
	if err != nil {
		return "", err
	}
	return filepath.Base(t), nil
}

// setLink atomically points <Dir>/<name> at version.
func (m *Manager) setLink(name, version string) error {
	tmp := filepath.Join(m.Dir, fmt.Sprintf(".%s.tmp-%d", name, time.Now().UnixNano()))
	if err := os.Symlink(version, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(m.Dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Switch makes an installed version current; the old current becomes
// previous.
func (m *Manager) Switch(version string) error {
	if !ValidVersion(version) {
		return fmt.Errorf("invalid version %q", version)
	}
	if fi, err := os.Stat(filepath.Join(m.Dir, version, binName)); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("version %s is not installed", version)
	}
	cur, err := m.link("current")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if cur == version {
		return nil
	}
	if err := m.setLink("current", version); err != nil {
		return err
	}
	if cur != "" {
		return m.setLink("previous", cur)
	}
	return nil
}

// Rollback swaps current and previous.
func (m *Manager) Rollback() (State, error) {
	prev, err := m.link("previous")
	if err != nil {
		return State{}, errors.New("no previous version to roll back to")
	}
	if err := m.Switch(prev); err != nil {
		return State{}, err
	}
	return m.List()
}

// List returns the installed versions.
func (m *Manager) List() (State, error) {
	var st State
	st.Current, _ = m.link("current")
	st.Previous, _ = m.link("previous")
	ents, err := os.ReadDir(m.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	for _, e := range ents {
		if !e.IsDir() || !ValidVersion(e.Name()) {
			continue
		}
		bin := filepath.Join(m.Dir, e.Name(), binName)
		fi, err := os.Stat(bin)
		if err != nil {
			continue
		}
		st.Installed = append(st.Installed, Installed{
			Version: e.Name(), Path: bin, Time: fi.ModTime(),
			Current: e.Name() == st.Current, Previous: e.Name() == st.Previous,
		})
	}
	sort.Slice(st.Installed, func(i, j int) bool { return st.Installed[i].Time.After(st.Installed[j].Time) })
	return st, nil
}

func (m *Manager) prune() {
	keep := m.Keep
	if keep <= 0 {
		keep = 3
	}
	st, err := m.List()
	if err != nil {
		return
	}
	n := 0
	for _, in := range st.Installed { // newest first
		if in.Current || in.Previous {
			continue
		}
		n++
		if n+2 > keep {
			os.RemoveAll(filepath.Join(m.Dir, in.Version))
		}
	}
}

// Newer reports whether release version a is newer than installed b.
// Alpha builds are compared by inequality only (they have no order).
func Newer(a, b string) bool {
	if b == "" {
		return a != ""
	}
	if strings.HasPrefix(a, "alpha-") || strings.HasPrefix(b, "alpha-") {
		return a != b
	}
	return CompareSemver(a, b) > 0
}

// CompareSemver compares dotted numeric versions ("v1.19.34", "1.104.7").
// Non-numeric suffixes are ignored.
func CompareSemver(a, b string) int {
	pa, pb := semverParts(a), semverParts(b)
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func semverParts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	return out
}
