package i18n

// Texts of a controller held by another program's mihomo (Clash Party,
// Clash Verge Rev…): the status reason, the banner action, the apply
// card. Embedded in Strings; fields are promoted.

// ForeignCtlStrings holds them.
type ForeignCtlStrings struct {
	// %[1]s: the controller address, %[2]s: the client's name.
	ReasonCtlForeignApp string
	// %s: the controller address (the client is not known).
	ReasonCtlForeign string
	// The banner's action: → the health row with both fixes.
	ForeignHowToFix string
	// A health row's evidence source: %s the controller as the check words it.
	HealthEvidenceFrom string
	// Twinnel's mihomo state while another program's holds the controller
	// (status.MihomoView): %s is the client's name (App) or none (Anon).
	MihomoForeignApp, MihomoForeignAnon string
	// Why a control that acts on mihomo is off then (its tooltip).
	MihomoForeignLocked string
	// The foreign state's actions: move Twinnel's controller to a free
	// port (the controller-owner fix), and how to quit the other program
	// (its health row).
	ForeignMovePort, ForeignQuitHow string
	// The foreign process's row label (pid · executable).
	ForeignProcess string
}

var zhForeignCtl = ForeignCtlStrings{
	ReasonCtlForeignApp: "%[1]s 被另一个 mihomo 占用（看起来是 %[2]s）",
	ReasonCtlForeign:    "%s 被另一个 mihomo 占用",
	ForeignHowToFix:     "怎么解决",
	HealthEvidenceFrom:  "证据来自 %s",
	MihomoForeignApp:    "%s 占用控制端口",
	MihomoForeignAnon:   "另一个 mihomo 占用控制端口",
	MihomoForeignLocked: "控制端口上是另一个 mihomo，Twinnel 不操作它",
	ForeignMovePort:     "换个控制端口",
	ForeignQuitHow:      "怎么退出它",
	ForeignProcess:      "占用的进程",
}

var enForeignCtl = ForeignCtlStrings{
	ReasonCtlForeignApp: "%[1]s is taken by another mihomo (it looks like %[2]s)",
	ReasonCtlForeign:    "%s is taken by another mihomo",
	ForeignHowToFix:     "How to fix",
	HealthEvidenceFrom:  "Evidence from %s",
	MihomoForeignApp:    "%s holds the controller port",
	MihomoForeignAnon:   "Another mihomo holds the controller port",
	MihomoForeignLocked: "Another mihomo is on the controller port; Twinnel leaves it alone",
	ForeignMovePort:     "Change Twinnel's port",
	ForeignQuitHow:      "How to quit it",
	ForeignProcess:      "Held by",
}

// CtlForeign words a foreign controller for the status reason.
func (s *Strings) CtlForeign(addr, client string) string {
	if client == "" {
		return F(s.ReasonCtlForeign, addr)
	}
	return F(s.ReasonCtlForeignApp, addr, client)
}

// MihomoForeignState words Twinnel's mihomo while another program's holds
// the controller; client is its product name, "" when not known.
func (s *Strings) MihomoForeignState(client string) string {
	if client == "" {
		return s.MihomoForeignAnon
	}
	return F(s.MihomoForeignApp, client)
}
