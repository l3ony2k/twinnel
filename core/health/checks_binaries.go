package health

// binary-integrity: the programs Twinnel installed are intact. An
// interrupted installer can leave tailscale.exe the right size but all
// zeros while tailscaled.exe (and the service) still run: everything else
// looks fine until the CLI is needed.

import (
	"context"
	"fmt"

	"github.com/l3ony2k/twinnel/core/binsane"
)

// BinaryRef is a program to check.
type BinaryRef struct {
	Name   string `json:"name"` // "mihomo", "tailscale", "tailscaled"
	Path   string `json:"path"`
	Signed bool   `json:"signed,omitempty"` // must carry a valid signature (Windows Authenticode)
}

// Reasons of binary-integrity.
const (
	ReasonBinMihomo    = "binary-integrity/mihomo-damaged"
	ReasonBinTailscale = "binary-integrity/tailscale-damaged"
)

// Fix actions of binary-integrity.
const (
	FixRepairMihomo    = "repair-mihomo"    // kernel.install repair, then restart mihomo
	FixRepairTailscale = "repair-tailscale" // tailscale.install repair
)

// checkBinary is binsane.Check; a variable for tests.
var checkBinary = binsane.Check

// BinaryIntegrity is the check.
type BinaryIntegrity struct{}

func (BinaryIntegrity) ID() string            { return "binary-integrity" }
func (c BinaryIntegrity) Title() string       { return c.title(&zhTexts) }
func (BinaryIntegrity) title(L *texts) string { return binText(L).title }
func (BinaryIntegrity) Platforms() []string   { return AllPlatforms }

func (BinaryIntegrity) Run(ctx context.Context, env Env) Result {
	B := binText(tx(ctx))
	bins := env.Settings().Binaries
	if len(bins) == 0 {
		return skipText(B.none)
	}
	res := Result{Status: StatusOK, Summary: B.ok}
	var bad []string
	denied := 0
	for _, b := range bins {
		r := checkBinary(b.Path, env.Platform(), b.Signed)
		line := fmt.Sprintf("%s: %s — ", b.Name, b.Path)
		switch {
		case r.Problem == binsane.OK:
			line += B.fine
		case r.Problem == binsane.Denied:
			// Not readable from here (a non-administrator run of an
			// ACL-protected dir): unknown, not damaged.
			denied++
			line += B.denied
		default:
			line += B.problem[r.Problem]
			if r.Detail != "" {
				line += " (" + r.Detail + ")"
			}
			bad = append(bad, b.Name)
			if b.Name == "mihomo" {
				res.because(ReasonBinMihomo)
			} else {
				res.because(ReasonBinTailscale)
			}
		}
		res.Evidence = append(res.Evidence, line)
	}
	switch {
	case len(bad) > 0:
		res.Status = StatusFail
		res.Summary = fmt.Sprintf(B.bad, joinNames(bad))
		res.Fix = &FixHint{Text: B.fix}
		if len(res.Reasons) == 1 && res.Reasons[0] == ReasonBinMihomo {
			res.Fix.AutoFix = FixRepairMihomo
		} else {
			res.Fix.AutoFix = FixRepairTailscale
		}
	case denied == len(bins):
		res.Status, res.Summary = StatusSkip, B.allDenied
	}
	return res
}

func joinNames(ns []string) string {
	out := ""
	for i, n := range ns {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

type binTexts struct {
	title, none, ok, fine, denied, allDenied, bad, fix string
	problem                                            map[binsane.Problem]string
}

var zhBin = binTexts{
	title:     "程序文件完整",
	none:      "这里不检查程序文件（由后台助手检查）",
	ok:        "Twinnel 安装的程序文件完好",
	fine:      "完好",
	denied:    "没有权限读取（不是不存在，可能完好）",
	allDenied: "没有权限读取程序文件，这次没法检查；由后台助手（管理员）检查",
	bad:       "%s 的程序文件损坏，需要重新安装",
	fix:       "重新安装损坏的程序",
	problem: map[binsane.Problem]string{
		binsane.Missing: "不存在", binsane.Empty: "是空文件", binsane.Zeroed: "内容全是 0（安装被中断）",
		binsane.BadHeader: "不是这个系统的程序", binsane.BadSign: "签名无效",
	},
}

var enBin = binTexts{
	title:     "Program files intact",
	none:      "Program files are checked by the helper, not here",
	ok:        "The programs Twinnel installed are intact",
	fine:      "intact",
	denied:    "access denied (not missing; may be fine)",
	allDenied: "Access to the program files was denied, so they were not checked; the helper (administrator) checks them",
	bad:       "%s is damaged and needs reinstalling",
	fix:       "Reinstall the damaged program",
	problem: map[binsane.Problem]string{
		binsane.Missing: "missing", binsane.Empty: "empty", binsane.Zeroed: "all zero bytes (an interrupted install)",
		binsane.BadHeader: "not a program for this system", binsane.BadSign: "invalid signature",
	},
}

func binText(L *texts) *binTexts {
	if L == textTables["en"] {
		return &enBin
	}
	return &zhBin
}

func init() {
	fixCatalog[ReasonBinMihomo] = FixSpec{Kind: FixAuto, Action: FixRepairMihomo}
	fixCatalog[ReasonBinTailscale] = FixSpec{Kind: FixAuto, Action: FixRepairTailscale}

	zhFixTexts[ReasonBinMihomo] = FixText{"Twinnel 下载的 mihomo 程序文件坏了（被截断、清零或被替换），代理随时会起不来。",
		"重新下载同一版本的 mihomo（校验 sha256）替换它，再重启 mihomo。期间会断网几秒。"}
	zhFixTexts[ReasonBinTailscale] = FixText{"Tailscale 的程序文件坏了，常见于安装被中断：服务还在跑，但 tailscale 命令用不了。",
		"重新安装 Tailscale 覆盖坏掉的文件，账号和设备不受影响。期间 tailnet 会断几秒。"}
	zhFixTexts[ReasonBinTailscale+"@"+Windows] = FixText{"Tailscale 的程序文件坏了（常见于安装被中断：tailscale.exe 全是 0 或签名无效），服务还在跑，但命令行和托盘用不了。",
		"用官方安装包修复（msiexec /fa，失败就重新安装），账号和设备不受影响。期间 tailnet 会断几秒。"}

	enFixTexts[ReasonBinMihomo] = FixText{"The mihomo program Twinnel downloaded is damaged (truncated, zeroed or replaced); the proxy may fail to start.",
		"Download the same mihomo version again (sha256-checked), replace it and restart mihomo. The network drops for a few seconds."}
	enFixTexts[ReasonBinTailscale] = FixText{"Tailscale's program files are damaged, usually by an interrupted install: the service runs but the tailscale command does not.",
		"Reinstall Tailscale over the damaged files; your account and devices are kept. The tailnet drops for a few seconds."}
	enFixTexts[ReasonBinTailscale+"@"+Windows] = FixText{"Tailscale's program files are damaged (usually an interrupted install: tailscale.exe is all zeros or its signature is invalid); the service runs but the CLI and tray app do not.",
		"Repair it with the official installer (msiexec /fa, else a fresh install); your account and devices are kept. The tailnet drops for a few seconds."}
}
