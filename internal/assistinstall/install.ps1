Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$stage = [IO.Path]::GetFullPath($env:WINDOWSAGENT_RELEASE_STAGE)
$catalogPath = [IO.Path]::GetFullPath($env:WINDOWSAGENT_RELEASE_CATALOG)
$dataDir = [IO.Path]::GetFullPath($env:WINDOWSAGENT_INSTALL_DATA_DIR)
$operation = [string]$env:WINDOWSAGENT_SETUP_OPERATION
$watchdogStartAtLogonText = [string]$env:WINDOWSAGENT_WATCHDOG_START_AT_LOGON
if ($operation -cnotin @("install", "update", "repair")) { throw "setup operation must be install, update, or repair" }
if ($watchdogStartAtLogonText -cnotin @("true", "false")) { throw "Watchdog start-at-logon setting must be true or false" }
$watchdogStartAtLogon = $watchdogStartAtLogonText -ceq "true"
if (-not (Test-Path -LiteralPath $stage -PathType Container)) { throw "release stage directory does not exist" }
if (-not (Test-Path -LiteralPath $catalogPath -PathType Leaf)) { throw "release catalog does not exist" }
if (-not $env:LOCALAPPDATA) { throw "LOCALAPPDATA is required" }
$expectedRoot = [IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"))
if ($dataDir -cne $expectedRoot) { throw "install data directory must equal the current user's WindowsAgent directory" }

$catalog = Get-Content -LiteralPath $catalogPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
if ([int]$catalog.schemaVersion -ne 1 -or [string]$catalog.target -cne "windows-amd64") { throw "release catalog identity is invalid" }
$selected = @($catalog.artifacts | Where-Object {
    $_.class -ceq "bootstrap" -or $_.class -ceq "runtime-required" -or $_.class -ceq "runtime-optional"
})
if ($selected.Count -eq 0) { throw "release catalog selected no WindowsAgent install artifacts" }
foreach ($artifact in $selected) {
    $name = [string]$artifact.name
    if ([IO.Path]::GetFileName($name) -cne $name -or -not $name.EndsWith(".exe", [StringComparison]::Ordinal)) { throw "release artifact name is unsafe: $name" }
    $source = Join-Path $stage $name
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw "staged release artifact is missing: $name" }
    if ((Get-Item -LiteralPath $source).Length -ne [long]$artifact.bytes) { throw "staged release artifact size mismatch: $name" }
    if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$artifact.sha256) { throw "staged release artifact SHA-256 mismatch: $name" }
}
foreach ($metadataName in @("windowsagent-release.json", "SHA256SUMS")) {
    if (-not (Test-Path -LiteralPath (Join-Path $stage $metadataName) -PathType Leaf)) { throw "staged release metadata is missing: $metadataName" }
}

$agentTask = "gameGuide Windows Capture Agent"
$eventTask = "gameGuide Windows Event Stream"
$watchdogTask = "gameGuide Windows Watchdog"
$agentDescription = "gameGuide Go WGC screenshot agent; interactive-user session required"
$eventDescription = "gameGuide durable local event stream; interactive-user session required"
$watchdogDescription = "gameGuide external process watchdog; no automatic self-recovery"
$taskContracts = [ordered]@{$agentTask=$agentDescription; $eventTask=$eventDescription; $watchdogTask=$watchdogDescription}
$previousTaskXML = @{}
$previousTaskRunning = @{}
foreach ($entry in $taskContracts.GetEnumerator()) {
    $task = Get-ScheduledTask -TaskName $entry.Key -ErrorAction SilentlyContinue
    if ($null -eq $task) { continue }
    if ([string]$task.Description -cne [string]$entry.Value) { throw "scheduled task '$($entry.Key)' is not owned by WindowsAgent" }
    $previousTaskXML[$entry.Key] = Export-ScheduledTask -TaskName $entry.Key
    $previousTaskRunning[$entry.Key] = $task.State -ceq "Running"
}

