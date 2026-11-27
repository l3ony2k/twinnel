// Package foreign tells whether the mihomo answering on the controller
// address is the one Twinnel manages, or another program's: a GUI client
// such as Clash Party or Clash Verge Rev running its own mihomo on the same
// port (report P0 #2). Twinnel must never hot-reload such a controller
// (PUT /configs): the other mihomo refuses Twinnel's path (SAFE_PATHS), or
// worse, loads it; and health must not judge the other client's config as
// if it were Twinnel's.
//
// The verdict comes from the process that listens on the controller port
// (platform code finds it: lsof on macOS, /proc on Linux, iphlpapi on
// Windows) compared with what Twinnel runs (the managed or adopted binary,
// the service's PID). When the port owner can't be read, a reload refused
// with mihomo's SAFE_PATHS error is the evidence instead.
package foreign

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Process is what the system says about the process listening on the
// controller port. Fields it could not read stay empty.
type Process struct {
	PID  int      `json:"pid"`
	PPID int      `json:"ppid,omitempty"`
	Exe  string   `json:"exe,omitempty"`  // full path of the executable
	Args []string `json:"args,omitempty"` // command line, argv[0] first
	Cwd  string   `json:"cwd,omitempty"`
	// ParentExe is the parent's executable (a GUI client starts its core
	// as a child: Clash Party.exe → mihomo.exe).
	ParentExe string `json:"parent_exe,omitempty"`
}

// Client is a known program that runs its own mihomo.
type Client string

const (
	ClientClashParty Client = "clash-party" // Clash Party, formerly Mihomo Party
	ClientVerge      Client = "clash-verge-rev"
	ClientClashXMeta Client = "clashx-meta"
	ClientFlClash    Client = "flclash"
)

// Clients lists the known clients in display order.
var Clients = []Client{ClientClashParty, ClientVerge, ClientClashXMeta, ClientFlClash}

// Name is the client's product name (the same in every language).
func (c Client) Name() string {
	switch c {
	case ClientClashParty:
		return "Clash Party"
	case ClientVerge:
		return "Clash Verge Rev"
	case ClientClashXMeta:
		return "ClashX Meta"
	case ClientFlClash:
		return "FlClash"
	}
	return ""
}

// markers: lower-case substrings of an executable path, a work dir or an
// argument that give a client away, on macOS, Windows and Linux. Paths
// are compared with forward slashes.
var markers = []struct {
	c    Client
	subs []string
}{
	// Clash Party: "Clash Party.app" / "Clash Party.exe" / clash-party
	// (Linux), work dir …/mihomo-party/work, macOS helper party.mihomo.helper,
	// its sidecar cores mihomo-smart / mihomo-alpha sit next to the app.
	{ClientClashParty, []string{"clash party", "clash-party", "clashparty", "mihomo-party", "mihomo party", "party.mihomo"}},
	// Clash Verge Rev: "Clash Verge.app", clash-verge.exe, sidecar
	// verge-mihomo(-alpha), data dir io.github.clash-verge-rev.clash-verge-rev,
	// clash-verge-service.
	{ClientVerge, []string{"clash verge", "clash-verge", "verge-mihomo", "io.github.clash-verge-rev"}},
	// ClashX Meta: "ClashX Meta.app", com.metacubex.ClashX.meta and its
	// helper com.metacubex.ClashX.ProxyConfigHelper.meta.
	{ClientClashXMeta, []string{"clashx meta", "clashx.meta", "com.metacubex.clashx", "clashx-meta"}},
	// FlClash: FlClash.exe / FlClash.app, its core FlClashCore, data dir
	// com.follow.clash.
	{ClientFlClash, []string{"flclash", "com.follow.clash"}},
}

