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

// notJudged is the result of a check whose evidence would have come from
// another program's mihomo.
func notJudged(L *texts, o foreign.Owner, was Result) Result {
	ot := otx(L)
	r := Result{Status: StatusSkip, Summary: fmt.Sprintf(ot.notJudged, o.Addr, clientName(ot, o))}
	if was.Summary != "" && was.Status != StatusSkip {
		r.Evidence = append(r.Evidence, fmt.Sprintf(ot.evWouldBe, was.Summary))
	}
	return r
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
		r := Result{Status: StatusFail, Summary: fmt.Sprintf(ot.foreign, o.Addr, clientName(ot, o))}
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
		r.Fix = &FixHint{Text: fmt.Sprintf(ot.fix, clientName(ot, o)), AutoFix: FixControllerPort}
		return r
	}
	if o.Addr == "" {
		return skipText(ot.unavailable)
	}
	return skipText(fmt.Sprintf(ot.nobody, o.Addr))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ownerTexts are the texts of the owner check and the evidence labels.
type ownerTexts struct {
	title, ok, foreign, nobody, unavailable  string
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
