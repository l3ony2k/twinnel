package kernel

// Installing Tailscale on Windows: the official client from its MSI.
//
// On Windows there is no open-source tailscaled to run on its own: the
// official client is the daemon (the "Tailscale" service, tailscaled.exe)
// plus a tray app. pkgs.tailscale.com publishes one MSI per architecture
// (tailscale-setup-<v>-<amd64|arm64|x86>.msi) with a .sha256 file next to
// it, like the Linux tarballs. The helper (LocalSystem) downloads it into
// its own state dir (only SYSTEM and Administrators can write there),
// checks the sha256 and runs
//
//	msiexec.exe /i <msi> /qn /norestart /l*v <log> TS_NOLAUNCH=true
//
// TS_NOLAUNCH keeps the installer from starting the tray app at the end
// of a silent install: the helper runs in session 0, where a GUI started
// by it is invisible to the user. The tray app still starts at the user's
// next sign-in (the MSI registers it), and the "Tailscale" service starts
// right away. No policy properties (TS_UNATTENDEDMODE and the like) are
// set: they write machine policy the user could then not change in the
// tray app. Login goes through the helper (tailscale.login, LocalAPI).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// TSSourceMSI is the Windows MSI on pkgs.tailscale.com.
const TSSourceMSI = "pkgs.tailscale.com (msi)"

var msiNameRE = regexp.MustCompile(`^tailscale-setup-(\d+\.\d+\.\d+)-(amd64|arm64|x86)\.msi$`)

// msiArch is the MSI's arch name for GOARCH.
func msiArch(goarch string) string {
	if goarch == "386" {
		return "x86"
	}
	return goarch
}

// UsesMSI reports whether this platform installs Tailscale from the
// official MSI (Windows) rather than unpacking tailscale + tailscaled.
func (t *TailscaleInstaller) UsesMSI() bool { return t.goos() == "windows" }

func (t *TailscaleInstaller) resolveMSI(ctx context.Context) (TailscaleAsset, error) {
	track := t.Track
	if track == "" {
		track = "stable"
	}
	if track != "stable" && track != "unstable" {
		return TailscaleAsset{}, fmt.Errorf("unknown track %q", track)
	}
	var idx struct {
		MSIs        map[string]string
		MSIsVersion string
	}
	base := t.pkgs() + "/" + track + "/"
	if err := t.getJSON(ctx, base+"?mode=json", &idx); err != nil {
		return TailscaleAsset{}, err
	}
	arch := msiArch(t.goarch())
	name := idx.MSIs[arch]
	m := msiNameRE.FindStringSubmatch(name)
	if m == nil || m[2] != arch {
		return TailscaleAsset{}, fmt.Errorf("pkgs.tailscale.com has no Windows installer for %s", arch)
	}
	a := TailscaleAsset{Version: m[1], Source: TSSourceMSI, Name: name, URL: base + name}
	sum, err := t.fetchSHA256(ctx, a.URL, name)
	if err != nil {
		return a, err
	}
	a.SHA256 = sum
	return a, nil
}

// MSIExitError is a failed msiexec run.
type MSIExitError struct {
	Code int
	Log  string
}

func (e *MSIExitError) Error() string {
	switch e.Code {
	case 1618:
		return "another installation is in progress (Windows Installer is busy); wait for it to finish and try again"
	case 1602:
		return "the Tailscale installation was canceled"
	case 1625, 1643, 1260:
		return "a system policy blocks installing Tailscale on this computer; ask its administrator to allow it"
	case 1638:
		return "a different version of Tailscale is already installed; remove it in Settings → Apps and try again"
	}
	return fmt.Sprintf("the Tailscale installer failed (msiexec exit code %d); its log is %s", e.Code, e.Log)
}

// MSIArgs are msiexec's arguments for a silent install of msi, logging to log.
func MSIArgs(msi, log string) []string {
	return []string{"/i", msi, "/qn", "/norestart", "/l*v", log, "TS_NOLAUNCH=true"}
}

// MSIExec is msiexec.exe in %SystemRoot%\System32 (never one found on PATH).
func MSIExec() string {
	root := os.Getenv("SystemRoot")
	if len(root) < 3 || root[1] != ':' {
		root = `C:\Windows`
	}
	return strings.TrimRight(root, `\`) + `\System32\msiexec.exe`
}

// RunMSIExec runs msiexec with MSIArgs and returns its exit code; err is
// for msiexec not starting at all.
func RunMSIExec(ctx context.Context, msi, log string) (int, error) {
	cmd := exec.CommandContext(ctx, MSIExec(), MSIArgs(msi, log)...)
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, fmt.Errorf("start msiexec: %w", err)
	}
	return 0, nil
}

// windowsTailscaleDir is where the MSI installs (%ProgramFiles%\Tailscale).
func windowsTailscaleDir() string {
	pf := os.Getenv("ProgramFiles")
	if len(pf) < 3 || pf[1] != ':' {
		pf = `C:\Program Files`
	}
	return strings.TrimRight(pf, `\`) + `\Tailscale`
}

// MSIInstalled is what InstallMSI did.
type MSIInstalled struct {
	Version string
	CLI     string // tailscale.exe
	Daemon  string // tailscaled.exe
	// RestartNeeded: msiexec asked for a restart (exit code 3010) to finish.
	RestartNeeded bool
}

// InstallMSI downloads a (an MSI from resolveMSI), verifies its sha256
// and runs the installer (RunMSI, default RunMSIExec) with msiexec's log
// at logPath. It does not wait for the service; the caller does.
func (t *TailscaleInstaller) InstallMSI(ctx context.Context, a TailscaleAsset, logPath string) (MSIInstalled, error) {
	if !ValidTailscaleVersion(a.Version) {
		return MSIInstalled{}, fmt.Errorf("invalid version %q", a.Version)
	}
	if !msiNameRE.MatchString(a.Name) {
		return MSIInstalled{}, fmt.Errorf("%q is not a Tailscale MSI", a.Name)
	}
	if !hex64(a.SHA256) {
		return MSIInstalled{}, fmt.Errorf("%s has no sha256; refusing to install", a.Name)
	}
	if err := t.checkTSURL(a); err != nil {
		return MSIInstalled{}, err
	}
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return MSIInstalled{}, err
	}
	tmp, err := os.MkdirTemp(t.Dir, ".install-")
	if err != nil {
		return MSIInstalled{}, err
	}
	defer os.RemoveAll(tmp)
	msi := filepath.Join(tmp, a.Name)
	if err := t.download(ctx, a, msi); err != nil {
		return MSIInstalled{}, err
	}
	run := t.RunMSI
	if run == nil {
		run = RunMSIExec
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	code, err := run(ctx, msi, logPath)
	if err != nil {
		return MSIInstalled{}, err
	}
	out := MSIInstalled{Version: a.Version}
	switch code {
	case 0:
	case 3010, 1641:
		out.RestartNeeded = true
	default:
		return MSIInstalled{}, &MSIExitError{Code: code, Log: logPath}
	}
	dir := t.WindowsDir
	if dir == "" {
		dir = windowsTailscaleDir()
	}
	out.CLI, out.Daemon = filepath.Join(dir, "tailscale.exe"), filepath.Join(dir, "tailscaled.exe")
	probe := t.Probe
	if probe == nil {
		probe = ProbeTailscale
	}
	if got, err := probe(ctx, out.CLI); err == nil && got != "" {
		out.Version = got
	} else if err != nil {
		return out, fmt.Errorf("Tailscale was installed but %s does not run: %w", out.CLI, err)
	}
	return out, nil
}
