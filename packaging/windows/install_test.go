// Package windows holds the Windows installer script; this test checks
// install.ps1 against core/binswap and, where PowerShell is available
// (pwsh on PATH, or TWINNEL_PWSH), runs its file swap in a temp dir.
package windows

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/binswap"
)

func script(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The script and the Go swap name their files alike, so each cleans what
// the other left.
func TestSameNamesAsBinswap(t *testing.T) {
	s := script(t)
	for _, want := range []string{
		"$OldInfix = '" + binswap.OldInfix + "'",
		"$NewInfix = '" + binswap.NewInfix + "'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("install.ps1 lacks %s", want)
		}
	}
}

// A dry run says it changed nothing: the "done" lines only print after
// the real thing.
func TestDryRunWording(t *testing.T) {
	s := script(t)
	for _, done := range []string{"'Twinnel removed.'", `"Twinnel is installed in`} {
		i := strings.Index(s, done)
		if i < 0 {
			t.Fatalf("no %s", done)
		}
		before := s[:i]
		line := s[strings.LastIndex(before, "\n")+1 : i]
		prev := before[:len(before)-len(line)]
		guarded := strings.Contains(line, "else") || regexp.MustCompile(`if \(\$DryRun\) \{\s*Write-Host 'Dry run: nothing was changed.'\s*exit 0\s*\}\s*$`).MatchString(prev)
		if !guarded {
			t.Errorf("%s is not gated on -DryRun:\n%s", done, before[max(0, len(before)-200):])
		}
	}
}

func pwsh(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("TWINNEL_PWSH"); p != "" {
		return p
	}
	p, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("no pwsh (set TWINNEL_PWSH to run the script's swap)")
	}
	return p
}

// runPS dot-sources install.ps1 (functions only) and runs body.
func runPS(t *testing.T, body string) string {
	t.Helper()
	abs, err := filepath.Abs("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	ps := "$ErrorActionPreference = 'Stop'\n. '" + abs + "' -InstallDir 'C:\\x\\Twinnel'\n" + body
	out, err := exec.Command(pwsh(t), "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return string(out)
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallFilesSwaps(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "a"), "new a")
	write(t, filepath.Join(src, "b"), "new b")
	write(t, filepath.Join(dst, "a"), "old a")
	write(t, filepath.Join(dst, "a.old-123"), "left by an earlier run")
	write(t, filepath.Join(dst, "a.old-x"), "not ours")
	out := runPS(t, `
Remove-Leftovers @('`+dst+`')
$r = Install-Files @(@{ Src = '`+src+`/a'; Dst = '`+dst+`/a' }, @{ Src = '`+src+`/b'; Dst = '`+dst+`/bin/b' })
"in use: $($r.Count)"
`)
	if !strings.Contains(out, "in use: 0") {
		t.Fatalf("output %s", out)
	}
	if read(t, filepath.Join(dst, "a")) != "new a" || read(t, filepath.Join(dst, "bin", "b")) != "new b" {
		t.Fatal("not installed")
	}
	ents, _ := os.ReadDir(dst)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "a a.old-x bin" {
		t.Fatalf("files %v", names)
	}
}

// The second file fails to swap (its destination is a folder): the first
// is put back, nothing staged stays.
func TestInstallFilesAllOrNone(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "a"), "new a")
	write(t, filepath.Join(src, "b"), "new b")
	write(t, filepath.Join(dst, "a"), "old a")
	write(t, filepath.Join(dst, "b", "inside"), "a folder where a file goes")
	out := runPS(t, `
try {
	Install-Files @(@{ Src = '`+src+`/a'; Dst = '`+dst+`/a' }, @{ Src = '`+src+`/b'; Dst = '`+dst+`/b' }) | Out-Null
	'no error'
} catch { "failed: $_" }
`)
	if !strings.Contains(out, "failed: install failed, the previous files are back in place") {
		t.Fatalf("output %s", out)
	}
	if read(t, filepath.Join(dst, "a")) != "old a" {
		t.Fatal("a not put back")
	}
	ents, _ := os.ReadDir(dst)
	if len(ents) != 2 {
		var n []string
		for _, e := range ents {
			n = append(n, e.Name())
		}
		t.Fatalf("leftovers %v", n)
	}
}

// The script finds what binswap leaves and the other way round.
func TestLeftoversShared(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "twinnel.exe"), "x")
	write(t, filepath.Join(dir, "twinnel.exe"+binswap.OldInfix+"20261128093000"), "deploy script stamp")
	write(t, filepath.Join(dir, "twinnel.exe"+binswap.NewInfix+"639000000000000000"), "ticks")
	out := runPS(t, `(Get-Leftovers '`+dir+`' | ForEach-Object { Split-Path -Leaf $_ } | Sort-Object) -join ','`)
	want := "twinnel.exe.new-639000000000000000,twinnel.exe.old-20261128093000"
	if strings.TrimSpace(out) != want {
		t.Fatalf("script: %q", out)
	}
	var got []string
	for _, p := range binswap.Leftovers(filepath.Join(dir, "twinnel.exe")) {
		got = append(got, filepath.Base(p))
	}
	if strings.Join(got, ",") != want {
		t.Fatalf("binswap: %v", got)
	}
}

// The process lookups run under StrictMode with nothing found (no
// property of an empty result is read).
func TestProcessLookupsStrict(t *testing.T) {
	out := runPS(t, `Set-StrictMode -Version 2.0
"$(@(Get-InstallProcesses).Count) $(@(Get-SessionApp).Count)"
Stop-Processes @() ''
Stop-Processes $null ''
'ok'`)
	if !strings.Contains(out, "0 0") || !strings.Contains(out, "ok") {
		t.Fatalf("output %s", out)
	}
}
