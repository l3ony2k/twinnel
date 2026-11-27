<#
.SYNOPSIS
Installs Twinnel for the current user from a release zip (Windows 10 1809+ / 11).

.DESCRIPTION
Copies Twinnel.exe and bin\ (twinnel.exe, twinnel-helper.exe) to
%LOCALAPPDATA%\Programs\Twinnel, adds a Start menu shortcut, puts bin\ on
the user's PATH (for `twinnel` and the agents' MCP setup), and starts
Twinnel. No administrator rights: the privileged helper is installed
afterwards from the app (its first screen), which asks Windows for
administrator approval (UAC) once.

An update works while Twinnel's programs run: an agent's `twinnel mcp`
keeps bin\twinnel.exe open, and Windows refuses to overwrite a running
program but lets it be renamed. Every new file is first copied next to
the one it replaces (<name>.new-<stamp>); then each running file is renamed
aside (<name>.old-<stamp>, it keeps running) and the new one moved into
place. If any of that fails, the files already replaced are put back: the
install is all new or all old. The next run deletes the renamed files that
no longer run. A Twinnel running in this session is stopped first and
started again afterwards (also with -NoLaunch); one running in another
session (another sign-in, an SSH session) keeps running the old version
until it is restarted.

The build is not signed. Windows marks files downloaded from the internet;
this script clears that mark on the files it installs (Unblock-File), so
SmartScreen does not stop Twinnel at every start. Run it with:

  powershell -ExecutionPolicy Bypass -File .\install.ps1

.PARAMETER InstallDir
Where to install (default %LOCALAPPDATA%\Programs\Twinnel). Must end in a
folder named Twinnel.