// Identify returns the client one of texts (paths, arguments) belongs to,
// "" if none is known.
func Identify(texts ...string) Client {
	for _, m := range markers {
		for _, t := range texts {
			t = strings.ToLower(strings.ReplaceAll(t, `\`, "/"))
			if t == "" {
				continue
			}
			for _, s := range m.subs {
				if strings.Contains(t, s) {
					return m.c
				}
			}
		}
	}
	return ""
}

// IdentifyProcess names the client of a process from its executable, its
// parent, its work dir and its arguments.
func IdentifyProcess(p Process) Client {
	return Identify(append([]string{p.Exe, p.ParentExe, p.Cwd}, p.Args...)...)
}

// WorkDir is the mihomo home of p: the -d argument, else its cwd.
func WorkDir(p Process) string {
	for i, a := range p.Args {
		if a == "-d" && i+1 < len(p.Args) {
			return p.Args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "-d="); ok {
			return v
		}
	}
	return p.Cwd
}

// Verdict is who owns the controller.
type Verdict string

const (
	// Managed: the mihomo Twinnel runs (the managed kernel, the adopted
	// binary or the managed service's process).
	Managed Verdict = "managed"
	// Foreign: another program's mihomo. Never reload it.
	Foreign Verdict = "foreign"
	// Unknown: nothing listens, or the owner could not be read. Twinnel
	// goes on as before (a refused reload still turns it Foreign).
	Unknown Verdict = "unknown"
)

// How a verdict was reached (Owner.Via).
const (
	ViaPort      = "port-owner" // the process listening on the port
	ViaSafePaths = "safe-paths" // mihomo refused the config path (SAFE_PATHS)
)

// Owner is the answer of the preflight: who listens on the controller
// address, and whether that is Twinnel's mihomo.
type Owner struct {
	Addr    string  `json:"addr"`
	Verdict Verdict `json:"verdict"`
	Client  Client  `json:"client,omitempty"`
	PID     int     `json:"pid,omitempty"`
	Exe     string  `json:"exe,omitempty"`
	Parent  string  `json:"parent,omitempty"` // the parent's executable
	// WorkDir is the other mihomo's home: its -d / cwd, or the directory
	// its SAFE_PATHS error allowed.
	WorkDir string    `json:"work_dir,omitempty"`
	Via     string    `json:"via,omitempty"`
	At      time.Time `json:"at,omitzero"`
	// Detail is the raw evidence for logs and reports (the reload error
	// with its SAFE_PATHS text); never shown as the message.
	Detail string `json:"detail,omitempty"`
}

// IsForeign reports whether o names another program's mihomo.
func (o *Owner) IsForeign() bool { return o != nil && o.Verdict == Foreign }

// Label is a short English label of the owner for evidence lines and
// logs: "127.0.0.1:9090 (Clash Party, pid 4242)".
func (o Owner) Label() string {
	var parts []string
	switch o.Verdict {
	case Managed:
		parts = append(parts, "Twinnel")
	case Foreign:
		if n := o.Client.Name(); n != "" {
			parts = append(parts, n)
		} else {
			parts = append(parts, "another mihomo")
		}
	}
	if o.PID > 0 {
		parts = append(parts, "pid "+strconv.Itoa(o.PID))
	}
	if len(parts) == 0 {
		return o.Addr
	}
	return o.Addr + " (" + strings.Join(parts, ", ") + ")"
}

// Expect is what Twinnel runs, to compare the port owner with.
type Expect struct {
	// Binaries are executables Twinnel runs (the adopted binary) and
	// Dirs directories its kernels live in (kernels/mihomo): an executable
	// at or under one is Twinnel's.
	Binaries []string
	Dirs     []string
	// PIDs are the managed service's process (and, on Windows, its
	// wrapper twinnel-svchost: the core is its child).
	PIDs []int
}

// Classify decides who owns addr from the listening process p (nil: none
// found).
func Classify(addr string, p *Process, exp Expect, now time.Time) Owner {
	o := Owner{Addr: addr, Verdict: Unknown, Via: ViaPort, At: now}
	if p == nil || p.PID <= 0 {
		return o
	}
	o.PID, o.Exe, o.Parent, o.WorkDir = p.PID, p.Exe, p.ParentExe, WorkDir(*p)
	for _, pid := range exp.PIDs {
		if pid > 0 && (pid == p.PID || pid == p.PPID) {
			o.Verdict = Managed
			return o
		}
	}
	if p.Exe != "" {
		for _, b := range exp.Binaries {
			if b != "" && samePath(p.Exe, b) {
				o.Verdict = Managed
				return o
			}
		}
		for _, d := range exp.Dirs {
			if d != "" && under(p.Exe, d) {
				o.Verdict = Managed
				return o
			}
		}
	}
	o.Client = IdentifyProcess(*p)
	// The executable is known and is none of Twinnel's, or the process is
	// a known client's even though its path could not be read.
	if p.Exe != "" || o.Client != "" {
		o.Verdict = Foreign
	}
	return o
}

func clean(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if len(p) >= 2 && p[1] == ':' { // Windows drive paths compare without case
		p = strings.ToLower(p)
	}
	return strings.TrimSuffix(p, "/")
}

func samePath(a, b string) bool {
	if clean(a) == clean(b) {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && clean(ra) == clean(rb)
}

func under(p, dir string) bool {
	cp, cd := clean(p), clean(dir)
	if strings.HasPrefix(cp, cd+"/") {
		return true
	}
	rp, err1 := filepath.EvalSymlinks(p)
	rd, err2 := filepath.EvalSymlinks(dir)
	return err1 == nil && err2 == nil && strings.HasPrefix(clean(rp), clean(rd)+"/")
}

// safePathsRe matches mihomo's refusal of a config path outside its home
// (hub/route/configs.go): "path is not subpath of home directory or
// SAFE_PATHS: <path> \n allowed paths: [<dir> <dir>]".
var safePathsRe = regexp.MustCompile(`(?s)path is not subpath of home directory or SAFE_PATHS(?::\s*(.*?))?(?:\s*allowed paths:\s*\[(.*?)\])?\s*$`)

// IsSafePathsError reports whether a reload error is mihomo's SAFE_PATHS
// refusal: the mihomo that answered was started with another home dir, so
// it is not the one that runs Twinnel's config.
func IsSafePathsError(text string) bool { return safePathsRe.MatchString(text) }

// SafePathsAllowed returns the directories a SAFE_PATHS refusal names as
// allowed (the other mihomo's home).
func SafePathsAllowed(text string) []string {
	m := safePathsRe.FindStringSubmatch(text)
	if m == nil || strings.TrimSpace(m[2]) == "" {
		return nil
	}
	// mihomo prints a Go slice: entries separated by spaces, and a Windows
	// path may hold spaces itself. Split at a space followed by the start
	// of a new absolute path.
	s := strings.TrimSpace(m[2])
	split := regexp.MustCompile(` (?:[A-Za-z]:[\\/]|/|~)`)
	var out []string
	for {
		loc := split.FindStringIndex(s)
		if loc == nil {
			break
		}
		out = append(out, s[:loc[0]])
		s = s[loc[0]+1:]
	}
	return append(out, s)
}

// FromSafePaths is the owner a SAFE_PATHS refusal reveals: foreign, its
// client guessed from the allowed directory.
func FromSafePaths(addr, errText string, now time.Time) Owner {
	allowed := SafePathsAllowed(errText)
	o := Owner{Addr: addr, Verdict: Foreign, Via: ViaSafePaths, At: now, Detail: errText, Client: Identify(allowed...)}
	if len(allowed) > 0 {
		o.WorkDir = allowed[0]
	}
	return o
}