$binDir = Join-Path $dataDir "bin"
$rulesDir = Join-Path $dataDir "Rules"
$installedAgent = Join-Path $binDir "windows-capture-agent.exe"
if ($operation -ceq "install" -and ((Test-Path -LiteralPath $installedAgent -PathType Leaf) -or $previousTaskXML.Count -ne 0)) { throw "WindowsAgent is already present; use Update or Repair" }
if ($operation -ceq "update" -and -not (Test-Path -LiteralPath $installedAgent -PathType Leaf)) { throw "WindowsAgent installation is incomplete; use Repair" }
if ($operation -ceq "update" -and (-not $previousTaskXML.ContainsKey($agentTask) -or -not $previousTaskXML.ContainsKey($eventTask) -or -not $previousTaskXML.ContainsKey($watchdogTask))) { throw "WindowsAgent installation is incomplete; use Repair" }
if ($operation -ceq "repair" -and -not (Test-Path -LiteralPath $installedAgent -PathType Leaf) -and $previousTaskXML.Count -eq 0 -and -not (Test-Path -LiteralPath (Join-Path $binDir "windowsagent-release.json") -PathType Leaf)) { throw "WindowsAgent is not installed; use Install" }
foreach ($directory in @($dataDir, $binDir, $rulesDir)) { New-Item -ItemType Directory -Path $directory -Force | Out-Null }
$bootstrapRules = Join-Path $stage "bootstrap-rules"
New-Item -ItemType Directory -Path $bootstrapRules -Force | Out-Null

$watchdogConfigSource = Join-Path $stage "watchdog-config.json"
$watchdogConfigInstalled = Join-Path $dataDir "watchdog\config.json"
$eventExecutable = Join-Path $binDir "windows-event-stream.exe"
$agentExecutable = Join-Path $binDir "windows-capture-agent.exe"
$watchdogConfig = [ordered]@{
    schemaVersion=1; checkIntervalMs=5000; targets=@(
        [ordered]@{id="event-stream"; desiredState="running"; startAfterHealthy=@(); failureThreshold=3; probes=@(
            [ordered]@{type="process"; executablePath=$eventExecutable; requireInteractiveSession=$true},
            [ordered]@{type="http-json"; url="http://127.0.0.1:8788/healthz"; timeoutMs=2000; expectedStatusCode=200; expectedJsonStatus="ok"}
        ); recovery=[ordered]@{scheduledTaskName=$eventTask; expectedTaskDescription=$eventDescription; maxAttempts=3; attemptWindowMs=300000; backoffMs=5000; actionTimeoutMs=20000; startupGraceMs=10000}},
        [ordered]@{id="capture-agent"; desiredState="running"; startAfterHealthy=@("event-stream"); failureThreshold=3; probes=@(
            [ordered]@{type="process"; executablePath=$agentExecutable; requireInteractiveSession=$true},
            [ordered]@{type="http-json"; url="http://127.0.0.1:8787/healthz"; timeoutMs=2000; expectedStatusCode=200; expectedJsonStatus="ok"}
        ); recovery=[ordered]@{scheduledTaskName=$agentTask; expectedTaskDescription=$agentDescription; maxAttempts=3; attemptWindowMs=300000; backoffMs=5000; actionTimeoutMs=20000; startupGraceMs=10000}}
    )
}
[IO.File]::WriteAllText($watchdogConfigSource, ($watchdogConfig | ConvertTo-Json -Depth 12), [Text.UTF8Encoding]::new($false))
$watchdogConfigForSetup = $watchdogConfigSource
if ($operation -in @("update", "repair") -and (Test-Path -LiteralPath $watchdogConfigInstalled -PathType Leaf)) {
    $watchdogConfigForSetup = $watchdogConfigInstalled
}

$repairTasks = @()
if ($operation -ceq "repair" -and (Test-Path -LiteralPath $watchdogConfigInstalled -PathType Leaf)) {
    $installedWatchdogConfig = Get-Content -LiteralPath $watchdogConfigInstalled -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if ([int]$installedWatchdogConfig.schemaVersion -ne 1) { throw "installed Watchdog configuration identity is invalid" }
    foreach ($target in @($installedWatchdogConfig.targets)) {
        $taskName = [string]$target.recovery.scheduledTaskName
        $description = [string]$target.recovery.expectedTaskDescription
        $task = Get-ScheduledTask -TaskName $taskName -ErrorAction Stop
        if ([string]$task.Description -cne $description -or @($task.Actions).Count -ne 1) { throw "Watchdog target Scheduled Task ownership mismatch: $taskName" }
        $execute = [IO.Path]::GetFullPath([string]$task.Actions[0].Execute)
        $dataPrefix = $dataDir.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
        if (-not $execute.StartsWith($dataPrefix, [StringComparison]::OrdinalIgnoreCase)) { throw "Watchdog target executable escaped the WindowsAgent data directory: $execute" }
        $repairTasks += [pscustomobject]@{Name=$taskName; Executable=$execute; WasRunning=($task.State -ceq "Running")}
    }
}

