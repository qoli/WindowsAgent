# Installs the windowless, read-only Event Web projection in the signed-in session.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [string]$EventListen = "127.0.0.1:8788",
    [string]$WebListen = "127.0.0.1:8790",
    [string]$TaskName = "gameGuide Windows Event Web",
    [timespan]$Timeout = ([timespan]::FromSeconds(20)),
    [ValidateSet("WatchdogManaged", "Standalone")]
    [string]$StartupMode = "WatchdogManaged",
    [uint64]$MinimumEventCursor = 0
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$taskDescription = "gameGuide windowless read-only Event Web projection; interactive-user session required"

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

function Assert-EventListen {
    param([Parameter(Mandatory = $true)][string]$Value, [Parameter(Mandatory = $true)][string]$Label)
    $match = [regex]::Match($Value, "^127\.0\.0\.1:([0-9]{1,5})$")
    if (-not $match.Success) {
        throw "$Label must use explicit loopback form 127.0.0.1:<port>"
    }
    $port = [int]$match.Groups[1].Value
    if ($port -lt 1 -or $port -gt 65535) {
        throw "$Label port must be between 1 and 65535"
    }
}

function Assert-WebListen {
    param([Parameter(Mandatory = $true)][string]$Value)
    $match = [regex]::Match($Value, "^([0-9]{1,3}(?:\.[0-9]{1,3}){3}):([0-9]{1,5})$")
    if (-not $match.Success) {
        throw "WebListen must use an explicit loopback or private LAN IPv4 address and port"
    }
    $address = $null
    if (-not [Net.IPAddress]::TryParse($match.Groups[1].Value, [ref]$address) -or
        $address.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork) {
        throw "WebListen must contain a canonical IPv4 address"
    }
    $bytes = $address.GetAddressBytes()
    $private = $bytes[0] -eq 127 -or $bytes[0] -eq 10 -or
        ($bytes[0] -eq 172 -and $bytes[1] -ge 16 -and $bytes[1] -le 31) -or
        ($bytes[0] -eq 192 -and $bytes[1] -eq 168)
    if (-not $private) {
        throw "WebListen must use loopback or an RFC1918 private LAN address"
    }
    $port = [int]$match.Groups[2].Value
    if ($port -lt 1 -or $port -gt 65535) {
        throw "WebListen port must be between 1 and 65535"
    }
}

if ($Timeout -le [timespan]::Zero) {
    throw "Timeout must be positive"
}
if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}
Assert-EventListen -Value $EventListen -Label "EventListen"
Assert-WebListen -Value $WebListen
if ($EventListen -eq $WebListen) {
    throw "EventListen and WebListen must use different ports"
}

$sourceExecutable = [IO.Path]::GetFullPath($ExecutablePath)
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    throw "Event Web executable does not exist: $sourceExecutable"
}
if ((Get-WindowsPESubsystem -Path $sourceExecutable) -ne 2) {
    throw "Event Web executable must use PE Windows GUI subsystem 2"
}
$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$binDir = Join-Path $resolvedDataDir "bin"
$logDir = Join-Path $resolvedDataDir "logs"
$installedExecutable = Join-Path $binDir "windows-event-web.exe"
$eventTokenFile = Join-Path $resolvedDataDir "event-api.token"
$webTokenFile = Join-Path $resolvedDataDir "event-web-api.token"
$logFile = Join-Path $logDir "event-web.jsonl"

if (-not (Test-Path -LiteralPath $eventTokenFile -PathType Leaf)) {
    throw "event API token does not exist: $eventTokenFile"
}
$eventTokenInfo = Get-Item -LiteralPath $eventTokenFile -Force
if ($eventTokenInfo.PSIsContainer -or $eventTokenInfo.Length -lt 32 -or $eventTokenInfo.Length -gt 4096) {
    throw "event API token must be a regular file between 32 and 4096 bytes: $eventTokenFile"
}
$eventHealth = Invoke-RestMethod -Method Get -Uri ("http://" + $EventListen + "/healthz") -TimeoutSec 2
if (-not $eventHealth -or $eventHealth.status -ne "ok") {
    throw "event service is not healthy at http://$EventListen/healthz"
}

New-Item -ItemType Directory -Path $binDir -Force | Out-Null
New-Item -ItemType Directory -Path $logDir -Force | Out-Null
if (-not (Test-Path -LiteralPath $webTokenFile)) {
    $tokenBytes = New-Object byte[] 32
    $tokenRng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $tokenRng.GetBytes($tokenBytes)
        [IO.File]::WriteAllText($webTokenFile, [Convert]::ToBase64String($tokenBytes))
    } finally {
        $tokenRng.Dispose()
    }
}
$webTokenInfo = Get-Item -LiteralPath $webTokenFile -Force
if ($webTokenInfo.PSIsContainer -or $webTokenInfo.Length -lt 32 -or $webTokenInfo.Length -gt 4096) {
    throw "Web API token must be a regular file between 32 and 4096 bytes: $webTokenFile"
}

$existing = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existing) {
    if ($existing.Description -ne $taskDescription) {
        throw "scheduled task '$TaskName' exists but is not owned by windows-event-web"
    }
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}
$stopDeadline = [DateTime]::UtcNow.Add($Timeout)
do {
    $running = @(Get-Process -Name "windows-event-web" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedExecutable })
    if ($running.Count -eq 0) { break }
    Start-Sleep -Milliseconds 200
} while ([DateTime]::UtcNow -lt $stopDeadline)
if ($running.Count -ne 0) {
    throw "existing Event Web process did not stop"
}

if ($sourceExecutable -ne $installedExecutable) {
    Copy-Item -LiteralPath $sourceExecutable -Destination $installedExecutable -Force
}
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $sourceExecutable).Hash -ne `
    (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash) {
    throw "installed Event Web hash differs from source"
}

$arguments = @(
    "--listen", (ConvertTo-NativeQuotedArgument $WebListen),
    "--event-api-url", (ConvertTo-NativeQuotedArgument ("http://" + $EventListen)),
    "--event-token-file", (ConvertTo-NativeQuotedArgument $eventTokenFile),
    "--web-token-file", (ConvertTo-NativeQuotedArgument $webTokenFile),
    "--log-file", (ConvertTo-NativeQuotedArgument $logFile)
)
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
$health = $null
$startDeadline = [DateTime]::UtcNow.Add($Timeout)
do {
    $process = Get-Process -Name "windows-event-web" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedExecutable } |
        Select-Object -First 1
    if ($process) {
        try {
            $health = Invoke-RestMethod -Method Get -Uri ("http://" + $WebListen + "/healthz") -TimeoutSec 2
            if ($health -and $health.status -eq "ok") { break }
        } catch {}
    }
    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $startDeadline)
if (-not $process -or -not $health -or $health.status -ne "ok") {
    throw "scheduled Event Web did not become healthy; inspect $logFile"
}
$process = Get-Process -Id $process.Id -ErrorAction Stop
if ($process.Path -ne $installedExecutable -or $process.SessionId -eq 0) {
    throw "Event Web is not running in the signed-in interactive session"
}
if ($process.MainWindowHandle -ne 0) {
    throw "Event Web unexpectedly created a visible main window"
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
    main_window_handle = $process.MainWindowHandle.ToInt64()
    web_url = "http://$WebListen/"
    web_token_file = $webTokenFile
    event_api = "http://$EventListen"
    cursor = [string]$health.cursor
    log_file = $logFile
} | ConvertTo-Json
