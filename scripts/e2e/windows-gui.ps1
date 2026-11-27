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
    click:X,Y          left click at screen X,Y (physical pixels)
    rclick:X,Y         right click
    wclick:X,Y         left click at X,Y relative to the largest Twinnel window
    key:KEYS           System.Windows.Forms.SendKeys syntax (e.g. ^1, {ESC})
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
if (-not ('QA.Win' -as [type])) {
    Add-Type -Namespace QA -Name Win -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr v);
[DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
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
[void][QA.Win]::SetProcessDpiAwarenessContext([IntPtr](-4)) # per-monitor v2: physical pixels

function Get-TwinnelWindows {
    $pids = @(Get-Process -Name Twinnel -ErrorAction SilentlyContinue | Where-Object { $_.Path -ieq $Exe } | ForEach-Object Id)
    $list = [System.Collections.Generic.List[object]]::new()
    $cb = [QA.Win+EnumProc] {
        param($h, $l)
        $wp = [uint32]0; [void][QA.Win]::GetWindowThreadProcessId($h, [ref]$wp)
        if ($pids -contains [int]$wp -and [QA.Win]::IsWindowVisible($h)) {
            $r = New-Object QA.Win+RECT; [void][QA.Win]::GetWindowRect($h, [ref]$r)
            $sb = New-Object System.Text.StringBuilder 256; [void][QA.Win]::GetWindowText($h, $sb, 256)
            $list.Add([pscustomobject]@{ Handle = $h; Title = $sb.ToString(); L = $r.L; T = $r.T; W = $r.R - $r.L; H = $r.B - $r.T })
        }
        return $true
    }
    [void][QA.Win]::EnumWindows($cb, [IntPtr]::Zero)
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
function Click([int]$X, [int]$Y, [bool]$Right = $false) {
    [void][QA.Win]::SetCursorPos($X, $Y); Start-Sleep -Milliseconds 80
    if ($Right) { [QA.Win]::mouse_event(0x8, 0, 0, 0, [UIntPtr]::Zero); [QA.Win]::mouse_event(0x10, 0, 0, 0, [UIntPtr]::Zero) }
    else { [QA.Win]::mouse_event(0x2, 0, 0, 0, [UIntPtr]::Zero); [QA.Win]::mouse_event(0x4, 0, 0, 0, [UIntPtr]::Zero) }
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
        'windows' { Get-TwinnelWindows | ForEach-Object { "window '{0}' at {1},{2} {3}x{4}" -f $_.Title, $_.L, $_.T, $_.W, $_.H } }
        'shot' { Save-Shot $arg $null }
        'winshot' { $w = Get-MainWindow; if ($w) { Save-Shot $arg $w } else { "no Twinnel window for $arg" } }
        'click' { $x, $y = $arg -split ','; Click $x $y }
        'rclick' { $x, $y = $arg -split ','; Click $x $y $true }
        'wclick' { $w = Get-MainWindow; $x, $y = $arg -split ','; if ($w) { Click ($w.L + [int]$x) ($w.T + [int]$y) } else { 'no window' } }
        'key' { [System.Windows.Forms.SendKeys]::SendWait($arg); "keys $arg" }
        'front' { $w = Get-MainWindow; if ($w) { "front $([QA.Win]::SetForegroundWindow($w.Handle))" } }
        'close' { Get-Process -Name Twinnel -ErrorAction SilentlyContinue | Where-Object { $_.Path -ieq $Exe } | ForEach-Object { Stop-Process -Id $_.Id -Force; "closed pid $($_.Id)" } }
        'wake' {
            [void][QA.Win]::SetThreadExecutionState([uint32]2147483651) # ES_CONTINUOUS|ES_SYSTEM_REQUIRED|ES_DISPLAY_REQUIRED
            [QA.Win]::mouse_event(0x1, 1, 0, 0, [UIntPtr]::Zero); Start-Sleep -Milliseconds 100
            [QA.Win]::mouse_event(0x1, [uint32]::MaxValue, 0, 0, [UIntPtr]::Zero) # -1
            Start-Sleep -Milliseconds 1500; 'wake'
        }
        'cursor' { $pt = New-Object QA.Win+POINT; [void][QA.Win]::GetCursorPos([ref]$pt); "cursor $($pt.X),$($pt.Y)" }
        default { "unknown step $s" }
    }
}