$transaction = [Guid]::NewGuid().ToString("N")
$backupDir = Join-Path (Join-Path $dataDir "backups") $transaction
New-Item -ItemType Directory -Path $backupDir -Force | Out-Null
$files = [Collections.Generic.List[object]]::new()
foreach ($artifact in $selected) { $files.Add([pscustomobject]@{Name=[string]$artifact.name; Source=(Join-Path $stage ([string]$artifact.name)); Destination=(Join-Path $binDir ([string]$artifact.name)); Sha256=[string]$artifact.sha256}) }
$selectedByName = @{}
foreach ($artifact in $selected) { $selectedByName[[string]$artifact.name] = $artifact }
foreach ($task in $repairTasks) {
    $name = [IO.Path]::GetFileName($task.Executable)
    if (-not $selectedByName.ContainsKey($name)) { continue }
    if (-not ($files | Where-Object { $_.Destination -ieq $task.Executable })) {
        $artifact = $selectedByName[$name]
        $files.Add([pscustomobject]@{Name=$name; Source=(Join-Path $stage $name); Destination=$task.Executable; Sha256=[string]$artifact.sha256})
    }
}
$files.Add([pscustomobject]@{Name="windowsagent-release.json"; Source=(Join-Path $stage "windowsagent-release.json"); Destination=(Join-Path $binDir "windowsagent-release.json"); Sha256=$null})
$files.Add([pscustomobject]@{Name="SHA256SUMS"; Source=(Join-Path $stage "SHA256SUMS"); Destination=(Join-Path $binDir "SHA256SUMS"); Sha256=$null})
$files.Add([pscustomobject]@{Name="watchdog-config.json"; Source=$watchdogConfigSource; Destination=$watchdogConfigInstalled; Sha256=$null})
$previousFiles = @{}
$backupIndex = 0
foreach ($file in $files) {
    if (Test-Path -LiteralPath $file.Destination -PathType Leaf) {
        $backup = Join-Path $backupDir (("{0:d3}-" -f $backupIndex) + $file.Name)
        Copy-Item -LiteralPath $file.Destination -Destination $backup
        $previousFiles[$file.Destination] = $backup
    }
    $backupIndex++
}
$eventTokenFile = Join-Path $dataDir "event-api.token"
$createdToken = -not (Test-Path -LiteralPath $eventTokenFile -PathType Leaf)

function Wait-ExecutableExit([string]$Path) {
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        $running = @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -and [IO.Path]::GetFullPath($_.Path) -ceq [IO.Path]::GetFullPath($Path) })
        if ($running.Count -eq 0) { return }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "installed executable is still running: $Path"
}

