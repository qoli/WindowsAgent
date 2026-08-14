# Transactionally validates and replaces the installed WindowsAgent binaries.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$PayloadRoot,
    [Parameter(Mandatory = $true)][ValidatePattern('^[0-9a-f]{32}$')][string]$DeploymentId,
    [Parameter(Mandatory = $true)][ValidatePattern('^[0-9a-f]{64}$')][string]$PayloadSha256,
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [bool]$ValidateOnly = $false,
    [ValidateRange(5, 300)][int]$TimeoutSeconds = 45
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$watchdogTaskName = "gameGuide Windows Watchdog"
$watchdogDescription = "gameGuide external process watchdog; no automatic self-recovery"
$expectedNames = @(
    "windows-capture-agent.exe", "windows-wgc-worker.exe", "windows-event-stream.exe",
    "windows-action-osd.exe", "windows-watchdog.exe", "windows-observer.exe",
    "windows-observation-script-runner.exe", "windows-observation-job.exe",
    "windows-evidence-recorder.exe", "windows-visual-log.exe"
)

function Get-Sha256 {
    param([Parameter(Mandatory = $true)][string]$Path)
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Wait-PathStopped {
    param([Parameter(Mandatory = $true)][string]$ExecutablePath)
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $running = @(Get-Process -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -eq $ExecutablePath })
        if ($running.Count -eq 0) { return }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "process did not stop before binary update: $ExecutablePath"
}

function Get-TaskSnapshot {
    param(
        [Parameter(Mandatory = $true)][string]$TaskName,
        [Parameter(Mandatory = $true)][string]$ExpectedDescription,
        [string]$OwnershipLabel = "Scheduled Task"
    )
    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction Stop
    if ([string]$task.Description -cne $ExpectedDescription -or @($task.Actions).Count -ne 1) {
        if ($OwnershipLabel -ceq "target Scheduled Task") {
            throw "target Scheduled Task ownership mismatch: $TaskName"
        }
        throw "$OwnershipLabel ownership mismatch: $TaskName"
    }
    $info = Get-ScheduledTaskInfo -TaskName $TaskName -ErrorAction Stop
    return [ordered]@{
        name = $TaskName
        description = [string]$task.Description
        execute = [string]$task.Actions[0].Execute
        arguments = [string]$task.Actions[0].Arguments
        state = $task.State.ToString()
        last_task_result = [long]$info.LastTaskResult
        last_run_time = $info.LastRunTime.ToUniversalTime().ToString("o")
        action_preserved = $true
    }
}

function Test-TaskActionPreserved {
    param([Parameter(Mandatory = $true)]$Before, [Parameter(Mandatory = $true)]$After)
    return (
        [string]$After.description -ceq [string]$Before.description -and
        [string]$After.execute -ceq [string]$Before.execute -and
        [string]$After.arguments -ceq [string]$Before.arguments
    )
}

