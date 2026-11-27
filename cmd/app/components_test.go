package main

import (
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/i18n"
	"github.com/l3ony2k/twinnel/core/mihomo"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestComponentSummary(t *testing.T) {
	day := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	none := &rpc.AccessoryCheck{Files: []rpc.AccessoryFile{
		{Name: "geoip", UpdateAvailable: true, RemoteSize: 10 << 20},
		{Name: "geosite", UpdateAvailable: true, RemoteSize: 5 << 20},
	}, Checked: true}
	s := summarize(none)
	if len(s.installed) != 0 || s.updates != 2 || s.size != 15<<20 {
		t.Fatalf("none: %+v", s)
	}
	if l := downloadLabel(s, true); !strings.HasPrefix(l, i18n.T.CompDownload) || !strings.Contains(l, "MB") {
		t.Errorf("label %q", l)
	}
	newer := &rpc.AccessoryCheck{Files: []rpc.AccessoryFile{
		{Name: "geoip", Installed: true, ModTime: day, Version: "latest", Source: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat",
			UpdateAvailable: true, RemoteTag: "latest", RemoteTime: day.Add(48 * time.Hour), RemoteSize: 1 << 20},
		{Name: "geosite", Installed: true, ModTime: day, Version: "latest", Source: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"},
	}, Checked: true}
	s = summarize(newer)
	if s.from != "2026-11-20" || s.to != "2026-11-22" || s.version != "" || s.updates != 1 || s.foreign {
		t.Errorf("newer: %+v", s)
	}
	if got := sourceName(s.source); got != "MetaCubeX/meta-rules-dat" {
		t.Errorf("source %q", got)
	}
	if l := downloadLabel(s, false); !strings.HasPrefix(l, i18n.T.CompUpdate) {
		t.Errorf("update label %q", l)
	}
	ui := &rpc.AccessoryCheck{Files: []rpc.AccessoryFile{{Name: "ui", Installed: true, Version: "v1.90.0", UpdateAvailable: true, RemoteTag: "v1.91.0"}}}
	if s := summarize(ui); s.from != "v1.90.0" || s.to != "v1.91.0" {
		t.Errorf("ui: %+v", s)
	}
	partial := &rpc.AccessoryCheck{Files: []rpc.AccessoryFile{{Name: "geoip", Installed: true}, {Name: "asn", UpdateAvailable: true}}}
	if s := summarize(partial); downloadLabel(s, false) != i18n.T.CompDownloadMiss || !s.foreign {
		t.Errorf("partial: %+v", s)
	}
	if downloadLabel(summarize(&rpc.AccessoryCheck{Files: []rpc.AccessoryFile{{Name: "geoip", Installed: true}}}), false) != "" {
		t.Error("nothing to download has a label")
	}
}

func TestProgressText(t *testing.T) {
	k := geoKind()
	if got := progressText(k, &rpc.AccessoryProgress{Kind: "geo", Stage: rpc.AccStageDownload, Name: "geosite", Done: 42, Total: 100}); !strings.Contains(got, "42%") || !strings.Contains(got, "geosite") {
		t.Errorf("download %q", got)
	}
	for stage, want := range map[string]string{rpc.AccStageVerify: i18n.T.CompStageVerify, rpc.AccStageInstall: i18n.T.CompStageInstall, rpc.AccStageReload: i18n.T.CompStageReload} {
		if got := progressText(k, &rpc.AccessoryProgress{Kind: "geo", Stage: stage}); got != want {
			t.Errorf("%s: %q", stage, got)
		}
	}
	if progressText(uiKind(), &rpc.AccessoryProgress{Kind: "ui", Stage: rpc.AccStageReload}) != i18n.T.CompStageConfigure {
		t.Error("ui reload")
	}
}

func TestDashboardSetupURL(t *testing.T) {
	got := mihomo.DashboardURL("http://127.0.0.1:19090", "/ui/")
	if got != "http://127.0.0.1:19090/ui/#/setup?hostname=127.0.0.1&port=19090" {
		t.Errorf("url %q", got)
	}
	if strings.Contains(got, "secret") {
		t.Error("secret in the URL")
	}
}

func TestTakeAccessoryProgress(t *testing.T) {
	a := &app{}
	evs := a.takeAccessoryProgress([]rpc.Event{
		{Type: rpc.EventAccessoryProgress, Data: []byte(`{"kind":"geo","stage":"verify"}`)},
		{Type: rpc.EventAccessoryChanged},
	})
	if len(evs) != 1 || a.hd.kern.geo.prog == nil || a.hd.kern.geo.prog.Stage != "verify" || a.hd.kern.ui.prog != nil {
		t.Errorf("evs %v prog %+v", evs, a.hd.kern.geo.prog)
	}
}
