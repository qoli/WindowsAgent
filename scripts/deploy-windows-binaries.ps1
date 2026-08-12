# Replaces installed binaries without defining or changing Watchdog policy.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$PayloadRoot,
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [timespan]$Timeout = ([timespan]::FromSeconds(45))
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$watchdogTaskName = "gameGuide Windows Watchdog"
$watchdogDescription = "gameGuide external process watchdog; no automatic self-recovery"
$expectedNames = @(
    "windows-capture-agent.exe",
    "windows-event-stream.exe",
    "windows-action-osd.exe",
    "windows-watchdog.exe",
    "windows-observer.exe",
    "windows-observation-script-runner.exe",
    "windows-observation-job.exe",
    "windows-evidence-recorder.exe",
    "windows-visual-log.exe"
)

function Wait-PathStopped {
    param([Parameter(Mandatory = $true)][string]$ExecutablePath)
    $deadline = [DateTime]::UtcNow.Add($Timeout)
    do {
        $running = @(Get-Process -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -eq $ExecutablePath })
        if ($running.Count -eq 0) { return }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "process did not stop before binary update: $ExecutablePath"
}

$payload = [IO.Path]::GetFullPath($PayloadRoot)
$root = [IO.Path]::GetFullPath($DataDir)
$sumPath = Join-Path $payload "SHA256SUMS"
$configPath = Join-Path $root "watchdog\config.json"
if (-not (Test-Path -LiteralPath $sumPath -PathType Leaf)) { throw "SHA256SUMS is missing" }
if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) { throw "installed Watchdog config is missing" }

$hashes = @{}
foreach ($line in Get-Content -LiteralPath $sumPath) {
    if ($line -notmatch '^([0-9a-f]{64})  ([A-Za-z0-9.-]+\.exe)$') { throw "invalid SHA256SUMS line: $line" }
    $hashes[$Matches[2]] = $Matches[1]
}
if ((($hashes.Keys | Sort-Object) -join "`n") -cne (($expectedNames | Sort-Object) -join "`n")) {
    throw "payload must contain exactly the nine deployed binaries"
}
foreach ($name in $expectedNames) {
    $source = Join-Path $payload $name
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw "payload binary is missing: $name" }
    $actual = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -cne $hashes[$name]) { throw "payload hash mismatch: $name" }
}

$watchdogTask = Get-ScheduledTask -TaskName $watchdogTaskName -ErrorAction Stop
if ($watchdogTask.Description -ne $watchdogDescription -or @($watchdogTask.Actions).Count -ne 1) {
    throw "Watchdog Scheduled Task ownership mismatch"
}
$watchdogPath = [IO.Path]::GetFullPath($watchdogTask.Actions[0].Execute)
$config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
$targets = @($config.targets)
if ($targets.Count -eq 0) { throw "installed Watchdog config has no targets" }

$destinations = @{"windows-watchdog.exe" = $watchdogPath}
$targetTasks = @()
foreach ($target in $targets) {
    $taskName = [string]$target.recovery.scheduledTaskName
    $task = Get-ScheduledTask -TaskName $taskName -ErrorAction Stop
    if ($task.Description -ne [string]$target.recovery.expectedTaskDescription -or @($task.Actions).Count -ne 1) {
        throw "target Scheduled Task ownership mismatch: $taskName"
    }
    $execute = [IO.Path]::GetFullPath($task.Actions[0].Execute)
    $name = [IO.Path]::GetFileName($execute)
    if ($destinations.ContainsKey($name)) { throw "duplicate installed executable: $name" }
    $destinations[$name] = $execute
    $targetTasks += $taskName
}

$capturePath = $destinations["windows-capture-agent.exe"]
if (-not $capturePath) { throw "Watchdog config does not identify windows-capture-agent.exe" }
$binDir = Split-Path -Parent $capturePath
foreach ($name in @("windows-observer.exe", "windows-observation-script-runner.exe", "windows-observation-job.exe")) {
    $destinations[$name] = Join-Path $binDir $name
}
if ((($destinations.Keys | Sort-Object) -join "`n") -cne (($expectedNames | Sort-Object) -join "`n")) {
    throw "installed Watchdog tasks do not map the complete binary set"
}

$deploymentError = $null
try {
    Stop-ScheduledTask -TaskName $watchdogTaskName -ErrorAction SilentlyContinue
    Wait-PathStopped -ExecutablePath $watchdogPath
    foreach ($taskName in $targetTasks) { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue }
    foreach ($name in $destinations.Keys) { Wait-PathStopped -ExecutablePath $destinations[$name] }

    foreach ($name in $expectedNames) {
        Copy-Item -LiteralPath (Join-Path $payload $name) -Destination $destinations[$name] -Force
        $actual = (Get-FileHash -LiteralPath $destinations[$name] -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -cne $hashes[$name]) { throw "installed hash mismatch: $name" }
    }
} catch {
    $deploymentError = $_
} finally {
    Start-ScheduledTask -TaskName $watchdogTaskName -ErrorAction Stop
}
if ($deploymentError) { throw $deploymentError }

$deadline = [DateTime]::UtcNow.Add($Timeout)
$status = $null
$watchdogProcess = $null
$unhealthy = @("status-missing")
do {
    $statusPath = Join-Path $root "watchdog\status.json"
    $status = if (Test-Path -LiteralPath $statusPath) {
        Get-Content -LiteralPath $statusPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction SilentlyContinue
    }
    $watchdogProcess = Get-Process -Name "windows-watchdog" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $watchdogPath } | Select-Object -First 1
    $unhealthy = if ($status) { @($status.targets | Where-Object { $_.state -ne "HEALTHY" }) } else { @("status-missing") }
    if ($watchdogProcess -and $status -and $status.watchdog.pid -eq $watchdogProcess.Id -and $unhealthy.Count -eq 0) { break }
    Start-Sleep -Milliseconds 500
} while ([DateTime]::UtcNow -lt $deadline)
if (-not $watchdogProcess -or -not $status -or $unhealthy.Count -ne 0) {
    throw "Watchdog did not restore all configured targets before the deadline"
}

[ordered]@{
    watchdog_pid = $watchdogProcess.Id
    targets = @($status.targets | ForEach-Object { [ordered]@{id=$_.id; state=$_.state} })
    binaries = @($expectedNames | ForEach-Object {
        [ordered]@{name=$_; path=$destinations[$_]; sha256=$hashes[$_]}
    })
} | ConvertTo-Json -Depth 5
