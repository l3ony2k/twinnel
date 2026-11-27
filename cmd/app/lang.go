package main

import (
	"context"
	"os"
	"runtime"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans" // MyGo's own texts (menu roles, dialogs) in Chinese
	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// The UI language: 设置 → 语言 picks 跟随系统 (status.Config.Language "")
// or one of i18n.Langs(); following the system means the user's preferred
// languages (zh-* → Chinese, anything else → English). TWINNEL_LANG forces a
// language for one run without saving it (screenshots).
const envLang = "TWINNEL_LANG"

// forcedLang returns TWINNEL_LANG's language, or "".
func forcedLang() string { return i18n.Match(os.Getenv(envLang)) }

// initLang tells MyGo the languages Twinnel is written in, before Run: its
// own controls, menus and dialogs then follow the same language. Under
// TWINNEL_LANG it shows that one for this run only (MYGO_LOCALE saves
// nothing).
func initLang() {
	tags := []string{i18n.Fallback}
	for _, l := range i18n.Langs() {
		if l.Tag != i18n.Fallback {
			tags = append(tags, l.Tag)
		}
	}
	mygo.App.SetLanguages(tags...)
	if f := forcedLang(); f != "" {
		os.Setenv("MYGO_LOCALE", f)
		i18n.Set(f)
		setLayout(f)
	}
}

// langLayout is what a language's longer words need of the layout:
// widths are set per language, never by shortening the text.
type langLayout struct {
	label    float32 // kv label column (labelWidth)
	halfCard float32 // narrowest a half-width card may be before cards stack
	logLevel float32 // 日志's level column: the widest level name on one line
}

var langLayouts = map[string]langLayout{
	"zh-Hans": {label: 96, logLevel: 32},
	"en":      {label: 116, halfCard: 360, logLevel: 56}, // "This Mac's name"; "Warning"
}

// halfCardMin is langLayout.halfCard of the current language. (The
// sidebar's width is measured from its labels, sidebarNeed.)
var halfCardMin float32

// levelColW is langLayout.logLevel of the current language.
var levelColW float32 = 32

// setLayout sizes the layout for tag. Main thread.
func setLayout(tag string) {
	l, ok := langLayouts[tag]
	if !ok {
		l = langLayouts["en"]
	}
	labelWidth, halfCardMin, levelColW = l.label, l.halfCard, l.logLevel
	labelGrown = 0 // the new language's labels measure afresh
}

// uiLang returns the language to show: TWINNEL_LANG, else the setting, else
// the system's. Main thread (PreferredLanguages).
func (a *app) uiLang() string {
	if f := forcedLang(); f != "" {
		return f
	}
	if m := i18n.Match(a.cfg.Language); m != "" {
		return m
	}
	return i18n.Resolve(mygo.App.PreferredLanguages())
}

// applyLang shows uiLang everywhere when it changed: the window and the
// flyout draw anew, the menus and the tray tooltip are rebuilt, the status
// reasons are worded again, and the health report is asked for in the new
// language. Main thread.
func (a *app) applyLang() {
	tag := a.uiLang()
	if tag == i18n.Tag() && a.langShown {
		return
	}
	first := !a.langShown
	a.langShown = true
	i18n.Set(tag)
	setLayout(tag)
	if first {
		return // ready builds the menus and the tray next
	}
	if runtime.GOOS == "darwin" {
		mygo.App.SetMenu(a.appMenu())
	}
	a.setDockMenu()
	if a.tray != nil {
		if plat.panelWindow {
			a.tray.SetMenu(a.trayMenu())
		}
		if a.mon != nil {
			a.tray.SetToolTip(i18n.T.AppName + " · " + a.mon.Snapshot().Level.Text())
		}
	}
	if mon := a.mon; mon != nil {
		go func() {
			mon.Refresh()
			mon.RunHealth(context.Background())
		}()
	}
	a.invalidate()
}

// setLanguage saves the 语言 setting ("" follows the system) and shows it.
// Main thread.
func (a *app) setLanguage(tag string) {
	cfg := a.cfg
	cfg.Language = tag
	if err := status.SaveConfig(a.cfgPath, cfg); err != nil {
		a.settings.msg, a.settings.failed = i18n.T.SaveFailed+err.Error(), true
		return
	}
	a.cfg = cfg
	if forcedLang() == "" {
		mygo.App.SetLocale(tag) // MyGo's own texts; "" follows the user again
	}
	a.applyLang()
}

// trayMenu is the tray's menu (right click; on Linux the icon's menu,
// which opens on a left click too where the panel does not activate it).
func (a *app) trayMenu() *mygo.Menu {
	var items []*mygo.MenuItem
	if plat.panelWindow {
		items = append(items, &mygo.MenuItem{Label: i18n.T.TrayShowPanel, Click: func(*mygo.MenuItem, *mygo.Window) { a.togglePanel() }})
	}
	return mygo.NewMenu(append(items, []*mygo.MenuItem{
		{Label: i18n.T.OpenMainWindow, Click: func(*mygo.MenuItem, *mygo.Window) { a.openMain() }},
		mygo.Separator(),
		{Label: i18n.T.Quit, Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	}...))
}

// langOptions are the 语言 choices: 跟随系统 in the UI language, then each
// language in its own (简体中文, English), with the setting value of each.
func langOptions() (labels, values []string) {
	labels, values = []string{i18n.T.LanguageSystem}, []string{""}
	for _, l := range i18n.Langs() {
		labels = append(labels, l.Name)
		values = append(values, l.Tag)
	}
	return labels, values
}

// languageRow is 设置 → 通用 → 界面语言: a pop-up, the macOS convention
// for a language (kubai R-26 left as is on purpose).
func (a *app) languageRow(c *ui.Context) {
	T := i18n.T
	labels, values := langOptions()
	sel := labels[0]
	if m := i18n.Match(a.cfg.Language); m != "" {
		for i, v := range values {
			if v == m {
				sel = labels[i]
			}
		}
	}
	settingsRow(c, T.LanguageLabel, "", nil, func() {
		if ui.Select(c.Key("language"), &sel, labels).Label(T.Language).Changed() {
			for i, l := range labels {
				if l == sel {
					a.setLanguage(values[i])
				}
			}
		}
	})
}
