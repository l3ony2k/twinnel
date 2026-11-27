package privfile

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// privateWrite is a file write that asks for owner-only access with a
// Unix mode. On Windows the mode is ignored and the file inherits its
// folder's ACL, which is how the export zip ended up readable by other
// accounts in %TEMP%: such writes go through this package instead.
var privateWrite = regexp.MustCompile(`(os\.WriteFile|OpenFile|os\.CreateTemp)\(.*0o?600\b`)

// allowed are the files that may still write 0600 directly, and why.
var allowed = map[string]string{
	"core/privfile/privfile_unix.go":   "the Unix implementation",
	"core/config/validate.go":          "copyFile fills the helper's geo cache (public GeoIP data, not secrets)",
	"core/kernel/mihomo.go":            "downloads a public release archive",
	"core/kernel/tailscale_install.go": "downloads a public release archive",
	"core/engine/harden.go":            "macOS only: writes inside a root-owned staging dir through os.Root",
	"cmd/cli/helper_migrate.go":        "copies the old state dir into the new one, which the helper's DACL (winsec.SecureStateDir) covers; WriteFile there keeps an existing file's mode",
}

// TestNoAdHocPrivateWrites keeps secret-bearing writes on one path: a new
// os.WriteFile(…, 0o600) anywhere else fails here.
func TestNoAdHocPrivateWrites(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != root && (strings.HasPrefix(n, ".") || n == "testdata" || n == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !privateWrite.MatchString(line) {
				continue
			}
			if _, ok := allowed[rel]; ok {
				seen[rel] = true
				continue
			}
			t.Errorf("%s:%d: write secret-bearing files with core/privfile (WriteFile, Create, MkdirTemp), not a 0600 mode, which Windows ignores:\n\t%s", rel, i+1, strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for f := range allowed {
		if !seen[f] {
			t.Errorf("%s no longer writes 0600 directly: drop it from the allow list", f)
		}
	}
}
