Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$action = [string]$env:WINDOWSAGENT_LIFECYCLE_ACTION
if ($action -cnotin @("inspect", "start", "stop")) { throw "unsupported lifecycle action: $action" }
if (-not $env:LOCALAPPDATA) { throw "LOCALAPPDATA is required" }
$dataDir = [IO.Path]::GetFullPath([string]$env:WINDOWSAGENT_INSTALL_DATA_DIR)
$expectedRoot = [IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"))
if ($dataDir -cne $expectedRoot) { throw "lifecycle data directory must equal the current user's WindowsAgent directory" }

$binDir = Join-Path $dataDir "bin"
$catalogPath = Join-Path $binDir "windowsagent-release.json"
$configPath = Join-Path $dataDir "watchdog\config.json"
$statusPath = Join-Path $dataDir "watchdog\status.json"
$watchdogTaskName = "gameGuide Windows Watchdog"
$watchdogDescription = "gameGuide external process watchdog; no automatic self-recovery"
$watchdogExecutable = Join-Path $binDir "windows-watchdog.exe"
$tailscaleStatusPath = Join-Path $dataDir "tailscale\status.json"
$tailscaleExecutable = Join-Path $binDir "windows-tailscale-adapter.exe"

function Get-ExactPathProcesses([string[]]$Paths) {
    $wanted = @{}
    foreach ($path in $Paths) {
        if ([string]::IsNullOrWhiteSpace($path)) { continue }
        $wanted[[IO.Path]::GetFullPath($path)] = $true
    }
    $matches = @()
    foreach ($process in @(Get-Process -ErrorAction SilentlyContinue)) {
        $path = $null
        try { $path = $process.Path } catch { continue }
        if (-not $path) { continue }
        $full = [IO.Path]::GetFullPath($path)
        if ($wanted.ContainsKey($full)) {
            $matches += [pscustomobject]@{
                Name = [IO.Path]::GetFileName($full)
                Path = $full
                ProcessId = [int]$process.Id
                SessionId = [int]$process.SessionId
            }
        }
    }
    return @($matches)
}

function Wait-PathsStopped([string[]]$Paths, [int]$Seconds, [bool]$ForceAfterTimeout) {
    $deadline = [DateTime]::UtcNow.AddSeconds($Seconds)
    do {
        $running = @(Get-ExactPathProcesses -Paths $Paths)
        if ($running.Count -eq 0) { return }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($ForceAfterTimeout) {
        foreach ($process in $running) { Stop-Process -Id $process.ProcessId -Force -ErrorAction Stop }
        $forceDeadline = [DateTime]::UtcNow.AddSeconds(10)
        do {
            $running = @(Get-ExactPathProcesses -Paths $Paths)
            if ($running.Count -eq 0) { return }
            Start-Sleep -Milliseconds 250
        } while ([DateTime]::UtcNow -lt $forceDeadline)
    }
    $identities = @($running | ForEach-Object { "$($_.Name) pid=$($_.ProcessId)" }) -join ", "
    throw "verified WindowsAgent processes did not stop: $identities"
}

function Read-TailscaleState {
    if (-not (Test-Path -LiteralPath $tailscaleStatusPath -PathType Leaf)) { return "DISABLED" }
    $status = Get-Content -LiteralPath $tailscaleStatusPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if ([int]$status.schemaVersion -ne 1) { throw "TailscaleAdapter status identity is invalid" }
    return [string]$status.state
}

function Stop-TailscaleAdapter {
    if (-not (Test-Path -LiteralPath $tailscaleStatusPath -PathType Leaf)) {
        Wait-PathsStopped -Paths @($tailscaleExecutable) -Seconds 1 -ForceAfterTimeout $false
        return
    }
    $status = Get-Content -LiteralPath $tailscaleStatusPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if ([int]$status.schemaVersion -ne 1) { throw "TailscaleAdapter status identity is invalid" }
    $adapterProcesses = @(Get-ExactPathProcesses -Paths @($tailscaleExecutable))
    if ([bool]$status.enabled) {
        if ($adapterProcesses.Count -eq 0) { throw "TailscaleAdapter status is enabled but its verified process is absent" }
        $stopFile = Join-Path $dataDir "tailscale\stop.request"
        [IO.File]::WriteAllText($stopFile, "stop`n", [Text.UTF8Encoding]::new($false))
        $deadline = [DateTime]::UtcNow.AddSeconds(20)
        do {
            Start-Sleep -Milliseconds 250
            $status = Get-Content -LiteralPath $tailscaleStatusPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
            if (-not [bool]$status.enabled -and [string]$status.state -ceq "DISABLED") { break }
        } while ([DateTime]::UtcNow -lt $deadline)
        if ([bool]$status.enabled -or [string]$status.state -cne "DISABLED") {
            throw "TailscaleAdapter did not confirm logout and DISABLED state"
        }
    }
    Wait-PathsStopped -Paths @($tailscaleExecutable) -Seconds 20 -ForceAfterTimeout $false
}

$installed = Test-Path -LiteralPath $catalogPath -PathType Leaf
if (-not $installed) {
    if ($action -cne "inspect") { throw "installed release catalog is missing; run Install or Repair" }
    [ordered]@{
        installed = $false
        version = ""
        watchdogInstalled = $false
        watchdogRunning = $false
        watchdogStartAtSignIn = $false
        processes = @()
        tailscaleState = (Read-TailscaleState)
    } | ConvertTo-Json -Depth 6 -Compress
    exit 0
}

$catalog = Get-Content -LiteralPath $catalogPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
if ([int]$catalog.schemaVersion -ne 1 -or [string]$catalog.target -cne "windows-amd64") { throw "installed release catalog identity is invalid" }
foreach ($artifact in @($catalog.artifacts)) {
    $name = [string]$artifact.name
    if ([string]::IsNullOrWhiteSpace($name) -or [IO.Path]::GetFileName($name) -cne $name -or -not $name.EndsWith(".exe", [StringComparison]::Ordinal)) {
        throw "installed release artifact name is unsafe: $name"
    }
}
$config = $null
$targets = @()
if (Test-Path -LiteralPath $configPath -PathType Leaf) {
    $config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if ([int]$config.schemaVersion -ne 1 -or @($config.targets).Count -eq 0) { throw "installed Watchdog configuration identity is invalid" }
    $targets = @($config.targets)
} elseif ($action -ceq "start") {
    throw "installed Watchdog configuration is missing; run Repair"
}

$runtimeArtifactNames = @($catalog.artifacts | Where-Object {
    ([string]$_.class -ceq "runtime-required" -or [string]$_.class -ceq "runtime-optional") -and [string]$_.role -cne "tailscale-adapter"
} | ForEach-Object { [string]$_.name })
$runtimePaths = @($runtimeArtifactNames | ForEach-Object { Join-Path $binDir $_ })
$rootPrefix = $dataDir.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
$ownedTasks = [ordered]@{
    "gameGuide Windows Capture Agent" = "gameGuide Go WGC screenshot agent; interactive-user session required"
    "gameGuide Windows Event Stream" = "gameGuide durable local event stream; interactive-user session required"
}
foreach ($target in $targets) {
    $taskName = [string]$target.recovery.scheduledTaskName
    $description = [string]$target.recovery.expectedTaskDescription
    if ([string]::IsNullOrWhiteSpace($taskName) -or [string]::IsNullOrWhiteSpace($description)) { throw "Watchdog target Scheduled Task ownership is invalid" }
    if ($ownedTasks.Contains($taskName) -and [string]$ownedTasks[$taskName] -cne $description) { throw "Watchdog target conflicts with repository Scheduled Task ownership: $taskName" }
    $ownedTasks[$taskName] = $description
}
$tasks = @()
foreach ($entry in $ownedTasks.GetEnumerator()) {
    $taskName = [string]$entry.Key
    $description = [string]$entry.Value
    $task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ($null -eq $task) {
        if ($action -ceq "start") { throw "Watchdog target Scheduled Task is missing; run Repair: $taskName" }
        continue
    }
    if ([string]$task.Description -cne $description -or @($task.Actions).Count -ne 1) { throw "Watchdog target Scheduled Task ownership mismatch: $taskName" }
    $execute = [IO.Path]::GetFullPath([string]$task.Actions[0].Execute)
    if (-not $execute.StartsWith($rootPrefix, [StringComparison]::OrdinalIgnoreCase)) { throw "Watchdog target executable escaped the WindowsAgent data directory: $execute" }
    if ($execute.Equals([IO.Path]::GetFullPath($tailscaleExecutable), [StringComparison]::OrdinalIgnoreCase)) { throw "TailscaleAdapter must not be a Watchdog target" }
    $tasks += [pscustomobject]@{Name=$taskName; Executable=$execute}
}
$watchdogTask = Get-ScheduledTask -TaskName $watchdogTaskName -ErrorAction SilentlyContinue
$startAtSignIn = $false
if ($null -eq $watchdogTask) {
    if ($action -ceq "start") { throw "Watchdog Scheduled Task is missing; run Repair" }
} else {
    if ([string]$watchdogTask.Description -cne $watchdogDescription -or @($watchdogTask.Actions).Count -ne 1) { throw "Watchdog Scheduled Task ownership mismatch" }
    $watchdogAction = [IO.Path]::GetFullPath([string]$watchdogTask.Actions[0].Execute)
    if (-not $watchdogAction.Equals([IO.Path]::GetFullPath($watchdogExecutable), [StringComparison]::OrdinalIgnoreCase)) { throw "Watchdog Scheduled Task executable mismatch" }
    $startAtSignIn = @($watchdogTask.Triggers | Where-Object { $_.CimClass.CimClassName -ceq "MSFT_TaskLogonTrigger" }).Count -gt 0
}

if ($action -ceq "start") {
    Start-ScheduledTask -TaskName $watchdogTaskName -ErrorAction Stop
    $deadline = [DateTime]::UtcNow.AddSeconds(180)
    $lastDetail = "Watchdog status has not been published"
    $healthy = $false
    do {
        $watchdogProcesses = @(Get-ExactPathProcesses -Paths @($watchdogExecutable))
        if ($watchdogProcesses.Count -eq 1 -and $watchdogProcesses[0].SessionId -ne 0 -and (Test-Path -LiteralPath $statusPath -PathType Leaf)) {
            try {
                $status = Get-Content -LiteralPath $statusPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
                $healthy = ([int]$status.schemaVersion -eq 1 -and [int]$status.watchdog.pid -eq $watchdogProcesses[0].ProcessId)
                $states = @{}
                foreach ($targetStatus in @($status.targets)) { $states[[string]$targetStatus.id] = [string]$targetStatus.state }
                foreach ($target in $targets) {
					$targetID = [string]$target.id
					$targetState = [string]$states[$targetID]
                    if (-not $states.ContainsKey($targetID) -or $targetState -cne "HEALTHY") {
                        $healthy = $false
						$lastDetail = "target '$targetID' is '$targetState'"
                        break
                    }
                }
                if ($healthy) { break }
            } catch { $lastDetail = $_.Exception.Message }
        }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    if (-not $healthy) { throw "Watchdog recovery did not become healthy within 180 seconds: $lastDetail" }
}

if ($action -ceq "stop") {
    if ($null -ne $watchdogTask) { Stop-ScheduledTask -TaskName $watchdogTaskName -ErrorAction SilentlyContinue }
    Wait-PathsStopped -Paths @($watchdogExecutable) -Seconds 30 -ForceAfterTimeout $true
    foreach ($task in $tasks) { Stop-ScheduledTask -TaskName $task.Name -ErrorAction SilentlyContinue }
    $taskPaths = @($tasks | ForEach-Object { $_.Executable })
    $allRuntimePaths = @(($taskPaths + $runtimePaths) | Select-Object -Unique)
    Wait-PathsStopped -Paths $allRuntimePaths -Seconds 30 -ForceAfterTimeout $true
    Stop-TailscaleAdapter
}

$watchdogTask = Get-ScheduledTask -TaskName $watchdogTaskName -ErrorAction SilentlyContinue
$processes = @(Get-ExactPathProcesses -Paths @($runtimePaths))
$watchdogRunning = @($processes | Where-Object { $_.Path -ieq $watchdogExecutable }).Count -eq 1
if ($action -ceq "stop" -and $processes.Count -ne 0) { throw "WindowsAgent process read-back found running installed runtime processes" }
[ordered]@{
    installed = $true
    version = [string]$catalog.version
    watchdogInstalled = ($null -ne $watchdogTask)
    watchdogRunning = $watchdogRunning
    watchdogStartAtSignIn = $startAtSignIn
    processes = @($processes)
    tailscaleState = (Read-TailscaleState)
} | ConvertTo-Json -Depth 6 -Compress
