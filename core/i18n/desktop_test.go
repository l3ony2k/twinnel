package i18n

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestLinuxWording checks that the Linux tables name no macOS thing in
// the texts the Linux app shows. The fields listed in macOnly are shown
// on macOS only (the Dock, Login Items approval, Homebrew, the app menu).
func TestLinuxWording(t *testing.T) {
	macTerms := []string{"Finder", "Keychain", "keychain", "钥匙串", "LaunchDaemon", "LaunchAgent", "this Mac", "This Mac", "这台 Mac",
		"Twinnel.app", "废纸篓", "Trash", "⌘", "⌥", "macOS", "管理员密码", "administrator password", "菜单栏", "menu bar"}
	macOnly := map[string]bool{
		"OpenAtLoginApprove": true, "ShowInDock": true, "DockOpen": true, "ViewMenu": true,
		"InvLaunchAgent": true, // only listed when ~/Library/LaunchAgents has one
		"SidebarToggle":  true, // the label, not the shortcut
	}
	for _, l := range Langs() {
		s := *l.S // a copy: the real tables stay macOS's on this host
		s.MsgTexts = l.S.MsgTexts
		fn := linuxWording[l.Tag]
		if fn == nil {
			t.Fatalf("%s: no Linux wording", l.Tag)
		}
		fn(&s)
		checkStrings(t, l.Tag, reflect.ValueOf(s), "", macTerms, macOnly)
		if l.S.MsgTexts["refuses-exposed"] != "" && strings.Contains(s.MsgTexts["refuses-exposed"], "Mac") {
			t.Errorf("%s: refuses-exposed still names the Mac", l.Tag)
		}
	}
	if strings.Contains(en.MsgTexts["refuses-exposed"], "this computer") && runtime.GOOS != "linux" {
		t.Errorf("the Linux wording changed the macOS table")
	}
}

// checkStrings walks the fields the Linux wording rewrites: those that
// linuxWording sets must not name a macOS thing.
func checkStrings(t *testing.T, tag string, v reflect.Value, path string, terms []string, skip map[string]bool) {
	t.Helper()
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		fv := v.Field(i)
		switch fv.Kind() {
		case reflect.Struct:
			checkStrings(t, tag, fv, path+f.Name+".", terms, skip)
		case reflect.String:
			if skip[f.Name] || !linuxShown[f.Name] {
				continue
			}
			for _, term := range terms {
				if strings.Contains(fv.String(), term) {
					t.Errorf("%s %s%s = %q names %q", tag, path, f.Name, fv.String(), term)
				}
			}
		}
	}
}

// linuxShown are the fields the Linux app shows that named a macOS thing
// in the macOS tables (the audit in docs/linux-gui.md).
var linuxShown = map[string]bool{
	"ShowInFinder": true, "InvHelperService": true, "InvSecrets": true, "ItemSecrets": true, "LeaveRemoveAllWhat": true,
	"TkEffStoreSub": true, "LayerReorderHint": true, "NotificationsUnsupportedTip": true, "ServiceInstallTitle": true,
	"OnboardPlanHint": true, "ItemApp": true, "ItemAppTrash": true, "TipLeaveRemoveAll": true, "TipOpenAtLogin": true,
	"TipPrimaryGroup": true, "TipAgentCLI": true, "TipAgent": true, "SelfIP": true, "SelfName": true, "InfoTun": true,
	"InfoExitNode": true, "OvThisDevice": true, "ConfigEmptyHint": true, "LeaveInstalled": true, "MenuBar": true,
	"TraySpeedSwitch": true,
}
