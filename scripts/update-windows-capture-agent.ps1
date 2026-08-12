# Transactionally updates an existing interactive Windows Capture Agent task.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [string]$TaskName = "gameGuide Windows Capture Agent",
    [uri]$HealthURI = "http://127.0.0.1:8787/healthz",
    [timespan]$Timeout = ([timespan]::FromSeconds(20))
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$taskDescription = "gameGuide Go WGC screenshot agent; interactive-user session required"

function Get-WindowsPESubsystem {
    param([Parameter(Mandatory = $true)][string]$Path)
    $bytes = [IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt 64 -or $bytes[0] -ne 0x4D -or $bytes[1] -ne 0x5A) {
        throw "executable does not have a valid DOS header: $Path"
    }
    $peOffset = [BitConverter]::ToInt32($bytes, 0x3C)
    $optionalOffset = $peOffset + 24
    if ($peOffset -lt 0 -or $optionalOffset + 0x46 -gt $bytes.Length -or `
        $bytes[$peOffset] -ne 0x50 -or $bytes[$peOffset + 1] -ne 0x45 -or `
        $bytes[$peOffset + 2] -ne 0 -or $bytes[$peOffset + 3] -ne 0) {
        throw "executable does not have a valid PE header: $Path"
    }
    $magic = [BitConverter]::ToUInt16($bytes, $optionalOffset)
    if ($magic -ne 0x10B -and $magic -ne 0x20B) {
        throw ("executable has unsupported PE optional-header magic 0x{0:X}: {1}" -f $magic, $Path)
    }
    return [BitConverter]::ToUInt16($bytes, $optionalOffset + 0x44)
}

function Assert-GUIExecutable {
    param([Parameter(Mandatory = $true)][string]$Path)
    $subsystem = Get-WindowsPESubsystem -Path $Path
    if ($subsystem -ne 2) {
        throw ("persistent agent executable must use PE Windows GUI subsystem 2; " +
            "found subsystem $subsystem. Build it with -ldflags '-H=windowsgui': $Path")
    }
}

function Assert-ConsoleExecutable {
    param([Parameter(Mandatory = $true)][string]$Path)
    $subsystem = Get-WindowsPESubsystem -Path $Path
    if ($subsystem -ne 3) {
        throw "WGC worker executable must use PE console subsystem 3; found subsystem $subsystem: $Path"
    }
}

if ($Timeout -le [timespan]::Zero) {
    throw "Timeout must be positive"
}
if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}

$sourceExecutable = [IO.Path]::GetFullPath($ExecutablePath)
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    throw "agent executable does not exist: $sourceExecutable"
}
Assert-GUIExecutable -Path $sourceExecutable
$sourceHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $sourceExecutable).Hash.ToLowerInvariant()
$sourceWorker = Join-Path (Split-Path -Parent $sourceExecutable) "windows-wgc-worker.exe"
if (-not (Test-Path -LiteralPath $sourceWorker -PathType Leaf)) {
    throw "WGC worker executable does not exist beside the agent: $sourceWorker"
}
Assert-ConsoleExecutable -Path $sourceWorker
$sourceWorkerHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $sourceWorker).Hash.ToLowerInvariant()

$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$installedExecutable = Join-Path $resolvedDataDir "bin\windows-capture-agent.exe"
$installedWorker = Join-Path $resolvedDataDir "bin\windows-wgc-worker.exe"
if (-not (Test-Path -LiteralPath $installedExecutable -PathType Leaf)) {
    throw "installed agent executable does not exist: $installedExecutable"
}
$installedWorkerExists = Test-Path -LiteralPath $installedWorker -PathType Leaf
if ($installedWorkerExists) {
    Assert-ConsoleExecutable -Path $installedWorker
}

