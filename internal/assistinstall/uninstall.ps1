Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not $env:LOCALAPPDATA) { throw "LOCALAPPDATA is required" }
$dataDir = [IO.Path]::GetFullPath($env:WINDOWSAGENT_INSTALL_DATA_DIR)
$expectedRoot = [IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"))
if ($dataDir -cne $expectedRoot) { throw "uninstall data directory must equal the current user's WindowsAgent directory" }
$binDir = Join-Path $dataDir "bin"
$catalogPath = Join-Path $binDir "windowsagent-release.json"
if (-not (Test-Path -LiteralPath $catalogPath -PathType Leaf)) {
    throw "installed release catalog is missing; run Repair before Uninstall"
}
$catalog = Get-Content -LiteralPath $catalogPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
if ([int]$catalog.schemaVersion -ne 1 -or [string]$catalog.target -cne "windows-amd64") {
    throw "installed release catalog identity is invalid"
}

$ownedTasks = [ordered]@{
    "gameGuide Windows Capture Agent" = "gameGuide Go WGC screenshot agent; interactive-user session required"
    "gameGuide Windows Event Stream" = "gameGuide durable local event stream; interactive-user session required"
    "gameGuide Windows Watchdog" = "gameGuide external process watchdog; no automatic self-recovery"
}
$watchdogConfigPath = Join-Path $dataDir "watchdog\config.json"
if (Test-Path -LiteralPath $watchdogConfigPath -PathType Leaf) {
    $watchdogConfig = Get-Content -LiteralPath $watchdogConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if ([int]$watchdogConfig.schemaVersion -ne 1) { throw "installed Watchdog configuration identity is invalid" }
    foreach ($target in @($watchdogConfig.targets)) {
        $taskName = [string]$target.recovery.scheduledTaskName
        $description = [string]$target.recovery.expectedTaskDescription
        if ([string]::IsNullOrWhiteSpace($taskName) -or [string]::IsNullOrWhiteSpace($description)) {
            throw "installed Watchdog target has an invalid Scheduled Task ownership contract"
        }
        if ($ownedTasks.Contains($taskName) -and [string]$ownedTasks[$taskName] -cne $description) {
            throw "installed Watchdog target conflicts with the WindowsAgent Scheduled Task contract: $taskName"
        }
        $ownedTasks[$taskName] = $description
    }
}
$tasks = @()
$rootPrefix = $dataDir.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
foreach ($entry in $ownedTasks.GetEnumerator()) {
    $task = Get-ScheduledTask -TaskName $entry.Key -ErrorAction SilentlyContinue
    if ($null -eq $task) { continue }
    if ([string]$task.Description -cne [string]$entry.Value -or @($task.Actions).Count -ne 1) {
        throw "scheduled task '$($entry.Key)' is not owned by WindowsAgent"
    }
    $execute = [IO.Path]::GetFullPath([string]$task.Actions[0].Execute)
    if (-not $execute.StartsWith($rootPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw "scheduled task '$($entry.Key)' executable escaped the WindowsAgent data directory"
    }
    $tasks += [pscustomobject]@{Name=[string]$entry.Key; Executable=$execute}
}

$tailscaleDir = Join-Path $dataDir "tailscale"
$tailscaleStatus = Join-Path $tailscaleDir "status.json"
if (Test-Path -LiteralPath $tailscaleStatus -PathType Leaf) {
    $status = Get-Content -LiteralPath $tailscaleStatus -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if ([bool]$status.enabled) {
        $stopFile = Join-Path $dataDir "tailscale\stop.request"
        [IO.File]::WriteAllText($stopFile, "stop`n", [Text.UTF8Encoding]::new($false))
        $deadline = [DateTime]::UtcNow.AddSeconds(20)
        do {
            Start-Sleep -Milliseconds 250
            $status = Get-Content -LiteralPath $tailscaleStatus -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
            if (-not [bool]$status.enabled -and [string]$status.state -ceq "DISABLED") { break }
        } while ([DateTime]::UtcNow -lt $deadline)
        if ([bool]$status.enabled -or [string]$status.state -cne "DISABLED") {
            throw "TailscaleAdapter did not confirm logout before uninstall"
        }
    }
}
$adapterExecutable = Join-Path $binDir "windows-tailscale-adapter.exe"
$adapterProcesses = @(Get-Process -Name "windows-tailscale-adapter" -ErrorAction SilentlyContinue | Where-Object {
    $_.Path -and [IO.Path]::GetFullPath($_.Path) -ceq [IO.Path]::GetFullPath($adapterExecutable)
})
if ($adapterProcesses.Count -ne 0) {
    throw "TailscaleAdapter is still running after logout request"
}
if (Test-Path -LiteralPath $tailscaleDir) {
    Remove-Item -LiteralPath $tailscaleDir -Recurse -Force -ErrorAction Stop
}

$watchdogTaskName = "gameGuide Windows Watchdog"
if ($tasks.Name -ccontains $watchdogTaskName) { Stop-ScheduledTask -TaskName $watchdogTaskName -ErrorAction SilentlyContinue }
foreach ($task in $tasks) {
    if ($task.Name -cne $watchdogTaskName) { Stop-ScheduledTask -TaskName $task.Name -ErrorAction SilentlyContinue }
}
$deadline = [DateTime]::UtcNow.AddSeconds(30)
do {
    $running = @()
    foreach ($task in $tasks) {
        $running += @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -and [IO.Path]::GetFullPath($_.Path) -ceq $task.Executable })
    }
    if ($running.Count -eq 0) { break }
    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $deadline)
