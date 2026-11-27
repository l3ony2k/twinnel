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
}

var zhForeignCtl = ForeignCtlStrings{
	ReasonCtlForeignApp: "%[1]s 被另一个 mihomo 占用（看起来是 %[2]s）",
	ReasonCtlForeign:    "%s 被另一个 mihomo 占用",
	ForeignHowToFix:     "怎么解决",
	HealthEvidenceFrom:  "证据来自 %s",
}

var enForeignCtl = ForeignCtlStrings{
	ReasonCtlForeignApp: "%[1]s is taken by another mihomo (it looks like %[2]s)",
	ReasonCtlForeign:    "%s is taken by another mihomo",
	ForeignHowToFix:     "How to fix",
	HealthEvidenceFrom:  "Evidence from %s",
}

// CtlForeign words a foreign controller for the status reason.
func (s *Strings) CtlForeign(addr, client string) string {
	if client == "" {
		return F(s.ReasonCtlForeign, addr)
	}
	return F(s.ReasonCtlForeignApp, addr, client)
}
