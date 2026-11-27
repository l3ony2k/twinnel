package health

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"github.com/l3ony2k/twinnel/core/foreign"
)

// Which controller the evidence came from (report pits #3/#16): when
// another program's mihomo (Clash Party, Clash Verge Rev…) holds the
// controller address, the rules, TUN and DNS that the checks read are that
// program's, not Twinnel's. The controller-owner check says so, and every
// check that read the running mihomo is marked with the controller it read
// (CheckReport.Controller); when that controller is foreign, the check is
// not judged.

// OwnerEnv is implemented by Envs that know who listens on the mihomo
// controller address (the helper's preflight, core/foreign).
type OwnerEnv interface {
	ControllerOwner(ctx context.Context) (foreign.Owner, error)
}

// ErrForeignController is what a check gets from the running mihomo when
// it belongs to another program.
var ErrForeignController = errors.New("the mihomo controller belongs to another program")

// WithOwner adds the controller owner to env.
func WithOwner(env Env, owner func(ctx context.Context) foreign.Owner) Env {
	return &ownerEnv{Env: env, owner: owner}
}

type ownerEnv struct {
	Env
	owner func(ctx context.Context) foreign.Owner
}

func (o *ownerEnv) ControllerOwner(ctx context.Context) (foreign.Owner, error) {
	return o.owner(ctx), nil
}

// EgressSupported forwards whether the wrapped Env can run the egress test.
func (o *ownerEnv) EgressSupported() bool { return egressSupported(o.Env) }

func (o *ownerEnv) DirectEgress(ctx context.Context, url string) (netip.Addr, error) {
	if e, ok := o.Env.(EgressEnv); ok {
		return e.DirectEgress(ctx, url)
	}
	return netip.Addr{}, ErrNotSupported
}

func (o *ownerEnv) ProxiedEgress(ctx context.Context, url string) (netip.Addr, []string, error) {
	if e, ok := o.Env.(EgressEnv); ok {
		return e.ProxiedEgress(ctx, url)
	}
	return netip.Addr{}, nil, ErrNotSupported
}

// ownerOf returns env's controller owner; ok is false when env does not
// know it.
func ownerOf(ctx context.Context, env Env) (foreign.Owner, bool) {
	oe, ok := env.(OwnerEnv)
	if !ok {
		return foreign.Owner{}, false
	}
	o, err := oe.ControllerOwner(ctx)
	return o, err == nil
}

// trackEnv records whether a check read the running mihomo (its
// controller, its DNS listener, its proxy port), and keeps a foreign one
// from being read at all.
type trackEnv struct {
	Env
	foreign bool
	mu      sync.Mutex
	touched bool
}

func (t *trackEnv) touch() bool {
	t.mu.Lock()
	t.touched = true
	t.mu.Unlock()
	return t.foreign
}

func (t *trackEnv) used() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.touched
}

func (t *trackEnv) MihomoRuntime(ctx context.Context) (*MihomoRuntime, error) {
	if t.touch() {
		return nil, ErrForeignController
	}
	return t.Env.MihomoRuntime(ctx)
}

func (t *trackEnv) LookupMihomo(ctx context.Context, name string) (DNSAnswer, error) {
	if t.touch() {
		return DNSAnswer{Name: name}, ErrForeignController
	}
	return t.Env.LookupMihomo(ctx, name)
}

// EgressSupported forwards whether the wrapped Env can run the egress test.
func (t *trackEnv) EgressSupported() bool { return egressSupported(t.Env) }

func (t *trackEnv) DirectEgress(ctx context.Context, url string) (netip.Addr, error) {
	if e, ok := t.Env.(EgressEnv); ok {
		return e.DirectEgress(ctx, url)
	}
	return netip.Addr{}, ErrNotSupported
}

func (t *trackEnv) ProxiedEgress(ctx context.Context, url string) (netip.Addr, []string, error) {
	if t.touch() {
		return netip.Addr{}, nil, ErrForeignController
	}
	if e, ok := t.Env.(EgressEnv); ok {
		return e.ProxiedEgress(ctx, url)
	}
	return netip.Addr{}, nil, ErrNotSupported
}

func (t *trackEnv) ControllerOwner(ctx context.Context) (foreign.Owner, error) {
	if oe, ok := t.Env.(OwnerEnv); ok {
		return oe.ControllerOwner(ctx)
	}
	return foreign.Owner{}, ErrNotSupported
}

