package i18n

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
)

// The errors of the review shots, in both languages: worded, never the
// helper's English (R-unslop-20, R-kubai-4).
func TestErrorText(t *testing.T) {
	for _, tt := range []struct {
		code   int
		raw    string
		zh, en string
	}{
		{rpc.CodeFailed, "subscription main has no content yet (refresh it first)", "订阅「main」还没有内容，先更新它", "Subscription “main” has no content yet; update it first"},
		{rpc.CodeFailed, "no active subscription", "没有在用的订阅", "No subscription is in use"},
		{rpc.CodeFailed, "takeover plan: live config: open /Users/someone/.config/mihomo: no such file or directory", "没找到 mihomo 的配置文件（~/.config/mihomo）", "mihomo's config file wasn't found (~/.config/mihomo)"},
		{rpc.CodeBusy, "an adopt is running", "另一个操作正在进行，稍后再试", "Another operation is running. Try again in a moment."},
		{rpc.CodeFailed, "the live config is now /a, the plan was made for /b; re-plan", "运行中的配置在生成计划后变了，重新生成计划", "The running config changed since the plan was made; make a new plan"},
		{rpc.CodeFailed, "backup: open /x: permission denied", "没能备份当前配置：没有权限", "Couldn't back up the current config: permission denied"},
		{rpc.CodeFailed, "backup: something odd", "没能备份当前配置", "Couldn't back up the current config"},
		{rpc.CodeFailed, "reload: Put \"http://127.0.0.1:9090/configs\": dial tcp 127.0.0.1:9090: connect: connection refused", "mihomo 重载失败，已恢复旧配置：对方拒绝了连接", "mihomo failed to reload; the old config was restored: the connection was refused"},
		{rpc.CodeFailed, "write /etc/mihomo/config.yaml: read-only file system", "没能写入 /etc/mihomo/config.yaml", "Couldn't write /etc/mihomo/config.yaml"},
		{rpc.CodeFailed, "something nobody knows", "后台助手返回了错误", "The helper returned an error"},
		{0, "open /x: no such file or directory", "后台助手返回了错误：找不到文件", "The helper returned an error: the file wasn't found"},
	} {
		if got := zh.RPCErrorText(tt.code, tt.raw); got != tt.zh {
			t.Errorf("zh %q = %q, want %q", tt.raw, got, tt.zh)
		}
		if got := en.RPCErrorText(tt.code, tt.raw); got != tt.en {
			t.Errorf("en %q = %q, want %q", tt.raw, got, tt.en)
		}
	}
}

func TestControlReason(t *testing.T) {
	if got := zh.ControlReason("mihomo", "connection refused"); got != "连不上 mihomo 控制接口" {
		t.Error(got)
	}
	if got := zh.ControlReason("tailscale", "no such socket"); got != "连不上 tailscaled" {
		t.Error(got)
	}
	if got := zh.ControlReason("mihomo", "timeout"); got != zh.NetTimeout {
		t.Error(got)
	}
	if got := zh.ControlReason("mihomo", "unexpected EOF"); got != zh.ErrGeneric {
		t.Error(got)
	}
}

// A message carrying an ID names it (R-unslop-21).
func TestMsgNamesSubjects(t *testing.T) {
	defer func(n func(string, string) string) { Namer = n }(Namer)
	Namer = func(kind, id string) string {
		if kind == "subscription" && id == "main" {
			return "Example Cloud"
		}
		return id
	}
	if got := zh.RPCErrorText(rpc.CodeFailed, "subscription main has no content yet (refresh it first)"); got != "订阅「Example Cloud」还没有内容，先更新它" {
		t.Error(got)
	}
	if got := zh.HistoryReasonText("subscription-updated", []string{"main"}, ""); !strings.Contains(got, "Example Cloud") {
		t.Error(got)
	}
}

func TestHomePath(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/someone/.config/mihomo": "~/.config/mihomo",
		"/home/x/a":                     "~/a",
		"/Users/someone":                "~",
		"/etc/mihomo":                   "/etc/mihomo",
	} {
		if got := HomePath(in); got != want {
			t.Errorf("HomePath(%q) = %q", in, got)
		}
	}
}

// No user text carries a Go error, an RPC code or a home path: those come
// from the system and are worded by ErrorText.
var rawError = regexp.MustCompile(`(?i)no such file|connection refused|dial tcp|rpc error|deadline exceeded|permission denied:|/Users/|%v`)

func TestNoRawErrorsInTables(t *testing.T) {
	for tag, tab := range map[string]*Strings{"zh": &zh, "en": &en} {
		walkTable(reflect.ValueOf(*tab), tag+".", func(path, s string) {
			if m := rawError.FindString(s); m != "" {
				t.Errorf("%s: %q has %q", path, s, m)
			}
		})
	}
}
