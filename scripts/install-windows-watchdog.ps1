# Installs the external, one-way-coupled WindowsAgent watchdog.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [Parameter(Mandatory = $true)]
    [string]$ConfigPath,

    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [string]$TaskName = "gameGuide Windows Watchdog",
    [timespan]$Timeout = ([timespan]::FromSeconds(20))
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$taskDescription = "gameGuide external process watchdog; no automatic self-recovery"

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

if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}
if ($Timeout -le [timespan]::Zero) {
    throw "Timeout must be positive"
}
$sourceExecutable = [IO.Path]::GetFullPath($ExecutablePath)
$sourceConfig = [IO.Path]::GetFullPath($ConfigPath)
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    throw "watchdog executable does not exist: $sourceExecutable"
}
if (-not (Test-Path -LiteralPath $sourceConfig -PathType Leaf)) {
    throw "watchdog config does not exist: $sourceConfig"
}
if ((Get-WindowsPESubsystem -Path $sourceExecutable) -ne 2) {
    throw "watchdog executable must use PE Windows GUI subsystem 2"
}

$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$watchdogDir = Join-Path $resolvedDataDir "watchdog"
$binDir = Join-Path $resolvedDataDir "bin"
$installedExecutable = Join-Path $binDir "windows-watchdog.exe"
$installedConfig = Join-Path $watchdogDir "config.json"
$statusFile = Join-Path $watchdogDir "status.json"
$logFile = Join-Path $watchdogDir "watchdog.jsonl"
New-Item -ItemType Directory -Path $watchdogDir -Force | Out-Null
New-Item -ItemType Directory -Path $binDir -Force | Out-Null

$sourceValidationArguments = @(
    "--config", (ConvertTo-NativeQuotedArgument $sourceConfig),
    "--validate-only"
) -join " "
$sourceValidation = Start-Process -FilePath $sourceExecutable -ArgumentList $sourceValidationArguments -Wait -PassThru
if ($sourceValidation.ExitCode -ne 0) {
    throw "watchdog rejected the supplied configuration"
}

$existing = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existing) {
    if ($existing.Description -ne $taskDescription) {
        throw "scheduled task '$TaskName' exists but is not owned by windows-watchdog"
    }
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}

$stopDeadline = [DateTime]::UtcNow.Add($Timeout)
do {
    $running = @(Get-Process -Name "windows-watchdog" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedExecutable })
    if ($running.Count -eq 0) { break }
    Start-Sleep -Milliseconds 200
} while ([DateTime]::UtcNow -lt $stopDeadline)
if ($running.Count -ne 0) {
    throw "existing watchdog process did not stop"
}

Copy-Item -LiteralPath $sourceExecutable -Destination $installedExecutable -Force
Copy-Item -LiteralPath $sourceConfig -Destination $installedConfig -Force
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $sourceExecutable).Hash -ne `
    (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash) {
    throw "installed watchdog hash differs from source"
}
$installedValidationArguments = @(
    "--config", (ConvertTo-NativeQuotedArgument $installedConfig),
    "--validate-only"
) -join " "
$installedValidation = Start-Process -FilePath $installedExecutable -ArgumentList $installedValidationArguments -Wait -PassThru
if ($installedValidation.ExitCode -ne 0) {
    throw "installed watchdog rejected the installed configuration"
}

$arguments = @(
    "--config", (ConvertTo-NativeQuotedArgument $installedConfig),
    "--status-file", (ConvertTo-NativeQuotedArgument $statusFile),
    "--log-file", (ConvertTo-NativeQuotedArgument $logFile)
) -join " "
$action = New-ScheduledTaskAction -Execute $installedExecutable -Argument $arguments
$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -StartWhenAvailable `
    -Hidden `
    -ExecutionTimeLimit ([timespan]::Zero) `
    -MultipleInstances IgnoreNew
$task = New-ScheduledTask -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description $taskDescription
Register-ScheduledTask -TaskName $TaskName -InputObject $task -Force | Out-Null
if (Test-Path -LiteralPath $statusFile -PathType Leaf) {
    Remove-Item -LiteralPath $statusFile -Force
}
Start-ScheduledTask -TaskName $TaskName

$process = $null
$deadline = [DateTime]::UtcNow.Add($Timeout)
do {
    $process = Get-Process -Name "windows-watchdog" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedExecutable } |
        Select-Object -First 1
    if ($process -and (Test-Path -LiteralPath $statusFile -PathType Leaf)) { break }
    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $deadline)
if (-not $process -or -not (Test-Path -LiteralPath $statusFile -PathType Leaf)) {
    throw "scheduled watchdog did not start and publish status; inspect $logFile"
}
$process = Get-Process -Id $process.Id -ErrorAction Stop
if ($process.Path -ne $installedExecutable -or $process.SessionId -eq 0) {
    throw "watchdog is not the installed interactive-session process"
}
$registeredTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction Stop
if ([int]$registeredTask.Settings.RestartCount -ne 0) {
    throw "watchdog Scheduled Task must not automatically restart itself"
}

[ordered]@{
    task_name = $TaskName
    task_state = $registeredTask.State.ToString()
    restart_count = [int]$registeredTask.Settings.RestartCount
    executable = $installedExecutable
    config = $installedConfig
    status_file = $statusFile
    log_file = $logFile
    process_id = $process.Id
    session_id = $process.SessionId
    self_recovery = $false
} | ConvertTo-Json
