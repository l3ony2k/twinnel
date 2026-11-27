package kernel

// Accessories of the mihomo kernel that live in its config dir: the geo
// databases (geoip/geosite/mmdb/asn) and the external UI bundle.
//
// Downloads are verified against the sha256 digest GitHub publishes for
// every release asset (api.github.com .../releases/tags/<tag> or
// .../releases/latest, field "digest") when the URL is a GitHub release
// download; other URLs are downloaded unverified (size limit and a format
// check only). Files are swapped as a set: everything is downloaded and
// checked first, then each file is renamed into place with the old one kept
// as <file>.twinnel-bak; if a rename fails, the files already replaced are
// put back.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/l3ony2k/twinnel/core/legacy"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Default geo database URLs (mihomo's own defaults for geox-url).
const (
	DefaultGeoIPURL   = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat"
	DefaultGeoSiteURL = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
	DefaultMMDBURL    = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.metadb"
	DefaultASNURL     = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/GeoLite2-ASN.mmdb"
	// DefaultUIURL is zashboard's latest release bundle.
	DefaultUIURL = "https://github.com/Zephyruso/zashboard/releases/latest/download/dist.zip"
)

// Suffixes of the files written next to an accessory.
const (
	NewSuffix    = ".twinnel-new"
	BackupSuffix = ".twinnel-bak"
	// UIStamp is written into the UI dir: where the bundle came from.
	UIStamp = ".twinnel-ui.json"
	// LegacyUIStamp is UIStamp as written before the rename; read when
	// UIStamp is missing.
	LegacyUIStamp = "." + legacy.Name + "-ui.json"
)

// Size limits.
const (
	maxGeoSize = 256 << 20
	maxUIZip   = 64 << 20
	maxUIFiles = 5000
	maxUITotal = 256 << 20
)

// GeoItem is one geo database.
type GeoItem struct {
	Name string // "geoip", "geosite", "mmdb", "asn"
	URL  string
	File string // absolute path in the config dir
	Rel  string // File relative to the config dir (a plain file name)
}

// GeoItems lists the geo databases of a config dir with mihomo's file
// naming: an existing file whose name matches case-insensitively is used
// (Country.mmdb / geoip.db / geoip.metadb for mmdb, ASN.mmdb, GeoIP.dat,
// GeoSite.dat), else mihomo's default name. urls are geox-url entries
// (keys geoip, geosite, mmdb, asn); missing ones get the defaults.
func GeoItems(confDir string, urls map[string]string) []GeoItem {
	entries, _ := os.ReadDir(confDir)
	find := func(def string, names ...string) string {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			for _, n := range names {
				if strings.EqualFold(e.Name(), n) {
					return e.Name()
				}
			}
		}
		return def
	}
	u := func(k, def string) string {
		if v := strings.TrimSpace(urls[k]); v != "" {
			return v
		}
		return def
	}
	items := []GeoItem{
		{Name: "geoip", URL: u("geoip", DefaultGeoIPURL), Rel: find("GeoIP.dat", "GeoIP.dat")},
		{Name: "geosite", URL: u("geosite", DefaultGeoSiteURL), Rel: find("GeoSite.dat", "GeoSite.dat")},
		{Name: "mmdb", URL: u("mmdb", DefaultMMDBURL), Rel: find("geoip.metadb", "Country.mmdb", "geoip.db", "geoip.metadb")},
		{Name: "asn", URL: u("asn", DefaultASNURL), Rel: find("ASN.mmdb", "ASN.mmdb")},
	}
	for i := range items {
		items[i].File = filepath.Join(confDir, items[i].Rel)
	}
	return items
}

// Remote is what is known about a download before fetching it.
type Remote struct {
	URL    string
	Tag    string // release tag, for GitHub release assets
	Size   int64  // 0 if unknown
	SHA256 string // hex; "" if unknown (unverifiable)
}

// Fetcher downloads accessories.
type Fetcher struct {
	HTTP *http.Client
	API  string // GitHub API base, default https://api.github.com

	// Release lookups are cached for the life of the Fetcher: the four
	// geo databases share one release, and the unauthenticated GitHub API
	// allows only 60 requests an hour.
	mu       sync.Mutex
	releases map[string]*ghRelease
}