try {
    $existingWatchdog = Get-ScheduledTask -TaskName $watchdogTask -ErrorAction SilentlyContinue
    if ($existingWatchdog) {
        Stop-ScheduledTask -TaskName $watchdogTask -ErrorAction SilentlyContinue
        Wait-ExecutableExit (Join-Path $binDir "windows-watchdog.exe")
    }
    if ($operation -ceq "repair") {
        foreach ($task in $repairTasks) { Stop-ScheduledTask -TaskName $task.Name -ErrorAction SilentlyContinue }
        foreach ($task in $repairTasks) { Wait-ExecutableExit $task.Executable }
    }
    if ($operation -ceq "update") {
        & (Join-Path $PSScriptRoot "install-windows-watchdog.ps1") -ExecutablePath (Join-Path $binDir "windows-watchdog.exe") -ConfigPath $watchdogConfigInstalled -DataDir $dataDir -StartAtLogon:$watchdogStartAtLogon | Out-Null
        $deployedNames = @(
            "windows-capture-agent.exe", "windows-wgc-worker.exe", "windows-event-stream.exe",
            "windows-action-osd.exe", "windows-watchdog.exe", "windows-observer.exe",
            "windows-observation-script-runner.exe", "windows-observation-job.exe",
            "windows-evidence-recorder.exe", "windows-visual-log.exe", "windows-event-web.exe",
            "windows-sftp.exe"
        )
        foreach ($file in $files | Where-Object { $_.Name -cnotin $deployedNames -and $_.Name -ne "watchdog-config.json" }) {
            New-Item -ItemType Directory -Path (Split-Path -Parent $file.Destination) -Force | Out-Null
            Copy-Item -LiteralPath $file.Source -Destination $file.Destination -Force
            if ($null -ne $file.Sha256 -and (Get-FileHash -LiteralPath $file.Destination -Algorithm SHA256).Hash.ToLowerInvariant() -cne $file.Sha256) { throw "installed release artifact SHA-256 mismatch: $($file.Name)" }
        }
        $deployPayload = Join-Path $stage "deploy-payload"
        New-Item -ItemType Directory -Path $deployPayload -Force | Out-Null
        $sumLines = @()
        foreach ($name in $deployedNames) {
            $source = Join-Path $stage $name
            Copy-Item -LiteralPath $source -Destination (Join-Path $deployPayload $name) -Force
            $sumLines += ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant() + "  " + $name)
        }
        $deploySums = Join-Path $deployPayload "SHA256SUMS"
        [IO.File]::WriteAllText($deploySums, (($sumLines -join "`n") + "`n"), [Text.UTF8Encoding]::new($false))
        $deployPayloadSHA256 = (Get-FileHash -LiteralPath $deploySums -Algorithm SHA256).Hash.ToLowerInvariant()
        # Fresh unsigned release binaries can spend tens of seconds in first-run
        # Windows security inspection before the dependency-ordered targets are
        # healthy. The repository deployer already supports this bounded window.
        $deployOutput = & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "deploy-windows-binaries.ps1") -PayloadRoot $deployPayload -DeploymentId $transaction -PayloadSha256 $deployPayloadSHA256 -DataDir $dataDir -TimeoutSeconds 180 2>&1
        if ($LASTEXITCODE -ne 0) { throw "repository binary deployment failed: $($deployOutput -join [Environment]::NewLine)" }
        $deployReceipt = @($deployOutput)[-1] | ConvertFrom-Json -ErrorAction Stop
        if ([string]$deployReceipt.status -cne "SUCCEEDED" -or -not [bool]$deployReceipt.published -or -not [bool]$deployReceipt.task_actions_preserved) {
            throw "repository binary deployment did not report a preserved successful publication"
        }
    } else {
        & (Join-Path $PSScriptRoot "install-windows-capture-agent.ps1") -ExecutablePath (Join-Path $stage "windows-capture-agent.exe") -RulesPath $bootstrapRules -AllowEmptyRules -DataDir $dataDir -Listen "0.0.0.0:8787" -EventListen "127.0.0.1:8788" -StartupMode WatchdogManaged -AgentRunLevel Limited | Out-Null
        $installerOwnedNames = @("windows-capture-agent.exe", "windows-wgc-worker.exe", "windows-event-stream.exe", "windows-observation-job.exe", "windows-observation-script-runner.exe", "windows-observer.exe", "windows-watchdog.exe", "watchdog-config.json")
        foreach ($file in $files | Where-Object { $_.Name -cnotin $installerOwnedNames }) {
            New-Item -ItemType Directory -Path (Split-Path -Parent $file.Destination) -Force | Out-Null
            Copy-Item -LiteralPath $file.Source -Destination $file.Destination -Force
            if ($null -ne $file.Sha256 -and (Get-FileHash -LiteralPath $file.Destination -Algorithm SHA256).Hash.ToLowerInvariant() -cne $file.Sha256) { throw "installed release artifact SHA-256 mismatch: $($file.Name)" }
        }
        & (Join-Path $PSScriptRoot "install-windows-watchdog.ps1") -ExecutablePath (Join-Path $stage "windows-watchdog.exe") -ConfigPath $watchdogConfigForSetup -DataDir $dataDir -StartAtLogon:$watchdogStartAtLogon | Out-Null
        foreach ($artifact in $selected) {
            $installed = Join-Path $binDir ([string]$artifact.name)
            if ((Get-FileHash -LiteralPath $installed -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$artifact.sha256) { throw "installed release artifact SHA-256 mismatch: $($artifact.name)" }
        }
    }
    $health = Invoke-RestMethod -Method Get -Uri "http://127.0.0.1:8787/healthz" -TimeoutSec 2
    if (-not $health -or [string]$health.status -cne "ok") { throw "Capture Agent health verification failed after setup" }
    Remove-Item -LiteralPath $backupDir -Recurse -Force -ErrorAction SilentlyContinue
    [ordered]@{status=$operation.ToUpperInvariant(); version=[string]$catalog.version; watchdogStartAtLogon=$watchdogStartAtLogon; dataDir=$dataDir} | ConvertTo-Json -Compress
} catch {
    $failure = $_
    $rollbackErrors = [Collections.Generic.List[string]]::new()
    foreach ($task in $repairTasks) {
        try { Stop-ScheduledTask -TaskName $task.Name -ErrorAction SilentlyContinue; Wait-ExecutableExit $task.Executable }
        catch { $rollbackErrors.Add("stop repair target $($task.Name): $($_.Exception.Message)") }
    }
    $taskExecutables = @{
        $watchdogTask = (Join-Path $binDir "windows-watchdog.exe")
        $agentTask = $agentExecutable
        $eventTask = $eventExecutable
    }
    foreach ($taskName in @($watchdogTask, $agentTask, $eventTask)) {
        try {
            $task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
            if ($task) {
                Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
                Wait-ExecutableExit $taskExecutables[$taskName]
                Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
            }
            if ($previousTaskXML.ContainsKey($taskName)) { Register-ScheduledTask -TaskName $taskName -Xml $previousTaskXML[$taskName] -Force | Out-Null }
        } catch { $rollbackErrors.Add("restore task ${taskName}: $($_.Exception.Message)") }
    }
    foreach ($file in $files) {
        try {
            if ($previousFiles.ContainsKey($file.Destination)) { Copy-Item -LiteralPath $previousFiles[$file.Destination] -Destination $file.Destination -Force }
            else { Remove-Item -LiteralPath $file.Destination -Force -ErrorAction SilentlyContinue }
        } catch { $rollbackErrors.Add("restore $($file.Name): $($_.Exception.Message)") }
    }
    if ($createdToken -and (Test-Path -LiteralPath $eventTokenFile -PathType Leaf)) { try { Remove-Item -LiteralPath $eventTokenFile -Force -ErrorAction Stop } catch { $rollbackErrors.Add("remove new event token: $($_.Exception.Message)") } }
    if ($operation -ceq "install") {
        foreach ($processName in @("windows-capture-agent.exe", "windows-wgc-worker.exe")) {
            try { Remove-Item -LiteralPath ("HKCU:\Software\Microsoft\Windows\Windows Error Reporting\LocalDumps\" + $processName) -Recurse -Force -ErrorAction SilentlyContinue }
            catch { $rollbackErrors.Add("remove crash-dump registry for ${processName}: $($_.Exception.Message)") }
        }
    }
    foreach ($taskName in $previousTaskXML.Keys) {
        if (-not $previousTaskRunning[$taskName]) { continue }
        try { Start-ScheduledTask -TaskName $taskName -ErrorAction Stop } catch { $rollbackErrors.Add("restart task ${taskName}: $($_.Exception.Message)") }
    }
    foreach ($task in $repairTasks) {
        if (-not $task.WasRunning -or $task.Name -in @($agentTask, $eventTask, $watchdogTask)) { continue }
        try { Start-ScheduledTask -TaskName $task.Name -ErrorAction Stop } catch { $rollbackErrors.Add("restart repair target $($task.Name): $($_.Exception.Message)") }
    }
    if ($rollbackErrors.Count -ne 0) { throw "setup failed: $($failure.Exception.Message); rollback also failed: $($rollbackErrors -join '; ')" }
    Remove-Item -LiteralPath $backupDir -Recurse -Force -ErrorAction SilentlyContinue
    throw $failure
}
