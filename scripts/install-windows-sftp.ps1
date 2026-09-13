# Installs the WindowsAgent SFTP filesystem data plane under the current Windows identity.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [string]$Listen = "0.0.0.0:2022",
    [string]$StatusListen = "127.0.0.1:8793",
    [string]$TaskName = "gameGuide Windows SFTP",
    [timespan]$Timeout = ([timespan]::FromSeconds(20)),
    [ValidateSet("WatchdogManaged", "Standalone")]
    [string]$StartupMode = "WatchdogManaged"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$taskDescription = "gameGuide WindowsAgent SFTP filesystem data plane; elevated current-user token"

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

function Assert-TCPListen {
    param(
        [Parameter(Mandatory = $true)][string]$Value,
        [Parameter(Mandatory = $true)][string]$Label,
        [Parameter(Mandatory = $true)][bool]$RequireLoopback
    )
    $uri = $null
    if (-not [Uri]::TryCreate("tcp://$Value", [UriKind]::Absolute, [ref]$uri) -or
        -not $uri.Host -or $uri.Port -lt 1 -or $uri.Port -gt 65535 -or
        $uri.AbsolutePath -cne "/" -or $uri.Query -or $uri.Fragment -or $uri.UserInfo) {
        throw "$Label must use a valid host and port"
    }
    if ($RequireLoopback) {
        $address = $null
        if (-not [Net.IPAddress]::TryParse($uri.Host, [ref]$address) -or
            -not [Net.IPAddress]::IsLoopback($address)) {
            throw "$Label must use an explicit loopback IP address"
        }
    }
    return [pscustomobject]@{ Host = $uri.Host; Port = $uri.Port }
}

if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}
if ($Timeout -le [timespan]::Zero) {
    throw "Timeout must be positive"
}
$null = Assert-TCPListen -Value $Listen -Label "Listen" -RequireLoopback $false
$statusEndpoint = Assert-TCPListen -Value $StatusListen -Label "StatusListen" -RequireLoopback $true
$windowsIdentity = [Security.Principal.WindowsIdentity]::GetCurrent()
$identity = $windowsIdentity.Name
if (-not $identity) {
    throw "could not resolve the current Windows identity"
}
$windowsPrincipal = New-Object Security.Principal.WindowsPrincipal($windowsIdentity)
if (-not $windowsPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "SFTP installation must run from an elevated Administrator session"
}

$sourceExecutable = [IO.Path]::GetFullPath($ExecutablePath)
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    throw "SFTP executable does not exist: $sourceExecutable"
}
if ((Get-WindowsPESubsystem -Path $sourceExecutable) -ne 2) {
    throw "SFTP executable must use PE Windows GUI subsystem 2"
}

$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$binDir = Join-Path $resolvedDataDir "bin"
$sftpDir = Join-Path $resolvedDataDir "sftp"
$logDir = Join-Path $resolvedDataDir "logs"
$installedExecutable = Join-Path $binDir "windows-sftp.exe"
$hostKeyFile = Join-Path $sftpDir "ssh_host_ed25519_key"
$logFile = Join-Path $logDir "sftp.jsonl"
New-Item -ItemType Directory -Path $binDir -Force | Out-Null
New-Item -ItemType Directory -Path $sftpDir -Force | Out-Null
New-Item -ItemType Directory -Path $logDir -Force | Out-Null

$existing = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existing) {
    if ($existing.Description -cne $taskDescription) {
        throw "scheduled task '$TaskName' exists but is not owned by windows-sftp"
    }
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}
$stopDeadline = [DateTime]::UtcNow.Add($Timeout)
do {
    $running = @(Get-Process -Name "windows-sftp" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedExecutable })
    if ($running.Count -eq 0) { break }
    Start-Sleep -Milliseconds 200
} while ([DateTime]::UtcNow -lt $stopDeadline)
if ($running.Count -ne 0) {
    throw "existing SFTP process did not stop"
}

