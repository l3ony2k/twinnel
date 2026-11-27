<#
.SYNOPSIS
  Read-only check of a Twinnel install on Windows 10/11 (a VM or Windows
  Sandbox first, then a real machine).

.DESCRIPTION
  Only READS state: services, the helper's named pipe, adapters, routes, DNS
  client settings, NRPT rules, and `twinnel status|doctor|helper status
  --json`. It changes nothing and does not need an elevated prompt.

  Prints one row per step (PASS / FAIL / WARN / SKIP) and exits 1 when any
  step fails. Raw JSON goes to -Out (default $env:TEMP\twinnel-e2e); it names
  the tailnet and nodes, so keep it out of the repo and out of bug reports
  unless redacted.

  Works in Windows PowerShell 5.1 and PowerShell 7.

.PARAMETER Twinnel
  Path of twinnel.exe (default: $env:ProgramFiles\Twinnel\twinnel.exe, else
  twinnel on PATH).

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File .\scripts\e2e\windows-check.ps1
#>
param(
    [string]$Twinnel = "",
    [string]$Out = (Join-Path $env:TEMP "twinnel-e2e")
)

$ErrorActionPreference = "Continue"
New-Item -ItemType Directory -Force -Path $Out | Out-Null

$HelperService = "TwinnelHelper"
$MihomoServiceDefault = "TwinnelMihomo"
$TailscaleService = "Tailscale"
$PipeName = "twinnel-helper"

$rows = New-Object System.Collections.Generic.List[object]
function Row([string]$Status, [string]$Step, [string]$Detail) {
    $rows.Add([pscustomobject]@{ Result = $Status; Step = $Step; Detail = $Detail })
}

# Run twinnel with arguments; save stdout to $file. Returns $true on exit 0.
function Invoke-Twinnel([string[]]$Arguments, [string]$File) {
    if (-not $script:Twinnel) { return $false }
    $err = "$File.err"
    & $script:Twinnel @Arguments 1> $File 2> $err
    return ($LASTEXITCODE -eq 0)
}

function Read-Json([string]$File) {
    try { return (Get-Content -Raw -Path $File -ErrorAction Stop | ConvertFrom-Json) } catch { return $null }
}

function First-Line([string]$File) {
    if (Test-Path $File) { return (Get-Content -Path $File -TotalCount 1) }
    return ""
}

function Service-Row([string]$Step, [string]$Name) {
    $s = Get-Service -Name $Name -ErrorAction SilentlyContinue
    if ($null -eq $s) { Row "FAIL" $Step "$Name not installed" }
    elseif ($s.Status -eq "Running") { Row "PASS" $Step "$Name running ($($s.StartType))" }
    else { Row "FAIL" $Step "$Name $($s.Status)" }
}

function Route-Alias([string]$Address) {
    try {
        $r = Find-NetRoute -RemoteIPAddress $Address -ErrorAction Stop | Where-Object { $_.InterfaceAlias } | Select-Object -First 1
        return $r.InterfaceAlias
    } catch { return "" }
}

# ---- 0. the CLI ------------------------------------------------------------------

if (-not $Twinnel) {
    $p = Join-Path $env:ProgramFiles "Twinnel\twinnel.exe"
    if (Test-Path $p) { $Twinnel = $p }
    else {
        $c = Get-Command twinnel -ErrorAction SilentlyContinue
        if ($c) { $Twinnel = $c.Source }
    }
}
$script:Twinnel = $Twinnel
$vf = Join-Path $Out "version.txt"
if (Invoke-Twinnel @("version") $vf) { Row "PASS" "twinnel CLI runs" (First-Line $vf) }
else { Row "FAIL" "twinnel CLI runs" ($(if ($Twinnel) { $Twinnel } else { "twinnel.exe not found" })); $script:Twinnel = "" }

# ---- 1. helper ---------------------------------------------------------------------