// controllerLabel words the controller a check read, in L's language.
func controllerLabel(L *texts, o foreign.Owner, known bool) string {
	ot := otx(L)
	if !known || o.Addr == "" {
		return ot.labelUnknown
	}
	switch o.Verdict {
	case foreign.Managed:
		return fmt.Sprintf(ot.labelManaged, o.Addr)
	case foreign.Foreign:
		return fmt.Sprintf(ot.labelForeign, o.Addr, clientName(ot, o))
	}
	return o.Addr
}

func clientName(ot *ownerTexts, o foreign.Owner) string {
	if n := o.Client.Name(); n != "" {
		return n
	}
	return ot.anotherMihomo
}

// byClient words o's owner with withApp when its client is known (the
// client's name is the last argument) and with anon otherwise, a text
// that says "another mihomo" itself: the unknown name is never repeated
// or spaced as a product name ("（看起来是 另一个 mihomo）", Windows QA
// round 2). addr is the controller address, the first argument ("" for
// texts without one).
func byClient(o foreign.Owner, withApp, anon, addr string) string {
	n := o.Client.Name()
	switch {
	case n != "" && addr != "":
		return fmt.Sprintf(withApp, addr, n)
	case n != "":
		return fmt.Sprintf(withApp, n)
	case addr != "":
		return fmt.Sprintf(anon, addr)
	}
	return anon
}

// notJudged is the result of a check whose evidence would have come from
// another program's mihomo.
func notJudged(L *texts, o foreign.Owner, was Result) Result {
	ot := otx(L)
	r := Result{Status: StatusSkip, Summary: byClient(o, ot.notJudged, ot.notJudgedAnon, o.Addr)}
	if was.Summary != "" && was.Status != StatusSkip {
		r.Evidence = append(r.Evidence, fmt.Sprintf(ot.evWouldBe, was.Summary))
	}
	return r
}

// Reasons of controller-owner: another program's mihomo (Clash Party,
// Clash Verge Rev…) holds the controller address. Quitting it is up to the
// user (other-client); moving Twinnel's controller to a free port is one
// click (port-shared). Registered with their fixes and texts in init, as
// the dataplane check does.
const (
	ReasonOwnerOtherClient = "controller-owner/other-client"
	ReasonOwnerPortShared  = "controller-owner/port-shared"
)

// FixControllerPort moves Twinnel's controller to a free port
// (controller.move).
const FixControllerPort = "controller-port"

var ownerFixTexts = map[string]map[string]FixText{
	"zh-Hans": {
		ReasonOwnerOtherClient: {"控制端口被另一个代理软件的 mihomo 占着，Twinnel 的配置没有在运行。",
			"Twinnel 不去关别的应用。在菜单栏点开那个软件（如 Clash Party、Clash Verge）选「退出」，并在它的设置里关掉开机启动；然后重新检查。"},
		ReasonOwnerOtherClient + "@" + Windows: {"控制端口被另一个代理软件的 mihomo 占着，Twinnel 的配置没有在运行。",
			"Twinnel 不去关别的应用。在任务栏通知区域右键那个软件（如 Clash Party、Clash Verge）选「退出」，并关掉它的开机启动（Clash Party 用的是任务计划程序里的 mihomo-party 任务）；然后重新检查。"},
		ReasonOwnerOtherClient + "@" + Linux: {"控制端口被另一个代理软件的 mihomo 占着，Twinnel 的配置没有在运行。",
			"Twinnel 不去关别的应用。从托盘退出那个软件（如 Clash Party、Clash Verge），并关掉它的开机启动（桌面的自动启动项或 systemd 用户服务）；然后重新检查。"},
		ReasonOwnerPortShared: {"Twinnel 的 mihomo 和那个软件用的是同一个控制端口。",
			"把 Twinnel 的控制端口换到一个空闲端口，写进配置，并重启 Twinnel 的 mihomo。那个软件的 TUN 和 DNS 仍会和 Twinnel 冲突，长期用 Twinnel 还是要退出它。"},
	},
	"en": {
		ReasonOwnerOtherClient:                 {"Another proxy app's mihomo holds the controller port; Twinnel's config is not running.", "Twinnel does not quit other apps. Open that app from the menu bar (Clash Party, Clash Verge…), choose Quit, and turn off its launch at login in its settings; then check again."},
		ReasonOwnerOtherClient + "@" + Windows: {"Another proxy app's mihomo holds the controller port; Twinnel's config is not running.", "Twinnel does not quit other apps. Right-click that app in the notification area (Clash Party, Clash Verge…), choose Quit, and turn off its start-up (Clash Party uses the mihomo-party task in Task Scheduler); then check again."},
		ReasonOwnerOtherClient + "@" + Linux:   {"Another proxy app's mihomo holds the controller port; Twinnel's config is not running.", "Twinnel does not quit other apps. Quit that app from its tray icon (Clash Party, Clash Verge…) and turn off its autostart (desktop autostart entry or systemd user service); then check again."},
		ReasonOwnerPortShared:                  {"Twinnel's mihomo is set to the same controller port as that app.", "Move Twinnel's controller to a free port, write it into the config and restart Twinnel's mihomo. That app's TUN and DNS still conflict with Twinnel; to use Twinnel for good, quit it."},
	},
}

