<#
.SYNOPSIS
  GUI steps for Twinnel on Windows, run in the user's session (through
  windows-usersession.ps1): start the app, list its windows, click, type,
  take screenshots, close it.

.DESCRIPTION
  Each argument is one step, or several separated by ';', run in order:
    launch[:K=V,K=V]   start Twinnel.exe (per-user install) with extra env vars
    open:VALUE         Twinnel.exe --open=VALUE (page:<id>, panel, 1): a running
                       copy turns to that page, else it starts there
    wait:MS            sleep
    windows            print Twinnel's top-level windows: handle, title, rect
    shot:NAME          full virtual screen to <Out>\NAME.png
    winshot:NAME       only the largest Twinnel window to <Out>\NAME.png
    printshot:NAME     the largest Twinnel window drawn by PrintWindow: works
                       when the console is locked or the window is covered
                       (shot/winshot then capture the lock screen); no
                       popups or tray flyout, as they are separate windows.
                       The PNG is the window's size in physical pixels: at
                       150% a window 2575 DIPs wide is 3862 px (the line
                       printed says the size and the scale)
    click:X,Y          left click at screen X,Y (physical pixels)
    rclick:X,Y         right click
    wclick:X,Y         left click at X,Y relative to the largest Twinnel window
    key:KEYS           System.Windows.Forms.SendKeys syntax (e.g. ^1, {ESC})
    wheel:N            scroll N notches over the middle of the largest Twinnel
                       window (negative: down)
    front              bring the largest Twinnel window to the front
    close              stop the Twinnel.exe processes of this install
    cursor             print the mouse position (to put it back later)
    wake               wake a sleeping display (a one-pixel mouse nudge); a
                       display that is off makes screenshots fail
  Screenshots name real data (nodes, tailnet): keep them out of the repo.

.EXAMPLE
  windows-usersession.ps1 -Script ...\windows-gui.ps1 -Arguments 'launch:TWINNEL_OPEN=1;wait:4000;windows;winshot:main'
#>
param([Parameter(ValueFromRemainingArguments)][string[]]$Steps)
$ErrorActionPreference = 'Continue'
$Out = Join-Path $env:TEMP 'TwinnelQA-gui'
New-Item -ItemType Directory -Force -Path $Out | Out-Null
$Exe = Join-Path $env:LOCALAPPDATA 'Programs\Twinnel\Twinnel.exe'

Add-Type -AssemblyName System.Windows.Forms, System.Drawing
if (-not ('QA.Gui' -as [type])) {
    Add-Type -Namespace QA -Name Gui -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr v);
[DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr v);
[DllImport("user32.dll")] public static extern IntPtr GetThreadDpiAwarenessContext();
[DllImport("user32.dll")] public static extern int GetAwarenessFromDpiAwarenessContext(IntPtr v);
[DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
[DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
[DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint f);
[DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
[DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
[DllImport("user32.dll")] public static extern bool GetCursorPos(out POINT p);
[DllImport("user32.dll")] public static extern void mouse_event(uint f, uint x, uint y, uint d, UIntPtr e);
[DllImport("kernel32.dll")] public static extern uint SetThreadExecutionState(uint f);
[DllImport("user32.dll")] public static extern int GetWindowText(IntPtr h, System.Text.StringBuilder s, int n);
[DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
public delegate bool EnumProc(IntPtr h, IntPtr l);
[DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc f, IntPtr l);
public struct RECT { public int L, T, R, B; }
public struct POINT { public int X, Y; }
'@
}
# Per-monitor v2: window rects in physical pixels, as PrintWindow draws.
# The process-wide call fails once the host set an awareness (its manifest,
# or an earlier run in the same PowerShell): then GetWindowRect returns the
# window's size scaled to the system DPI (2575x1417 for a 3862x2125 window
# at 150%), and PrintWindow's full-size drawing was cropped into that
# bitmap, which looked like a page shifted right and cut off (round 4).
# The thread's awareness can always be set; it is checked, and the size is
# scaled from the window's DPI if it still did not take.
[void][QA.Gui]::SetProcessDpiAwarenessContext([IntPtr](-4))
[void][QA.Gui]::SetThreadDpiAwarenessContext([IntPtr](-4))
$PerMonitor = [QA.Gui]::GetAwarenessFromDpiAwarenessContext([QA.Gui]::GetThreadDpiAwarenessContext()) -eq 2
if (-not $PerMonitor) { 'warning: not per-monitor DPI aware; window sizes are scaled from each window''s DPI' }

function Get-TwinnelWindows {
    $pids = @(Get-Process -Name Twinnel -ErrorAction SilentlyContinue | Where-Object { $_.Path -ieq $Exe } | ForEach-Object Id)
    $list = [System.Collections.Generic.List[object]]::new()
    $cb = [QA.Gui+EnumProc] {
        param($h, $l)
        $wp = [uint32]0; [void][QA.Gui]::GetWindowThreadProcessId($h, [ref]$wp)
        if ($pids -contains [int]$wp -and [QA.Gui]::IsWindowVisible($h)) {
            $r = New-Object QA.Gui+RECT; [void][QA.Gui]::GetWindowRect($h, [ref]$r)
            $sb = New-Object System.Text.StringBuilder 256; [void][QA.Gui]::GetWindowText($h, $sb, 256)
            $dpi = [int][QA.Gui]::GetDpiForWindow($h); if ($dpi -le 0) { $dpi = 96 }
            $w, $ht = ($r.R - $r.L), ($r.B - $r.T)
            if (-not $PerMonitor) { $w = [int][math]::Round($w * $dpi / 96); $ht = [int][math]::Round($ht * $dpi / 96) }
            $list.Add([pscustomobject]@{ Handle = $h; Title = $sb.ToString(); L = $r.L; T = $r.T; W = $w; H = $ht; Dpi = $dpi })
        }
        return $true
    }
    [void][QA.Gui]::EnumWindows($cb, [IntPtr]::Zero)
    return $list
}
function Get-MainWindow { Get-TwinnelWindows | Sort-Object { $_.W * $_.H } -Descending | Select-Object -First 1 }

function Save-Shot([string]$Name, $Rect) {
    if (-not $Rect) { $b = [System.Windows.Forms.SystemInformation]::VirtualScreen; $Rect = [pscustomobject]@{ L = $b.X; T = $b.Y; W = $b.Width; H = $b.Height } }
    $bmp = New-Object System.Drawing.Bitmap $Rect.W, $Rect.H
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($Rect.L, $Rect.T, 0, 0, $bmp.Size)
    $f = Join-Path $Out "$Name.png"
    $bmp.Save($f, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    "shot $f ($($Rect.W)x$($Rect.H))"
}
# Print-Shot asks the window to draw itself (PW_RENDERFULLCONTENT), so the
# result does not depend on what the screen shows.
function Print-Shot([string]$Name, $W) {
    $bmp = New-Object System.Drawing.Bitmap $W.W, $W.H
    $g = [System.Drawing.Graphics]::FromImage($bmp); $dc = $g.GetHdc()
    $ok = [QA.Gui]::PrintWindow($W.Handle, $dc, 2); $g.ReleaseHdc($dc)
    $f = Join-Path $Out "$Name.png"
    $bmp.Save($f, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    "printshot $f ($($W.W)x$($W.H) px at $([int]($W.Dpi * 100 / 96))%) ok=$ok"
}

function Click([int]$X, [int]$Y, [bool]$Right = $false) {
    [void][QA.Gui]::SetCursorPos($X, $Y); Start-Sleep -Milliseconds 80
    if ($Right) { [QA.Gui]::mouse_event(0x8, 0, 0, 0, [UIntPtr]::Zero); [QA.Gui]::mouse_event(0x10, 0, 0, 0, [UIntPtr]::Zero) }
    else { [QA.Gui]::mouse_event(0x2, 0, 0, 0, [UIntPtr]::Zero); [QA.Gui]::mouse_event(0x4, 0, 0, 0, [UIntPtr]::Zero) }
    "click $X,$Y right=$Right"
}

foreach ($s in ($Steps -split ';' | Where-Object { $_ })) {
    $op, $arg = $s -split ':', 2
    switch ($op) {
        'launch' {
            $psi = New-Object System.Diagnostics.ProcessStartInfo $Exe
            $psi.UseShellExecute = $false; $psi.WorkingDirectory = Split-Path $Exe
            if ($arg) { foreach ($kv in $arg -split ',') { $k, $v = $kv -split '=', 2; $psi.EnvironmentVariables[$k] = $v } }
            $p = [Diagnostics.Process]::Start($psi); "launched pid $($p.Id)"
        }
        'open' {
            $p = Start-Process -FilePath $Exe -ArgumentList "--open=$arg" -WorkingDirectory (Split-Path $Exe) -PassThru
            "open $arg (pid $($p.Id))"
        }
        'wait' { Start-Sleep -Milliseconds ([int]$arg) }
        'windows' { Get-TwinnelWindows | ForEach-Object { "window '{0}' at {1},{2} {3}x{4} px, {5}%" -f $_.Title, $_.L, $_.T, $_.W, $_.H, [int]($_.Dpi * 100 / 96) } }
        'shot' { Save-Shot $arg $null }
        'winshot' { $w = Get-MainWindow; if ($w) { Save-Shot $arg $w } else { "no Twinnel window for $arg" } }
        'printshot' { $w = Get-MainWindow; if ($w) { Print-Shot $arg $w } else { "no Twinnel window for $arg" } }
        'click' { $x, $y = $arg -split ','; Click $x $y }
        'rclick' { $x, $y = $arg -split ','; Click $x $y $true }
        'wclick' { $w = Get-MainWindow; $x, $y = $arg -split ','; if ($w) { Click ($w.L + [int]$x) ($w.T + [int]$y) } else { 'no window' } }
        'wheel' {
            $w = Get-MainWindow
            if ($w) {
                [void][QA.Gui]::SetCursorPos($w.L + [int]($w.W / 2), $w.T + [int]($w.H / 2)); Start-Sleep -Milliseconds 80
                $n = [int]$arg; $step = if ($n -lt 0) { -120 } else { 120 }
                for ($i = 0; $i -lt [Math]::Abs($n); $i++) { [QA.Gui]::mouse_event(0x800, 0, 0, [uint32][BitConverter]::ToUInt32([BitConverter]::GetBytes([int]$step), 0), [UIntPtr]::Zero); Start-Sleep -Milliseconds 40 }
                "wheel $n"
            } else { 'no window' }
        }
        'key' { [System.Windows.Forms.SendKeys]::SendWait($arg); "keys $arg" }
        'front' { $w = Get-MainWindow; if ($w) { "front $([QA.Gui]::SetForegroundWindow($w.Handle))" } }
        'close' { Get-Process -Name Twinnel -ErrorAction SilentlyContinue | Where-Object { $_.Path -ieq $Exe } | ForEach-Object { Stop-Process -Id $_.Id -Force; "closed pid $($_.Id)" } }
        'wake' {
            [void][QA.Gui]::SetThreadExecutionState([uint32]2147483651) # ES_CONTINUOUS|ES_SYSTEM_REQUIRED|ES_DISPLAY_REQUIRED
            [QA.Gui]::mouse_event(0x1, 1, 0, 0, [UIntPtr]::Zero); Start-Sleep -Milliseconds 100
            [QA.Gui]::mouse_event(0x1, [uint32]::MaxValue, 0, 0, [UIntPtr]::Zero) # -1
            Start-Sleep -Milliseconds 1500; 'wake'
        }
        'cursor' { $pt = New-Object QA.Gui+POINT; [void][QA.Gui]::GetCursorPos([ref]$pt); "cursor $($pt.X),$($pt.Y)" }
        default { "unknown step $s" }
    }
}