.PARAMETER Uninstall
Removes the helper first (`twinnel helper uninstall`, one UAC prompt) when
it is installed, stops Twinnel's programs that run from the install folder
(the app, agents' `twinnel mcp`), then removes the app folder, the
shortcut, the PATH entry and the start-at-login entry. mihomo, Tailscale
and the helper's state in %ProgramData%\Twinnel stay.

.PARAMETER NoLaunch
Does not start Twinnel after installing, unless it was running in this
session before (then it is started again).

.PARAMETER DryRun
Prints what it would do and changes nothing.
#>
[CmdletBinding()]
param(
	[string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\Twinnel'),
	[switch]$Uninstall,
	[switch]$NoLaunch,
	[switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

$Marker = '.twinnel-install'            # the app only treats a folder with it as its install
$RunKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$ApprovedKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run'
$RunValue = 'io.github.l3ony2k.twinnel'  # mygo.json "identifier": 登录时打开
$InstallDir = $InstallDir.TrimEnd('\')
$Bin = "$InstallDir\bin"
$AppExe = "$InstallDir\Twinnel.exe"
# File names of a swap (same as core/binswap: OldInfix, NewInfix).
$OldInfix = '.old-'
$NewInfix = '.new-'

function Step([string]$What, [scriptblock]$Do) {
	if ($DryRun) { Write-Host "would: $What"; return }
	Write-Host $What
	& $Do
}

function Test-InstallDir([string]$Dir) {
	# X:\...\Twinnel, below the drive root.
	return ($Dir -match '^[A-Za-z]:\\.+\\Twinnel$') -and ($Dir -notmatch '\\\.\.(\\|$)')
}

# --- files that may be running ---------------------------------------------

# A stamp of digits for the names of one swap.
function New-Stamp { return [string][DateTime]::UtcNow.Ticks }

# Whether a file is open by a running program (a running .exe can be
# renamed but not opened for exclusive write).
function Test-InUse([string]$Path) {
	if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return $false }
	try {
		[IO.File]::Open($Path, 'Open', 'ReadWrite', 'None').Dispose()
		return $false
	} catch {
		return $true
	}
}

# The renamed-aside and staged files earlier runs left in a folder.
function Get-Leftovers([string]$Dir) {
	if (-not (Test-Path -LiteralPath $Dir -PathType Container)) { return @() }
	return @(Get-ChildItem -LiteralPath $Dir -File -Force |
			Where-Object { $_.Name -match ('(' + [regex]::Escape($OldInfix) + '|' + [regex]::Escape($NewInfix) + ')\d+$') } |
			ForEach-Object { $_.FullName })
}

# Deletes the leftovers that no longer run; the running ones stay for the
# next time.
function Remove-Leftovers([string[]]$Dirs) {
	foreach ($d in $Dirs) {
		foreach ($p in @(Get-Leftovers $d)) {
			try { Remove-Item -LiteralPath $p -Force } catch { Write-Verbose "still in use: $p" }
		}
	}
}

# Replaces each @{ Src; Dst } with a copy of Src, all or none (see the
# description at the top). Returns the files that were running and now run
# from their renamed copy.
function Install-Files([object[]]$Files) {
	$stamp = New-Stamp
	$staged = New-Object System.Collections.Generic.List[string]
	$swapped = New-Object System.Collections.Generic.List[object]
	$inUse = New-Object System.Collections.Generic.List[string]
	try {
		foreach ($f in $Files) {
			$s = $f.Dst + $NewInfix + $stamp
			New-Item -ItemType Directory -Force -Path (Split-Path -Parent $f.Dst) | Out-Null
			$staged.Add($s)
			Copy-Item -LiteralPath $f.Src -Destination $s -Force
		}
		for ($i = 0; $i -lt $Files.Count; $i++) {
			$f = $Files[$i]
			$old = ''
			if (Test-Path -LiteralPath $f.Dst) {
				$old = $f.Dst + $OldInfix + $stamp
				[IO.File]::Move($f.Dst, $old)
			}
			try {
				[IO.File]::Move($staged[$i], $f.Dst)
			} catch {
				if ($old) { [IO.File]::Move($old, $f.Dst) }
				throw
			}
			$swapped.Add(@{ Dst = $f.Dst; Old = $old })
		}
	} catch {
		$err = $_
		for ($i = $swapped.Count - 1; $i -ge 0; $i--) {
			$d = $swapped[$i]
			try {
				Remove-Item -LiteralPath $d.Dst -Force
				if ($d.Old) { [IO.File]::Move($d.Old, $d.Dst) }
			} catch {
				Write-Warning "could not put back $($d.Dst): $_"
			}
		}
		foreach ($s in $staged) { Remove-Item -LiteralPath $s -Force -ErrorAction SilentlyContinue }
		throw "install failed, the previous files are back in place: $err"
	}
	foreach ($d in $swapped) {
		if (-not $d.Old) { continue }
		try { Remove-Item -LiteralPath $d.Old -Force } catch { $inUse.Add($d.Dst) }
	}
	return , $inUse.ToArray()
}

# --- the app -------------------------------------------------------------------

# Twinnel's processes that run from the install folder: the app, agents'
# `twinnel mcp`, a CLI run (also from a renamed-aside copy).
function Get-InstallProcesses {
	$prefix = $InstallDir + '\'
	return @(Get-Process -ErrorAction SilentlyContinue | Where-Object {
			$p = $null
			try { $p = $_.Path } catch { }
			$p -and $p.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)
		})
}

# The installed app running in this session (the one this script can
# start again). An app of another session is left running.
function Get-SessionApp {
	$session = (Get-Process -Id $PID).SessionId
	return @(@(Get-InstallProcesses) | Where-Object { ($_.Path -ieq $AppExe) -and ($_.SessionId -eq $session) })
}

function Stop-Processes([object[]]$Procs, [string]$Why) {
	$Procs = @($Procs | Where-Object { $_ })
	foreach ($p in $Procs) {
		$id = $p.Id
		$name = Split-Path -Leaf $p.Path
		Step "stop $name (pid $id)$Why" { Stop-Process -Id $id -Force -ErrorAction SilentlyContinue }
	}
	if (-not $DryRun -and $Procs.Count -gt 0) {
		foreach ($p in $Procs) { try { $p.WaitForExit(5000) | Out-Null } catch { } }
	}
}

function Start-App {
	Start-Process -FilePath $AppExe -WorkingDirectory $InstallDir
}

# --- PATH ------------------------------------------------------------------------

# The user's PATH as stored: entries such as %LOCALAPPDATA%\... stay
# unexpanded and the value stays REG_EXPAND_SZ ([Environment]::
# SetEnvironmentVariable would write it back expanded, as REG_SZ).
function Get-UserPath {
	$key = Get-Item -LiteralPath 'HKCU:\Environment'
	$p = $key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
	return @($p -split ';' | Where-Object { $_ })
}

function Test-OnUserPath([string[]]$Parts, [string]$Dir) {
	foreach ($p in $Parts) {
		if ([Environment]::ExpandEnvironmentVariables($p).TrimEnd('\') -ieq $Dir) { return $true }
	}
	return $false
}

function Set-UserPath([string[]]$Parts) {
	Set-ItemProperty -LiteralPath 'HKCU:\Environment' -Name 'Path' -Value ($Parts -join ';') -Type ExpandString
	Send-EnvironmentChange
}

# Tells Explorer the environment changed (WM_SETTINGCHANGE "Environment"),
# so terminals opened from now on get the new PATH. Terminals already open
# keep the old one.
function Send-EnvironmentChange {
	if (-not ('Twinnel.EnvBroadcast' -as [type])) {
		Add-Type -Namespace Twinnel -Name EnvBroadcast -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
	}
	$result = [UIntPtr]::Zero
	# HWND_BROADCAST, WM_SETTINGCHANGE, SMTO_ABORTIFHUNG, 5 s
	[void][Twinnel.EnvBroadcast]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
}

# Dot-sourced (tests): the functions only.
if ($MyInvocation.InvocationName -eq '.') { return }

$Shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) 'Twinnel.lnk'

if (-not (Test-InstallDir $InstallDir)) {
	throw "InstallDir must be an absolute path ending in \Twinnel: $InstallDir"
}

if ($Uninstall) {
	$helper = Join-Path $env:ProgramFiles 'Twinnel\twinnel-helper.exe'
	$cli = Join-Path $Bin 'twinnel.exe'
	if ((Test-Path -LiteralPath $helper) -and (Test-Path -LiteralPath $cli)) {
		Step "uninstall the helper service (Windows asks for administrator approval)" {
			& $cli helper uninstall --yes
			if ($LASTEXITCODE -ne 0) { throw "twinnel helper uninstall failed ($LASTEXITCODE)" }
		}
	}
	$ours = Test-Path -LiteralPath (Join-Path $InstallDir $Marker)
	if ($ours) {
		# A running program cannot be deleted: Twinnel's own ones go first
		# (an agent's MCP server reconnects to nothing; Twinnel is going).
		Stop-Processes @(Get-InstallProcesses) ', it runs from the install folder'
	}
	if (Test-Path -LiteralPath $Shortcut) { Step "remove $Shortcut" { Remove-Item -LiteralPath $Shortcut -Force } }
	$parts = Get-UserPath
	if (Test-OnUserPath $parts $Bin) {
		Step "remove $Bin from the user's PATH" {
			Set-UserPath @($parts | Where-Object { [Environment]::ExpandEnvironmentVariables($_).TrimEnd('\') -ine $Bin })
		}
	}
	if (Get-ItemProperty -Path $RunKey -Name $RunValue -ErrorAction SilentlyContinue) {
		Step "remove the start-at-login entry" {
			Remove-ItemProperty -Path $RunKey -Name $RunValue
			Remove-ItemProperty -Path $ApprovedKey -Name $RunValue -ErrorAction SilentlyContinue
		}
	}
	if ($ours) {
		Step "remove $InstallDir" {
			# A folder that is some process's current directory cannot be
			# removed: not this one's.
			if ([Environment]::CurrentDirectory.StartsWith($InstallDir, [StringComparison]::OrdinalIgnoreCase)) {
				[Environment]::CurrentDirectory = $env:TEMP
			}
			if ((Get-Location).ProviderPath.StartsWith($InstallDir, [StringComparison]::OrdinalIgnoreCase)) {
				Set-Location -LiteralPath $env:TEMP
			}
			# The marker goes last, so a removal that stops half way can be
			# run again.
			for ($try = 1; ; $try++) {
				try {
					Get-ChildItem -LiteralPath $InstallDir -Force | Where-Object { $_.Name -ne $Marker } |
						Remove-Item -Recurse -Force
					Remove-Item -LiteralPath $InstallDir -Recurse -Force
					break
				} catch {
					if ($try -ge 10) { throw "could not remove ${InstallDir}: $_" }
					Start-Sleep -Milliseconds 500 # a stopped program lets go of its file
				}
			}
		}
	} elseif (Test-Path -LiteralPath $InstallDir) {
		Write-Warning "$InstallDir has no ${Marker} file, so it was not removed (not installed by this script)"
	}
	if ($DryRun) { Write-Host 'Dry run: nothing was changed.' } else { Write-Host 'Twinnel removed.' }
	exit 0
}

$src = $PSScriptRoot
foreach ($f in @('Twinnel.exe', 'bin\twinnel.exe', 'bin\twinnel-helper.exe')) {
	if (-not (Test-Path -LiteralPath (Join-Path $src $f))) { throw "$f must be next to this script (unpack the whole zip)" }
}
if ($src.TrimEnd('\') -ieq $InstallDir) { throw 'run install.ps1 from the unpacked zip, not from the install folder' }

# What goes where: the app, everything in bin\, the docs, and the marker
# (with the version) last.
$files = New-Object System.Collections.Generic.List[object]
$files.Add(@{ Src = (Join-Path $src 'Twinnel.exe'); Dst = $AppExe })
foreach ($b in (Get-ChildItem -LiteralPath (Join-Path $src 'bin') -File)) {
	$files.Add(@{ Src = $b.FullName; Dst = (Join-Path $Bin $b.Name) })
}
foreach ($doc in @('install.ps1', 'INSTALL.md', 'LICENSE', 'README.md')) {
	$p = Join-Path $src $doc
	if (Test-Path -LiteralPath $p) { $files.Add(@{ Src = $p; Dst = (Join-Path $InstallDir $doc) }) }
}

$app = @(Get-SessionApp)
$appIds = @($app | ForEach-Object { $_.Id })
$otherApps = @(@(Get-InstallProcesses) | Where-Object { ($_.Path -ieq $AppExe) -and ($appIds -notcontains $_.Id) })
$restart = $app.Count -gt 0
$launch = $restart -or -not $NoLaunch
$installed = $false
try {
	Stop-Processes $app ''
	Step "copy Twinnel to $InstallDir" {
		Remove-Leftovers @($InstallDir, $Bin)
		$version = ''
		try { $version = (& (Join-Path $src 'bin\twinnel.exe') version --short 2>$null) } catch { }
		$markerSrc = Join-Path ([IO.Path]::GetTempPath()) ("twinnel-install-marker-" + (New-Stamp))
		Set-Content -LiteralPath $markerSrc -Value "$version" -Encoding ASCII
		try {
			$all = @($files) + @(@{ Src = $markerSrc; Dst = (Join-Path $InstallDir $Marker) })
			$running = @(Install-Files $all)
		} finally {
			Remove-Item -LiteralPath $markerSrc -Force -ErrorAction SilentlyContinue
		}
		foreach ($r in $running) {
			Write-Host "  $(Split-Path -Leaf $r) was in use: the programs running it keep the old version until they restart"
		}
	}
	if ($DryRun) {
		foreach ($f in $files) {
			if (Test-InUse $f.Dst) { Write-Host "  $(Split-Path -Leaf $f.Dst) is in use: it would be renamed aside and keep running" }
		}
	}
	$installed = $true
} finally {
	# A failed copy leaves the previous version: start that one again.
	if (-not $installed -and $restart -and -not $DryRun) { Start-App }
}

Step 'clear the downloaded-file mark on the installed programs (unsigned build)' {
	Get-ChildItem -LiteralPath $InstallDir -Recurse -File -Include '*.exe' | Unblock-File
}
Step "add the Start menu shortcut $Shortcut" {
	$shell = New-Object -ComObject WScript.Shell
	$lnk = $shell.CreateShortcut($Shortcut)
	$lnk.TargetPath = $AppExe
	$lnk.WorkingDirectory = $InstallDir
	$lnk.Description = 'Twinnel'
	$lnk.Save()
}
$parts = Get-UserPath
$pathAdded = $false
if (-not (Test-OnUserPath $parts $Bin)) {
	Step "add $Bin to the user's PATH" { Set-UserPath (@($parts) + $Bin) }
	$pathAdded = $true
}
if (-not $DryRun -and -not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $Bin })) {
	$env:Path = "$env:Path;$Bin" # this window too
}
if ($launch) {
	$what = 'start Twinnel'
	if ($restart) { $what = 'start Twinnel again' }
	Step $what { Start-App }
}
if ($DryRun) {
	Write-Host 'Dry run: nothing was changed.'
	exit 0
}
Write-Host "Twinnel is installed in $InstallDir. Its icon is in the notification area of the taskbar."
foreach ($p in $otherApps) {
	Write-Host "Twinnel is also running in another session (pid $($p.Id)): it keeps the old version until it is restarted there."
}
if ($pathAdded) {
	Write-Host "The twinnel command works in this window and in terminals you open from now on. Terminals (and editors such as VS Code) that were already open need to be restarted to find it."
}
