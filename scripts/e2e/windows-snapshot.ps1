<#
.SYNOPSIS
  Snapshot (and later verify) the Twinnel state of a Windows PC before a
  test that changes state.

.DESCRIPTION
  snapshot: writes into -Dir (default %ProgramData%\TwinnelQA\snap-<ts>):
    export.zip            config.export over the helper's pipe (holds secrets:
                          it stays on the PC, in an admin-only folder)
    settings.json, layers.json, status.json, doctor.json, version.txt
    dns.json              Get-DnsClientServerAddress of every adapter
    services.json         Twinnel*, Tailscale service states
  and prints the folder.

  verify -Dir <snap>: compares the PC with the snapshot and checks it works:
    services running as before, DNS server lists equal, doctor has no fail,
    HTTPS to a DIRECT target and to a PROXY target (through the TUN: no
    proxy set), sshd running. Exit 1 when anything does not match.

  It only reads (snapshot) or reads and fetches two URLs (verify). It does
  not restore: the test that changed something restores it, then runs verify.

.EXAMPLE
  pwsh -File windows-snapshot.ps1 snapshot
  pwsh -File windows-snapshot.ps1 verify -Dir C:\ProgramData\TwinnelQA\snap-20261128T120000
#>
param(
    [Parameter(Position = 0)][ValidateSet('snapshot', 'verify')][string]$Action = 'snapshot',
    [string]$Dir = '',
    [string]$Twinnel = (Join-Path $env:LOCALAPPDATA 'Programs\Twinnel\bin\twinnel.exe'),
    [string]$Pipe = 'twinnel-helper',
    [string]$DirectUrl = 'https://www.baidu.com',
    [string]$ProxyUrl = 'https://www.gstatic.com/generate_204'
)
$ErrorActionPreference = 'Continue'

# One JSON-RPC call over the helper's named pipe (newline-delimited JSON).
function Invoke-HelperRpc([string]$Method, $Params) {
    $c = New-Object System.IO.Pipes.NamedPipeClientStream('.', $Pipe, [System.IO.Pipes.PipeDirection]::InOut)
    $c.Connect(5000)
    try {
        $enc = New-Object System.Text.UTF8Encoding($false)
        $w = New-Object System.IO.StreamWriter($c, $enc); $w.AutoFlush = $true
        $r = New-Object System.IO.StreamReader($c, $enc)
        $req = @{ jsonrpc = '2.0'; id = 1; method = $Method }
        if ($null -ne $Params) { $req.params = $Params }
        $w.WriteLine(($req | ConvertTo-Json -Compress -Depth 10))
        $line = $r.ReadLine()
        $resp = $line | ConvertFrom-Json
        if ($resp.error) { throw "$Method : $($resp.error.message)" }
        return $resp.result
    } finally { $c.Dispose() }
}

function Get-DnsState {
    Get-DnsClientServerAddress -AddressFamily IPv4 |
        Sort-Object InterfaceAlias |
        ForEach-Object { [pscustomobject]@{ Alias = $_.InterfaceAlias; Servers = (@($_.ServerAddresses) -join ',') } }
}

function Get-ServiceState {
    Get-Service -Name 'Twinnel*', 'Tailscale', 'sshd' -ErrorAction SilentlyContinue |
        Sort-Object Name |
        ForEach-Object { [pscustomobject]@{ Name = $_.Name; Status = "$($_.Status)"; StartType = "$($_.StartType)" } }
}

if ($Action -eq 'snapshot') {
    if (-not $Dir) { $Dir = Join-Path $env:ProgramData ("TwinnelQA\snap-" + (Get-Date -Format 'yyyyMMddTHHmmss')) }
    New-Item -ItemType Directory -Force -Path $Dir | Out-Null
    try {
        $e = Invoke-HelperRpc 'config.export' @{}
        [IO.File]::WriteAllBytes((Join-Path $Dir 'export.zip'), [Convert]::FromBase64String($e.zip))
        "export.zip: layers=$($e.layers) subscriptions=$($e.subscriptions)"
    } catch { Write-Warning "export failed: $_" }
    & $Twinnel settings --json > (Join-Path $Dir 'settings.json') 2>&1
    & $Twinnel layer list --json > (Join-Path $Dir 'layers.json') 2>&1
    if ($LASTEXITCODE -ne 0) { & $Twinnel layer list > (Join-Path $Dir 'layers.txt') 2>&1 }
    & $Twinnel status --json > (Join-Path $Dir 'status.json') 2>&1
    & $Twinnel doctor --json > (Join-Path $Dir 'doctor.json') 2>&1
    & $Twinnel version > (Join-Path $Dir 'version.txt') 2>&1
    Get-DnsState | ConvertTo-Json | Set-Content -Path (Join-Path $Dir 'dns.json') -Encoding UTF8
    Get-ServiceState | ConvertTo-Json | Set-Content -Path (Join-Path $Dir 'services.json') -Encoding UTF8
    Write-Output "SNAPSHOT $Dir"
    exit 0
}

# verify
if (-not $Dir -or -not (Test-Path $Dir)) { Write-Error "verify needs -Dir <snapshot>"; exit 2 }
$fail = 0
function Check([bool]$Ok, [string]$What, [string]$Detail) {
    $s = if ($Ok) { 'PASS' } else { $script:fail++; 'FAIL' }
    Write-Output ("{0}  {1}  {2}" -f $s, $What, $Detail)
}

$want = Get-Content -Raw (Join-Path $Dir 'services.json') | ConvertFrom-Json
$have = Get-ServiceState
foreach ($w in @($want)) {
    $h = $have | Where-Object Name -eq $w.Name
    Check ($h -and $h.Status -eq $w.Status) "service $($w.Name)" "want $($w.Status) have $($h.Status)"
}
$wd = Get-Content -Raw (Join-Path $Dir 'dns.json') | ConvertFrom-Json
$hd = Get-DnsState
foreach ($w in @($wd)) {
    $h = $hd | Where-Object Alias -eq $w.Alias
    if (-not $h) { continue } # adapters come and go (TUN recreated)
    Check ($h.Servers -eq $w.Servers) "dns $($w.Alias)" "want [$($w.Servers)] have [$($h.Servers)]"
}
$doc = & $Twinnel doctor --json 2>$null | Out-String | ConvertFrom-Json -ErrorAction SilentlyContinue
if ($doc) {
    $checks = if ($doc.checks) { $doc.checks } elseif ($doc.Checks) { $doc.Checks } else { @($doc) }
    $bad = @($checks | Where-Object { ($_.status, $_.Status, $_.level, $_.Level) -contains 'fail' })
    Check ($bad.Count -eq 0) 'doctor' ("fail: " + (($bad | ForEach-Object { $_.id, $_.ID, $_.name } | Where-Object { $_ }) -join ','))
} else { Check $false 'doctor' 'no JSON' }
foreach ($u in @($DirectUrl, $ProxyUrl)) {
    try {
        $r = Invoke-WebRequest -Uri $u -TimeoutSec 10 -UseBasicParsing -NoProxy
        Check ($r.StatusCode -lt 400) "https $u" "$($r.StatusCode)"
    } catch { Check $false "https $u" "$($_.Exception.Message)" }
}
$sshd = Get-Service sshd -ErrorAction SilentlyContinue
Check ($sshd -and $sshd.Status -eq 'Running') 'sshd' "$($sshd.Status)"
if ($fail) { Write-Output "VERIFY FAIL ($fail)"; exit 1 }
Write-Output 'VERIFY OK'
