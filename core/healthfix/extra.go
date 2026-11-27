package healthfix

// The fixes of competitor-autostart (disable-autostart),
// binary-integrity (repair-tailscale, repair-mihomo) and system-dns's
// no-backup (dns-backup).

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
)

type extraTexts struct {
	stepAutostart, stepRepairTS, stepRepairMihomo string
	stepDNSBackup, stepDNSBackupKeep              string
}

var zhExtra = extraTexts{
	stepDNSBackup:     "%s：备份记为 自动（DHCP）",
	stepDNSBackupKeep: "现在的 DNS 不变；mihomo 停了时看门狗临时恢复成自动获取",
	stepAutostart:     "由后台助手停用这些自启动项（不删除）：%s",
	stepRepairTS:      "重新安装 Tailscale，覆盖损坏的文件（tailnet 会断几秒）",
	stepRepairMihomo:  "重新下载当前版本的 mihomo（校验 sha256）替换损坏的文件，再重启 mihomo（网络会断几秒）",
}

var enExtra = extraTexts{
	stepDNSBackup:     "%s: backup recorded as Automatic (DHCP)",
	stepDNSBackupKeep: "The DNS stays as it is; if mihomo stops, the watchdog switches it to automatic for the time being",
	stepAutostart:     "The helper disables these autostart entries (nothing is deleted): %s",
	stepRepairTS:      "Reinstall Tailscale over the damaged files (the tailnet drops for a few seconds)",
	stepRepairMihomo:  "Download the current mihomo version again (sha256-checked), replace the damaged files and restart mihomo (the network drops for a few seconds)",
}

func txExtra(tag string) *extraTexts {
	if tx(tag) == &en {
		return &enExtra
	}
	return &zhExtra
}

// planExtra fills p for the actions of this file; ok is false for others.
func (f *Fixer) planExtra(ctx context.Context, p *Plan) bool {
	L, X := tx(f.Lang), txExtra(f.Lang)
	need := func(ok bool, why string) {
		if !ok && p.Blocked == "" {
			p.Blocked = why
		}
	}
	switch p.Action {
	case health.FixDisableAutostart:
		need(f.Helper != nil, L.blockedHelper)
		ids := splitParam(p.Param)
		p.Steps = []string{fmt.Sprintf(X.stepAutostart, strings.Join(ids, ", "))}
		if f.Helper != nil && len(ids) > 0 {
			var r rpc.AutostartChangeResult
			if err := f.Helper.Call(ctx, rpc.MethodAutostartChange, rpc.AutostartChangeParams{IDs: ids, Op: "disable", Preview: true}, &r); err == nil {
				p.Steps = append(p.Steps, r.Steps...)
			} else {
				need(false, err.Error())
			}
		}
		p.Undo = true
	case health.FixDNSBackup:
		need(f.Helper != nil, L.blockedHelper)
		if f.Helper != nil {
			var ds rpc.DNSStatus
			if err := f.Helper.Call(ctx, rpc.MethodDNSStatus, rpc.Empty{}, &ds); err == nil {
				need(ds.Supported, L.blockedDNS)
				for _, n := range ds.Unprotected {
					p.Steps = append(p.Steps, fmt.Sprintf(X.stepDNSBackup, n))
				}
			}
		}
		p.Steps = append(p.Steps, X.stepDNSBackupKeep)
	case health.FixRepairTailscale:
		need(f.Helper != nil, L.blockedHelper)
		p.Steps = []string{X.stepRepairTS}
	case health.FixRepairMihomo:
		need(f.Helper != nil, L.blockedHelper)
		p.Steps = []string{X.stepRepairMihomo}
	default:
		return false
	}
	return true
}

// applyExtra runs the actions of this file; ok is false for others.
func (f *Fixer) applyExtra(ctx context.Context, p Plan) (*Undo, bool, error) {
	switch p.Action {
	case health.FixDisableAutostart:
		var r rpc.AutostartChangeResult
		err := f.Helper.Call(ctx, rpc.MethodAutostartChange, rpc.AutostartChangeParams{IDs: splitParam(p.Param), Op: "disable"}, &r)
		if err != nil {
			return nil, true, err
		}
		u := r.Undo
		if u == nil {
			u = &rpc.AutostartChangeParams{IDs: splitParam(p.Param), Op: "enable"}
		}
		return &Undo{Action: p.Action, Autostart: u}, true, nil
	case health.FixDNSBackup:
		// Nothing to undo: only a restore value is recorded; the DNS is
		// as it was.
		var ds rpc.DNSStatus
		return nil, true, f.Helper.Call(ctx, rpc.MethodDNSBackup, rpc.Empty{}, &ds)
	case health.FixRepairTailscale:
		var r rpc.TailscaleInstallResult
		return nil, true, f.Helper.Call(ctx, rpc.MethodTailscaleInstall, rpc.TailscaleInstallParams{Repair: true}, &r)
	case health.FixRepairMihomo:
		var l rpc.KernelList
		if err := f.Helper.Call(ctx, rpc.MethodKernelInstall, rpc.KernelInstallParams{Repair: true}, &l); err != nil {
			return nil, true, err
		}
		var r rpc.ServiceOpResult
		return nil, true, f.Helper.Call(ctx, rpc.MethodServiceRestart, rpc.ServiceParams{Name: "mihomo"}, &r)
	}
	return nil, false, nil
}

// undoAutostart enables the entries a disable-autostart fix turned off.
func (f *Fixer) undoAutostart(ctx context.Context, u *rpc.AutostartChangeParams) error {
	if u.Op != "enable" {
		return errors.New("an autostart undo only enables entries again")
	}
	var r rpc.AutostartChangeResult
	return f.helper().Call(ctx, rpc.MethodAutostartChange, *u, &r)
}