function Get-TargetState {
    param([Parameter(Mandatory = $true)]$target)
    $processProbes = @()
    $httpProbes = @()
    $lastError = $null
    $healthy = $true
    foreach ($probe in @($target.probes)) {
        switch ([string]$probe.type) {
            "process" {
                $entry = [ordered]@{
                    executable_path = [string]$probe.executablePath
                    require_interactive_session = [bool]$probe.requireInteractiveSession
                    process_count = 0
                    process_id = $null
                    session_id = $null
                    healthy = $false
                    error = $null
                }
                try {
                    $matches = @(Get-Process -ErrorAction Stop |
                        Where-Object { $_.Path -eq [string]$probe.executablePath })
                    $entry.process_count = $matches.Count
                    if ($matches.Count -gt 0) {
                        $entry.process_id = [long]$matches[0].Id
                        $entry.session_id = [int]$matches[0].SessionId
                    }
                    $entry.healthy = ($matches.Count -eq 1 -and
                        (-not [bool]$probe.requireInteractiveSession -or $matches[0].SessionId -ne 0))
                    if (-not $entry.healthy) {
                        $entry.error = "expected exactly one matching process in the required session"
                    }
                } catch {
                    $entry.error = $_.Exception.Message
                }
                if (-not $entry.healthy) {
                    $healthy = $false
                    $lastError = [string]$entry.error
                }
                $processProbes += [pscustomobject]$entry
            }
            "http-json" {
                $entry = [ordered]@{
                    url = [string]$probe.url
                    expected_status_code = [int]$probe.expectedStatusCode
                    expected_json_status = [string]$probe.expectedJSONStatus
                    status_code = $null
                    json_status = $null
                    healthy = $false
                    error = $null
                }
                try {
                    $response = Invoke-WebRequest -Uri ([string]$probe.url) -UseBasicParsing `
                        -TimeoutSec ([Math]::Max(1, [Math]::Ceiling([uint64]$probe.timeoutMs / 1000)))
                    $entry.status_code = [int]$response.StatusCode
                    $body = $response.Content | ConvertFrom-Json -ErrorAction Stop
                    $entry.json_status = [string]$body.status
                    $entry.healthy = ($entry.status_code -eq $entry.expected_status_code -and
                        $entry.json_status -ceq $entry.expected_json_status)
                    if (-not $entry.healthy) {
                        $entry.error = "HTTP status or JSON status did not match the configured probe"
                    }
                } catch {
                    $responseProperty = $_.Exception.PSObject.Properties["Response"]
                    if ($null -ne $responseProperty -and $null -ne $responseProperty.Value) {
                        $entry.status_code = [int]$responseProperty.Value.StatusCode
                    }
                    $entry.error = $_.Exception.Message
                }
                if (-not $entry.healthy) {
                    $healthy = $false
                    $lastError = [string]$entry.error
                }
                $httpProbes += [pscustomobject]$entry
            }
            default { throw "unsupported installed Watchdog probe type: $($probe.type)" }
        }
    }
    return [ordered]@{
        id = [string]$target.id
        state = $(if ($healthy) { "HEALTHY" } else { "UNHEALTHY" })
        process_probes = @($processProbes)
        http_probes = @($httpProbes)
        last_error = $lastError
    }
}

function Get-RuntimeState {
    param(
        [Parameter(Mandatory = $true)][object[]]$Targets,
        [Parameter(Mandatory = $true)][string]$WatchdogPath,
        [Parameter(Mandatory = $true)][hashtable]$TargetTaskNames
    )
    $watchdogProcesses = @(Get-Process -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $WatchdogPath })
    $watchdogHealthy = ($watchdogProcesses.Count -eq 1 -and $watchdogProcesses[0].SessionId -ne 0)
    $watchdogTask = Get-TaskSnapshot -TaskName $watchdogTaskName -ExpectedDescription $watchdogDescription
    $targetResults = @()
    $allHealthy = $watchdogHealthy
    foreach ($target in $Targets) {
        $targetResult = Get-TargetState -Target $target
        $taskSnapshot = Get-TaskSnapshot -TaskName $TargetTaskNames[[string]$target.id] `
            -ExpectedDescription ([string]$target.recovery.expectedTaskDescription)
        $targetResult["task"] = [pscustomobject]$taskSnapshot
        $targetResults += [pscustomobject]$targetResult
        if ([string]$targetResult.state -cne "HEALTHY") { $allHealthy = $false }
    }
    return [ordered]@{
        healthy = $allHealthy
        watchdog = [ordered]@{
            state = $(if ($watchdogHealthy) { "HEALTHY" } else { "UNHEALTHY" })
            executable_path = $WatchdogPath
            process_count = $watchdogProcesses.Count
            process_id = $(if ($watchdogProcesses.Count -gt 0) { [long]$watchdogProcesses[0].Id } else { $null })
            session_id = $(if ($watchdogProcesses.Count -gt 0) { [int]$watchdogProcesses[0].SessionId } else { $null })
            task = [pscustomobject]$watchdogTask
            last_error = $(if ($watchdogHealthy) { $null } else { "expected one interactive watchdog process" })
        }
        targets = @($targetResults)
    }
}

function Wait-RuntimeHealthy {
    param(
        [Parameter(Mandatory = $true)][object[]]$Targets,
        [Parameter(Mandatory = $true)][string]$WatchdogPath,
        [Parameter(Mandatory = $true)][hashtable]$TargetTaskNames
    )
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    $state = $null
    do {
        $state = Get-RuntimeState -Targets $Targets -WatchdogPath $WatchdogPath `
            -TargetTaskNames $TargetTaskNames
        if ($state.healthy) { return $state }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    return $state
}

function Assert-TaskActionsPreserved {
    param([Parameter(Mandatory = $true)][hashtable]$Before, [Parameter(Mandatory = $true)]$State)
    $preserved = $true
    $allTasks = @($State.watchdog.task) + @($State.targets | ForEach-Object { $_.task })
    foreach ($task in $allTasks) {
        $task.action_preserved = Test-TaskActionPreserved -Before $Before[[string]$task.name] -After $task
        if (-not $task.action_preserved) { $preserved = $false }
    }
    if (-not $preserved) { throw "binary deployment changed Scheduled Task configuration" }
    return $true
}

function Write-Receipt {
    param(
        [Parameter(Mandatory = $true)]$Receipt,
        [Parameter(Mandatory = $true)][string]$StageRoot,
        [string]$PersistentPath
    )
    $json = $Receipt | ConvertTo-Json -Depth 30
    $stagePath = Join-Path $StageRoot "deployment-receipt.json"
    [IO.File]::WriteAllText($stagePath + ".tmp", $json + "`n", [Text.UTF8Encoding]::new($false))
    Move-Item -LiteralPath ($stagePath + ".tmp") -Destination $stagePath -Force
    if (-not [string]::IsNullOrWhiteSpace($PersistentPath)) {
        New-Item -ItemType Directory -Path (Split-Path -Parent $PersistentPath) -Force | Out-Null
        [IO.File]::WriteAllText($PersistentPath + ".tmp", $json + "`n", [Text.UTF8Encoding]::new($false))
        Move-Item -LiteralPath ($PersistentPath + ".tmp") -Destination $PersistentPath -Force
    }
    return ($Receipt | ConvertTo-Json -Depth 30 -Compress)
}

$payload = [IO.Path]::GetFullPath($PayloadRoot)
$root = [IO.Path]::GetFullPath($DataDir)
$persistentReceipt = if ($ValidateOnly) {
    $null
} else {
    Join-Path $root ("deployments\binaries\receipts\" + $DeploymentId + ".json")
}
$receipt = [ordered]@{
    schema_version = 1
    deployment_id = $DeploymentId
    payload_sha256 = $PayloadSha256
    started_at = [DateTime]::UtcNow.ToString("o")
    completed_at = $null
    status = "FAILED"
    phase = "initialize"
    failed_phase = $null
    validated_only = $ValidateOnly
    published = $false
    rollback_performed = $false
    rollback_verified = $false
    task_actions_preserved = $false
    transaction_path = $null
    backup_path = $null
    staging_retained = $true
    staging_cleanup = "caller_on_success"
    transaction_retention = "retained"
    binaries = @()
    preflight = $null
    final_state = $null
    error = $null
    rollback_error = $null
    failed_snapshot_errors = @()
    remote_receipt = $persistentReceipt
}

$targets = @()
$targetTaskNames = @{}
$targetTaskSnapshots = @{}
$destinations = @{}
$hashes = @{}
$previousHashes = @{}
$watchdogPath = $null
$transactionRoot = $null
$backupRoot = $null
$failedRoot = $null
$mutationStarted = $false
$runtimeStopped = $false
$exitCode = 0

try {
    $receipt.phase = "payload_validation"
    if (-not (Test-Path -LiteralPath $payload -PathType Container)) { throw "payload root is missing" }
    if ($root -ceq [IO.Path]::GetPathRoot($root) -or -not (Test-Path -LiteralPath $root -PathType Container)) {
        throw "installed data directory must be an existing bounded directory"
    }
    if (((Get-Item -LiteralPath $root).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "installed data directory must not be a reparse point"
    }
    $sumPath = Join-Path $payload "SHA256SUMS"
    $configPath = Join-Path $root "watchdog\config.json"
    if (-not (Test-Path -LiteralPath $sumPath -PathType Leaf)) { throw "SHA256SUMS is missing" }
    if ((Get-Sha256 -Path $sumPath) -cne $PayloadSha256) { throw "SHA256SUMS payload identity mismatch" }
    if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) { throw "installed Watchdog config is missing" }

    foreach ($line in Get-Content -LiteralPath $sumPath) {
        if ($line -notmatch '^([0-9a-f]{64})  ([A-Za-z0-9.-]+\.exe)$') { throw "invalid SHA256SUMS line: $line" }
        if ($hashes.ContainsKey($Matches[2])) { throw "duplicate SHA256SUMS binary: $($Matches[2])" }
        $hashes[$Matches[2]] = $Matches[1]
    }
    if ((($hashes.Keys | Sort-Object) -join "`n") -cne (($expectedNames | Sort-Object) -join "`n")) {
        throw "payload must contain exactly the ten deployed binaries"
    }
    foreach ($name in $expectedNames) {
        $source = Join-Path $payload $name
        if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw "payload binary is missing: $name" }
        if ((Get-Sha256 -Path $source) -cne $hashes[$name]) { throw "payload hash mismatch: $name" }
    }

    $receipt.phase = "installed_mapping"
    $watchdogSnapshot = Get-TaskSnapshot -TaskName $watchdogTaskName -ExpectedDescription $watchdogDescription
    $watchdogPath = [IO.Path]::GetFullPath($watchdogSnapshot.execute)
    $destinations["windows-watchdog.exe"] = $watchdogPath
    $targetTaskSnapshots[$watchdogTaskName] = $watchdogSnapshot
    $config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    $targets = @($config.targets)
    if ($targets.Count -eq 0) { throw "installed Watchdog config has no targets" }

    foreach ($target in $targets) {
        $targetID = [string]$target.id
        $taskName = [string]$target.recovery.scheduledTaskName
        if ($targetTaskNames.ContainsKey($targetID)) { throw "duplicate Watchdog target id: $targetID" }
        if ($targetTaskSnapshots.ContainsKey($taskName)) { throw "duplicate Watchdog target Scheduled Task: $taskName" }
        $snapshot = Get-TaskSnapshot -TaskName $taskName `
            -ExpectedDescription ([string]$target.recovery.expectedTaskDescription) `
            -OwnershipLabel "target Scheduled Task"
        $execute = [IO.Path]::GetFullPath($snapshot.execute)
        $name = [IO.Path]::GetFileName($execute)
        if ($destinations.ContainsKey($name)) { throw "duplicate installed executable: $name" }
        $destinations[$name] = $execute
        $targetTaskNames[$targetID] = $taskName
        $targetTaskSnapshots[$taskName] = $snapshot
    }

    $capturePath = $destinations["windows-capture-agent.exe"]
    if (-not $capturePath) { throw "Watchdog config does not identify windows-capture-agent.exe" }
    $binDir = Split-Path -Parent $capturePath
    foreach ($name in @("windows-wgc-worker.exe", "windows-observer.exe", "windows-observation-script-runner.exe", "windows-observation-job.exe")) {
        if ($destinations.ContainsKey($name)) { throw "Watchdog target unexpectedly owns internal binary: $name" }
        $destinations[$name] = Join-Path $binDir $name
    }
    if ((($destinations.Keys | Sort-Object) -join "`n") -cne (($expectedNames | Sort-Object) -join "`n")) {
        throw "installed Watchdog tasks do not map the complete binary set"
    }

    $binaryReceipt = @()
    $rootPrefix = $root.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    foreach ($name in $expectedNames) {
        $destination = [IO.Path]::GetFullPath([string]$destinations[$name])
        if (-not $destination.StartsWith($rootPrefix, [StringComparison]::OrdinalIgnoreCase)) {
            throw "installed binary escaped the bounded data directory: $destination"
        }
        if (-not (Test-Path -LiteralPath $destination -PathType Leaf)) { throw "installed binary is missing: $destination" }
        if (((Get-Item -LiteralPath $destination).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "installed binary must not be a reparse point: $destination"
        }
        $previousHashes[$name] = Get-Sha256 -Path $destination
        $binaryReceipt += [pscustomobject][ordered]@{
            name = $name
            path = $destination
            previous_sha256 = $previousHashes[$name]
            payload_sha256 = $hashes[$name]
            final_sha256 = $previousHashes[$name]
            restored = $false
        }
    }
    $receipt.binaries = @($binaryReceipt)
    $receipt.phase = "preflight"
    $preflight = Get-RuntimeState -Targets $targets -WatchdogPath $watchdogPath -TargetTaskNames $targetTaskNames
    [void](Assert-TaskActionsPreserved -Before $targetTaskSnapshots -State $preflight)
    $receipt.preflight = $preflight
    $receipt.task_actions_preserved = $true
    if (-not $preflight.healthy) { throw "installed runtime is unhealthy before deployment; inspect receipt preflight details" }

    if ($ValidateOnly) {
        $receipt.status = "VALIDATED"
        $receipt.phase = "complete"
        $receipt.final_state = $preflight
    } else {
        $receipt.phase = "backup"
        $transactionRoot = Join-Path $root ("deployments\binaries\transactions\" + $DeploymentId)
        $backupRoot = Join-Path $transactionRoot "backup"
        $failedRoot = Join-Path $transactionRoot "failed"
        if (Test-Path -LiteralPath $transactionRoot) { throw "binary deployment transaction already exists: $transactionRoot" }
        New-Item -ItemType Directory -Path $backupRoot -Force | Out-Null
        $receipt.transaction_path = $transactionRoot
        $receipt.backup_path = $backupRoot
        foreach ($name in $expectedNames) {
            $backup = Join-Path $backupRoot $name
            Copy-Item -LiteralPath $destinations[$name] -Destination $backup
            if ((Get-Sha256 -Path $backup) -cne $previousHashes[$name]) { throw "backup hash mismatch: $name" }
        }

        $receipt.phase = "stop_runtime"
        Stop-ScheduledTask -TaskName $watchdogTaskName -ErrorAction SilentlyContinue
        $runtimeStopped = $true
        Wait-PathStopped -ExecutablePath $watchdogPath
        foreach ($taskName in $targetTaskNames.Values) { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue }
        foreach ($name in $expectedNames) { Wait-PathStopped -ExecutablePath $destinations[$name] }
        $mutationStarted = $true

        $receipt.phase = "replace_binaries"
        foreach ($name in $expectedNames) {
            Copy-Item -LiteralPath (Join-Path $payload $name) -Destination $destinations[$name] -Force
            if ((Get-Sha256 -Path $destinations[$name]) -cne $hashes[$name]) { throw "installed hash mismatch: $name" }
        }

        $receipt.phase = "configure_crash_dumps"
        $dumpDir = Join-Path $root "dumps"
        New-Item -ItemType Directory -Path $dumpDir -Force | Out-Null
        foreach ($processName in @("windows-capture-agent.exe", "windows-wgc-worker.exe")) {
            $dumpRegistryPath = "HKCU:\Software\Microsoft\Windows\Windows Error Reporting\LocalDumps\$processName"
            New-Item -Path $dumpRegistryPath -Force | Out-Null
            New-ItemProperty -Path $dumpRegistryPath -Name "DumpFolder" -PropertyType ExpandString -Value $dumpDir -Force | Out-Null
            New-ItemProperty -Path $dumpRegistryPath -Name "DumpType" -PropertyType DWord -Value 2 -Force | Out-Null
            New-ItemProperty -Path $dumpRegistryPath -Name "DumpCount" -PropertyType DWord -Value 5 -Force | Out-Null
        }

        $receipt.phase = "start_and_verify"
        Start-ScheduledTask -TaskName $watchdogTaskName -ErrorAction Stop
        $runtimeStopped = $false
        $finalState = Wait-RuntimeHealthy -Targets $targets -WatchdogPath $watchdogPath -TargetTaskNames $targetTaskNames
        [void](Assert-TaskActionsPreserved -Before $targetTaskSnapshots -State $finalState)
        $receipt.final_state = $finalState
        if (-not $finalState.healthy) { throw "Watchdog recovery timed out; inspect receipt final_state target and probe errors" }
        foreach ($binary in $receipt.binaries) { $binary.final_sha256 = Get-Sha256 -Path $binary.path }
        $receipt.status = "SUCCEEDED"
        $receipt.published = $true
        $receipt.phase = "complete"
    }
} catch {
    $failure = $_
    $receipt.failed_phase = $receipt.phase
    $receipt.error = $failure.Exception.Message
    $receipt.status = "FAILED"
    $exitCode = 1
    if ($mutationStarted) {
        $receipt.phase = "rollback"
        try {
            Stop-ScheduledTask -TaskName $watchdogTaskName -ErrorAction SilentlyContinue
            if ($watchdogPath) { Wait-PathStopped -ExecutablePath $watchdogPath }
            foreach ($taskName in $targetTaskNames.Values) { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue }
            foreach ($name in $expectedNames) { Wait-PathStopped -ExecutablePath $destinations[$name] }
            $canSnapshotFailed = $true
            try {
                New-Item -ItemType Directory -Path $failedRoot -Force | Out-Null
            } catch {
                $canSnapshotFailed = $false
                $receipt.failed_snapshot_errors += "create failed snapshot directory: $($_.Exception.Message)"
            }
            foreach ($name in $expectedNames) {
                if ($canSnapshotFailed -and (Test-Path -LiteralPath $destinations[$name] -PathType Leaf)) {
                    try {
                        Copy-Item -LiteralPath $destinations[$name] -Destination (Join-Path $failedRoot $name) -Force
                    } catch {
                        $receipt.failed_snapshot_errors += "snapshot ${name}: $($_.Exception.Message)"
                    }
                }
                Copy-Item -LiteralPath (Join-Path $backupRoot $name) -Destination $destinations[$name] -Force
                if ((Get-Sha256 -Path $destinations[$name]) -cne $previousHashes[$name]) { throw "rollback hash mismatch: $name" }
            }
            Start-ScheduledTask -TaskName $watchdogTaskName -ErrorAction Stop
            $runtimeStopped = $false
            $rollbackState = Wait-RuntimeHealthy -Targets $targets -WatchdogPath $watchdogPath -TargetTaskNames $targetTaskNames
            [void](Assert-TaskActionsPreserved -Before $targetTaskSnapshots -State $rollbackState)
            $receipt.final_state = $rollbackState
            if (-not $rollbackState.healthy) { throw "previous runtime did not recover before rollback deadline" }
            foreach ($binary in $receipt.binaries) {
                $binary.final_sha256 = Get-Sha256 -Path $binary.path
                $binary.restored = ($binary.final_sha256 -ceq $binary.previous_sha256)
            }
            $receipt.rollback_performed = $true
            $receipt.rollback_verified = $true
            $receipt.task_actions_preserved = $true
            $receipt.status = "ROLLED_BACK"
            $receipt.phase = "rollback_complete"
        } catch {
            $receipt.rollback_performed = $true
            $receipt.rollback_error = $_.Exception.Message
            $receipt.status = "ROLLBACK_FAILED"
        }
    } elseif ($runtimeStopped -and $watchdogPath) {
        try {
            Start-ScheduledTask -TaskName $watchdogTaskName -ErrorAction Stop
            $runtimeStopped = $false
            $receipt.final_state = Wait-RuntimeHealthy -Targets $targets -WatchdogPath $watchdogPath -TargetTaskNames $targetTaskNames
        } catch {
            $receipt.rollback_error = "restart after pre-mutation failure: $($_.Exception.Message)"
        }
    }
} finally {
    $receipt.completed_at = [DateTime]::UtcNow.ToString("o")
}

try {
    Write-Output (Write-Receipt -Receipt $receipt -StageRoot $payload -PersistentPath $persistentReceipt)
} catch {
    $receipt.error = "$($receipt.error); receipt write failed: $($_.Exception.Message)"
    Write-Output ($receipt | ConvertTo-Json -Depth 30 -Compress)
    $exitCode = 1
}
exit $exitCode
