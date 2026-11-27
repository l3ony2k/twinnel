package health

// Texts of tun-dataplane, kept with the check instead of in the shared
// tables (strings.go, fixtexts.go). TestDataplaneTextsComplete checks them
// like TestTextsComplete.

type dpTexts struct {
	title           string
	msgOK           string
	msgDead         string
	msgDialErr      string
	msgTUNOff       string
	msgNoTarget     string
	msgNotSupported string
	evTarget        string
	evStack         string
	evNotTUN        string
	evTCPOK         string
	evTCPRefused    string
	evTCPTimeout    string
	evPingOK        string
	evPingFail      string
	evProxyOK       string
	evProxyFail     string
	fixGvisor       string
	fixRestart      string
	fixRerun        string
}

var zhDP = dpTexts{
	title:           "TUN 数据通路",
	msgOK:           "经 TUN 的 TCP 连接正常（%s 栈）",
	msgDead:         "TUN 开着，但经它的 TCP 连接没有应答，上网会超时",
	msgDialErr:      "经 TUN 的测试连接出错：%v",
	msgTUNOff:       "mihomo 的 TUN 没有开启",
	msgNoTarget:     "测试地址都没有经过 mihomo 的 TUN，这次测不了",
	msgNotSupported: "这里测不了数据通路：%v",
	evTarget:        "测试目标 %s，路由经 %s",
	evStack:         "TUN 栈：%s（%s）",
	evNotTUN:        "%s 的路由经 %s，不是 mihomo 的 TUN",
	evTCPOK:         "TCP 握手 %s 完成",
	evTCPRefused:    "mihomo 拒绝了这个连接（REJECT 规则），通路本身正常",
	evTCPTimeout:    "TCP %s：%s 内没有应答",
	evPingOK:        "ICMP 到 TUN 网关 %s：有应答",
	evPingFail:      "ICMP 到 TUN 网关 %s：%v",
	evProxyOK:       "同一目标经 mihomo 的代理端口 %s：能连上（出站和规则正常）",
	evProxyFail:     "同一目标经 mihomo 的代理端口 %s：%v",
	fixGvisor:       "改用 gvisor 栈：加一个覆写把 tun.stack 设为 gvisor 并应用，可以撤销",
	fixRestart:      "重启 mihomo，让它重建 TUN",
	fixRerun:        "重新检查一次",
}

var enDP = dpTexts{
	title:           "TUN data path",
	msgOK:           "TCP through the TUN works (%s stack)",
	msgDead:         "The TUN is on, but TCP through it gets no answer: connections time out",
	msgDialErr:      "The test connection through the TUN failed: %v",
	msgTUNOff:       "mihomo's TUN is off",
	msgNoTarget:     "No test address goes through mihomo's TUN, so this cannot be measured now",
	msgNotSupported: "The data path cannot be tested here: %v",
	evTarget:        "Test target %s, routed via %s",
	evStack:         "TUN stack: %s (%s)",
	evNotTUN:        "%s is routed via %s, not mihomo's TUN",
	evTCPOK:         "TCP handshake done in %s",
	evTCPRefused:    "mihomo refused the connection (a REJECT rule); the path itself works",
	evTCPTimeout:    "TCP %s: no answer within %s",
	evPingOK:        "ICMP to the TUN gateway %s: answered",
	evPingFail:      "ICMP to the TUN gateway %s: %v",
	evProxyOK:       "The same target through mihomo's proxy port %s: connects (outbound and rules work)",
	evProxyFail:     "The same target through mihomo's proxy port %s: %v",
	fixGvisor:       "Switch to the gvisor stack: add an override that sets tun.stack to gvisor and apply it; it can be undone",
	fixRestart:      "Restart mihomo so it builds the TUN again",
	fixRerun:        "Check again",
}

// dpFor returns the tun-dataplane texts in L's language.
func dpFor(L *texts) *dpTexts {
	if L == &enTexts {
		return &enDP
	}
	return &zhDP
}

// dpFixTexts are the fix catalog's texts of the tun-dataplane reasons,
// merged into fixTextTables by checks_dataplane.go's init.
var dpFixTexts = map[string]map[string]FixText{
	"zh-Hans": {
		ReasonDataplaneTCP: {"TUN 开着，但经它的 TCP 连接没有应答，上网会超时。",
			"改用 gvisor 栈：加一个覆写把 tun.stack 设为 gvisor 并应用，TUN 会重建，网络断一两秒。可以撤销。"},
		ReasonDataplaneGvisor: {"TUN 开着，但经它的 TCP 连接没有应答，上网会超时。",
			"重启 mihomo，让它重建 TUN。期间网络会断几秒。"},
		ReasonDataplaneDial: {"经 TUN 的测试连接出错，这次没测出结果。", "重新检查一次。"},
	},
	"en": {
		ReasonDataplaneTCP: {"The TUN is on, but TCP through it gets no answer: connections time out.",
			"Switch to the gvisor stack: add an override that sets tun.stack to gvisor and apply it. The TUN is rebuilt and the network drops for a second or two. It can be undone."},
		ReasonDataplaneGvisor: {"The TUN is on, but TCP through it gets no answer: connections time out.",
			"Restart mihomo so it builds the TUN again. The network drops for a few seconds."},
		ReasonDataplaneDial: {"The test connection through the TUN failed, so there is no result this time.", "Check again."},
	},
}
