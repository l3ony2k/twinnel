<#
.SYNOPSIS
  Run a PowerShell script in the signed-in user's interactive desktop
  session from an elevated (SSH) session, and wait for it.

.DESCRIPTION
  GUI checks need the user's session (session 1): an SSH login runs in a
  service session that has no desktop. This registers a one-shot scheduled
  task for the console user (Interactive logon, limited rights), runs
  -Script with a hidden console (conhost --headless, so no window flashes
  on the user's screen), waits up to -Timeout seconds, copies the output
  back to this console and removes the task.

  The script runs as the user, not elevated. Write its files under the
  user's %TEMP% (it can not write to admin-only folders).

  Each run registers its own task: -Task, else TwinnelQA-us-<script
  name>-<this process id>, so two runs at once (two agents, two batches)
  never replace each other's task or output. Only that task is removed
  afterwards; the output is left next to -Script as <task>.out.txt.

.EXAMPLE
  pwsh -File windows-usersession.ps1 -Script $env:TEMP\TwinnelQA-gui\step.ps1
  pwsh -File windows-usersession.ps1 -Script $env:TEMP\TwinnelQA-gui\step.ps1 -Task TwinnelQA-batch2
#>
param(
    [Parameter(Mandatory)][string]$Script,
    [string]$Arguments = '',
    [int]$Timeout = 120,
    [string]$Task = ''
)
$ErrorActionPreference = 'Stop'

$console = (Get-CimInstance Win32_ComputerSystem).UserName  # DOMAIN\user at the console
if (-not $console) { throw 'nobody is signed in at the console' }
if (Get-Process LogonUI -ErrorAction SilentlyContinue) { Write-Warning 'the console session looks locked: screenshots will be blank' }

$task = if ($Task) { $Task } else {
    $base = [IO.Path]::GetFileNameWithoutExtension($Script) -replace '[^A-Za-z0-9_-]', '_'
    "TwinnelQA-us-$base-$PID"
}
if ($task -notmatch '^[A-Za-z0-9_.-]{1,200}$') { throw "task name '$task': letters, digits, '.', '_' and '-' only" }
if (Get-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue) { throw "a task named $task already exists: another run is using it (pass a different -Task)" }
$log = Join-Path ([IO.Path]::GetDirectoryName($Script)) "$task.out.txt"  # per run, like the task
Remove-Item -LiteralPath $log -ErrorAction SilentlyContinue
$pwsh = (Get-Command pwsh).Source
$quoted = if ($Arguments) { "'" + ($Arguments -replace "'", "''") + "'" } else { '' }
$cmd = "& '$Script' $quoted *> '$log'"
$arg = "--headless `"$pwsh`" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command `"$cmd`""
$a = New-ScheduledTaskAction -Execute "$env:SystemRoot\System32\conhost.exe" -Argument $arg
$p = New-ScheduledTaskPrincipal -UserId $console -LogonType Interactive -RunLevel Limited
$s = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Seconds ($Timeout + 30))
Register-ScheduledTask -TaskName $task -Action $a -Principal $p -Settings $s | Out-Null
try {
    Start-ScheduledTask -TaskName $task
    Start-Sleep -Milliseconds 800
    $deadline = (Get-Date).AddSeconds($Timeout)
    while ((Get-ScheduledTask -TaskName $task).State -eq 'Running' -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
    if ((Get-ScheduledTask -TaskName $task).State -eq 'Running') { Stop-ScheduledTask -TaskName $task; Write-Warning "timed out after $Timeout s" }
    if (Test-Path -LiteralPath $log) { Get-Content -LiteralPath $log }
} finally {
    Unregister-ScheduledTask -TaskName $task -Confirm:$false -ErrorAction SilentlyContinue
}
