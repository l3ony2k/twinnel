package main

import (
	"slices"
	"testing"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/status"
)

// 关于 → 入门's first step opens the install guide, a page without a
// sidebar row whose way back is 关于.
func TestInstallPageRouting(t *testing.T) {
	a := &app{}
	steps, names := a.aboutStepList(status.Snapshot{})
	if len(steps) != 4 || len(steps[0].pages) != 1 || steps[0].pages[0] != pageInstall || names[pageInstall] != i18n.T.StepInstall {
		t.Fatalf("step 1: %+v, label %q", steps[0], names[pageInstall])
	}
	if names[pageInstall] != "安装" {
		t.Errorf("step 1 button %q, want 安装", names[pageInstall])
	}
	// Steps 2–4 still open their pages, labelled with the pages' names.
	want := [][]string{{pageTakeover, pageConfig}, {pageHealth}, {pageProxies, pageTailscale}}
	for i, w := range want {
		st := steps[i+1]
		if len(st.pages) != len(w) {
			t.Fatalf("step %d pages %v", i+2, st.pages)
		}
		for j, p := range w {
			if st.pages[j] != p || names[p] == "" {
				t.Errorf("step %d: %v (%q)", i+2, st.pages, names[p])
			}
		}
	}
	// No sidebar row; 关于 stands for it, and ‹ / ⌘[ go there.
	for _, sec := range navSections(0, 0, toneNeutral) {
		for _, it := range sec.items {
			if it.id == pageInstall {
				t.Error("pageInstall has a sidebar row")
			}
			if navParent(it.id) != it.id || pageBack(it.id) != "" {
				t.Errorf("%s: parent %q back %q", it.id, navParent(it.id), pageBack(it.id))
			}
		}
	}
	defer clear(subFrom)
	if navParent(pageInstall) != pageSettings || pageBack(pageInstall) != pageSettings {
		t.Errorf("install: parent %q back %q", navParent(pageInstall), pageBack(pageInstall))
	}
	noteSubPage(pageInstall, pageOverview) // opened from 开始使用 on 概览
	if navParent(pageInstall) != pageOverview || pageBack(pageInstall) != pageOverview {
		t.Errorf("install from overview: parent %q back %q", navParent(pageInstall), pageBack(pageInstall))
	}
}

// Step 1 shows done once the helper answers.
func TestInstallStepDone(t *testing.T) {
	a := &app{}
	steps, _ := a.aboutStepList(status.Snapshot{Helper: status.Helper{State: status.HelperNotInstalled}})
	if *steps[0].done {
		t.Error("not installed reads as done")
	}
	steps, _ = a.aboutStepList(status.Snapshot{Helper: status.Helper{State: status.HelperConnected}})
	if !*steps[0].done {
		t.Error("connected reads as not done")
	}
}

// A sub-page keeps the row of a page that links to it selected, and
// otherwise shows under its home row: never under whatever page the
// window showed before (Windows QA round 3: 停止使用 opened over 接管 by a
// launch target, or after a visit from 接管, highlighted 接管).
func TestSubPageParent(t *testing.T) {
	defer clear(subFrom)
	for page, sp := range subPages {
		if sp.home == "" || !slices.Contains(sp.from, sp.home) {
			t.Errorf("%s: home %q not among the pages linking to it %v", page, sp.home, sp.from)
		}
	}
	if navParent(pageLeave) != pageSettings {
		t.Fatalf("leave: parent %q, want 设置", navParent(pageLeave))
	}
	noteSubPage(pageLeave, pageTakeover) // 接管 → 撤销接管
	if navParent(pageLeave) != pageTakeover || pageBack(pageLeave) != pageTakeover {
		t.Fatalf("leave from takeover: parent %q", navParent(pageLeave))
	}
	noteSubPage(pageLeave, pageLogs) // e.g. the flyout while the window showed 日志
	if navParent(pageLeave) != pageSettings {
		t.Fatalf("leave from logs: parent %q, want its home", navParent(pageLeave))
	}
	noteSubPage(pageLeave, pageTakeover)
	forgetSubFrom(pageLeave) // a launch target
	if navParent(pageLeave) != pageSettings || pageBack(pageLeave) != pageSettings {
		t.Fatalf("leave as a launch target: parent %q", navParent(pageLeave))
	}
}
