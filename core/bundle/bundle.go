// Package bundle reads and writes Twinnel's export: a zip with the
// person's subscriptions and override layers, and the running config for
// reference. It also reads the other things a person may bring to a new
// machine: a plain list of subscription URLs, or one Clash Verge Rev /
// Clash Party override file.
//
// The zip layout (manifest version 1):
//
//	manifest.json              what is in the zip (Manifest)
//	config.yaml                the running config, controller secret removed (not imported)
//	overrides/<id>.yaml        one per layer in Manifest.Layers
//	subscriptions/<id>.yaml    a subscription's last download (optional)
//
// Exports made before the manifest existed have config.yaml and
// overrides/<id>.yaml only; Read accepts them (Source "twinnel-legacy").
//
// Subscription URLs and the configs hold credentials. Nothing in this
// package logs, and Bundle's String hides the URLs.
package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/store"
	"gopkg.in/yaml.v3"
)

// Manifest format and version. A newer version is refused; an older one
// (or none) is read.
const (
	Format  = "twinnel-export"
	Version = 1

	ManifestFile = "manifest.json"
	ConfigFile   = "config.yaml"
)

// Limits. MaxFile keeps a request inside the RPC's 16 MiB line with base64.
const (
	MaxFile    = 11 << 20 // the file as handed in (zip or text)
	MaxEntry   = 32 << 20 // one file inside the zip, uncompressed
	MaxTotal   = 96 << 20 // every file inside the zip, uncompressed
	MaxEntries = 512
)

// Sources: what kind of file a Bundle came from.
const (
	SourceTwinnel  = "twinnel"        // an export with a manifest
	SourceLegacy   = "twinnel-legacy" // an export from before the manifest
	SourceURLs     = "url-list"       // subscription URLs, one per line
	SourceOverride = "override"       // one Verge Rev / Clash Party override file
)

// Manifest is manifest.json.
type Manifest struct {
	Format        string         `json:"format"`
	Version       int            `json:"version"`
	Created       time.Time      `json:"created,omitzero"`
	App           string         `json:"app,omitempty"` // Twinnel version that wrote it
	Layers        []Layer        `json:"layers"`
	Subscriptions []Subscription `json:"subscriptions,omitempty"`
	// LeakGuardGroups: the proxy groups the leak guard covered. The guard
	// layer itself is rebuilt; its setting travels so setup can reuse it.
	LeakGuardGroups []string `json:"leak_guard_groups,omitempty"`
}

// Layer is one override in compose order.
type Layer struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Stage    string   `json:"stage"`  // store.StageBase or store.StageUser
	Format   string   `json:"format"` // store.FormatMerge or store.FormatVerge
	Enabled  bool     `json:"enabled"`
	NameCode string   `json:"name_code,omitempty"`
	NameArgs []string `json:"name_args,omitempty"`
	File     string   `json:"file"` // overrides/<id>.yaml

	Content string `json:"-"`
}

// Subscription is one subscription. URL is a secret.
type Subscription struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Active          bool   `json:"active,omitempty"`
	IntervalMinutes int    `json:"interval_minutes"`
	URL             string `json:"url,omitempty"`
	File            string `json:"file,omitempty"` // subscriptions/<id>.yaml, when the content is included

	Content string `json:"-"`
}

// Bundle is what an import brings.
type Bundle struct {
	Source  string
	Version int       // the manifest's version; 0 without one
	Created time.Time // from the manifest
	App     string    // from the manifest

	Layers          []Layer
	Subscriptions   []Subscription
	LeakGuardGroups []string
	Config          string // config.yaml, for reference; never imported
}

// HasSecrets reports whether the bundle carries credentials: subscription
// URLs, downloaded subscriptions or the running config (node passwords).
func (b *Bundle) HasSecrets() bool {
	if b.Config != "" {
		return true
	}
	for _, s := range b.Subscriptions {
		if s.URL != "" || s.Content != "" {
			return true
		}
	}
	return false
}

// String summarizes the bundle without its contents or URLs.
func (b *Bundle) String() string {
	return fmt.Sprintf("bundle{source:%s version:%d layers:%d subscriptions:%d}", b.Source, b.Version, len(b.Layers), len(b.Subscriptions))
}

// GoString keeps %#v from printing URLs.
func (b *Bundle) GoString() string { return b.String() }

// LayerFile and SubscriptionFile are the zip paths of an item.
func LayerFile(id string) string        { return "overrides/" + id + ".yaml" }
func SubscriptionFile(id string) string { return "subscriptions/" + id + ".yaml" }

