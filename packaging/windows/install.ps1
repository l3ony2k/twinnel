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

The build is not signed. Windows marks files downloaded from the internet;
this script clears that mark on the files it installs (Unblock-File), so
SmartScreen does not stop Twinnel at every start. Run it with:

  powershell -ExecutionPolicy Bypass -File .\install.ps1

.PARAMETER InstallDir
Where to install (default %LOCALAPPDATA%\Programs\Twinnel). Must end in a
folder named Twinnel.

.PARAMETER Uninstall
Removes the helper first (`twinnel helper uninstall`, one UAC prompt) when
it is installed, then the app folder, the shortcut, the PATH entry and the
start-at-login entry. mihomo, Tailscale and the helper's state in
%ProgramData%\Twinnel stay.

.PARAMETER NoLaunch
Does not start Twinnel after installing.

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
$Shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) 'Twinnel.lnk'
$InstallDir = $InstallDir.TrimEnd('\')
$Bin = Join-Path $InstallDir 'bin'

function Step([string]$What, [scriptblock]$Do) {
	if ($DryRun) { Write-Host "would: $What"; return }
	Write-Host $What
	& $Do
}

function Test-InstallDir([string]$Dir) {
	# X:\...\Twinnel, below the drive root.
	return ($Dir -match '^[A-Za-z]:\\.+\\Twinnel$') -and ($Dir -notmatch '\\\.\.(\\|$)')
}

function Stop-Twinnel {
	$exe = Join-Path $InstallDir 'Twinnel.exe'
	Get-Process -Name 'Twinnel' -ErrorAction SilentlyContinue |
		Where-Object { $_.Path -and ($_.Path -ieq $exe) } |
		ForEach-Object {
			$id = $_.Id
			Step "stop the running Twinnel (pid $id)" { Stop-Process -Id $id -Force; Start-Sleep -Milliseconds 500 }
		}
}

function Get-UserPath {
	$p = [Environment]::GetEnvironmentVariable('Path', 'User')
	if ($null -eq $p) { return @() }
	return @($p -split ';' | Where-Object { $_ })
}

function Set-UserPath([string[]]$Parts) {
	[Environment]::SetEnvironmentVariable('Path', ($Parts -join ';'), 'User')
}

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
	Stop-Twinnel
	if (Test-Path -LiteralPath $Shortcut) { Step "remove $Shortcut" { Remove-Item -LiteralPath $Shortcut -Force } }
	$parts = Get-UserPath
	if ($parts | Where-Object { $_.TrimEnd('\') -ieq $Bin }) {
		Step "remove $Bin from the user's PATH" { Set-UserPath @($parts | Where-Object { $_.TrimEnd('\') -ine $Bin }) }
	}
	if (Get-ItemProperty -Path $RunKey -Name $RunValue -ErrorAction SilentlyContinue) {
		Step "remove the start-at-login entry" {
			Remove-ItemProperty -Path $RunKey -Name $RunValue
			Remove-ItemProperty -Path $ApprovedKey -Name $RunValue -ErrorAction SilentlyContinue
		}
	}
	if (Test-Path -LiteralPath (Join-Path $InstallDir $Marker)) {
		Step "remove $InstallDir" { Remove-Item -LiteralPath $InstallDir -Recurse -Force }
	} elseif (Test-Path -LiteralPath $InstallDir) {
		Write-Warning "$InstallDir has no ${Marker} file, so it was not removed (not installed by this script)"
	}
	Write-Host 'Twinnel removed.'
	exit 0
}

$src = $PSScriptRoot
foreach ($f in @('Twinnel.exe', 'bin\twinnel.exe', 'bin\twinnel-helper.exe')) {
	if (-not (Test-Path -LiteralPath (Join-Path $src $f))) { throw "$f must be next to this script (unpack the whole zip)" }
}
if ($src.TrimEnd('\') -ieq $InstallDir) { throw 'run install.ps1 from the unpacked zip, not from the install folder' }

Stop-Twinnel
Step "copy Twinnel to $InstallDir" {
	New-Item -ItemType Directory -Force -Path $Bin | Out-Null
	Copy-Item -LiteralPath (Join-Path $src 'Twinnel.exe') -Destination $InstallDir -Force
	Copy-Item -Path (Join-Path $src 'bin\*') -Destination $Bin -Force
	foreach ($doc in @('install.ps1', 'INSTALL.md', 'LICENSE', 'README.md')) {
		$p = Join-Path $src $doc
		if (Test-Path -LiteralPath $p) { Copy-Item -LiteralPath $p -Destination $InstallDir -Force }
	}
	$version = (& (Join-Path $Bin 'twinnel.exe') version --short 2>$null)
	Set-Content -LiteralPath (Join-Path $InstallDir $Marker) -Value "$version" -Encoding ASCII
}
Step 'clear the downloaded-file mark on the installed programs (unsigned build)' {
	Get-ChildItem -LiteralPath $InstallDir -Recurse -File -Include '*.exe' | Unblock-File
}
Step "add the Start menu shortcut $Shortcut" {
	$shell = New-Object -ComObject WScript.Shell
	$lnk = $shell.CreateShortcut($Shortcut)
	$lnk.TargetPath = Join-Path $InstallDir 'Twinnel.exe'
	$lnk.WorkingDirectory = $InstallDir
	$lnk.Description = 'Twinnel'
	$lnk.Save()
}
$parts = Get-UserPath
if (-not ($parts | Where-Object { $_.TrimEnd('\') -ieq $Bin })) {
	Step "add $Bin to the user's PATH (new terminals find twinnel)" { Set-UserPath (@($parts) + $Bin) }
}
if (-not $NoLaunch) {
	Step 'start Twinnel' { Start-Process -FilePath (Join-Path $InstallDir 'Twinnel.exe') -WorkingDirectory $InstallDir }
}
Write-Host "Twinnel is installed in $InstallDir. Its icon is in the notification area of the taskbar."
