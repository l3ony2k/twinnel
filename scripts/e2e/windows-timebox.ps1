<#
.SYNOPSIS
  Time-box a test that breaks traffic: run a restore script automatically
  after N seconds, as SYSTEM, even if the SSH session that started the test
  dies.

.DESCRIPTION
  arm -Name N -Restore FILE -Seconds S
      registers a one-shot scheduled task "TwinnelQA-<N>" (SYSTEM, highest
      privileges) that runs `pwsh -File FILE` S seconds from now. The
      restore script's output goes to FILE.log.
  disarm -Name N
      removes the task (after the test restored things itself).
  run -Name N
      runs the task now (restore early) and waits for it to finish.
  status -Name N
      prints the task's state and last result.

  Needs an elevated PowerShell. The restore script must be idempotent: it
  can run after the test already restored.

.EXAMPLE
  pwsh -File windows-timebox.ps1 arm -Name tunstack -Restore C:\ProgramData\TwinnelQA\bin\restore-tunstack.ps1 -Seconds 240
#>
param(
    [Parameter(Position = 0, Mandatory)][ValidateSet('arm', 'disarm', 'run', 'status')][string]$Action,
    [Parameter(Mandatory)][string]$Name,
    [string]$Restore = '',
    [int]$Seconds = 180
)
$ErrorActionPreference = 'Stop'
$task = "TwinnelQA-$Name"

switch ($Action) {
    'arm' {
        if (-not (Test-Path -LiteralPath $Restore)) { throw "no restore script $Restore" }
        $pwsh = (Get-Command pwsh -ErrorAction SilentlyContinue).Source
        if (-not $pwsh) { $pwsh = (Get-Command powershell).Source }
        $log = "$Restore.log"
        $arg = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command `"& '$Restore' *>> '$log'`""
        $a = New-ScheduledTaskAction -Execute $pwsh -Argument $arg
        $at = (Get-Date).AddSeconds($Seconds)
        $tr = New-ScheduledTaskTrigger -Once -At $at
        $p = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
        $s = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 10)
        Register-ScheduledTask -TaskName $task -Action $a -Trigger $tr -Principal $p -Settings $s -Force | Out-Null
        Write-Output "ARMED $task at $($at.ToString('HH:mm:ss')) -> $Restore (log $log)"
    }
    'disarm' {
        Unregister-ScheduledTask -TaskName $task -Confirm:$false -ErrorAction SilentlyContinue
        Write-Output "DISARMED $task"
    }
    'run' {
        Start-ScheduledTask -TaskName $task
        Start-Sleep -Seconds 2
        for ($i = 0; $i -lt 300 -and (Get-ScheduledTask -TaskName $task).State -eq 'Running'; $i++) { Start-Sleep -Seconds 1 }
        $info = Get-ScheduledTaskInfo -TaskName $task
        Write-Output "RAN $task result=$($info.LastTaskResult)"
    }
    'status' {
        $t = Get-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue
        if (-not $t) { Write-Output "$task not armed"; break }
        $info = Get-ScheduledTaskInfo -TaskName $task
        Write-Output "$task state=$($t.State) next=$($info.NextRunTime) last=$($info.LastRunTime) result=$($info.LastTaskResult)"
    }
}