func (f *Fetcher) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return http.DefaultClient
}

func (f *Fetcher) api() string {
	if f.API != "" {
		return strings.TrimRight(f.API, "/")
	}
	return defaultAPI
}

// checkURL allows https only. The URLs come from the running config
// (geox-url, external-ui-url), which a subscription can set: plain http
// would let a network attacker swap the bundle, and http to loopback would
// let a subscription make the root helper send requests to local services.
func checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("bad URL: %w", err)
	}
	h := u.Hostname()
	ip := net.ParseIP(h)
	switch {
	case strings.EqualFold(h, "localhost") || (ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())):
		return nil, fmt.Errorf("refusing URL %q: downloads from local or private addresses are not allowed", redact(raw))
	case u.Scheme == "https" && u.Host != "":
	default:
		return nil, fmt.Errorf("refusing URL %q: only https downloads are allowed", redact(raw))
	}
	return u, nil
}

// redact drops the query of a URL for messages (it may carry a token).
func redact(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i] + "?…"
	}
	return raw
}

// githubAsset splits https://github.com/O/R/releases/download/TAG/NAME and
// https://github.com/O/R/releases/latest/download/NAME. tag is "" for
// latest.
func githubAsset(u *url.URL) (repo, tag, name string, ok bool) {
	if u.Host != "github.com" {
		return "", "", "", false
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(p) == 6 && p[2] == "releases" && p[3] == "download":
		return p[0] + "/" + p[1], p[4], p[5], true
	case len(p) == 6 && p[2] == "releases" && p[3] == "latest" && p[4] == "download":
		return p[0] + "/" + p[1], "", p[5], true
	}
	return "", "", "", false
}

// Resolve finds the size and digest of a download. For GitHub release
// assets it asks the GitHub API; for any other URL it returns only the URL
// (unverifiable). An asset without a digest is an error only if it is a
// GitHub asset that should have one; it is reported as unverifiable.
func (f *Fetcher) Resolve(ctx context.Context, raw string) (Remote, error) {
	r := Remote{URL: raw}
	u, err := checkURL(raw)
	if err != nil {
		return r, err
	}
	repo, tag, name, ok := githubAsset(u)
	if !ok {
		return r, nil
	}
	api := f.api() + "/repos/" + repo + "/releases/latest"
	if tag != "" {
		api = f.api() + "/repos/" + repo + "/releases/tags/" + url.PathEscape(tag)
	}
	rel, err := f.release(ctx, api)
	if err != nil {
		return r, err
	}
	for _, a := range rel.Assets {
		if a.Name != name {
			continue
		}
		r.Tag, r.Size = rel.TagName, a.Size
		if h, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && len(h) == 64 {
			r.SHA256 = strings.ToLower(h)
		}
		return r, nil
	}
	return r, fmt.Errorf("release %s of %s has no asset %q", orStr(rel.TagName, "latest"), repo, name)
}

func (f *Fetcher) release(ctx context.Context, api string) (*ghRelease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if rel, ok := f.releases[api]; ok {
		return rel, nil
	}
	rel := &ghRelease{}
	if err := getJSON(ctx, f.client(), api, rel); err != nil {
		return nil, err
	}
	if f.releases == nil {
		f.releases = map[string]*ghRelease{}
	}
	f.releases[api] = rel
	return rel, nil
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func getJSON(ctx context.Context, c *http.Client, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		reset := ""
		if t, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			reset = " until " + time.Unix(t, 0).Format("15:04")
		}
		return fmt.Errorf("GitHub API rate limit reached (60 requests an hour without a token); try again%s", reset)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v)
}

