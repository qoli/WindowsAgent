# Installs the screenshot capability hosted by the broader Windows Agent project.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [string]$Listen = "0.0.0.0:8787",
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [timespan]$CaptureTimeout = ([timespan]::FromSeconds(5)),
    [ValidateRange(1, 100000)]
    [int]$Retention = 100,
    [ValidateSet("debug", "info", "warn", "error")]
    [string]$LogLevel = "info",
    [string]$TaskName = "gameGuide Windows Capture Agent"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$taskDescription = "gameGuide Go WGC screenshot agent; interactive-user session required"
$minimumVersion = [version]"10.0.18362.0"
$osVersion = [Environment]::OSVersion.Version
if (-not [Environment]::Is64BitOperatingSystem) {
    throw "windows-capture-agent requires a 64-bit Windows operating system"
}
if ($osVersion -lt $minimumVersion) {
    throw "windows-capture-agent requires Windows 10 build 18362 or newer; found $osVersion"
}
if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}

$sourceExecutable = [IO.Path]::GetFullPath($ExecutablePath)
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    throw "agent executable does not exist: $sourceExecutable"
}
$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
if (-not [IO.Path]::IsPathRooted($resolvedDataDir)) {
    throw "DataDir must be an absolute path"
}
if ($CaptureTimeout -le [timespan]::Zero) {
    throw "CaptureTimeout must be positive"
}
$captureTimeoutArgument = ([long][Math]::Ceiling($CaptureTimeout.TotalMilliseconds)).ToString() + "ms"

$listenMatch = [regex]::Match($Listen, "^(0\.0\.0\.0|127\.0\.0\.1):([0-9]{1,5})$")
if (-not $listenMatch.Success) {
    throw "persistent installation requires Listen in 0.0.0.0:<port> or 127.0.0.1:<port> form"
}
$listenPort = [int]$listenMatch.Groups[2].Value
if ($listenPort -lt 1 -or $listenPort -gt 65535) {
    throw "Listen port must be between 1 and 65535"
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
if (-not $identity) {
    throw "could not resolve the current Windows identity"
}

$binDir = Join-Path $resolvedDataDir "bin"
$logDir = Join-Path $resolvedDataDir "logs"
$installedExecutable = Join-Path $binDir "windows-capture-agent.exe"
$logFile = Join-Path $logDir "agent.jsonl"
New-Item -ItemType Directory -Path $binDir -Force | Out-Null
New-Item -ItemType Directory -Path $logDir -Force | Out-Null

$existingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existingTask) {
    if ($existingTask.Description -ne $taskDescription) {
        throw "scheduled task '$TaskName' exists but is not owned by windows-capture-agent"
    }
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}

$portDeadline = [DateTime]::UtcNow.AddSeconds(10)
do {
    $listeners = @(Get-NetTCPConnection -State Listen -LocalPort $listenPort -ErrorAction SilentlyContinue)
    if ($listeners.Count -eq 0) {
        break
    }
    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $portDeadline)
if ($listeners.Count -ne 0) {
    $owners = ($listeners | Select-Object -ExpandProperty OwningProcess -Unique) -join ","
    throw "Listen port $listenPort is already occupied by PID(s) $owners"
}

if ($sourceExecutable -ne $installedExecutable) {
    Copy-Item -LiteralPath $sourceExecutable -Destination $installedExecutable -Force
}

function ConvertTo-NativeQuotedArgument {
    param([Parameter(Mandatory = $true)][string]$Value)
    if ($Value.Contains('"')) {
        throw "task arguments must not contain a double quote: $Value"
    }
    return '"' + $Value + '"'
}

$agentArguments = @(
    "--listen", (ConvertTo-NativeQuotedArgument $Listen),
    "--data-dir", (ConvertTo-NativeQuotedArgument $resolvedDataDir),
    "--capture-timeout", (ConvertTo-NativeQuotedArgument $captureTimeoutArgument),
    "--retention", (ConvertTo-NativeQuotedArgument $Retention.ToString()),
    "--log-level", (ConvertTo-NativeQuotedArgument $LogLevel),
    "--log-file", (ConvertTo-NativeQuotedArgument $logFile)
) -join " "

$action = New-ScheduledTaskAction -Execute $installedExecutable -Argument $agentArguments
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -StartWhenAvailable `
    -Hidden `
    -ExecutionTimeLimit ([timespan]::Zero) `
    -MultipleInstances IgnoreNew `
    -RestartCount 3 `
    -RestartInterval ([timespan]::FromMinutes(1))
$task = New-ScheduledTask `
    -Action $action `
    -Trigger $trigger `
    -Principal $principal `
    -Settings $settings `
    -Description $taskDescription
Register-ScheduledTask -TaskName $TaskName -InputObject $task -Force | Out-Null
Start-ScheduledTask -TaskName $TaskName

$healthURI = "http://127.0.0.1:$listenPort/healthz"
$healthDeadline = [DateTime]::UtcNow.AddSeconds(20)
$health = $null
do {
    try {
        $health = Invoke-RestMethod -Method Get -Uri $healthURI -TimeoutSec 2
    } catch {
        $health = $null
    }
    if ($health -and $health.status -eq "ok") {
        break
    }
    Start-Sleep -Milliseconds 500
} while ([DateTime]::UtcNow -lt $healthDeadline)
if (-not $health -or $health.status -ne "ok") {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    throw "scheduled agent did not become healthy at $healthURI; inspect $logFile"
}

$listener = Get-NetTCPConnection -State Listen -LocalPort $listenPort -ErrorAction Stop |
    Select-Object -First 1
$process = Get-Process -Id $listener.OwningProcess -ErrorAction Stop
if ($process.Path -ne $installedExecutable) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    throw "port $listenPort is served by unexpected executable: $($process.Path)"
}
if ($process.SessionId -eq 0) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    throw "agent started in Session 0; WGC requires the interactive user session"
}

[ordered]@{
    task_name = $TaskName
    task_state = (Get-ScheduledTask -TaskName $TaskName).State.ToString()
    executable = $installedExecutable
    data_dir = $resolvedDataDir
    log_file = $logFile
    listen = $Listen
    process_id = $process.Id
    session_id = $process.SessionId
    health = $health.status
    firewall_changed = $false
    service_created = $false
} | ConvertTo-Json