// Write writes b as a zip with a manifest. Layer and subscription File
// fields are set from their IDs; a subscription without Content gets no
// file.
func Write(w io.Writer, b *Bundle, created time.Time, app string) error {
	m := Manifest{Format: Format, Version: Version, Created: created.UTC(), App: app, Layers: []Layer{}, LeakGuardGroups: b.LeakGuardGroups}
	for _, l := range b.Layers {
		if !store.ValidID(l.ID) {
			return fmt.Errorf("invalid layer id %q", l.ID)
		}
		l.File = LayerFile(l.ID)
		m.Layers = append(m.Layers, l)
	}
	for _, s := range b.Subscriptions {
		if !store.ValidID(s.ID) {
			return fmt.Errorf("invalid subscription id %q", s.ID)
		}
		s.File = ""
		if s.Content != "" {
			s.File = SubscriptionFile(s.ID)
		}
		m.Subscriptions = append(m.Subscriptions, s)
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	put := func(name string, content []byte) error {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: created})
		if err != nil {
			return err
		}
		_, err = fw.Write(content)
		return err
	}
	if err := put(ManifestFile, append(mb, '\n')); err != nil {
		return err
	}
	if b.Config != "" {
		if err := put(ConfigFile, []byte(b.Config)); err != nil {
			return err
		}
	}
	for _, l := range m.Layers {
		if err := put(l.File, []byte(l.Content)); err != nil {
			return err
		}
	}
	for _, s := range m.Subscriptions {
		if s.File != "" {
			if err := put(s.File, []byte(s.Content)); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

// Parse reads any importable file: a Twinnel export zip, a list of
// subscription URLs, or one override file. name is the file's name (its
// base name names a single override); it may be empty.
func Parse(name string, data []byte) (*Bundle, error) {
	if len(data) == 0 {
		return nil, errors.New("the file is empty")
	}
	if len(data) > MaxFile {
		return nil, fmt.Errorf("the file is larger than %d MiB", MaxFile>>20)
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) || bytes.HasPrefix(data, []byte("PK\x05\x06")) {
		return Read(data)
	}
	if !isText(data) {
		return nil, errors.New("not a Twinnel export, a subscription URL list or an override file")
	}
	if subs, ok := parseURLList(string(data)); ok {
		return &Bundle{Source: SourceURLs, Subscriptions: subs}, nil
	}
	return parseOverride(name, data)
}

// Read reads a Twinnel export zip.
func Read(data []byte) (*Bundle, error) {
	if len(data) > MaxFile {
		return nil, fmt.Errorf("the file is larger than %d MiB", MaxFile>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a readable zip: %v", err)
	}
	if len(zr.File) > MaxEntries {
		return nil, fmt.Errorf("the zip has more than %d files", MaxEntries)
	}
	files := map[string]string{}
	var order []string
	total := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, err := cleanName(f.Name)
		if err != nil {
			return nil, err
		}
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("the zip has %s twice", name)
		}
		if !wanted(name) {
			continue // a stray file (a Finder __MACOSX, a README): ignored
		}
		b, err := readEntry(f, MaxTotal-total)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		total += len(b)
		files[name] = string(b)
		order = append(order, name)
	}
	if mb, ok := files[ManifestFile]; ok {
		return fromManifest(mb, files)
	}
	return fromLegacy(files, order)
}

// wanted reports whether a zip path is one the export writes.
func wanted(name string) bool {
	if name == ManifestFile || name == ConfigFile {
		return true
	}
	dir, file := path.Split(name)
	return (dir == "overrides/" || dir == "subscriptions/") && strings.HasSuffix(file, ".yaml")
}

func cleanName(name string) (string, error) {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return "", fmt.Errorf("the zip has an unsafe path %q", name)
	}
	c := path.Clean(name)
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("the zip has an unsafe path %q", name)
	}
	return c, nil
}

