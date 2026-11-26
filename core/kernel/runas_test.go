//go:build unix

package kernel

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// rootFI makes a FileInfo look owned by root:wheel (tests cannot chown).
type rootFI struct{ fs.FileInfo }

func (r rootFI) Sys() any {
	st := *r.FileInfo.Sys().(*syscall.Stat_t)
	st.Uid, st.Gid = 0, 0
	return &st
}

func withTrustHooks(t *testing.T, euid int, rootOwned func(string) bool) {
	oldE, oldL := trustGeteuid, trustLstat
	trustGeteuid = func() int { return euid }
	trustLstat = func(p string) (fs.FileInfo, error) {
		fi, err := os.Lstat(p)
		if err == nil && rootOwned(p) {
			return rootFI{fi}, nil
		}
		return fi, err
	}
	t.Cleanup(func() { trustGeteuid, trustLstat = oldE, oldL })
}

func tempBin(t *testing.T) (dir, bin string) {
	dir = t.TempDir()
	bin = filepath.Join(dir, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, bin
}

func TestCheckExec(t *testing.T) {
	dir, bin := tempBin(t)
	real, _ := filepath.EvalSymlinks(bin)
	me := os.Getuid()

	// Everything root-owned: trusted.
	withTrustHooks(t, 0, func(string) bool { return true })
	if tr, err := CheckExec(bin); err != nil || !tr.Trusted || tr.Path != real {
		t.Fatalf("root-owned: %+v %v", tr, err)
	}

	// The file is the user's: run as the user.
	withTrustHooks(t, 0, func(p string) bool { return p != real })
	tr, err := CheckExec(bin)
	if err != nil || tr.Trusted || tr.RunAs.UID != me {
		t.Fatalf("user-owned file: %+v %v", tr, err)
	}

	// Root-owned file in a user-owned dir: the user can swap it.
	realDir := filepath.Dir(real)
	withTrustHooks(t, 0, func(p string) bool { return p != realDir })
	if tr, _ := CheckExec(bin); tr.Trusted || tr.RunAs.UID != me {
		t.Fatalf("user-owned dir: %+v", tr)
	}

	// A world-writable dir: anyone can swap it; run as nobody.
	ww := filepath.Join(dir, "ww")
	os.Mkdir(ww, 0o777)
	os.Chmod(ww, 0o777)
	b2 := filepath.Join(ww, "tool")
	os.WriteFile(b2, []byte("x"), 0o755)
	withTrustHooks(t, 0, func(string) bool { return true })
	if tr, _ := CheckExec(b2); tr.Trusted || tr.RunAs != Nobody {
		t.Fatalf("world-writable dir: %+v", tr)
	}

	// A symlink is resolved: the link's own dir does not matter, the
	// target is what runs.
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(bin, link)
	withTrustHooks(t, 0, func(string) bool { return true })
	if tr, _ := CheckExec(link); tr.Path != real {
		t.Fatalf("link not resolved: %+v", tr)
	}
}

func TestCommandDropsPrivileges(t *testing.T) {
	_, bin := tempBin(t)
	real, _ := filepath.EvalSymlinks(bin)
	me := os.Getuid()
	withTrustHooks(t, 0, func(p string) bool { return p != real })
	cmd, err := Command(context.Background(), bin, "-v")
	if err != nil {
		t.Fatal(err)
	}
	c := cmd.SysProcAttr.Credential
	if c == nil || int(c.Uid) != me || len(c.Groups) != 0 || cmd.Path != real {
		t.Fatalf("not dropped: %+v %s", c, cmd.Path)
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "HOME=/var/root") {
			t.Errorf("root's HOME leaked: %v", cmd.Env)
		}
	}

	// Trusted: plain command.
	withTrustHooks(t, 0, func(string) bool { return true })
	cmd, _ = Command(context.Background(), bin)
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Fatal("trusted binary dropped")
	}
	// Not root: plain command, path as given.
	withTrustHooks(t, me, func(string) bool { return false })
	cmd, _ = Command(context.Background(), bin)
	if cmd.SysProcAttr != nil {
		t.Fatal("non-root helper changed the command")
	}
	if err := RequireTrusted(bin); err == nil {
		t.Fatal("RequireTrusted accepted a user-owned binary")
	}
}

func zipOf(t *testing.T, add func(*zip.Writer)) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add(zw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func extractTest(t *testing.T, data []byte) (string, error) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(dir, "b.zip"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, extractZip(root, "b.zip", "out")
}

func TestExtractZipRefusesEscapes(t *testing.T) {
	for name, entry := range map[string]string{
		"dotdot":   "../evil.txt",
		"abs":      "/tmp/evil.txt",
		"nested":   "a/../../evil.txt",
		"backslsh": `..\evil.txt`,
	} {
		data := zipOf(t, func(zw *zip.Writer) {
			w, _ := zw.Create("index.html")
			w.Write([]byte("x"))
			w, _ = zw.Create(entry)
			w.Write([]byte("evil"))
		})
		dir, err := extractTest(t, data)
		if err == nil {
			t.Errorf("%s: extracted", name)
		}
		if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
			t.Errorf("%s: wrote outside", name)
		}
	}
	// Symlink entries are refused.
	data := zipOf(t, func(zw *zip.Writer) {
		w, _ := zw.Create("index.html")
		w.Write([]byte("x"))
		h := &zip.FileHeader{Name: "link", Modified: time.Now()}
		h.SetMode(fs.ModeSymlink | 0o777)
		w, _ = zw.CreateHeader(h)
		w.Write([]byte("/etc/passwd"))
	})
	if _, err := extractTest(t, data); err == nil {
		t.Error("symlink entry extracted")
	}
}

// A bundle with too many entries is refused before anything is written.
func TestExtractZipSizeLimit(t *testing.T) {
	data := zipOf(t, func(zw *zip.Writer) {
		w, _ := zw.Create("index.html")
		w.Write([]byte("x"))
		for i := 0; i < maxUIFiles+1; i++ {
			zw.Create("f" + strconv.Itoa(i))
		}
	})
	if _, err := extractTest(t, data); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("too many files: %v", err)
	}
}

func TestCheckURL(t *testing.T) {
	for _, u := range []string{"https://github.com/a/b", "https://example.com/x.dat?t=1"} {
		if _, err := checkURL(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"http://example.com/x", "http://127.0.0.1:9090/configs", "https://127.0.0.1/x",
		"https://localhost/x", "https://[::1]/x", "https://192.168.1.1/x", "https://169.254.169.254/x", "file:///etc/passwd", "ftp://x/y"} {
		if _, err := checkURL(u); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
}
