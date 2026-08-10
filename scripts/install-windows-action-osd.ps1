# Installs the display-only Action OSD into the signed-in user's session.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [string]$EventListen = "127.0.0.1:8788",
    [string]$TaskName = "gameGuide Windows Action OSD",
    [timespan]$Timeout = ([timespan]::FromSeconds(20)),
    [ValidateSet("WatchdogManaged", "Standalone")]
    [string]$StartupMode = "WatchdogManaged",
    [uint64]$MinimumEventCursor = 0,
    [switch]$AllowCapture
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$taskDescription = "gameGuide display-only Streaming Action OSD; interactive-user session required"

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
    return [BitConverter]::ToUInt16($bytes, $optionalOffset + 0x44)
}

function ConvertTo-NativeQuotedArgument {
    param([Parameter(Mandatory = $true)][string]$Value)
    if ($Value.Contains('"')) {
        throw "task arguments must not contain a double quote: $Value"
    }
    return '"' + $Value + '"'
}

if ($Timeout -le [timespan]::Zero) {
    throw "Timeout must be positive"
}
if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}
$eventMatch = [regex]::Match($EventListen, "^127\.0\.0\.1:([0-9]{1,5})$")
if (-not $eventMatch.Success) {
    throw "EventListen must use explicit loopback form 127.0.0.1:<port>"
}
$eventPort = [int]$eventMatch.Groups[1].Value
if ($eventPort -lt 1 -or $eventPort -gt 65535) {
    throw "EventListen port must be between 1 and 65535"
}

$sourceExecutable = [IO.Path]::GetFullPath($ExecutablePath)
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    throw "Action OSD executable does not exist: $sourceExecutable"
}
if ((Get-WindowsPESubsystem -Path $sourceExecutable) -ne 2) {
    throw "Action OSD executable must use PE Windows GUI subsystem 2"
}
$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$binDir = Join-Path $resolvedDataDir "bin"
$logDir = Join-Path $resolvedDataDir "logs"
$installedExecutable = Join-Path $binDir "windows-action-osd.exe"
$tokenFile = Join-Path $resolvedDataDir "event-api.token"
$logFile = Join-Path $logDir "action-osd.jsonl"
if (-not (Test-Path -LiteralPath $tokenFile -PathType Leaf)) {
    throw "event API token does not exist: $tokenFile"
}
$tokenInfo = Get-Item -LiteralPath $tokenFile -Force
if ($tokenInfo.PSIsContainer -or $tokenInfo.Length -lt 32 -or $tokenInfo.Length -gt 4096) {
    throw "event API token must be a regular file between 32 and 4096 bytes: $tokenFile"
}

$eventHealth = Invoke-RestMethod -Method Get -Uri ("http://" + $EventListen + "/healthz") -TimeoutSec 2
if (-not $eventHealth -or $eventHealth.status -ne "ok") {
    throw "event service is not healthy at http://$EventListen/healthz"
}

New-Item -ItemType Directory -Path $binDir -Force | Out-Null
New-Item -ItemType Directory -Path $logDir -Force | Out-Null
$existing = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existing) {
    if ($existing.Description -ne $taskDescription) {
        throw "scheduled task '$TaskName' exists but is not owned by windows-action-osd"
    }
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}

$stopDeadline = [DateTime]::UtcNow.Add($Timeout)
do {
    $running = @(Get-Process -Name "windows-action-osd" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedExecutable })
    if ($running.Count -eq 0) { break }
    Start-Sleep -Milliseconds 200
} while ([DateTime]::UtcNow -lt $stopDeadline)
if ($running.Count -ne 0) {
    throw "existing Action OSD process did not stop"
}

if ($sourceExecutable -ne $installedExecutable) {
    Copy-Item -LiteralPath $sourceExecutable -Destination $installedExecutable -Force
}
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $sourceExecutable).Hash -ne `
    (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash) {
    throw "installed Action OSD hash differs from source"
}

$arguments = @(
    "--event-api-url", (ConvertTo-NativeQuotedArgument ("http://" + $EventListen)),
    "--event-token-file", (ConvertTo-NativeQuotedArgument $tokenFile),
    "--log-file", (ConvertTo-NativeQuotedArgument $logFile)
)
if ($AllowCapture) {
    $arguments += "--allow-capture"
}
if ($MinimumEventCursor -gt 0) {
    $arguments += @("--minimum-event-cursor", $MinimumEventCursor.ToString([Globalization.CultureInfo]::InvariantCulture))
}
$action = New-ScheduledTaskAction -Execute $installedExecutable -Argument ($arguments -join " ")
$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
$settingsArguments = @{
    AllowStartIfOnBatteries = $true
    DontStopIfGoingOnBatteries = $true
    StartWhenAvailable = $true
    Hidden = $true
    ExecutionTimeLimit = [timespan]::Zero
    MultipleInstances = "IgnoreNew"
}
if ($StartupMode -eq "Standalone") {
    $settingsArguments.RestartCount = 3
    $settingsArguments.RestartInterval = [timespan]::FromMinutes(1)
}
$settings = New-ScheduledTaskSettingsSet @settingsArguments
$taskArguments = @{
    Action = $action
    Principal = $principal
    Settings = $settings
    Description = $taskDescription
}
if ($StartupMode -eq "Standalone") {
    $taskArguments.Trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
}
$task = New-ScheduledTask @taskArguments
Register-ScheduledTask -TaskName $TaskName -InputObject $task -Force | Out-Null
Start-ScheduledTask -TaskName $TaskName

$process = $null
$startDeadline = [DateTime]::UtcNow.Add($Timeout)
do {
    $process = Get-Process -Name "windows-action-osd" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedExecutable } |
        Select-Object -First 1
    if ($process) { break }
    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $startDeadline)
if (-not $process) {
    throw "scheduled Action OSD did not start; inspect $logFile"
}
Start-Sleep -Seconds 2
$process = Get-Process -Id $process.Id -ErrorAction Stop
if ($process.Path -ne $installedExecutable -or $process.SessionId -eq 0 -or -not $process.Responding) {
    throw "Action OSD is not a responding interactive-session process"
}

[ordered]@{
	startup_mode = $StartupMode
    task_name = $TaskName
    task_state = (Get-ScheduledTask -TaskName $TaskName).State.ToString()
	task_trigger_count = @((Get-ScheduledTask -TaskName $TaskName).Triggers | Where-Object { $null -ne $_ }).Count
	task_restart_count = [int](Get-ScheduledTask -TaskName $TaskName).Settings.RestartCount
    executable = $installedExecutable
    sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash.ToLowerInvariant()
    process_id = $process.Id
    session_id = $process.SessionId
    responding = $process.Responding
    capture_excluded = (-not $AllowCapture.IsPresent)
    event_api = "http://$EventListen"
    log_file = $logFile
} | ConvertTo-Json
