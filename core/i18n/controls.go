package i18n

import "fmt"

// ControlStrings are the texts of the app's controls and their states:
// pending and failed changes, loading, unreachable services, operation
// results. Embedded in Strings, so they read as T.X.
type ControlStrings struct {
	// Unreachable mihomo or Tailscale, said in words; the raw error is
	// secondary (UnreachableDetail).
	UnreachableMihomoTitle, UnreachableMihomoBody, UnreachableMihomoShort string
	UnreachableTSTitle, UnreachableTSBody, UnreachableTSShort             string
	UnreachableDetail, Retry, OpenSettings                                string

	// A network error said in words (PlainNetError): 没能连上 <host>, the
	// proxy it went through, then the reason after NetReasonSep.
	NetUnreachableF, NetViaProxyF, NetReasonSep                             string
	NetProxyDown, NetRefused, NetTimeout, NetNoHost, NetTLS, NetRateLimited string

	// A control whose change failed (shown by the control, %s: reason).
	ModeFailed, TunFailed, TSPowerFailed, ExitNodeFailed, NodeFailed string

	// Configuration written but not applied yet (预览与应用 heading).
	Unapplied, UnappliedHint string

	// Operation results (toasts). %s: the item.
	DoneDeleted, DoneSubRefreshed, DoneSubSaved, DoneLayerSaved string
	DoneApplied, DoneRolledBack                                 string
	// A long operation that ended while the window was closed or in the
	// background: a system notification. %s: the operation.
	OpFinishedTitle, OpFailedTitle, OpFinishedBody string
	OpWarnings                                     string // %d: count

	// Flyout foot rows.
	PanelOpen, PanelQuit string
}

var zhControls = ControlStrings{
	UnreachableMihomoTitle: "无法连接 mihomo",
	UnreachableMihomoBody:  "duet 连不上 mihomo 的控制器。mihomo 可能没在运行，或者设置里的控制器地址、密钥不对。",
	UnreachableMihomoShort: "连不上 mihomo 的控制器",
	UnreachableTSTitle:     "无法连接 Tailscale",
	UnreachableTSBody:      "duet 连不上 tailscaled。Tailscale 可能没在运行，或者没有安装。",
	UnreachableTSShort:     "连不上 tailscaled",
	UnreachableDetail:      "详细信息",
	Retry:                  "重试",
	OpenSettings:           "打开设置",

	NetUnreachableF: "没能连上 %s",
	NetViaProxyF:    "（经由代理 %s）",
	NetReasonSep:    "：",
	NetProxyDown:    "代理没有响应，mihomo 可能没在运行",
	NetRefused:      "对方拒绝了连接",
	NetTimeout:      "连接超时",
	NetNoHost:       "找不到这个域名",
	NetTLS:          "证书校验失败",
	NetRateLimited:  "请求被限流，稍后再试",

	ModeFailed:     "模式没有切换：%s",
	TunFailed:      "TUN 没有切换：%s",
	TSPowerFailed:  "Tailscale 没有切换：%s",
	ExitNodeFailed: "出口节点没有切换：%s",
	NodeFailed:     "节点没有切换：%s",

	Unapplied:     "有未应用的改动",
	UnappliedHint: "订阅或覆写已保存，应用后才会生效",

	DoneDeleted:      "已删除「%s」",
	DoneSubRefreshed: "已更新订阅「%s」",
	DoneSubSaved:     "已保存订阅「%s」",
	DoneLayerSaved:   "已保存覆写「%s」",
	DoneApplied:      "已应用",
	DoneRolledBack:   "已回滚",
	OpFinishedTitle:  "%s：已完成",
	OpFailedTitle:    "%s：失败",
	OpFinishedBody:   "点按打开 duet 查看结果",
	OpWarnings:       "%d 条提醒",

	PanelOpen: "打开 duet…",
	PanelQuit: "退出 duet",
}

// NetUnreachable is NetUnreachableF for host.
func (s *ControlStrings) NetUnreachable(host string) string {
	return fmt.Sprintf(s.NetUnreachableF, host)
}

// NetViaProxy is NetViaProxyF for addr.
func (s *ControlStrings) NetViaProxy(addr string) string {
	return fmt.Sprintf(s.NetViaProxyF, addr)
}