$task = Get-ScheduledTask -TaskName $TaskName -ErrorAction Stop
if ($task.Description -ne $taskDescription) {
    throw "scheduled task '$TaskName' is not owned by windows-capture-agent"
}
$taskActions = @($task.Actions)
if ($taskActions.Count -ne 1 -or `
    [IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($taskActions[0].Execute)) -ne $installedExecutable) {
    throw "scheduled task '$TaskName' does not execute the expected installed agent"
}

$installedHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash.ToLowerInvariant()
$installedWorkerHash = if ($installedWorkerExists) {
    (Get-FileHash -Algorithm SHA256 -LiteralPath $installedWorker).Hash.ToLowerInvariant()
} else {
    $null
}
if ($installedHash -eq $sourceHash -and $installedWorkerExists -and $installedWorkerHash -eq $sourceWorkerHash) {
    Assert-GUIExecutable -Path $installedExecutable
    $health = Invoke-RestMethod -Method Get -Uri $HealthURI -TimeoutSec 2
    if (-not $health -or $health.status -ne "ok") {
        throw "installed agent bytes match, but health is not ok at $HealthURI"
    }
    [ordered]@{
        changed = $false
        executable = $installedExecutable
        sha256 = $installedHash
        wgc_worker = $installedWorker
        wgc_worker_sha256 = $installedWorkerHash
        pe_subsystem = 2
        task_state = $task.State.ToString()
        health = $health.status
        backup = $null
    } | ConvertTo-Json
    return
}

$binDir = Split-Path -Parent $installedExecutable
$transactionID = [Guid]::NewGuid().ToString("N")
$incomingExecutable = Join-Path $binDir (".windows-capture-agent." + $transactionID + ".incoming.exe")
$incomingWorker = Join-Path $binDir (".windows-wgc-worker." + $transactionID + ".incoming.exe")
$backupExecutable = Join-Path $binDir (
    "windows-capture-agent.pre-update-" + [DateTime]::UtcNow.ToString("yyyyMMddTHHmmssZ") + ".exe"
)
$backupWorker = Join-Path $binDir (
    "windows-wgc-worker.pre-update-" + [DateTime]::UtcNow.ToString("yyyyMMddTHHmmssZ") + ".exe"
)
$agentReplaced = $false
$workerReplaced = $false
$taskStopped = $false
$rollbackHealth = $null

try {
    Copy-Item -LiteralPath $sourceExecutable -Destination $incomingExecutable
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $incomingExecutable).Hash.ToLowerInvariant() -ne $sourceHash) {
        throw "staged executable hash differs from source"
    }
    Assert-GUIExecutable -Path $incomingExecutable
    Copy-Item -LiteralPath $sourceWorker -Destination $incomingWorker
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $incomingWorker).Hash.ToLowerInvariant() -ne $sourceWorkerHash) {
        throw "staged WGC worker hash differs from source"
    }
    Assert-ConsoleExecutable -Path $incomingWorker
    Copy-Item -LiteralPath $installedExecutable -Destination $backupExecutable
    if ($installedWorkerExists) {
        Copy-Item -LiteralPath $installedWorker -Destination $backupWorker
    }

    Stop-ScheduledTask -TaskName $TaskName
    $taskStopped = $true
    $stopDeadline = [DateTime]::UtcNow.Add($Timeout)
    do {
        $listener = @(Get-NetTCPConnection -State Listen -LocalPort $HealthURI.Port -ErrorAction SilentlyContinue)
        if ($listener.Count -eq 0) { break }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $stopDeadline)
    if ($listener.Count -ne 0) {
        throw "agent listener on port $($HealthURI.Port) did not stop"
    }

    Move-Item -LiteralPath $incomingExecutable -Destination $installedExecutable -Force
    $agentReplaced = $true
    Move-Item -LiteralPath $incomingWorker -Destination $installedWorker -Force
    $workerReplaced = $true
    Start-ScheduledTask -TaskName $TaskName
    $taskStopped = $false

    $health = $null
    $healthDeadline = [DateTime]::UtcNow.Add($Timeout)
    do {
        try {
            $health = Invoke-RestMethod -Method Get -Uri $HealthURI -TimeoutSec 2
        } catch {
            $health = $null
        }
        if ($health -and $health.status -eq "ok") { break }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $healthDeadline)
    if (-not $health -or $health.status -ne "ok") {
        throw "updated agent did not become healthy at $HealthURI"
    }

    $listener = Get-NetTCPConnection -State Listen -LocalPort $HealthURI.Port -ErrorAction Stop |
        Select-Object -First 1
    $process = Get-Process -Id $listener.OwningProcess -ErrorAction Stop
    if ($process.Path -ne $installedExecutable -or $process.SessionId -eq 0) {
        throw "health listener is not the installed interactive-session agent"
    }
    $deployedHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash.ToLowerInvariant()
    if ($deployedHash -ne $sourceHash) {
        throw "installed executable hash differs from source"
    }
    $deployedWorkerHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $installedWorker).Hash.ToLowerInvariant()
    if ($deployedWorkerHash -ne $sourceWorkerHash) {
        throw "installed WGC worker hash differs from source"
    }

    [ordered]@{
        changed = $true
        executable = $installedExecutable
        sha256 = $deployedHash
        wgc_worker = $installedWorker
        wgc_worker_sha256 = $deployedWorkerHash
        pe_subsystem = 2
        process_id = $process.Id
        session_id = $process.SessionId
        task_state = (Get-ScheduledTask -TaskName $TaskName).State.ToString()
        health = $health.status
        backup = $backupExecutable
        worker_backup = if ($installedWorkerExists) { $backupWorker } else { $null }
    } | ConvertTo-Json
} catch {
    $updateError = $_
    if (($agentReplaced -or $workerReplaced) -and (Test-Path -LiteralPath $backupExecutable -PathType Leaf)) {
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        $taskStopped = $true
        $rollbackDeadline = [DateTime]::UtcNow.Add($Timeout)
        do {
            $listener = @(Get-NetTCPConnection -State Listen -LocalPort $HealthURI.Port -ErrorAction SilentlyContinue)
            if ($listener.Count -eq 0) { break }
            Start-Sleep -Milliseconds 200
        } while ([DateTime]::UtcNow -lt $rollbackDeadline)
        if ($agentReplaced) {
            Copy-Item -LiteralPath $backupExecutable -Destination $installedExecutable -Force
        }
        if ($workerReplaced -and (Test-Path -LiteralPath $backupWorker -PathType Leaf)) {
            Copy-Item -LiteralPath $backupWorker -Destination $installedWorker -Force
        } elseif ($workerReplaced -and -not $installedWorkerExists) {
            Remove-Item -LiteralPath $installedWorker -Force -ErrorAction SilentlyContinue
        }
        Start-ScheduledTask -TaskName $TaskName
        $taskStopped = $false
        $rollbackDeadline = [DateTime]::UtcNow.Add($Timeout)
        do {
            try {
                $rollbackHealth = Invoke-RestMethod -Method Get -Uri $HealthURI -TimeoutSec 2
            } catch {
                $rollbackHealth = $null
            }
            if ($rollbackHealth -and $rollbackHealth.status -eq "ok") { break }
            Start-Sleep -Milliseconds 250
        } while ([DateTime]::UtcNow -lt $rollbackDeadline)
    } elseif ($taskStopped) {
        Start-ScheduledTask -TaskName $TaskName
        $taskStopped = $false
    }
    $rollbackState = if ($rollbackHealth -and $rollbackHealth.status -eq "ok") { "rollback healthy" } else { "rollback not verified" }
    throw "agent update failed: $($updateError.Exception.Message); $rollbackState"
} finally {
    if (Test-Path -LiteralPath $incomingExecutable -PathType Leaf) {
        Remove-Item -LiteralPath $incomingExecutable -Force
    }
    if (Test-Path -LiteralPath $incomingWorker -PathType Leaf) {
        Remove-Item -LiteralPath $incomingWorker -Force
    }
}
