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
	stepTUNStack, stepTUNRebuild, layerTUNStack    string
}

var zh = texts{
	stepRerun:             "重新运行健康检查",
	stepEgress:            "直连和经 mihomo 各请求一次 IP 回显地址，比较出口",
	stepCoexistOn:         "打开「Tailscale 共存」覆写",
	stepCoexistRegen:      "按当前 tailnet 重新生成「Tailscale 共存」覆写",
	stepApply:             "检查并应用配置：mihomo 热重载，有毫秒级停顿；失败会自动回到原配置",
	stepGuard:             "「防泄漏」覆写保护这些策略组：%s",
	stepLoopback:          "加一个覆写：external-controller 改为 %s",
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
}

var en = texts{
	stepRerun:             "Run the health checks again",
	stepEgress:            "Fetch an IP-echo address once directly and once through mihomo, and compare",
	stepCoexistOn:         "Turn on the Tailscale coexistence override",
	stepCoexistRegen:      "Regenerate the Tailscale coexistence override from the tailnet now",
	stepApply:             "Check and apply the config: mihomo reloads with a pause of milliseconds; a failure goes back to the old config",
	stepGuard:             "The leak guard override protects these groups: %s",
	stepLoopback:          "Add an override: external-controller becomes %s",
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
}

func tx(tag string) *texts {
	if health.LangOf(health.WithLang(context.Background(), tag)) == "en" {
		return &en
	}
	return &zh
}