if ($sourceExecutable -ine $installedExecutable) {
    Copy-Item -LiteralPath $sourceExecutable -Destination $installedExecutable -Force
}
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $sourceExecutable).Hash -cne `
    (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash) {
    throw "installed SFTP hash differs from source"
}

$hostKeySource = "preserved"
if (-not (Test-Path -LiteralPath $hostKeyFile)) {
    $keyInitArguments = @(
        "host-key", "init", "--path", (ConvertTo-NativeQuotedArgument $hostKeyFile)
    ) -join " "
    $keyInit = Start-Process -FilePath $installedExecutable -ArgumentList $keyInitArguments -Wait -PassThru
    if ($keyInit.ExitCode -ne 0) {
        throw "windows-sftp failed to initialize the persistent SSH host key"
    }
    $hostKeySource = "generated"
}
if (-not (Test-Path -LiteralPath $hostKeyFile -PathType Leaf)) {
    throw "SFTP host key is not a regular file: $hostKeyFile"
}
$hostKey = Get-Item -LiteralPath $hostKeyFile -Force
if ($hostKey.PSIsContainer -or $hostKey.Length -le 0) {
    throw "SFTP host key is empty or not a regular file: $hostKeyFile"
}

$arguments = @(
    "--listen", (ConvertTo-NativeQuotedArgument $Listen),
    "--status-listen", (ConvertTo-NativeQuotedArgument $StatusListen),
    "--host-key-file", (ConvertTo-NativeQuotedArgument $hostKeyFile),
    "--log-file", (ConvertTo-NativeQuotedArgument $logFile)
) -join " "
$action = New-ScheduledTaskAction -Execute $installedExecutable -Argument $arguments
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Highest
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
$health = $null
$healthURI = ([UriBuilder]::new("http", $statusEndpoint.Host, $statusEndpoint.Port, "healthz")).Uri.AbsoluteUri
$startDeadline = [DateTime]::UtcNow.Add($Timeout)
do {
    $process = Get-Process -Name "windows-sftp" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedExecutable } |
        Select-Object -First 1
    if ($process) {
        try {
            $health = Invoke-RestMethod -Method Get -Uri $healthURI -TimeoutSec 2
            if ($health -and
                $health.status -ceq "ok" -and
                $health.runtime -ceq "windows-sftp-v1" -and
                $health.username -ceq "windowsagent" -and
                $health.clientAuthentication -ceq "none" -and
                $health.sftpListen -and
                $health.hostKeyFingerprint -and
                ([string]$health.hostKeyFingerprint).StartsWith("SHA256:")) { break }
        } catch {
            $health = $null
        }
    }
    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $startDeadline)
if (-not $process -or -not $health -or
    $health.status -cne "ok" -or
    $health.runtime -cne "windows-sftp-v1" -or
    $health.username -cne "windowsagent" -or
    $health.clientAuthentication -cne "none" -or
    -not $health.sftpListen -or
    -not $health.hostKeyFingerprint -or
    -not ([string]$health.hostKeyFingerprint).StartsWith("SHA256:")) {
    throw "scheduled SFTP service did not become healthy at $healthURI; inspect $logFile"
}
$process = Get-Process -Id $process.Id -ErrorAction Stop
if ($process.Path -ine $installedExecutable) {
    throw "SFTP service is not running from the installed executable"
}
$registeredTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction Stop
if ([string]$registeredTask.Principal.RunLevel -cne "Highest") {
    throw "SFTP Scheduled Task must use RunLevel Highest"
}

$watchdogTarget = [ordered]@{
    id = "sftp"
    desiredState = "running"
    startAfterHealthy = @()
    failureThreshold = 3
    probes = @(
        [ordered]@{
            type = "process"
            executablePath = $installedExecutable
            requireInteractiveSession = $false
        },
        [ordered]@{
            type = "http-json"
            url = $healthURI
            expectedStatusCode = 200
            expectedJsonStatus = "ok"
            timeoutMs = 2000
        }
    )
    recovery = [ordered]@{
        scheduledTaskName = $TaskName
        expectedTaskDescription = $taskDescription
        maxAttempts = 3
        attemptWindowMs = 300000
        backoffMs = 5000
        actionTimeoutMs = 10000
        startupGraceMs = 5000
    }
}

[ordered]@{
    startup_mode = $StartupMode
    task_name = $TaskName
    task_state = $registeredTask.State.ToString()
    task_trigger_count = @($registeredTask.Triggers | Where-Object { $null -ne $_ }).Count
    task_restart_count = [int]$registeredTask.Settings.RestartCount
    task_run_level = [string]$registeredTask.Principal.RunLevel
    executable = $installedExecutable
    sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash.ToLowerInvariant()
    process_id = $process.Id
    session_id = $process.SessionId
    listen = $Listen
    status_url = $healthURI
    host_key_file = $hostKeyFile
    host_key_source = $hostKeySource
    log_file = $logFile
    watchdog_target = $watchdogTarget
} | ConvertTo-Json -Depth 8
