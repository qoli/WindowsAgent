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

$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$installedExecutable = Join-Path $resolvedDataDir "bin\windows-capture-agent.exe"
if (-not (Test-Path -LiteralPath $installedExecutable -PathType Leaf)) {
    throw "installed agent executable does not exist: $installedExecutable"
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
if ($installedHash -eq $sourceHash) {
    Assert-GUIExecutable -Path $installedExecutable
    $health = Invoke-RestMethod -Method Get -Uri $HealthURI -TimeoutSec 2
    if (-not $health -or $health.status -ne "ok") {
        throw "installed agent bytes match, but health is not ok at $HealthURI"
    }
    [ordered]@{
        changed = $false
        executable = $installedExecutable
        sha256 = $installedHash
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
$backupExecutable = Join-Path $binDir (
    "windows-capture-agent.pre-update-" + [DateTime]::UtcNow.ToString("yyyyMMddTHHmmssZ") + ".exe"
)
$replaced = $false
$taskStopped = $false
$rollbackHealth = $null

try {
    Copy-Item -LiteralPath $sourceExecutable -Destination $incomingExecutable
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $incomingExecutable).Hash.ToLowerInvariant() -ne $sourceHash) {
        throw "staged executable hash differs from source"
    }
    Assert-GUIExecutable -Path $incomingExecutable
    Copy-Item -LiteralPath $installedExecutable -Destination $backupExecutable

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
    $replaced = $true
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

    [ordered]@{
        changed = $true
        executable = $installedExecutable
        sha256 = $deployedHash
        pe_subsystem = 2
        process_id = $process.Id
        session_id = $process.SessionId
        task_state = (Get-ScheduledTask -TaskName $TaskName).State.ToString()
        health = $health.status
        backup = $backupExecutable
    } | ConvertTo-Json
} catch {
    $updateError = $_
    if ($replaced -and (Test-Path -LiteralPath $backupExecutable -PathType Leaf)) {
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        $taskStopped = $true
        $rollbackDeadline = [DateTime]::UtcNow.Add($Timeout)
        do {
            $listener = @(Get-NetTCPConnection -State Listen -LocalPort $HealthURI.Port -ErrorAction SilentlyContinue)
            if ($listener.Count -eq 0) { break }
            Start-Sleep -Milliseconds 200
        } while ([DateTime]::UtcNow -lt $rollbackDeadline)
        Copy-Item -LiteralPath $backupExecutable -Destination $installedExecutable -Force
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
}
