package i18n

import (
	"regexp"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/msg"
	"github.com/l3ony2k/duet/core/rpc"
)

// The helper reports compose, apply and takeover warnings and errors as
// English text, coded by core/msg (the helper sends the codes as
// rpc.Msg; an older helper's texts are coded here with the same table).
// Strings.Msg words a code in the UI language.

// WarningKind names the kind of a known warning ("reload-disrupts", ...),
// or "" for one duet does not know. The UI uses it to skip what another
// row already says.
func WarningKind(w string) string { return msg.Kind(w) }

// Warning returns w in the UI language when it is a text duet knows, w
// itself otherwise.
func (s *Strings) Warning(w string) string {
	if out, ok := s.Msg(msg.Parse(w)); ok {
		return out
	}
	return w
}

// Msg words a coded helper message; false when the code has no text
// (unknown, or none).
func (s *Strings) Msg(m rpc.Msg) (string, bool) {
	if m.Code == "" {
		return "", false
	}
	tmpl, ok := s.WarningTexts[m.Code]
	if !ok {
		tmpl, ok = s.MsgTexts[m.Code]
	}
	if !ok {
		return "", false
	}
	args := make([]any, len(m.Args))
	for i, v := range m.Args {
		args[i] = strings.ReplaceAll(v, "\n", s.ListSep)
	}
	return F(tmpl, args...), true
}

// MsgText words m for display: its text, or, for a code the app does not
// know, a generic line (raw is then true: the caller keeps m.Text behind a
// copy affordance).
func (s *Strings) MsgText(m rpc.Msg, warning bool) (text string, raw bool) {
	if out, ok := s.Msg(m); ok {
		return out, false
	}
	if warning {
		return s.MsgUnknownWarn, true
	}
	return s.MsgUnknown, true
}

// --- history reasons ---

// Reasons of history entries written before the helper sent codes.
var legacyReasons = []struct {
	code string
	re   *regexp.Regexp
}{
	{"backup-before-apply", regexp.MustCompile(`^file on disk before apply$`)},
	{"apply", regexp.MustCompile(`^apply$`)},
	{"rollback", regexp.MustCompile(`^rollback to (\S+)$`)},
	{"adopt-failed", regexp.MustCompile(`^adopt (\S+) failed$`)},
	{"takeover-undo", regexp.MustCompile(`^adopt (\S+) rolled back$`)},
	{"adopt", regexp.MustCompile(`^adopt (\S+)$`)},
	{"controller-secure", regexp.MustCompile(`^controller\.secure$`)},
	{"harden", regexp.MustCompile(`^harden (\S+)$`)},
	{"subscription-updated", regexp.MustCompile(`^subscription (\S+) updated$`)},
	{"tailnet-changed", regexp.MustCompile(`^tailnet changed$`)},
}

// HistoryReasonCode returns an entry's reason code: its own, or one read
// from an older entry's English reason ("" for the user's own words).
func HistoryReasonCode(code string, args []string, reason string) (string, []string) {
	if code != "" {
		return code, args
	}
	for _, l := range legacyReasons {
		if m := l.re.FindStringSubmatch(reason); m != nil {
			return l.code, m[1:]
		}
	}
	return "", nil
}

// HistoryReasonText words a history entry's reason: a code in the UI
// language, the user's own words as written. A rollback names the time of
// the entry it went back to (from the ID, which starts with it).
func (s *Strings) HistoryReasonText(code string, args []string, reason string) string {
	code, args = HistoryReasonCode(code, args, reason)
	tmpl, ok := s.HistReasons[code]
	if !ok {
		return reason
	}
	var fa []any
	switch code {
	case "rollback":
		ref := s.HistRollbackRef
		if len(args) > 0 {
			if t, ok := HistoryIDTime(args[0]); ok {
				ref = t.Local().Format("01-02 15:04:05")
			}
		}
		fa = []any{ref}
	case "subscription-updated":
		if len(args) > 0 {
			fa = []any{args[0]}
		} else {
			fa = []any{""}
		}
	}
	return F(tmpl, fa...)
}

// HistoryIDTime reads the time a history ID starts with
// (20261127T060539Z-…).
func HistoryIDTime(id string) (time.Time, bool) {
	if len(id) < 16 {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102T150405Z", id[:16])
	return t, err == nil
}

// --- reload impact ---

// ImpactLines words what a hot reload does beyond its millisecond pause,
// from the impact's fields (one line each; none for a gentle reload).
func (s *Strings) ImpactLines(im *rpc.ReloadImpact) []string {
	if im == nil {
		return nil
	}
	if im.Unknown {
		return []string{s.ImpactUnknown}
	}
	var out []string
	if im.TUNRebuild {
		var why []string
		for _, w := range im.TUNWhy {
			if v, ok := s.ImpactTUNWhy[w]; ok {
				why = append(why, v)
			}
		}
		if len(why) == 0 {
			why = append(why, s.ImpactTUNWhy["tun"])
		}
		out = append(out, F(s.ImpactTUN, strings.Join(why, s.ListSep)))
	}
	if im.DNSListener {
		out = append(out, s.ImpactDNS)
	}
	if len(im.Listeners) > 0 {
		out = append(out, F(s.ImpactListeners, strings.Join(im.Listeners, s.ListSep)))
	}
	if len(im.ProviderConnsReset) > 0 {
		out = append(out, F(s.ImpactProviders, strings.Join(im.ProviderConnsReset, s.ListSep)))
	}
	if im.SelectionReset {
		out = append(out, s.ImpactSelection)
	}
	return out
}
