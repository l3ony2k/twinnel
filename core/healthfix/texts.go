package healthfix

import (
	"context"

	"github.com/l3ony2k/twinnel/core/health"
)

// texts are the fix steps as the preview lists them.
type texts struct {
	stepRerun, stepEgress                          string
	stepCoexistOn, stepCoexistRegen, stepApply     string
	stepGuard, stepLoopback                        string
	stepSecret, stepSecretNote                     string
	stepDNS, stepDNSService                        string
	stepModeRule, stepExitOff                      string
	stepRestartMihomo, stepRestartTailscaled       string
	blockedHelper, blockedMihomo, blockedTailscale string
	blockedDNS                                     string
	errRerun, auto, layerLoopback                  string
	stepPortMove, stepPortWrite, stepPortRestart   string
	stepPortNote                                   string
	stepTUNStack, stepTUNRebuild, layerTUNStack    string
	// The undo's preview (UndoPreview).
	undoLayerBack, undoLayerDelete, undoLayerHistory string
	undoSettings, undoDNS, undoMode, undoExit        string
	undoAutostart, undoNothing                       string
	undoFailsAgain, undoFailsReason                  string
}

var zh = texts{
	stepRerun:             "重新运行健康检查",
	stepEgress:            "直连和经 mihomo 各请求一次 IP 回显地址，比较出口",
	stepCoexistOn:         "打开「Tailscale 共存」覆写",
	stepCoexistRegen:      "按当前 tailnet 重新生成「Tailscale 共存」覆写",
	stepApply:             "检查并应用配置：mihomo 热重载，有毫秒级停顿；失败会自动回到原配置",
	stepGuard:             "「防泄漏」覆写保护这些策略组：%s",
	stepLoopback:          "加一个覆写：external-controller 改为 %s",
	stepPortMove:          "Twinnel 的控制端口：%s → %s（覆写 controller-port）",
	stepPortWrite:         "写入配置；占着 %s 的那个 mihomo 不去动它",
	stepPortRestart:       "重启 Twinnel 的 mihomo，让它在新端口上监听（网络会断几秒）",
	stepPortNote:          "不能撤销：旧端口还被那个软件占着。它的 TUN 和 DNS 仍会和 Twinnel 冲突",
	stepSecret:            "生成随机的控制接口密钥，存进后台助手",
	stepSecretNote:        "网页面板、脚本等其他控制工具需要换用新密钥",
	stepDNS:               "把活动网络连接的 DNS 指向 mihomo",
	stepDNSService:        "%s：%s → %s",
	stepModeRule:          "把 mihomo 切到规则模式",
	stepExitOff:           "Tailscale 停用出口节点",
	stepRestartMihomo:     "重启 mihomo（网络会断几秒）",
	stepRestartTailscaled: "重启 tailscaled（tailnet 会断几秒）",
	blockedHelper:         "需要后台助手：先安装或启动它",
	blockedMihomo:         "连不上 mihomo 的控制接口",
	blockedTailscale:      "连不上 Tailscale",
	blockedDNS:            "这个系统上 Twinnel 还不能设置系统 DNS",
	errRerun:              "已修复，但重新检查失败",
	auto:                  "自动（DHCP）",
	layerLoopback:         "控制接口只听本机",
	stepTUNStack:          "加一个覆写：tun.stack 改为 gvisor",
	stepTUNRebuild:        "TUN 会重建，网络断一两秒",
	layerTUNStack:         "TUN 用 gvisor 栈",
	undoLayerBack:         "覆写「%s」恢复为修复前的内容",
	undoLayerDelete:       "删除修复加的覆写「%s」",
	undoLayerHistory:      "覆写「%s」恢复为历史记录 %s 里的内容",
	undoSettings:          "设置恢复为修复前的值",
	undoDNS:               "各网络连接的 DNS 恢复为修复前的设置",
	undoMode:              "mihomo 切回 %s 模式",
	undoExit:              "Tailscale 重新使用出口节点 %s",
	undoAutostart:         "重新启用这些自启动项：%s",
	undoNothing:           "这个修复没有改动覆写或设置，没有可撤销的内容",
	undoFailsAgain:        "撤销后回到修复前的状态，这项检查会再次不通过。",
	undoFailsReason:       "撤销后回到修复前的状态，这个问题会再出现：%s",
}

var en = texts{
	stepRerun:             "Run the health checks again",
	stepEgress:            "Fetch an IP-echo address once directly and once through mihomo, and compare",
	stepCoexistOn:         "Turn on the Tailscale coexistence override",
	stepCoexistRegen:      "Regenerate the Tailscale coexistence override from the tailnet now",
	stepApply:             "Check and apply the config: mihomo reloads with a pause of milliseconds; a failure goes back to the old config",
	stepGuard:             "The leak guard override protects these groups: %s",
	stepLoopback:          "Add an override: external-controller becomes %s",
	stepPortMove:          "Twinnel's controller port: %s → %s (override controller-port)",
	stepPortWrite:         "Write the config; the mihomo holding %s is left alone",
	stepPortRestart:       "Restart Twinnel's mihomo so it listens on the new port (the network drops for a few seconds)",
	stepPortNote:          "Cannot be undone: the old port is still held by that app. Its TUN and DNS still conflict with Twinnel",
	stepSecret:            "Generate a random controller secret and keep it in the helper",
	stepSecretNote:        "Other controller tools (a web dashboard, scripts) need the new secret",
	stepDNS:               "Point the DNS of the active network connections at mihomo",
	stepDNSService:        "%s: %s → %s",
	stepModeRule:          "Switch mihomo to rule mode",
	stepExitOff:           "Stop using the Tailscale exit node",
	stepRestartMihomo:     "Restart mihomo (the network drops for a few seconds)",
	stepRestartTailscaled: "Restart tailscaled (the tailnet drops for a few seconds)",
	blockedHelper:         "Needs the helper: install or start it first",
	blockedMihomo:         "mihomo's controller does not answer",
	blockedTailscale:      "Tailscale does not answer",
	blockedDNS:            "Twinnel cannot set the system DNS on this system yet",
	errRerun:              "fixed, but checking again failed",
	auto:                  "automatic (DHCP)",
	layerLoopback:         "Controller on this computer only",
	stepTUNStack:          "Add an override: tun.stack becomes gvisor",
	stepTUNRebuild:        "The TUN is rebuilt; the network drops for a second or two",
	layerTUNStack:         "TUN on the gvisor stack",
	undoLayerBack:         "Put the override “%s” back as it was before the fix",
	undoLayerDelete:       "Remove the override “%s” the fix added",
	undoLayerHistory:      "Put the override “%s” back as in history entry %s",
	undoSettings:          "Put the settings back as they were before the fix",
	undoDNS:               "Put the DNS of the network connections back as before the fix",
	undoMode:              "Switch mihomo back to %s mode",
	undoExit:              "Use the Tailscale exit node %s again",
	undoAutostart:         "Enable these autostart entries again: %s",
	undoNothing:           "this fix changed no override or setting, so there is nothing to undo",
	undoFailsAgain:        "Undoing goes back to the state before the fix, and this check fails again.",
	undoFailsReason:       "Undoing goes back to the state before the fix, and this problem comes back: %s",
}

func tx(tag string) *texts {
	if health.LangOf(health.WithLang(context.Background(), tag)) == "en" {
		return &en
	}
	return &zh
}