func init() {
	fixCatalog[ReasonOwnerOtherClient] = FixSpec{Kind: FixManual, Action: FixRerun}
	fixCatalog[ReasonOwnerPortShared] = FixSpec{Kind: FixAuto, Action: FixControllerPort}
	for tag, tbl := range fixTextTables {
		for k, v := range ownerFixTexts[tag] {
			tbl[k] = v
		}
	}
}

// ControllerOwnerCheck: the mihomo answering on the controller address is
// the one Twinnel runs.
type ControllerOwnerCheck struct{}

func (ControllerOwnerCheck) ID() string            { return "controller-owner" }
func (c ControllerOwnerCheck) Title() string       { return c.title(&zhTexts) }
func (ControllerOwnerCheck) title(L *texts) string { return otx(L).title }
func (ControllerOwnerCheck) Platforms() []string   { return AllPlatforms }

func (ControllerOwnerCheck) Run(ctx context.Context, env Env) Result {
	L := tx(ctx)
	ot := otx(L)
	o, ok := ownerOf(ctx, env)
	if !ok {
		return skipText(ot.unavailable)
	}
	switch o.Verdict {
	case foreign.Managed:
		r := Result{Status: StatusOK, Summary: fmt.Sprintf(ot.ok, o.Addr)}
		if o.PID > 0 {
			r.Evidence = append(r.Evidence, fmt.Sprintf("pid %d: %s", o.PID, o.Exe))
		}
		return r
	case foreign.Foreign:
		r := Result{Status: StatusFail, Summary: byClient(o, ot.foreign, ot.foreignAnon, o.Addr)}
		if o.PID > 0 {
			r.Evidence = append(r.Evidence, fmt.Sprintf("pid %d: %s", o.PID, orDash(o.Exe)))
		}
		if o.Parent != "" {
			r.Evidence = append(r.Evidence, fmt.Sprintf(ot.evParent, o.Parent))
		}
		if o.WorkDir != "" {
			r.Evidence = append(r.Evidence, fmt.Sprintf(ot.evWorkDir, o.WorkDir))
		}
		if o.Via == foreign.ViaSafePaths {
			r.Evidence = append(r.Evidence, ot.evSafePaths)
		}
		r.Evidence = append(r.Evidence, ot.evWhy)
		r.because(ReasonOwnerOtherClient)
		r.because(ReasonOwnerPortShared)
		r.Fix = &FixHint{Text: byClient(o, ot.fix, ot.fixAnon, ""), AutoFix: FixControllerPort}
		return r
	}
	if o.Addr == "" {
		return skipText(ot.unavailable)
	}
	return skipText(fmt.Sprintf(ot.nobody, o.Addr))
}

// ownerTexts are the texts of the owner check and the evidence labels.
type ownerTexts struct {
	title, ok, foreign, nobody, unavailable  string
	foreignAnon, fixAnon, notJudgedAnon      string // the client is not known
	evParent, evWorkDir, evSafePaths, evWhy  string
	fix                                      string
	labelManaged, labelForeign, labelUnknown string
	anotherMihomo, notJudged, evWouldBe      string
	labelController                          string
}

