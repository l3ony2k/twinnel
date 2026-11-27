package i18n

import (
	"strings"

	"github.com/l3ony2k/duet/core/msg"
	"github.com/l3ony2k/duet/core/rpc"
)

// Errors said in words (review R-unslop-20). The helper answers in English
// (an RPC error's message, often a Go error chain); a page shows a line in
// the UI language instead: the coded message (MsgTexts), a network error
// (PlainNetError), a kind of failure the RPC code names, or the system's
// reason at the end of the chain. What duet has no words for is the
// generic MsgUnknown line, never the English text itself.

// ErrorStrings are the words for failures that have no message code.
type ErrorStrings struct {
	// Why a system call failed (the end of a Go error chain).
	ErrNoFile, ErrPermission string
	// ErrGeneric is a failure duet has no words for, outside the helper.
	ErrGeneric string
	// ErrWithReason joins a generic line and a reason: %[1]s line, %[2]s reason.
	ErrWithReason string
	// RPC codes without a message code.
	ErrBusy, ErrNotFound string
}

var zhErrors = ErrorStrings{
	ErrGeneric:    "操作没有完成",
	ErrNoFile:     "找不到文件",
	ErrPermission: "没有权限",
	ErrWithReason: "%[1]s：%[2]s",
	ErrBusy:       "另一个操作正在进行，稍后再试",
	ErrNotFound:   "这一项已经不存在，刷新后再试",
}

var enErrors = ErrorStrings{
	ErrGeneric:    "The operation didn't complete",
	ErrNoFile:     "the file wasn't found",
	ErrPermission: "permission denied",
	ErrWithReason: "%[1]s: %[2]s",
	ErrBusy:       "Another operation is running. Try again in a moment.",
	ErrNotFound:   "This item no longer exists. Refresh and try again.",
}

// ErrorText words an error text in the UI language: a coded helper
// message, a network error, or the generic line (with the system's reason
// when duet knows it). Never the English text itself.
func (s *Strings) ErrorText(raw string) string {
	return s.wordError(0, raw, s.ErrGeneric)
}

// RPCErrorText is ErrorText for an RPC error: its code words a busy helper
// or a missing item when the message has no code of its own.
func (s *Strings) RPCErrorText(code int, raw string) string {
	return s.wordError(code, raw, s.MsgUnknown)
}

func (s *Strings) wordError(code int, raw, generic string) string {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, s.ErrorPrefix))
	if raw == "" {
		return generic
	}
	if out, ok := s.Msg(msg.Parse(raw)); ok {
		return out
	}
	switch code {
	case rpc.CodeBusy:
		return s.ErrBusy
	case rpc.CodeNotFound:
		return s.ErrNotFound
	}
	if out := plainNetError(s, raw); out != "" {
		return out
	}
	if why := s.reasonOf(raw); why != "" {
		return F(s.ErrWithReason, generic, why)
	}
	return generic
}

// ControlReason words why a control's change failed (the %s of ModeFailed
// and the like): a service that did not answer is named (svc "mihomo" or
// "tailscale"), anything else is ErrorText.
func (s *Strings) ControlReason(svc, raw string) string {
	r := strings.ToLower(raw)
	if strings.Contains(r, "connection refused") || strings.Contains(r, "no such socket") || strings.Contains(r, "no such file") {
		if svc == "tailscale" {
			return s.UnreachableTSShort
		}
		return s.UnreachableMihomoShort
	}
	if why := s.reasonOf(raw); why != "" {
		return why
	}
	return s.ErrorText(raw)
}

// sysReason is the system's reason at the end of an error chain in the UI
// language, "" when duet does not know it.
func (s *Strings) sysReason(raw string) string {
	r := strings.ToLower(raw)
	switch {
	case strings.HasSuffix(r, "no such file or directory"), strings.HasSuffix(r, "no such socket"):
		return s.ErrNoFile
	case strings.HasSuffix(r, "permission denied"), strings.HasSuffix(r, "operation not permitted"):
		return s.ErrPermission
	case strings.Contains(r, "connection refused"):
		return s.NetRefused
	case strings.Contains(r, "deadline exceeded"), strings.HasSuffix(r, "timeout"), strings.Contains(r, "timed out"):
		return s.NetTimeout
	}
	return ""
}
