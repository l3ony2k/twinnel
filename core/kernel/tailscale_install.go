package kernel

// Installing the open-source Tailscale daemon (tailscale + tailscaled) for
// a machine that has none. Never the macOS app (it uses a
// NetworkExtension, the system VPN slot, which mihomo's TUN conflicts
// with).
//
// Where the binaries come from:
//
//   - Linux: the official static tarballs on pkgs.tailscale.com
//     (tailscale_<v>_<arch>.tgz), verified against the .sha256 file
//     published next to each one.
//   - macOS: pkgs.tailscale.com publishes no macOS tarball (only the app's
//     zip and pkg), and Tailscale's GitHub releases have no assets. The
//     open-source daemon built for macOS is Homebrew's bottle of the
//     tailscale formula: the formula API (formulae.brew.sh) names the
//     bottle and its sha256, the bytes come from ghcr.io. Only arm64
//     bottles are published; an Intel Mac gets an error (install it with
//     Homebrew or from source).
//
// Layout under TailscaleInstaller.Dir (normally <state>/tailscale):
//
//	<version>/tailscale, <version>/tailscaled
//	current -> <version>

import (
	"archive/tar"

	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/l3ony2k/twinnel/core/binsane"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Tailscale asset sources.
const (
	TSSourcePkgs     = "pkgs.tailscale.com"
	TSSourceHomebrew = "homebrew"
)

const (
	defaultBrewAPI   = "https://formulae.brew.sh/api"
	defaultPkgs      = "https://pkgs.tailscale.com"
	maxTailscaleSize = 128 << 20
)

// TailscaleAsset is a downloadable build of tailscale + tailscaled.
type TailscaleAsset struct {
	Version string // "1.104.7"
	Source  string // TSSourcePkgs or TSSourceHomebrew
	Name    string // file name or bottle tag
	URL     string
	SHA256  string // hex
}

// TailscaleInstalled is an installed build.
type TailscaleInstalled struct {
	Version string
	CLI     string // <Dir>/current/tailscale
	Daemon  string // <Dir>/current/tailscaled
}

// TailscaleInstaller downloads and unpacks the open-source Tailscale.
type TailscaleInstaller struct {
	Dir     string
	PkgsURL string // default https://pkgs.tailscale.com
	BrewAPI string // default https://formulae.brew.sh/api
	Track   string // "stable" (default) or "unstable" (pkgs only)
	HTTP    *http.Client
	GOOS    string
	GOARCH  string
	// MacOSMajor is the macOS major version (15, 26, 27...); it picks the
	// bottle. Default: from `sw_vers` through OSMajor.
	MacOSMajor int
	// Probe runs the new CLI and returns its version (default
	// ProbeTailscale: `tailscale version`).
	Probe func(ctx context.Context, cli string) (string, error)
	// RunMSI runs the Windows installer (default RunMSIExec) and returns
	// msiexec's exit code.
	RunMSI func(ctx context.Context, msi, log string) (int, error)
	// WindowsDir is where the MSI puts tailscale.exe and tailscaled.exe
	// (default %ProgramFiles%\Tailscale).
	WindowsDir string
	// Repair reinstalls over an install whose files are damaged (Windows:
	// msiexec /fa, then a plain install; elsewhere the managed build is
	// unpacked again).
	Repair bool
	// RepairMSI runs the Windows repair (default RunMSIRepair).
	RepairMSI func(ctx context.Context, msi, log string) (int, error)
	// Sane checks one installed binary (default binsane.Check for this
	// platform, signature included on Windows).
	Sane func(path string) binsane.Result
}

func (t *TailscaleInstaller) goos() string {
	if t.GOOS != "" {
		return t.GOOS
	}
	return runtime.GOOS
}

func (t *TailscaleInstaller) goarch() string {
	if t.GOARCH != "" {
		return t.GOARCH
	}
	return runtime.GOARCH
}

func (t *TailscaleInstaller) client() *http.Client {
	if t.HTTP != nil {
		return t.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (t *TailscaleInstaller) pkgs() string {
	if t.PkgsURL != "" {
		return strings.TrimRight(t.PkgsURL, "/")
	}
	return defaultPkgs
}

func (t *TailscaleInstaller) brew() string {
	if t.BrewAPI != "" {
		return strings.TrimRight(t.BrewAPI, "/")
	}
	return defaultBrewAPI
}

// Binaries are the paths a service runs: <Dir>/current/{tailscale,tailscaled}.
func (t *TailscaleInstaller) Binaries() (cli, daemon string) {
	return filepath.Join(t.Dir, "current", "tailscale"), filepath.Join(t.Dir, "current", "tailscaled")
}

// Resolve finds the newest build for this platform.
func (t *TailscaleInstaller) Resolve(ctx context.Context) (TailscaleAsset, error) {
	switch t.goos() {
	case "linux":
		return t.resolvePkgs(ctx)
	case "darwin":
		return t.resolveBrew(ctx)
	case "windows":
		return t.resolveMSI(ctx)
	}
	return TailscaleAsset{}, fmt.Errorf("installing Tailscale is not supported on %s", t.goos())
}

func (t *TailscaleInstaller) getJSON(ctx context.Context, raw string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "twinnel-helper")
	resp, err := t.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", hostOf(raw), resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v); err != nil {
		return fmt.Errorf("%s: %w", hostOf(raw), err)
	}
	return nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

var tarballRE = regexp.MustCompile(`^tailscale_(\d+\.\d+\.\d+)_[a-z0-9]+\.tgz$`)

func (t *TailscaleInstaller) resolvePkgs(ctx context.Context) (TailscaleAsset, error) {
	track := t.Track
	if track == "" {
		track = "stable"
	}
	if track != "stable" && track != "unstable" {
		return TailscaleAsset{}, fmt.Errorf("unknown track %q", track)
	}
	var idx struct {
		Tarballs        map[string]string
		TarballsVersion string
	}
	base := t.pkgs() + "/" + track + "/"
	if err := t.getJSON(ctx, base+"?mode=json", &idx); err != nil {
		return TailscaleAsset{}, err
	}
	name := idx.Tarballs[t.goarch()]
	m := tarballRE.FindStringSubmatch(name)
	if m == nil {
		return TailscaleAsset{}, fmt.Errorf("pkgs.tailscale.com has no tarball for linux/%s", t.goarch())
	}
	a := TailscaleAsset{Version: m[1], Source: TSSourcePkgs, Name: name, URL: base + name}
	sum, err := t.fetchSHA256(ctx, a.URL, name)
	if err != nil {
		return a, err
	}
	a.SHA256 = sum
	return a, nil
}

// fetchSHA256 reads the checksum pkgs.tailscale.com publishes next to a
// file, on the same host (<url>.sha256).
func (t *TailscaleInstaller) fetchSHA256(ctx context.Context, fileURL, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL+".sha256", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "twinnel-helper")
	resp, err := t.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s.sha256: %s", name, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	sum := strings.ToLower(strings.TrimSpace(strings.Fields(string(b) + " x")[0]))
	if !hex64(sum) {
		return "", fmt.Errorf("%s.sha256: not a sha256", name)
	}
	return sum, nil
}

func hex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// macOS names of Homebrew's bottle tags, oldest first.
var macBottleNames = []struct {
	major int
	name  string
}{
	{13, "ventura"}, {14, "sonoma"}, {15, "sequoia"}, {26, "tahoe"}, {27, "golden_gate"},
}

// pickBottle picks the arm64 bottle tag for macOS major: the host's own,
// else the newest one built for an older macOS, else the newest there is.
// The binaries are static Go builds, so any arm64 macOS bottle runs.
func pickBottle(files map[string]string, major int) string {
	known := map[string]int{}
	for _, n := range macBottleNames {
		known["arm64_"+n.name] = n.major
	}
	var tags []string
	for tag := range files {
		if strings.HasPrefix(tag, "arm64_") && tag != "arm64_linux" {
			tags = append(tags, tag)
		}
	}
	// Newest first; unknown names (newer than this table) count as newest.
	sort.Slice(tags, func(i, j int) bool {
		mi, oki := known[tags[i]]
		mj, okj := known[tags[j]]
		if !oki {
			mi = 1000
		}
		if !okj {
			mj = 1000
		}
		if mi != mj {
			return mi > mj
		}
		return tags[i] < tags[j]
	})
	for _, tag := range tags {
		if m, ok := known[tag]; ok && major > 0 && m <= major {
			return tag
		}
	}
	if len(tags) > 0 {
		return tags[0]
	}
	return ""
}

func (t *TailscaleInstaller) resolveBrew(ctx context.Context) (TailscaleAsset, error) {
	if t.goarch() != "arm64" {
		return TailscaleAsset{}, fmt.Errorf("no open-source Tailscale build for macOS/%s: install it with Homebrew (brew install tailscale)", t.goarch())
	}
	var f struct {
		Versions struct {
			Stable string `json:"stable"`
		} `json:"versions"`
		Revision int `json:"revision"`
		Bottle   struct {
			Stable struct {
				Files map[string]struct {
					URL    string `json:"url"`
					SHA256 string `json:"sha256"`
				} `json:"files"`
			} `json:"stable"`
		} `json:"bottle"`
	}
	if err := t.getJSON(ctx, t.brew()+"/formula/tailscale.json", &f); err != nil {
		return TailscaleAsset{}, err
	}
	if !ValidTailscaleVersion(f.Versions.Stable) {
		return TailscaleAsset{}, fmt.Errorf("formulae.brew.sh: unexpected version %q", f.Versions.Stable)
	}
	names := map[string]string{}
	for tag, file := range f.Bottle.Stable.Files {
		names[tag] = file.URL
	}
	major := t.MacOSMajor
	if major == 0 {
		major = OSMajor()
	}
	tag := pickBottle(names, major)
	if tag == "" {
		return TailscaleAsset{}, errors.New("formulae.brew.sh: no arm64 macOS bottle for tailscale")
	}
	file := f.Bottle.Stable.Files[tag]
	sum := strings.ToLower(file.SHA256)
	if !hex64(sum) {
		return TailscaleAsset{}, fmt.Errorf("bottle %s has no sha256", tag)
	}
	return TailscaleAsset{Version: f.Versions.Stable, Source: TSSourceHomebrew, Name: tag, URL: file.URL, SHA256: sum}, nil
}

var tsVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// ValidTailscaleVersion reports whether v is a plain x.y.z version (a
// directory name).
func ValidTailscaleVersion(v string) bool { return tsVersionRE.MatchString(v) }

// checkTSURL only lets downloads come from the source's own host (or, in
// tests, from the configured hosts).
func (t *TailscaleInstaller) checkTSURL(a TailscaleAsset) error {
	u, err := url.Parse(a.URL)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, b := range []string{t.pkgs(), t.brew()} {
		if bu, err := url.Parse(b); err == nil {
			allowed[bu.Scheme+"://"+bu.Host] = true
		}
	}
	if a.Source == TSSourceHomebrew && t.BrewAPI == "" {
		allowed["https://ghcr.io"] = true
	}
	if !allowed[u.Scheme+"://"+u.Host] {
		return fmt.Errorf("refusing download outside the release host: %s", u.Redacted())
	}
	return nil
}

// Install downloads a, verifies its sha256, unpacks tailscale and
// tailscaled, probes the CLI and makes the version current. On failure
// current is unchanged.
func (t *TailscaleInstaller) Install(ctx context.Context, a TailscaleAsset) (TailscaleInstalled, error) {
	if !ValidTailscaleVersion(a.Version) {
		return TailscaleInstalled{}, fmt.Errorf("invalid version %q", a.Version)
	}
	if !hex64(a.SHA256) {
		return TailscaleInstalled{}, fmt.Errorf("%s has no sha256; refusing to install", a.Name)
	}
	if err := t.checkTSURL(a); err != nil {
		return TailscaleInstalled{}, err
	}
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return TailscaleInstalled{}, err
	}
	tmp, err := os.MkdirTemp(t.Dir, ".install-")
	if err != nil {
		return TailscaleInstalled{}, err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, "download.tgz")
	if err := t.download(ctx, a, archive); err != nil {
		return TailscaleInstalled{}, err
	}
	if err := extractTailscale(archive, tmp); err != nil {
		return TailscaleInstalled{}, err
	}
	os.Remove(archive)
	if bad := t.Damaged(filepath.Join(tmp, "tailscale"), filepath.Join(tmp, "tailscaled")); len(bad) > 0 {
		return TailscaleInstalled{}, fmt.Errorf("the downloaded Tailscale is not a %s program: %s", t.goos(), bad[0].Detail)
	}
	probe := t.Probe
	if probe == nil {
		probe = ProbeTailscale
	}
	got, err := probe(ctx, filepath.Join(tmp, "tailscale"))
	if err != nil {
		return TailscaleInstalled{}, fmt.Errorf("new tailscale does not run: %w", err)
	}
	if got != "" && got != a.Version {
		return TailscaleInstalled{}, fmt.Errorf("new tailscale reports version %q, expected %q", got, a.Version)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return TailscaleInstalled{}, err
	}
	dst := filepath.Join(t.Dir, a.Version)
	if cur, _ := os.Readlink(filepath.Join(t.Dir, "current")); filepath.Base(cur) == a.Version && !t.Repair {
		cli, daemon := t.Binaries()
		return TailscaleInstalled{Version: a.Version, CLI: cli, Daemon: daemon}, nil
	}
	if err := os.RemoveAll(dst); err != nil {
		return TailscaleInstalled{}, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return TailscaleInstalled{}, err
	}
	link := filepath.Join(t.Dir, fmt.Sprintf(".current.tmp-%d", time.Now().UnixNano()))
	if err := os.Symlink(a.Version, link); err != nil {
		return TailscaleInstalled{}, err
	}
	if err := os.Rename(link, filepath.Join(t.Dir, "current")); err != nil {
		os.Remove(link)
		return TailscaleInstalled{}, err
	}
	cli, daemon := t.Binaries()
	return TailscaleInstalled{Version: a.Version, CLI: cli, Daemon: daemon}, nil
}