// readEntry reads one zip file, never more than MaxEntry or left bytes,
// whatever its header claims.
func readEntry(f *zip.File, left int) ([]byte, error) {
	limit := min(MaxEntry, left)
	if limit <= 0 {
		return nil, fmt.Errorf("the zip unpacks to more than %d MiB", MaxTotal>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		if limit < MaxEntry {
			return nil, fmt.Errorf("the zip unpacks to more than %d MiB", MaxTotal>>20)
		}
		return nil, fmt.Errorf("larger than %d MiB", MaxEntry>>20)
	}
	return b, nil
}

func fromManifest(raw string, files map[string]string) (*Bundle, error) {
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("manifest.json: %v", err)
	}
	if m.Format != Format {
		return nil, fmt.Errorf("manifest.json: not a Twinnel export (format %q)", m.Format)
	}
	if m.Version < 1 {
		return nil, fmt.Errorf("manifest.json: bad version %d", m.Version)
	}
	if m.Version > Version {
		return nil, fmt.Errorf("made by a newer Twinnel (export version %d; this one reads up to %d): update Twinnel first", m.Version, Version)
	}
	b := &Bundle{Source: SourceTwinnel, Version: m.Version, Created: m.Created, App: m.App, Config: files[ConfigFile]}
	for _, g := range m.LeakGuardGroups {
		if g != "" && len(g) <= 128 && !slices.Contains(b.LeakGuardGroups, g) {
			b.LeakGuardGroups = append(b.LeakGuardGroups, g)
		}
	}
	seen := map[string]bool{}
	for i, l := range m.Layers {
		if !store.ValidID(l.ID) {
			return nil, fmt.Errorf("manifest.json: layer %d: invalid id %q", i+1, l.ID)
		}
		if seen[l.ID] {
			return nil, fmt.Errorf("manifest.json: layer %q listed twice", l.ID)
		}
		seen[l.ID] = true
		f := l.File
		if f == "" {
			f = LayerFile(l.ID)
		}
		c, ok := files[f]
		if !ok {
			return nil, fmt.Errorf("manifest.json lists %s, which is not in the zip", f)
		}
		l.Content, l.File = c, f
		if l.Name == "" {
			l.Name = l.ID
		}
		if l.Stage == "" {
			l.Stage = store.StageUser
		}
		if l.Stage != store.StageUser && l.Stage != store.StageBase {
			return nil, fmt.Errorf("manifest.json: layer %q: unknown stage %q", l.ID, l.Stage)
		}
		if l.Format == "" {
			l.Format = DetectFormat([]byte(c))
		}
		if l.Format != store.FormatMerge && l.Format != store.FormatVerge {
			return nil, fmt.Errorf("manifest.json: layer %q: unknown format %q", l.ID, l.Format)
		}
		b.Layers = append(b.Layers, l)
	}
	seen = map[string]bool{}
	active := false
	for i, s := range m.Subscriptions {
		if !store.ValidID(s.ID) {
			return nil, fmt.Errorf("manifest.json: subscription %d: invalid id %q", i+1, s.ID)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("manifest.json: subscription %q listed twice", s.ID)
		}
		seen[s.ID] = true
		if s.File != "" {
			c, ok := files[s.File]
			if !ok {
				return nil, fmt.Errorf("manifest.json lists %s, which is not in the zip", s.File)
			}
			s.Content = c
		}
		if s.URL == "" && s.Content == "" {
			return nil, fmt.Errorf("manifest.json: subscription %q has neither a URL nor content", s.ID)
		}
		if s.Name == "" {
			s.Name = s.ID
		}
		if s.IntervalMinutes < 0 {
			s.IntervalMinutes = 0
		}
		if s.Active && active {
			s.Active = false // one active subscription
		}
		active = active || s.Active
		b.Subscriptions = append(b.Subscriptions, s)
	}
	return b, nil
}

// fromLegacy reads an export without a manifest: every override enabled,
// in the zip's order (the order the export wrote them in).
func fromLegacy(files map[string]string, order []string) (*Bundle, error) {
	b := &Bundle{Source: SourceLegacy, Config: files[ConfigFile]}
	for _, name := range order {
		dir, file := path.Split(name)
		if dir != "overrides/" {
			continue
		}
		id := strings.TrimSuffix(file, ".yaml")
		if !store.ValidID(id) {
			return nil, fmt.Errorf("%s: invalid override id %q", name, id)
		}
		c := files[name]
		b.Layers = append(b.Layers, Layer{ID: id, Name: id, Stage: store.StageUser, Format: DetectFormat([]byte(c)),
			Enabled: true, File: name, Content: c})
	}
	if len(b.Layers) == 0 && b.Config == "" {
		return nil, errors.New("not a Twinnel export: no manifest.json, config.yaml or overrides/")
	}
	return b, nil
}

// DetectFormat tells a Clash Verge Rev "Merge" file (prepend-rules, ...)
// from a Clash Party / Twinnel merge override.
func DetectFormat(content []byte) string {
	root, err := config.Parse(content)
	if err != nil || root.Kind != yaml.MappingNode {
		return store.FormatMerge
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i].Value
		if strings.HasPrefix(k, "prepend-") || strings.HasPrefix(k, "append-") {
			return store.FormatVerge
		}
	}
	return store.FormatMerge
}

func isText(b []byte) bool {
	return !bytes.ContainsRune(b, 0) && strings.ToValidUTF8(string(b), "�") == string(b)
}
