package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/l3ony2k/twinnel/core/i18n"
)

// 6.11: the logs' search is a search field on Windows (the magnifier,
// a search box for assistive technology), and the find keys put the focus in it from anywhere
// on the page: Ctrl+F (⌘F on macOS), and F3 on Windows.
func TestLogSearchFindKeys(t *testing.T) {
	defer i18n.Set("zh")
	i18n.Set("zh")
	for _, goos := range []string{"windows", "darwin", "linux"} {
		usePlat(t, goos)
		resetFlags()
		a, _ := layoutApp(t)
		a.hc = nil
		old := a.page
		a.page = pageLogs
		a.pageChanged(old)
		tr := ui.NewTester(func(c *ui.Context) { a.mainView(c) }, 1280, 800)
		tr.SetTitleBar(titleBarFor(goos))
		tr.Frame()
		tr.Frame()
		info, ok := tr.Access(i18n.T.LogSearch)
		if !ok {
			t.Fatalf("%s: no search field", goos)
		}
		// A search field (WinUI's, with the magnifier) on Windows.
		if info.Search != (goos == "windows") {
			t.Errorf("%s: search field %v", goos, info.Search)
		}
		keys := findKeys()
		for _, k := range keys {
			tr.SetFocused(true)
			if err := tr.Click(i18n.T.PageLogs); err != nil { // the focus away from the field
				t.Fatal(err)
			}
			tr.Frame()
			if tr.Focused(i18n.T.LogSearch) {
				t.Fatalf("%s: search focused before the key", goos)
			}
			tr.Key(k.mods, k.key)
			tr.Frame()
			tr.Frame()
			if !tr.Focused(i18n.T.LogSearch) {
				t.Errorf("%s: %v+%v did not focus the search", goos, k.mods, k.key)
				continue
			}
		}
	}
}