// Current is the installed current version, "" when none.
func (t *TailscaleInstaller) Current() string {
	cur, err := os.Readlink(filepath.Join(t.Dir, "current"))
	if err != nil {
		return ""
	}
	return filepath.Base(cur)
}

func (t *TailscaleInstaller) download(ctx context.Context, a TailscaleAsset, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "twinnel-helper")
	if a.Source == TSSourceHomebrew {
		// ghcr.io serves public blobs to an anonymous bearer token, as
		// Homebrew itself does.
		req.Header.Set("Authorization", "Bearer QQ==")
	}
	cl := *t.client()
	first := req.URL.Host
	cl.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "https" && r.URL.Host != first {
			return fmt.Errorf("refusing redirect to %s", r.URL.Redacted())
		}
		// The token is for ghcr.io only, not its blob CDN.
		if r.URL.Host != first {
			r.Header.Del("Authorization")
		}
		return nil
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", a.Name, resp.Status)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxTailscaleSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n > maxTailscaleSize {
		return errors.New("download too large")
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.SHA256) {
		return fmt.Errorf("checksum mismatch: got sha256:%s, want sha256:%s", got, a.SHA256)
	}
	return nil
}

// extractTailscale writes the tailscale and tailscaled files of a tgz
// (pkgs: tailscale_<v>_<arch>/tailscale; Homebrew: tailscale/<v>/bin/
// tailscale) into dir. Nothing else is extracted, and no path from the
// archive is used.
func extractTailscale(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	tr := tar.NewReader(zr)
	found := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("unpack: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Base(h.Name)
		if name != "tailscale" && name != "tailscaled" || found[name] {
			continue
		}
		parent := path.Base(path.Dir(h.Name))
		if parent != "bin" && !strings.HasPrefix(parent, "tailscale_") {
			continue
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, io.LimitReader(tr, maxTailscaleSize))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("unpack: %w", err)
		}
		if err := os.Chmod(filepath.Join(dir, name), 0o755); err != nil {
			return err
		}
		found[name] = true
	}
	if !found["tailscale"] || !found["tailscaled"] {
		return errors.New("unpack: the archive has no tailscale and tailscaled")
	}
	return nil
}

var tsProbeRE = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)

// ProbeTailscale runs `cli version` and returns the first line's x.y.z.
func ProbeTailscale(ctx context.Context, cli string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := CombinedOutput(ctx, cli, "version")
	if err != nil {
		return "", fmt.Errorf("%s version: %w: %s", cli, err, strings.TrimSpace(string(out)))
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	m := tsProbeRE.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", fmt.Errorf("%s version: unexpected output %q", cli, line)
	}
	return m[1], nil
}

// OSMajor is the macOS major version (`sw_vers -productVersion`), 0
// elsewhere or when unknown. A variable for tests.
var OSMajor = func() int {
	if runtime.GOOS != "darwin" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := Output(ctx, "/usr/bin/sw_vers", "-productVersion")
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range strings.TrimSpace(string(out)) {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