var zhOwnerTexts = ownerTexts{
	title:           "控制端口归属",
	ok:              "%s 是 Twinnel 管理的 mihomo",
	foreign:         "%s 被另一个 mihomo 占用（看起来是 %s）",
	foreignAnon:     "%s 被另一个 mihomo 占用",
	fixAnon:         "退出占着端口的那个 mihomo（并关掉它的开机自启），或把 Twinnel 的控制端口换到空闲端口",
	notJudgedAnon:   "%s 被另一个 mihomo 占用，读到的是它的配置，这项不判断",
	nobody:          "%s 上没有程序在监听",
	unavailable:     "没有读到控制端口上是哪个程序",
	evParent:        "父进程：%s",
	evWorkDir:       "它的工作目录：%s",
	evSafePaths:     "它拒绝了 Twinnel 的配置路径（SAFE_PATHS），说明它用的是另一个工作目录",
	evWhy:           "Twinnel 不会去重载别人的 mihomo，否则会改乱那个程序的配置",
	fix:             "退出 %s（并关掉它的开机自启），或把 Twinnel 的控制端口换到空闲端口",
	labelManaged:    "%s（Twinnel）",
	labelForeign:    "%s（%s）",
	labelUnknown:    "未确认归属的 mihomo",
	anotherMihomo:   "另一个 mihomo",
	notJudged:       "%s 被 %s 占用，读到的是它的配置，这项不判断",
	evWouldBe:       "按它的配置会得到：%s",
	labelController: "证据来源",
}

var enOwnerTexts = ownerTexts{
	title:           "Controller owner",
	ok:              "%s is the mihomo Twinnel manages",
	foreign:         "%s is taken by another mihomo (it looks like %s)",
	foreignAnon:     "%s is taken by another mihomo",
	fixAnon:         "Quit the program that runs that mihomo (and turn off its launch at login), or move Twinnel's controller to a free port",
	notJudgedAnon:   "%s is taken by another mihomo; what it reports is that program's config, so this check is not judged",
	nobody:          "Nothing listens on %s",
	unavailable:     "Could not read which program listens on the controller port",
	evParent:        "parent process: %s",
	evWorkDir:       "its work dir: %s",
	evSafePaths:     "it refused Twinnel's config path (SAFE_PATHS), so it runs from another work dir",
	evWhy:           "Twinnel does not reload another program's mihomo; that would break that program's config",
	fix:             "Quit %s (and turn off its launch at login), or move Twinnel's controller to a free port",
	labelManaged:    "%s (Twinnel)",
	labelForeign:    "%s (%s)",
	labelUnknown:    "mihomo of unconfirmed owner",
	anotherMihomo:   "another mihomo",
	notJudged:       "%s is taken by %s; what it reports is that program's config, so this check is not judged",
	evWouldBe:       "its config would give: %s",
	labelController: "Evidence from",
}

func otx(L *texts) *ownerTexts {
	if L == &enTexts {
		return &enOwnerTexts
	}
	return &zhOwnerTexts
}

// The dataplane probes (tun-dataplane) pass through both wrappers like
// Memo passes them: ErrNotSupported when the wrapped Env has none.

func (o *ownerEnv) DialTCP(ctx context.Context, addr string) error {
	if e, ok := o.Env.(DataplaneEnv); ok {
		return e.DialTCP(ctx, addr)
	}
	return ErrNotSupported
}

func (o *ownerEnv) DialViaProxy(ctx context.Context, proxy, addr, serverName string) error {
	if e, ok := o.Env.(DataplaneEnv); ok {
		return e.DialViaProxy(ctx, proxy, addr, serverName)
	}
	return ErrNotSupported
}

func (o *ownerEnv) Ping(ctx context.Context, ip string) error {
	if e, ok := o.Env.(DataplaneEnv); ok {
		return e.Ping(ctx, ip)
	}
	return ErrNotSupported
}

func (t *trackEnv) DialTCP(ctx context.Context, addr string) error {
	if e, ok := t.Env.(DataplaneEnv); ok {
		return e.DialTCP(ctx, addr)
	}
	return ErrNotSupported
}

// DialViaProxy goes through mihomo's proxy port: it reads the running
// mihomo.
func (t *trackEnv) DialViaProxy(ctx context.Context, proxy, addr, serverName string) error {
	if t.touch() {
		return ErrForeignController
	}
	if e, ok := t.Env.(DataplaneEnv); ok {
		return e.DialViaProxy(ctx, proxy, addr, serverName)
	}
	return ErrNotSupported
}

func (t *trackEnv) Ping(ctx context.Context, ip string) error {
	if e, ok := t.Env.(DataplaneEnv); ok {
		return e.Ping(ctx, ip)
	}
	return ErrNotSupported
}
