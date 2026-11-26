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
// as <file>.duet-bak; if a rename fails, the files already replaced are
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
	"io"
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
	NewSuffix    = ".duet-new"
	BackupSuffix = ".duet-bak"
	// UIStamp is written into the UI dir: where the bundle came from.
	UIStamp = ".duet-ui.json"
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
					return filepath.Join(confDir, e.Name())
				}
			}
		}
		return filepath.Join(confDir, def)
	}
	u := func(k, def string) string {
		if v := strings.TrimSpace(urls[k]); v != "" {
			return v
		}
		return def
	}
	return []GeoItem{
		{Name: "geoip", URL: u("geoip", DefaultGeoIPURL), File: find("GeoIP.dat", "GeoIP.dat")},
		{Name: "geosite", URL: u("geosite", DefaultGeoSiteURL), File: find("GeoSite.dat", "GeoSite.dat")},
		{Name: "mmdb", URL: u("mmdb", DefaultMMDBURL), File: find("geoip.metadb", "Country.mmdb", "geoip.db", "geoip.metadb")},
		{Name: "asn", URL: u("asn", DefaultASNURL), File: find("ASN.mmdb", "ASN.mmdb")},
	}
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

// checkURL allows https only (http for loopback test servers).
func checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("bad URL: %w", err)
	}
	switch {
	case u.Scheme == "https" && u.Host != "":
	case u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"):
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

// download writes the body of r.URL to dst (created 0644) and checks size
// and digest. It returns the sha256 of what it wrote.
func (f *Fetcher) download(ctx context.Context, r Remote, dst string, limit int64) (string, error) {
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
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, limit+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
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
		os.Remove(dst)
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
func checkGeoFormat(name, p string) error {
	b, err := os.ReadFile(p)
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

// Swap is one prepared replacement: New is renamed to Dst, the old Dst
// (if any) to Dst+BackupSuffix.
type Swap struct {
	Dst, New string
	moved    bool
	hadOld   bool
}

// Commit renames every prepared file into place. On a failure, the files
// already replaced are restored and every New is removed.
func Commit(swaps []*Swap) error {
	for i, s := range swaps {
		bak := s.Dst + BackupSuffix
		if _, err := os.Lstat(s.Dst); err == nil {
			os.RemoveAll(bak)
			if err := os.Rename(s.Dst, bak); err != nil {
				Rollback(swaps[:i])
				Discard(swaps)
				return err
			}
			s.hadOld = true
		}
		if err := os.Rename(s.New, s.Dst); err != nil {
			if s.hadOld {
				os.Rename(bak, s.Dst)
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
		os.RemoveAll(s.Dst)
		if s.hadOld {
			os.Rename(s.Dst+BackupSuffix, s.Dst)
		}
		s.moved = false
	}
}

// Discard removes downloaded files that were not committed.
func Discard(swaps []*Swap) {
	for _, s := range swaps {
		if !s.moved {
			os.RemoveAll(s.New)
		}
	}
}

// PrepareGeo downloads one database next to its target and checks it.
func (f *Fetcher) PrepareGeo(ctx context.Context, it GeoItem, r Remote) (*Swap, string, error) {
	tmp := it.File + NewSuffix
	sum, err := f.download(ctx, r, tmp, maxGeoSize)
	if err != nil {
		return nil, "", err
	}
	if err := checkGeoFormat(it.Name, tmp); err != nil {
		os.Remove(tmp)
		return nil, "", err
	}
	return &Swap{Dst: it.File, New: tmp}, sum, nil
}

// UIStampData is the content of UIStamp.
type UIStampData struct {
	URL    string `json:"url"`
	Tag    string `json:"tag,omitempty"`
	SHA256 string `json:"sha256"`
}

// ReadUIStamp reads the stamp of an installed UI dir.
func ReadUIStamp(dir string) (UIStampData, bool) {
	var s UIStampData
	b, err := os.ReadFile(filepath.Join(dir, UIStamp))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return UIStampData{}, false
	}
	return s, true
}

// PrepareUI downloads the UI zip, extracts it into dir+NewSuffix and
// stamps it.
func (f *Fetcher) PrepareUI(ctx context.Context, dir string, r Remote) (*Swap, string, error) {
	zipPath := dir + ".zip" + NewSuffix
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, "", err
	}
	sum, err := f.download(ctx, r, zipPath, maxUIZip)
	if err != nil {
		return nil, "", err
	}
	defer os.Remove(zipPath)
	tmp := dir + NewSuffix
	os.RemoveAll(tmp)
	if err := extractZip(zipPath, tmp); err != nil {
		os.RemoveAll(tmp)
		return nil, "", err
	}
	stamp, _ := json.MarshalIndent(UIStampData{URL: r.URL, Tag: r.Tag, SHA256: sum}, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, UIStamp), stamp, 0o644); err != nil {
		os.RemoveAll(tmp)
		return nil, "", err
	}
	return &Swap{Dst: dir, New: tmp}, sum, nil
}

// extractZip unpacks regular files and dirs only (no symlinks), refusing
// paths that leave dst, and strips a single top-level directory (as
// mihomo's own UI updater does). The bundle must contain index.html.
func extractZip(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("ui bundle: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > maxUIFiles {
		return fmt.Errorf("ui bundle: too many files (%d)", len(zr.File))
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
			if err := os.MkdirAll(target, 0o755); err != nil {
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
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeZipFile(f, target); err != nil {
			return err
		}
	}
	if !hasIndex {
		return errors.New("ui bundle: no index.html")
	}
	return nil
}

func writeZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
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