Service-Row "helper service" $HelperService
$pipes = @()
try { $pipes = [System.IO.Directory]::GetFiles("\\.\pipe\") } catch { }
if ($pipes | Where-Object { $_ -like "*\$PipeName" }) { Row "PASS" "helper pipe" "\\.\pipe\$PipeName" }
else { Row "FAIL" "helper pipe" "\\.\pipe\$PipeName missing" }

$hsf = Join-Path $Out "helper-status.json"
$mihomoService = $MihomoServiceDefault
if (Invoke-Twinnel @("helper", "status", "--json", "--timeout", "15s") $hsf) {
    $hs = Read-Json $hsf
    Row "PASS" "helper answers" "version $($hs.version)"
    if ($hs.mihomo.label) { $mihomoService = $hs.mihomo.label }
} else { Row "FAIL" "helper answers" (First-Line "$hsf.err") }

# ---- 2. mihomo ------------------------------------------------------------------------

Service-Row "mihomo service" $mihomoService
$ssf = Join-Path $Out "status.json"
$ss = $null
if (Invoke-Twinnel @("status", "--json", "--timeout", "15s") $ssf) { $ss = Read-Json $ssf }
if ($ss -and $ss.Mihomo.Reachable) { Row "PASS" "mihomo controller" "mihomo $($ss.Mihomo.Version), mode $($ss.Mihomo.Config.mode)" }
else { Row "FAIL" "mihomo controller" "unreachable: $($ss.Mihomo.Err)" }

# ---- 3. TUN and routes -------------------------------------------------------------------

$dev = ""
if ($ss) { $dev = $ss.Mihomo.Config.tun.device }
if ($ss -and $ss.Mihomo.Config.tun.enable) { Row "PASS" "TUN enabled" "adapter $dev, stack $($ss.Mihomo.Config.tun.stack)" }
else { Row "FAIL" "TUN enabled" "mihomo reports tun.enable=false" }
$a = $null
if ($dev) { $a = Get-NetAdapter -Name $dev -ErrorAction SilentlyContinue }
if ($a -and $a.Status -eq "Up") { Row "PASS" "TUN adapter up" "$dev ($($a.InterfaceDescription))" }
else { Row "FAIL" "TUN adapter up" "$(if ($dev) { $dev } else { 'no adapter' }) not up" }
$ifc = Route-Alias "1.1.1.1"
if ($dev -and $ifc -eq $dev) { Row "PASS" "internet routed into TUN" "1.1.1.1 -> $ifc" }
else { Row "FAIL" "internet routed into TUN" "1.1.1.1 -> $(if ($ifc) { $ifc } else { 'no route' }) (TUN $dev)" }

# ---- 4. DNS ----------------------------------------------------------------------------------

$def = Get-NetRoute -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue |
    Where-Object { $_.InterfaceAlias -ne $dev -and $_.InterfaceAlias -notlike "Tailscale*" } |
    Sort-Object { $_.RouteMetric + $_.InterfaceMetric } | Select-Object -First 1
if ($def) {
    $dns = (Get-DnsClientServerAddress -InterfaceIndex $def.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue).ServerAddresses
    if ($dns -contains "127.0.0.1") { Row "PASS" "system DNS" "$($def.InterfaceAlias) -> 127.0.0.1" }
    else { Row "WARN" "system DNS" "$($def.InterfaceAlias) -> $($dns -join ', ') (127.0.0.1 not set; mihomo's dns-hijack may still catch it; see health: system-dns)" }
} else { Row "WARN" "system DNS" "no physical default route found" }
try {
    Resolve-DnsName -Name example.com -Type A -ErrorAction Stop | Out-Null
    Row "PASS" "name resolution" "example.com resolves"
} catch { Row "FAIL" "name resolution" "example.com does not resolve" }

# ---- 5. Tailscale -------------------------------------------------------------------------------

Service-Row "Tailscale service" $TailscaleService
if ($ss -and $ss.Tailscale.Reachable) {
    $bs = $ss.Tailscale.State.BackendState
    if ($bs -eq "Running") { Row "PASS" "Tailscale logged in" "BackendState Running" }
    else { Row "FAIL" "Tailscale logged in" "BackendState $bs" }
} else { Row "FAIL" "Tailscale LocalAPI" "unreachable: $($ss.Tailscale.Err)" }
$tif = Route-Alias "100.100.100.100"
if ($tif -like "Tailscale*") { Row "PASS" "tailnet bypasses mihomo TUN" "100.100.100.100 -> $tif" }
else { Row "FAIL" "tailnet bypasses mihomo TUN" "100.100.100.100 -> $(if ($tif) { $tif } else { 'no route' }) (want Tailscale)" }
$nrpt = @(Get-DnsClientNrptPolicy -ErrorAction SilentlyContinue | Where-Object { $_.NameServers -contains "100.100.100.100" })
if ($nrpt.Count -gt 0) { Row "PASS" "MagicDNS NRPT rules" "$($nrpt.Count) rule(s) -> 100.100.100.100" }
else { Row "WARN" "MagicDNS NRPT rules" "none (Use Tailscale DNS off?)" }

# ---- 6. overall status and health ------------------------------------------------------------------

switch ($(if ($ss) { $ss.Level } else { "" })) {
    "ok" { Row "PASS" "status level" "ok" }
    "warn" { Row "WARN" "status level" "warn" }
    default { Row "FAIL" "status level" "$(if ($ss) { $ss.Level } else { 'no snapshot' })" }
}
$djf = Join-Path $Out "doctor.json"
if (Invoke-Twinnel @("doctor", "--json") $djf) {
    $dj = Read-Json $djf
    foreach ($c in $dj.checks) {
        switch ($c.status) {
            "ok" { Row "PASS" "health: $($c.id)" $c.summary }
            "warn" { Row "WARN" "health: $($c.id)" $c.summary }
            "skip" { Row "SKIP" "health: $($c.id)" $c.summary }
            default { Row "FAIL" "health: $($c.id)" $c.summary }
        }
    }
    switch ($dj.overall) {
        "ok" { Row "PASS" "health overall" "ok ($(@($dj.checks).Count) checks)" }
        "warn" { Row "WARN" "health overall" "warn" }
        default { Row "FAIL" "health overall" "$($dj.overall)" }
    }
} else {
    $e = First-Line "$djf.err"
    if ($e -like "*not available*") { Row "SKIP" "health (twinnel doctor)" $e }
    else { Row "FAIL" "health (twinnel doctor)" $e }
}
$sf = Join-Path $Out "setup-status.json"
if (Invoke-Twinnel @("setup", "status", "--json") $sf) { Row "PASS" "setup status" "saved setup-status.json" }
else { Row "SKIP" "setup status" "not in this build" }

# ---- 7. app ------------------------------------------------------------------------------------------

$app = Get-Process -Name "Twinnel", "twinnel-app" -ErrorAction SilentlyContinue | Select-Object -First 1
if ($app) { Row "PASS" "app running" "$($app.ProcessName) pid $($app.Id)" }
else { Row "WARN" "app running" "no Twinnel process (open it to check the tray icon)" }

# ---- table ----------------------------------------------------------------------------------------------

$rows | Format-Table -AutoSize -Wrap | Out-String -Width 200 | Write-Output
$fails = @($rows | Where-Object { $_.Result -eq "FAIL" }).Count
$warns = @($rows | Where-Object { $_.Result -eq "WARN" }).Count
Write-Output "$fails failed, $warns warnings. Raw output: $Out"
if ($fails -gt 0) { exit 1 }
exit 0
