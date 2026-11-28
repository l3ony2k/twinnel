package linux

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/l3ony2k/twinnel/core/status"
)

// /proc gives mihomo's memory, uptime and CPU (testdata/proc: synthetic):
// mihomo started 3600 s after boot, 7200.5 s up, 36 s of CPU.
func TestProcProcesses(t *testing.T) {
	list, err := Processes(context.Background(), "testdata/proc")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(list, func(e status.ProcessEntry) bool { return e.PID == 4242 })
	if i < 0 {
		t.Fatalf("no mihomo in %+v", list)
	}
	m := list[i]
	page := int64(os.Getpagesize())
	if m.Command != "mihomo" || m.RSS != 12800*page {
		t.Errorf("mihomo = %+v", m)
	}
	if want := 3600500 * time.Millisecond; m.Elapsed != want {
		t.Errorf("elapsed = %v, want %v", m.Elapsed, want)
	}
	if m.CPU < 0.99 || m.CPU > 1.01 {
		t.Errorf("CPU = %v, want ~1", m.CPU)
	}
	if !status.IsMihomoCommand(m.Command) {
		t.Error("not recognized as mihomo")
	}
	j := slices.IndexFunc(list, func(e status.ProcessEntry) bool { return e.PID == 77 })
	if j < 0 || list[j].Command != "tail scaled (x)" || list[j].RSS != 256*page {
		t.Errorf("name with spaces and parentheses: %+v", list)
	}
}
