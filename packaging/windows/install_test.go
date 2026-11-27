// Package windows holds the Windows installer script. These tests check
// install.ps1 against core/binswap and the PowerShell rules it keeps, and,
// under every PowerShell available (pwsh on PATH or TWINNEL_PWSH; on
// Windows also powershell.exe), run its functions in a temp dir.
// install_windows_test.go runs the whole script end to end.
package windows

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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

// shells are the PowerShells the script must run under: PowerShell 7
// (TWINNEL_PWSH, else pwsh on PATH) and, on Windows, Windows PowerShell 5.1
// (powershell.exe), the one install.ps1's own instructions and the app's
// 完全移除 start.
func shells() []string {
	var out []string
	if p := os.Getenv("TWINNEL_PWSH"); p != "" {
		out = append(out, p)
	} else if p, err := exec.LookPath("pwsh"); err == nil {
		out = append(out, p)
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("powershell.exe"); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// eachShell runs f once per shell, as a subtest named after it.
func eachShell(t *testing.T, f func(t *testing.T, sh string)) {
	t.Helper()
	sh := shells()
	if len(sh) == 0 {
		t.Skip("no PowerShell (set TWINNEL_PWSH to run the script's functions)")
	}
	for _, s := range sh {
		t.Run(strings.TrimSuffix(filepath.Base(s), ".exe"), func(t *testing.T) { f(t, s) })
	}
}

// runPS dot-sources install.ps1 (functions only) under shell sh and runs
// body.
func runPS(t *testing.T, sh, body string) string {
	t.Helper()
	abs, err := filepath.Abs("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	ps := "$ErrorActionPreference = 'Stop'\n. '" + abs + "' -InstallDir 'C:\\x\\Twinnel'\n" + body
	out, err := exec.Command(sh, "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput()
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
	eachShell(t, func(t *testing.T, sh string) {
		src, dst := t.TempDir(), t.TempDir()
		write(t, filepath.Join(src, "a"), "new a")
		write(t, filepath.Join(src, "b"), "new b")
		write(t, filepath.Join(dst, "a"), "old a")
		write(t, filepath.Join(dst, "a.old-123"), "left by an earlier run")
		write(t, filepath.Join(dst, "a.old-x"), "not ours")
		out := runPS(t, sh, `
Remove-Leftovers @('`+dst+`')
$r = @(Install-Files @(@{ Src = '`+src+`/a'; Dst = '`+dst+`/a' }, @{ Src = '`+src+`/b'; Dst = '`+dst+`/bin/b' }))
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
	})
}

// The second file fails to swap (its destination is a folder): the first
// is put back, nothing staged stays.
func TestInstallFilesAllOrNone(t *testing.T) {
	eachShell(t, func(t *testing.T, sh string) {
		src, dst := t.TempDir(), t.TempDir()
		write(t, filepath.Join(src, "a"), "new a")
		write(t, filepath.Join(src, "b"), "new b")
		write(t, filepath.Join(dst, "a"), "old a")
		write(t, filepath.Join(dst, "b", "inside"), "a folder where a file goes")
		out := runPS(t, sh, `
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
	})
}

// The main path's file list (round-2 QA A0: @() around a New-Object list
// threw "Argument types do not match" on every install): read back, joined
// with the marker, and handed to Install-Files, under StrictMode as the
// script runs.
func TestInstallFileList(t *testing.T) {
	eachShell(t, func(t *testing.T, sh string) {
		src := t.TempDir()
		for _, f := range []string{"Twinnel.exe", "bin/twinnel.exe", "bin/twinnel-helper.exe", "README.md", "unrelated.txt"} {
			write(t, filepath.Join(src, f), f)
		}
		out := runPS(t, sh, `
$files = @(Get-InstallFiles '`+src+`')
$all = $files + @(@{ Src = 'marker'; Dst = 'marker' })
"files $($files.Count) all $($all.Count)"
'src ' + (($all | ForEach-Object { Split-Path -Leaf $_.Src }) -join ',')
`)
		for _, want := range []string{"files 4 all 5", "src Twinnel.exe,twinnel-helper.exe,twinnel.exe,README.md,marker"} {
			if !strings.Contains(out, want) {
				t.Errorf("want %q in:\n%s", want, out)
			}
		}
	})
}

// The script finds what binswap leaves and the other way round.
func TestLeftoversShared(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "twinnel.exe"), "x")
	write(t, filepath.Join(dir, "twinnel.exe"+binswap.OldInfix+"20261128093000"), "deploy script stamp")
	write(t, filepath.Join(dir, "twinnel.exe"+binswap.NewInfix+"639000000000000000"), "ticks")
	want := "twinnel.exe.new-639000000000000000,twinnel.exe.old-20261128093000"
	var got []string
	for _, p := range binswap.Leftovers(filepath.Join(dir, "twinnel.exe")) {
		got = append(got, filepath.Base(p))
	}
	if strings.Join(got, ",") != want {
		t.Fatalf("binswap: %v", got)
	}
	eachShell(t, func(t *testing.T, sh string) {
		out := runPS(t, sh, `(Get-Leftovers '`+dir+`' | ForEach-Object { Split-Path -Leaf $_ } | Sort-Object) -join ','`)
		if strings.TrimSpace(out) != want {
			t.Fatalf("script: %q", out)
		}
	})
}

// The process lookups run under StrictMode with nothing found (no
// property of an empty result is read).
func TestProcessLookupsStrict(t *testing.T) {
	eachShell(t, func(t *testing.T, sh string) {
		out := runPS(t, sh, `Set-StrictMode -Version 2.0
"$(@(Get-InstallProcesses).Count) $(@(Get-SessionApp).Count)"
Stop-Processes @() ''
Stop-Processes $null ''
'ok'`)
		if !strings.Contains(out, "0 0") || !strings.Contains(out, "ok") {
			t.Fatalf("output %s", out)
		}
	})
}

// psTraps are PowerShell constructs that break under one of the two
// editions or change what a caller gets; see the rules at the top of
// install.ps1's functions.
var psTraps = []struct {
	re  *regexp.Regexp
	why string
}{
	{regexp.MustCompile(`(?i)New-Object\s+(-TypeName\s+)?['"]?(System\.)?Collections\.`),
		`a New-Object collection: @() around it throws "Argument types do not match"; use [System.Collections.Generic.List[T]]::new()`},
	{regexp.MustCompile(`(?im)^\s*return\s*,`),
		`return , x: the caller's @() then holds the whole array as one item; return the items and let the caller wrap them in @()`},
}

// Every PowerShell script in the repo (install.ps1, the QA scripts) keeps
// clear of the traps.
func TestPowerShellTraps(t *testing.T) {
	root := filepath.Join("..", "..")
	n := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".ps1") {
			return nil
		}
		n++
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, tr := range psTraps {
				if tr.re.MatchString(line) {
					t.Errorf("%s:%d: %s\n\t%s", p, i+1, tr.why, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("found %d .ps1 files under %s", n, root)
	}
}