if ($running.Count -ne 0) { throw "WindowsAgent processes did not stop before uninstall" }

foreach ($task in $tasks) {
    Unregister-ScheduledTask -TaskName $task.Name -Confirm:$false -ErrorAction Stop
}

$removed = @()
$installArtifactNames = @($catalog.artifacts | Where-Object {
    $_.class -ceq "bootstrap" -or $_.class -ceq "runtime-required" -or $_.class -ceq "runtime-optional"
} | ForEach-Object { [string]$_.name })
foreach ($task in $tasks) {
    $name = [IO.Path]::GetFileName($task.Executable)
    if ($name -cin $installArtifactNames -and (Test-Path -LiteralPath $task.Executable -PathType Leaf)) {
        Remove-Item -LiteralPath $task.Executable -Force -ErrorAction Stop
        $removed += $name
    }
}
foreach ($artifact in @($catalog.artifacts | Where-Object {
    $_.class -ceq "bootstrap" -or $_.class -ceq "runtime-required" -or $_.class -ceq "runtime-optional"
})) {
    $name = [string]$artifact.name
    if ([IO.Path]::GetFileName($name) -cne $name -or -not $name.EndsWith(".exe", [StringComparison]::Ordinal)) {
        throw "installed release artifact name is unsafe: $name"
    }
    $path = Join-Path $binDir $name
    if (Test-Path -LiteralPath $path -PathType Leaf) {
        Remove-Item -LiteralPath $path -Force -ErrorAction Stop
        $removed += $name
    }
}
foreach ($metadata in @("windowsagent-release.json", "SHA256SUMS")) {
    $path = Join-Path $binDir $metadata
    if (Test-Path -LiteralPath $path -PathType Leaf) { Remove-Item -LiteralPath $path -Force -ErrorAction Stop }
}
foreach ($processName in @("windows-capture-agent.exe", "windows-wgc-worker.exe")) {
    Remove-Item -LiteralPath ("HKCU:\Software\Microsoft\Windows\Windows Error Reporting\LocalDumps\" + $processName) -Recurse -Force -ErrorAction SilentlyContinue
}

[ordered]@{
    status = "UNINSTALLED"
    removedArtifacts = @($removed)
    removedTasks = @($tasks | ForEach-Object { $_.Name })
    dataPreserved = $true
    tailscaleStateRemoved = $true
    dataDir = $dataDir
} | ConvertTo-Json -Depth 5 -Compress