// download writes the body of r.URL to name in root (created exclusively,
// 0644: a file or symlink already there is removed first, never written
// through) and checks size and digest. It returns the sha256 of what it
// wrote.
func (f *Fetcher) download(ctx context.Context, r Remote, root *os.Root, name string, limit int64) (string, error) {
	if _, err := checkURL(r.URL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", redact(r.URL), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", redact(r.URL), resp.Status)
	}
	if err := root.RemoveAll(name); err != nil {
		return "", err
	}
	out, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, limit+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		root.Remove(name)
		return "", fmt.Errorf("download %s: %w", redact(r.URL), err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	switch {
	case n > limit:
		err = fmt.Errorf("download %s: larger than %d bytes", redact(r.URL), limit)
	case n == 0:
		err = fmt.Errorf("download %s: empty", redact(r.URL))
	case r.Size > 0 && n != r.Size:
		err = fmt.Errorf("download %s: got %d bytes, want %d", redact(r.URL), n, r.Size)
	case r.SHA256 != "" && sum != r.SHA256:
		err = fmt.Errorf("download %s: sha256 %s does not match the published digest %s", redact(r.URL), sum, r.SHA256)
	}
	if err != nil {
		root.Remove(name)
		return "", err
	}
	return sum, nil
}

// FileSHA256 returns the hex sha256 and size of a file.
func FileSHA256(p string) (string, int64, error) {
	fh, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer fh.Close()
	h := sha256.New()
	n, err := io.Copy(h, fh)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// checkGeoFormat is a cheap sanity check, so an HTML error page or a
// truncated file is never put in place: MaxMind DB files carry their
// metadata marker; the .dat files are protobuf lists starting with field 1
// (0x0a).
func checkGeoFormat(name string, root *os.Root, p string) error {
	b, err := root.ReadFile(p)
	if err != nil {
		return err
	}
	switch name {
	case "mmdb", "asn":
		if !bytes.Contains(b[max(0, len(b)-128<<10):], []byte("\xab\xcd\xefMaxMind.com")) {
			return fmt.Errorf("%s: not a MaxMind DB file", name)
		}
	case "geoip", "geosite":
		if len(b) < 2 || b[0] != 0x0a {
			return fmt.Errorf("%s: not a geodata .dat file", name)
		}
	}
	return nil
}

// Swap is one prepared replacement inside Root: New is renamed to Dst, the
// old Dst (if any) to Dst+BackupSuffix. Dst and New are relative to Root,
// so a symlink planted in a user-owned config dir can never redirect the
// helper's renames and removals outside it.
type Swap struct {
	Root     *os.Root
	Dst, New string
	moved    bool
	hadOld   bool
}

// Commit renames every prepared file into place. On a failure, the files
// already replaced are restored and every New is removed.
func Commit(swaps []*Swap) error {
	for i, s := range swaps {
		bak := s.Dst + BackupSuffix
		if _, err := s.Root.Lstat(s.Dst); err == nil {
			s.Root.RemoveAll(bak)
			if err := s.Root.Rename(s.Dst, bak); err != nil {
				Rollback(swaps[:i])
				Discard(swaps)
				return err
			}
			s.hadOld = true
		}
		if err := s.Root.Rename(s.New, s.Dst); err != nil {
			if s.hadOld {
				s.Root.Rename(bak, s.Dst)
			}
			Rollback(swaps[:i])
			Discard(swaps)
			return err
		}
		s.moved = true
	}
	return nil
}

// Rollback puts back what Commit replaced (used when a check after the
// swap fails).
func Rollback(swaps []*Swap) {
	for i := len(swaps) - 1; i >= 0; i-- {
		s := swaps[i]
		if !s.moved {
			continue
		}
		s.Root.RemoveAll(s.Dst)
		if s.hadOld {
			s.Root.Rename(s.Dst+BackupSuffix, s.Dst)
		}
		s.moved = false
	}
}

// Discard removes downloaded files that were not committed.
func Discard(swaps []*Swap) {
	for _, s := range swaps {
		if !s.moved {
			s.Root.RemoveAll(s.New)
		}
	}
}

// PrepareGeo downloads one database next to its target (it.Rel in root)
// and checks it.
func (f *Fetcher) PrepareGeo(ctx context.Context, root *os.Root, it GeoItem, r Remote) (*Swap, string, error) {
	if !filepath.IsLocal(it.Rel) {
		return nil, "", fmt.Errorf("%s: bad file name %q", it.Name, it.Rel)
	}
	tmp := it.Rel + NewSuffix
	sum, err := f.download(ctx, r, root, tmp, maxGeoSize)
	if err != nil {
		return nil, "", err
	}
	if err := checkGeoFormat(it.Name, root, tmp); err != nil {
		root.Remove(tmp)
		return nil, "", err
	}
	return &Swap{Root: root, Dst: it.Rel, New: tmp}, sum, nil
}

// UIStampData is the content of UIStamp.
type UIStampData struct {
	URL    string `json:"url"`
	Tag    string `json:"tag,omitempty"`
	SHA256 string `json:"sha256"`
}

// ReadUIStamp reads the stamp of an installed UI dir (dir relative to root).
func ReadUIStamp(root *os.Root, dir string) (UIStampData, bool) {
	var s UIStampData
	b, err := root.ReadFile(filepath.Join(dir, UIStamp))
	if errors.Is(err, fs.ErrNotExist) {
		b, err = root.ReadFile(filepath.Join(dir, LegacyUIStamp))
	}
	if err != nil || json.Unmarshal(b, &s) != nil {
		return UIStampData{}, false
	}
	return s, true
}

// PrepareUI downloads the UI zip, extracts it into dir+NewSuffix and stamps
// it. dir is relative to root.
func (f *Fetcher) PrepareUI(ctx context.Context, root *os.Root, dir string, r Remote) (*Swap, string, error) {
	if !filepath.IsLocal(dir) {
		return nil, "", fmt.Errorf("ui dir %q is not inside the config dir", dir)
	}
	zipPath := dir + ".zip" + NewSuffix
	if err := root.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, "", err
	}
	sum, err := f.download(ctx, r, root, zipPath, maxUIZip)
	if err != nil {
		return nil, "", err
	}
	defer root.Remove(zipPath)
	tmp := dir + NewSuffix
	if err := root.RemoveAll(tmp); err != nil {
		return nil, "", err
	}
	if err := extractZip(root, zipPath, tmp); err != nil {
		root.RemoveAll(tmp)
		return nil, "", err
	}
	stamp, _ := json.MarshalIndent(UIStampData{URL: r.URL, Tag: r.Tag, SHA256: sum}, "", "  ")
	if err := writeNew(root, filepath.Join(tmp, UIStamp), stamp); err != nil {
		root.RemoveAll(tmp)
		return nil, "", err
	}
	return &Swap{Root: root, Dst: dir, New: tmp}, sum, nil
}

