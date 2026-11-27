package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestServiceLogTail(t *testing.T) {
	e := freshEngine(t)
	dir := e.Store.Path("logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var old, cur strings.Builder
	for i := range 5 {
		fmt.Fprintf(&old, "old %d\n", i)
	}
	cur.WriteString(`time="2026-01-02T03:04:05Z" level=fatal msg="Parse config error: proxy-providers: Get \"https://sub.example.invalid/x?token=synthetic-secret\""` + "\n")
	cur.WriteString("new 1\r\n")
	os.WriteFile(filepath.Join(dir, "mihomo.out.log.1"), []byte(old.String()), 0o644)
	os.WriteFile(filepath.Join(dir, "mihomo.out.log"), []byte(cur.String()), 0o644)
	os.WriteFile(filepath.Join(dir, "mihomo.err.log"), []byte("panic: boom\n"), 0o644)

	got, err := e.ServiceLog(rpc.ServiceLogParams{Name: "mihomo", Lines: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 2 || got.Files[0].File != "mihomo.out.log" || got.Files[1].File != "mihomo.err.log" {
		t.Fatalf("files %+v", got.Files)
	}
	lines := got.Files[0].Lines
	if len(lines) != 4 || lines[0] != "old 3" || lines[3] != "new 1" {
		t.Fatalf("lines %q", lines)
	}
	if strings.Contains(strings.Join(lines, "\n"), "synthetic-secret") {
		t.Fatalf("token not redacted: %q", lines[2])
	}
	if _, err := e.ServiceLog(rpc.ServiceLogParams{Name: "tailscaled"}); err == nil {
		t.Fatal("tailscaled has no output file")
	}
	// Since: the lines from then on; a line without a time goes with the
	// one before it.
	got, err = e.ServiceLog(rpc.ServiceLogParams{Name: "mihomo", Lines: 10, Since: time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)})
	if err != nil || len(got.Files) == 0 {
		t.Fatal(got, err)
	}
	if l := got.Files[0].Lines; len(l) != 2 || !strings.Contains(l[0], "Parse config error") || l[1] != "new 1" {
		t.Fatalf("since: %q", l)
	}
	got, _ = e.ServiceLog(rpc.ServiceLogParams{Name: "mihomo", Lines: 10, Since: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)})
	if l := got.Files[0].Lines; len(l) != 0 {
		t.Fatalf("since after every line: %q", l)
	}
}

// service.log {helper}: the helper's own log, redacted, so the app and the
// CLI can show it where the file is admin-only.
func TestServiceLogHelper(t *testing.T) {
	e := freshEngine(t)
	p := HelperLogFile(e.Store)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p+".1", []byte("time=2026-01-02T03:00:00.000Z level=INFO msg=\"helper started\" version=1\n"), 0o644)
	os.WriteFile(p, []byte("time=2026-01-02T03:04:05.000Z level=WARN msg=\"subscription fetch\" err=\"Get https://u:pw@sub.example.invalid/x?token=synthetic-secret: EOF\"\n"), 0o644)
	got, err := e.ServiceLog(rpc.ServiceLogParams{Name: rpc.ServiceLogHelper})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].File != HelperLogName || len(got.Files[0].Lines) != 2 {
		t.Fatalf("%+v", got)
	}
	all := strings.Join(got.Files[0].Lines, "\n")
	if strings.Contains(all, "synthetic-secret") || strings.Contains(all, "u:pw") {
		t.Fatalf("not redacted: %q", all)
	}
	got, _ = e.ServiceLog(rpc.ServiceLogParams{Name: rpc.ServiceLogHelper, Since: time.Date(2026, 1, 2, 3, 1, 0, 0, time.UTC)})
	if l := got.Files[0].Lines; len(l) != 1 || !strings.Contains(l[0], "subscription fetch") {
		t.Fatalf("since: %q", l)
	}
}

// The helper rotates its own log on every platform, mihomo's output only
// when RotateServiceLogs is on.
func TestRotateHelperLog(t *testing.T) {
	e := freshEngine(t)
	e.RotateServiceLogs = false
	dir := e.Store.Path("logs")
	os.MkdirAll(dir, 0o755)
	big := strings.Repeat("x", maxServiceLog+1)
	os.WriteFile(HelperLogFile(e.Store), []byte(big), 0o644)
	os.WriteFile(filepath.Join(dir, "mihomo.out.log"), []byte(big), 0o644)
	e.rotateServiceLogs()
	if fi, err := os.Stat(HelperLogFile(e.Store)); err != nil || fi.Size() != 0 {
		t.Fatalf("helper.log not rotated: %v %v", fi, err)
	}
	if _, err := os.Stat(HelperLogFile(e.Store) + ".1"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "mihomo.out.log")); fi.Size() == 0 {
		t.Fatal("mihomo's output rotated with RotateServiceLogs off")
	}
}

func TestServiceLogEmpty(t *testing.T) {
	e := freshEngine(t)
	got, err := e.ServiceLog(rpc.ServiceLogParams{Name: "mihomo"})
	if err != nil || len(got.Files) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestCopyTruncate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mihomo.out.log")
	os.WriteFile(p, []byte("0123456789"), 0o644)
	if err := copyTruncate(p, 100); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "0123456789" {
		t.Fatal("rotated below the limit")
	}
	// A writer in append mode, as launchd/systemd hold it, keeps writing.
	w, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := copyTruncate(p, 5); err != nil {
		t.Fatal(err)
	}
	w.WriteString("after")
	if b, _ := os.ReadFile(p + ".1"); string(b) != "0123456789" {
		t.Fatalf(".1 = %q", b)
	}
	if b, _ := os.ReadFile(p); string(b) != "after" {
		t.Fatalf("current = %q", b)
	}
	if err := copyTruncate(filepath.Join(t.TempDir(), "missing"), 5); err != nil {
		t.Fatal(err)
	}
}

// Rotation and reads stay inside the state dir.
func TestServiceLogFilesInStateDir(t *testing.T) {
	e := freshEngine(t)
	st, _ := e.Store.Settings()
	st.Mihomo.LogDir = t.TempDir() // outside
	if err := e.Store.SaveSettings(st); err != nil {
		t.Skip("cannot save settings here:", err)
	}
	fs, err := e.mihomoLogFiles()
	if err != nil || len(fs) != 0 {
		t.Fatalf("%v %v", fs, err)
	}
}
