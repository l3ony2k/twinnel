package engine

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/service"
	"github.com/l3ony2k/twinnel/core/store"
)

// fakeManager plans like launchd does, in miniature.
type fakeManager struct{ specs []service.Spec }

func (m *fakeManager) Render(s service.Spec) ([]byte, error) {
	return []byte(s.Label + " " + s.Program + " " + strings.Join(s.Args, " ")), nil
}
func (m *fakeManager) DefinitionPath(l string) string { return "/defs/" + l + ".plist" }
func (m *fakeManager) Plan(op service.Op, s service.Spec) ([]service.Step, error) {
	m.specs = append(m.specs, s)
	switch op {
	case service.OpInstall:
		b, _ := m.Render(s)
		return []service.Step{{Kind: service.StepWrite, Path: m.DefinitionPath(s.Label), Content: b, Mode: 0o644},
			{Kind: service.StepRun, Argv: []string{"launchctl", "bootstrap", "system", m.DefinitionPath(s.Label)}}}, nil
	case service.OpUninstall:
		return []service.Step{{Kind: service.StepRun, Argv: []string{"launchctl", "bootout", "system/" + s.Label}, IgnoreError: true},
			{Kind: service.StepRemove, Path: m.DefinitionPath(s.Label)}}, nil
	}
	return []service.Step{{Kind: service.StepRun, Argv: []string{"launchctl", string(op), "system/" + s.Label}}}, nil
}
func (m *fakeManager) Status(_ context.Context, l string) (service.Status, error) {
	return service.Status{Label: l, Loaded: true}, nil
}

type recExec struct{ runs [][]string }

func (r *recExec) Run(_ context.Context, argv []string) ([]byte, error) {
	r.runs = append(r.runs, argv)
	return nil, nil
}

type recFiles struct{ ops []string }

func (f *recFiles) MkdirAll(p string, _ fs.FileMode) error {
	f.ops = append(f.ops, "mkdir "+p)
	return nil
}
func (f *recFiles) WriteFile(p string, b []byte, _ fs.FileMode) error {
	f.ops = append(f.ops, "write "+p+": "+string(b))
	return nil
}
func (f *recFiles) Copy(s, d string, _ fs.FileMode) error {
	f.ops = append(f.ops, "copy "+d)
	return nil
}
func (f *recFiles) Remove(p string) error { f.ops = append(f.ops, "rm "+p); return nil }

func TestServiceOps(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := &fakeManager{}
	h.e.Services = m

	// No runner: dry run.
	r, err := h.e.ServiceOp(ctx, "mihomo", service.OpRestart)
	if err != nil || !r.DryRun || len(r.Steps) != 1 || r.Steps[0] != "launchctl restart system/com.l3ony2k.twinnel.mihomo" {
		t.Fatalf("dry run: %+v %v", r, err)
	}

	ex, files := &recExec{}, &recFiles{}
	h.e.ServiceRunner = &ServiceRunner{Exec: ex, Files: files}
	r, err = h.e.ServiceOp(ctx, "mihomo", service.OpInstall)
	if err != nil || r.DryRun {
		t.Fatalf("install: %+v %v", r, err)
	}
	st, _ := h.e.Store.Settings()
	want := "write /defs/com.l3ony2k.twinnel.mihomo.plist: com.l3ony2k.twinnel.mihomo " + st.Mihomo.Binary + " -d " + h.confDir + " -f " + h.cfgPath
	if len(files.ops) != 1 || files.ops[0] != want {
		t.Errorf("files %q\nwant %q", files.ops, want)
	}
	if len(ex.runs) != 1 || ex.runs[0][1] != "bootstrap" {
		t.Errorf("runs %v", ex.runs)
	}
	if _, err := h.e.ServiceOp(ctx, "tailscaled", service.OpStop); err != nil {
		t.Error(err)
	}
	if got := ex.runs[len(ex.runs)-1]; got[2] != "system/com.tailscale.tailscaled" {
		t.Errorf("tailscaled label %v", got)
	}

	// Not allowed.
	for _, c := range []struct {
		name string
		op   service.Op
	}{{"tailscaled", service.OpInstall}, {"tailscaled", service.OpUninstall}, {"helper", service.OpRestart}, {"sshd", service.OpStart}} {
		if _, err := h.e.ServiceOp(ctx, c.name, c.op); err == nil {
			t.Errorf("%s %s allowed", c.op, c.name)
		} else if re, ok := err.(*rpc.Error); !ok || re.Code != rpc.CodeInvalidParams {
			t.Errorf("%s %s: %v", c.op, c.name, err)
		}
	}
}

func TestServiceInstallManagedKernel(t *testing.T) {
	h := newHarness(t)
	m := &fakeManager{}
	h.e.Services = m
	h.e.Store.UpdateSettings(func(st *store.Settings) error { st.Mihomo.Binary = ""; return nil })
	if _, err := h.e.ServiceOp(context.Background(), "mihomo", service.OpInstall); err == nil {
		t.Fatal("install without a kernel")
	}
	bin := h.e.Kernels.Binary()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("x"), 0o755)
	r, err := h.e.ServiceOp(context.Background(), "mihomo", service.OpInstall)
	if err != nil || m.specs[len(m.specs)-1].Program != bin {
		t.Fatalf("%+v %v", r, err)
	}
}