func writeNew(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// extractZip unpacks regular files and dirs only (no symlinks), refusing
// paths that leave dst, and strips a single top-level directory (as
// mihomo's own UI updater does). The bundle must contain index.html.
// zipPath and dst are relative to root; nothing is written outside it.
func extractZip(root *os.Root, zipPath, dst string) error {
	zf, err := root.Open(zipPath)
	if err != nil {
		return fmt.Errorf("ui bundle: %w", err)
	}
	defer zf.Close()
	fi, err := zf.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(zf, fi.Size())
	if err != nil {
		return fmt.Errorf("ui bundle: %w", err)
	}
	if len(zr.File) > maxUIFiles {
		return fmt.Errorf("ui bundle: too many files (%d)", len(zr.File))
	}
	if err := root.Mkdir(dst, 0o755); err != nil {
		return err
	}
	prefix := commonTopDir(zr.File)
	var total int64
	hasIndex := false
	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, prefix)
		if name == "" || strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		clean := path.Clean(name)
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(name, "\\") {
			return fmt.Errorf("ui bundle: unsafe path %q", f.Name)
		}
		target := filepath.Join(dst, filepath.FromSlash(clean))
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := root.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case !mode.IsRegular():
			return fmt.Errorf("ui bundle: %q is not a regular file", f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > maxUITotal {
			return errors.New("ui bundle: too large when extracted")
		}
		if clean == "index.html" {
			hasIndex = true
		}
		if err := root.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeZipFile(root, f, target); err != nil {
			return err
		}
	}
	if !hasIndex {
		return errors.New("ui bundle: no index.html")
	}
	return nil
}

func writeZipFile(root *os.Root, f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// commonTopDir returns "dir/" when every entry is inside one top-level
// directory, else "".
func commonTopDir(files []*zip.File) string {
	top := ""
	for _, f := range files {
		if strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		first, _, found := strings.Cut(f.Name, "/")
		if !found {
			return ""
		}
		if top == "" {
			top = first
		} else if top != first {
			return ""
		}
	}
	if top == "" {
		return ""
	}
	return top + "/"
}
