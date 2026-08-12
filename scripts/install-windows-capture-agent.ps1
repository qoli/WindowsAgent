# Installs the screenshot capability hosted by the broader Windows Agent project.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [Parameter(Mandatory = $true)]
    [string]$RulesPath,

    [string]$OCRRuntimeBundlePath,

    [switch]$ArchiveIncompatibleCaptures,

    [string]$Listen = "0.0.0.0:8787",
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [timespan]$CaptureTimeout = ([timespan]::FromSeconds(5)),
    [ValidateRange(1, 100000)]
    [int]$Retention = 100,
    [ValidateSet("debug", "info", "warn", "error")]
    [string]$LogLevel = "info",
    [bool]$WGCTrace = $true,
    [ValidateSet("WatchdogManaged", "Standalone")]
    [string]$StartupMode = "WatchdogManaged",
    [string]$TaskName = "gameGuide Windows Capture Agent",
    [string]$EventListen = "127.0.0.1:8788",
    [string]$EventTaskName = "gameGuide Windows Event Stream"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$taskDescription = "gameGuide Go WGC screenshot agent; interactive-user session required"
$eventTaskDescription = "gameGuide durable local event stream; interactive-user session required"

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

function Assert-GUIExecutable {
    param([Parameter(Mandatory = $true)][string]$Path, [Parameter(Mandatory = $true)][string]$Label)
    $subsystem = Get-WindowsPESubsystem -Path $Path
    if ($subsystem -ne 2) {
        throw ("$Label must use PE Windows GUI subsystem 2; found subsystem $subsystem. " +
            "Persistent Scheduled Tasks cannot hide a console-subsystem executable.")
    }
}

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
Assert-GUIExecutable -Path $sourceExecutable -Label "capture agent executable"
$sourceBinDir = Split-Path -Parent $sourceExecutable
$runtimeSources = [ordered]@{
    "windows-observation-job.exe" = Join-Path $sourceBinDir "windows-observation-job.exe"
    "windows-observation-script-runner.exe" = Join-Path $sourceBinDir "windows-observation-script-runner.exe"
    "windows-observer.exe" = Join-Path $sourceBinDir "windows-observer.exe"
    "windows-event-stream.exe" = Join-Path $sourceBinDir "windows-event-stream.exe"
}
foreach ($runtime in $runtimeSources.GetEnumerator()) {
    if (-not (Test-Path -LiteralPath $runtime.Value -PathType Leaf)) {
        throw "required Starlark launcher runtime does not exist: $($runtime.Value)"
    }
}
Assert-GUIExecutable -Path $runtimeSources["windows-event-stream.exe"] -Label "event stream executable"
$sourceRules = [IO.Path]::GetFullPath($RulesPath)
if (-not (Test-Path -LiteralPath $sourceRules -PathType Container)) {
    throw "Rules directory does not exist: $sourceRules"
}
$sourceRuleDirectories = @(Get-ChildItem -LiteralPath $sourceRules -Directory)
if ($sourceRuleDirectories.Count -eq 0) {
    throw "Rules directory must contain at least one executable Rule plugin"
}
$requiresOCRRuntime = $false
foreach ($sourceRuleDirectory in $sourceRuleDirectories) {
    $ruleDescriptor = Get-Content -LiteralPath (Join-Path $sourceRuleDirectory.FullName "rule.json") `
        -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if (@($ruleDescriptor.runtimeProfiles.PSObject.Properties).Count -ne 0) {
        $requiresOCRRuntime = $true
    }
}
$sourceOCRRuntime = $null
if ($requiresOCRRuntime) {
    if ([String]::IsNullOrWhiteSpace($OCRRuntimeBundlePath)) {
        throw "at least one Rule declares a resident runtime profile; OCRRuntimeBundlePath is required"
    }
    $sourceOCRRuntime = [IO.Path]::GetFullPath($OCRRuntimeBundlePath)
    if (-not (Test-Path -LiteralPath $sourceOCRRuntime -PathType Container)) {
        throw "OCR runtime bundle directory does not exist: $sourceOCRRuntime"
    }
    foreach ($name in @(
        "PpOcr.DirectML.exe",
        "runtime-config.json",
        "text-regions-runtime-config.json",
        "ppocrv6-small-det.onnx",
        "ppocrv6-small-rec-w480.onnx",
        "ppocrv6-small-characters.json"
    )) {
        if (-not (Test-Path -LiteralPath (Join-Path $sourceOCRRuntime $name) -PathType Leaf)) {
            throw "OCR runtime bundle is missing required file: $name"
        }
    }
}
$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
if (-not [IO.Path]::IsPathRooted($resolvedDataDir)) {
    throw "DataDir must be an absolute path"
}
if ($CaptureTimeout -le [timespan]::Zero) {
    throw "CaptureTimeout must be positive"
}
$capturesDir = Join-Path $resolvedDataDir "captures"
$incompatibleCaptureMetadata = @()
if (Test-Path -LiteralPath $capturesDir -PathType Container) {
    foreach ($metadataFile in @(Get-ChildItem -LiteralPath $capturesDir -Filter "metadata.json" -File -Recurse)) {
        try {
            $metadata = Get-Content -LiteralPath $metadataFile.FullName -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
        } catch {
            throw "could not inspect existing capture metadata '$($metadataFile.FullName)': $($_.Exception.Message)"
        }
        $metadataProperties = @($metadata.PSObject.Properties.Name)
        $hasRule = $metadataProperties -contains "rule" -and $null -ne $metadata.rule
        $ruleProperties = @()
        if ($hasRule) {
            $ruleProperties = @($metadata.rule.PSObject.Properties.Name)
        }
        $hasAgents = $hasRule -and $ruleProperties -contains "agents"
        $usesRemovedAgentSHA = $false
        if ($hasAgents -and $null -ne $metadata.rule.agents) {
            $usesRemovedAgentSHA = (
                @($metadata.rule.agents.PSObject.Properties.Name) -contains "sha256"
            )
        }
        $missingScriptNavigation = (
            $hasRule -and
            $ruleProperties -contains "status" -and
            $metadata.rule.status -eq "matched" -and
            -not ($ruleProperties -contains "scripts")
        )
        $missingActionNavigation = (
            $hasRule -and
            $ruleProperties -contains "status" -and
            $metadata.rule.status -eq "matched" -and
            -not ($ruleProperties -contains "actions")
        )
        $missingRegistrationNavigation = (
            $hasRule -and
            $ruleProperties -contains "status" -and
            $metadata.rule.status -eq "matched" -and
            -not ($ruleProperties -contains "registrations")
        )
        $missingRuntimeNavigation = (
            $hasRule -and
            $ruleProperties -contains "status" -and
            $metadata.rule.status -eq "matched" -and
            -not ($ruleProperties -contains "runtimes")
        )
        if ($usesRemovedAgentSHA -or $missingScriptNavigation -or $missingActionNavigation -or `
            $missingRegistrationNavigation -or $missingRuntimeNavigation) {
            $incompatibleCaptureMetadata += $metadataFile.FullName
        }
    }
}
if ($incompatibleCaptureMetadata.Count -ne 0 -and -not $ArchiveIncompatibleCaptures) {
    throw ("existing captures use an incompatible Rule navigation contract; " +
        "rerun with -ArchiveIncompatibleCaptures to preserve them outside the active capture store")
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
$eventListenMatch = [regex]::Match($EventListen, "^127\.0\.0\.1:([0-9]{1,5})$")
if (-not $eventListenMatch.Success) {
    throw "EventListen must use explicit loopback form 127.0.0.1:<port>"
}
$eventListenPort = [int]$eventListenMatch.Groups[1].Value
if ($eventListenPort -lt 1 -or $eventListenPort -gt 65535) {
    throw "EventListen port must be between 1 and 65535"
}
if ($eventListenPort -eq $listenPort) {
    throw "Listen and EventListen must use different ports"
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
if (-not $identity) {
    throw "could not resolve the current Windows identity"
}

$binDir = Join-Path $resolvedDataDir "bin"
$logDir = Join-Path $resolvedDataDir "logs"
$installedExecutable = Join-Path $binDir "windows-capture-agent.exe"
$installedRules = Join-Path $resolvedDataDir "Rules"
$logFile = Join-Path $logDir "agent.jsonl"
$runtimeLogFile = Join-Path $logDir "runtime-stderr.log"
$eventLogFile = Join-Path $logDir "event-stream.jsonl"
$eventDataDir = Join-Path $resolvedDataDir "events"
$dumpDir = Join-Path $resolvedDataDir "dumps"
$eventTokenFile = Join-Path $resolvedDataDir "event-api.token"
$installedEventExecutable = Join-Path $binDir "windows-event-stream.exe"
$installedOCRRuntime = Join-Path $resolvedDataDir "runtimes\ppocr-w480"
$legacyScriptAPITokenFile = Join-Path $resolvedDataDir "script-api.token"
New-Item -ItemType Directory -Path $binDir -Force | Out-Null
New-Item -ItemType Directory -Path $logDir -Force | Out-Null
New-Item -ItemType Directory -Path $dumpDir -Force | Out-Null

$dumpRegistryPath = "HKCU:\Software\Microsoft\Windows\Windows Error Reporting\LocalDumps\windows-capture-agent.exe"
New-Item -Path $dumpRegistryPath -Force | Out-Null
New-ItemProperty -Path $dumpRegistryPath -Name "DumpFolder" -PropertyType ExpandString -Value $dumpDir -Force | Out-Null
New-ItemProperty -Path $dumpRegistryPath -Name "DumpType" -PropertyType DWord -Value 2 -Force | Out-Null
New-ItemProperty -Path $dumpRegistryPath -Name "DumpCount" -PropertyType DWord -Value 5 -Force | Out-Null

$existingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existingTask) {
    if ($existingTask.Description -ne $taskDescription) {
        throw "scheduled task '$TaskName' exists but is not owned by windows-capture-agent"
    }
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}
$existingEventTask = Get-ScheduledTask -TaskName $EventTaskName -ErrorAction SilentlyContinue
if ($existingEventTask) {
    if ($existingEventTask.Description -ne $eventTaskDescription) {
        throw "scheduled task '$EventTaskName' exists but is not owned by windows-event-stream"
    }
    Stop-ScheduledTask -TaskName $EventTaskName -ErrorAction SilentlyContinue
}
$legacyScriptAPITokenRemoved = $false
if (Test-Path -LiteralPath $legacyScriptAPITokenFile -PathType Leaf) {
    Remove-Item -LiteralPath $legacyScriptAPITokenFile -Force
    $legacyScriptAPITokenRemoved = $true
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
$eventPortDeadline = [DateTime]::UtcNow.AddSeconds(10)
do {
    $eventListeners = @(Get-NetTCPConnection -State Listen -LocalPort $eventListenPort -ErrorAction SilentlyContinue)
    if ($eventListeners.Count -eq 0) {
        break
    }
    Start-Sleep -Milliseconds 250
} while ([DateTime]::UtcNow -lt $eventPortDeadline)
if ($eventListeners.Count -ne 0) {
    $owners = ($eventListeners | Select-Object -ExpandProperty OwningProcess -Unique) -join ","
    throw "EventListen port $eventListenPort is already occupied by PID(s) $owners"
}

if ($requiresOCRRuntime) {
    $installedOCRExecutable = Join-Path $installedOCRRuntime "PpOcr.DirectML.exe"
    $residentOCRProcesses = @(Get-Process -Name "PpOcr.DirectML" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installedOCRExecutable })
    foreach ($residentOCRProcess in $residentOCRProcesses) {
        Stop-Process -Id $residentOCRProcess.Id -Force -ErrorAction Stop
    }
    $ocrStopDeadline = [DateTime]::UtcNow.AddSeconds(10)
    do {
        $residentOCRProcesses = @(Get-Process -Name "PpOcr.DirectML" -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -eq $installedOCRExecutable })
        if ($residentOCRProcesses.Count -eq 0) { break }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $ocrStopDeadline)
    if ($residentOCRProcesses.Count -ne 0) {
        throw "resident OCR runtime did not stop before installation: $installedOCRExecutable"
    }
}

$captureArchive = $null
if ($incompatibleCaptureMetadata.Count -ne 0) {
    $captureArchive = Join-Path $resolvedDataDir (
        "captures.pre-external-rules-" + [DateTime]::UtcNow.ToString("yyyyMMddTHHmmssZ")
    )
    if (Test-Path -LiteralPath $captureArchive) {
        throw "capture archive target already exists: $captureArchive"
    }
    [IO.Directory]::Move($capturesDir, $captureArchive)
}

if ($sourceExecutable -ne $installedExecutable) {
    Copy-Item -LiteralPath $sourceExecutable -Destination $installedExecutable -Force
}
foreach ($runtime in $runtimeSources.GetEnumerator()) {
    Copy-Item -LiteralPath $runtime.Value -Destination (Join-Path $binDir $runtime.Key) -Force
}
if ($requiresOCRRuntime) {
    New-Item -ItemType Directory -Path $installedOCRRuntime -Force | Out-Null
    foreach ($name in @(
        "PpOcr.DirectML.exe",
        "runtime-config.json",
        "text-regions-runtime-config.json",
        "ppocrv6-small-det.onnx",
        "ppocrv6-small-rec-w480.onnx",
        "ppocrv6-small-characters.json"
    )) {
        Copy-Item -LiteralPath (Join-Path $sourceOCRRuntime $name) `
            -Destination (Join-Path $installedOCRRuntime $name) -Force
    }
}
if (Test-Path -LiteralPath $eventTokenFile) {
    $eventTokenInfo = Get-Item -LiteralPath $eventTokenFile -Force
    if ($eventTokenInfo.PSIsContainer -or $eventTokenInfo.Length -lt 32 -or $eventTokenInfo.Length -gt 4096) {
        throw "existing event API token must be a regular file between 32 and 4096 bytes: $eventTokenFile"
    }
    $eventToken = [IO.File]::ReadAllText($eventTokenFile)
    if ($eventToken.Trim() -ne $eventToken) {
        throw "existing event API token must not contain leading or trailing whitespace: $eventTokenFile"
    }
} else {
    $tokenBytes = New-Object byte[] 32
    $random = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $random.GetBytes($tokenBytes)
    } finally {
        $random.Dispose()
    }
    $eventToken = [Convert]::ToBase64String($tokenBytes)
    [IO.File]::WriteAllText($eventTokenFile, $eventToken, [Text.UTF8Encoding]::new($false))
}
New-Item -ItemType Directory -Path $eventDataDir -Force | Out-Null
New-Item -ItemType Directory -Path $installedRules -Force | Out-Null
foreach ($sourceRuleDirectory in $sourceRuleDirectories) {
    & (Join-Path $PSScriptRoot "sync-windows-agent-rule.ps1") `
        -SourceRulePath $sourceRuleDirectory.FullName `
        -DestinationRulesDir $installedRules | Out-Null
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
    "--rules-dir", (ConvertTo-NativeQuotedArgument $installedRules),
    "--capture-timeout", (ConvertTo-NativeQuotedArgument $captureTimeoutArgument),
    "--retention", (ConvertTo-NativeQuotedArgument $Retention.ToString()),
    "--log-level", (ConvertTo-NativeQuotedArgument $LogLevel),
    "--log-file", (ConvertTo-NativeQuotedArgument $logFile),
    "--runtime-log-file", (ConvertTo-NativeQuotedArgument $runtimeLogFile),
    ("--wgc-trace=" + $WGCTrace.ToString().ToLowerInvariant()),
    "--ocr-runtime-root", (ConvertTo-NativeQuotedArgument $installedOCRRuntime),
    "--event-api-url", (ConvertTo-NativeQuotedArgument ("http://" + $EventListen)),
    "--event-token-file", (ConvertTo-NativeQuotedArgument $eventTokenFile)
) -join " "
$eventArguments = @(
    "--listen", (ConvertTo-NativeQuotedArgument $EventListen),
    "--data-dir", (ConvertTo-NativeQuotedArgument $eventDataDir),
    "--token-file", (ConvertTo-NativeQuotedArgument $eventTokenFile),
    "--log-file", (ConvertTo-NativeQuotedArgument $eventLogFile)
) -join " "

$action = New-ScheduledTaskAction -Execute $installedExecutable -Argument $agentArguments
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
$eventAction = New-ScheduledTaskAction -Execute $installedEventExecutable -Argument $eventArguments
$eventTaskArguments = @{
    Action = $eventAction
    Principal = $principal
    Settings = $settings
    Description = $eventTaskDescription
}
if ($StartupMode -eq "Standalone") {
    $eventTaskArguments.Trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
}
$eventTask = New-ScheduledTask @eventTaskArguments
Register-ScheduledTask -TaskName $EventTaskName -InputObject $eventTask -Force | Out-Null
Start-ScheduledTask -TaskName $EventTaskName

$eventHealthURI = "http://127.0.0.1:$eventListenPort/healthz"
$eventHealthDeadline = [DateTime]::UtcNow.AddSeconds(20)
$eventHealth = $null
do {
    try {
        $eventHealth = Invoke-RestMethod -Method Get -Uri $eventHealthURI -TimeoutSec 2
    } catch {
        $eventHealth = $null
    }
    if ($eventHealth -and $eventHealth.status -eq "ok") {
        break
    }
    Start-Sleep -Milliseconds 500
} while ([DateTime]::UtcNow -lt $eventHealthDeadline)
if (-not $eventHealth -or $eventHealth.status -ne "ok") {
    Stop-ScheduledTask -TaskName $EventTaskName -ErrorAction SilentlyContinue
    throw "scheduled event stream did not become healthy at $eventHealthURI; inspect $eventLogFile"
}
$eventListener = Get-NetTCPConnection -State Listen -LocalPort $eventListenPort -ErrorAction Stop |
    Select-Object -First 1
$eventProcess = Get-Process -Id $eventListener.OwningProcess -ErrorAction Stop
if ($eventProcess.Path -ne $installedEventExecutable) {
    Stop-ScheduledTask -TaskName $EventTaskName -ErrorAction SilentlyContinue
    throw "port $eventListenPort is served by unexpected executable: $($eventProcess.Path)"
}
if ($eventProcess.SessionId -eq 0) {
    Stop-ScheduledTask -TaskName $EventTaskName -ErrorAction SilentlyContinue
    throw "event stream started in Session 0; an interactive user session is required"
}

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
    Stop-ScheduledTask -TaskName $EventTaskName -ErrorAction SilentlyContinue
    throw "scheduled agent did not become healthy at $healthURI; inspect $logFile"
}

$listener = Get-NetTCPConnection -State Listen -LocalPort $listenPort -ErrorAction Stop |
    Select-Object -First 1
$process = Get-Process -Id $listener.OwningProcess -ErrorAction Stop
if ($process.Path -ne $installedExecutable) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    Stop-ScheduledTask -TaskName $EventTaskName -ErrorAction SilentlyContinue
    throw "port $listenPort is served by unexpected executable: $($process.Path)"
}
if ($process.SessionId -eq 0) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    Stop-ScheduledTask -TaskName $EventTaskName -ErrorAction SilentlyContinue
    throw "agent started in Session 0; WGC requires the interactive user session"
}

[ordered]@{
	startup_mode = $StartupMode
    task_name = $TaskName
    task_state = (Get-ScheduledTask -TaskName $TaskName).State.ToString()
	task_trigger_count = @((Get-ScheduledTask -TaskName $TaskName).Triggers | Where-Object { $null -ne $_ }).Count
	task_restart_count = [int](Get-ScheduledTask -TaskName $TaskName).Settings.RestartCount
    executable = $installedExecutable
    script_launcher = (Join-Path $binDir "windows-observation-job.exe")
    script_runner = (Join-Path $binDir "windows-observation-script-runner.exe")
    observer = (Join-Path $binDir "windows-observer.exe")
    event_task_name = $EventTaskName
    event_task_state = (Get-ScheduledTask -TaskName $EventTaskName).State.ToString()
	event_task_trigger_count = @((Get-ScheduledTask -TaskName $EventTaskName).Triggers | Where-Object { $null -ne $_ }).Count
	event_task_restart_count = [int](Get-ScheduledTask -TaskName $EventTaskName).Settings.RestartCount
    event_executable = $installedEventExecutable
    event_data_dir = $eventDataDir
    event_token_file = $eventTokenFile
    event_log_file = $eventLogFile
    event_listen = $EventListen
    event_process_id = $eventProcess.Id
    event_session_id = $eventProcess.SessionId
    event_health = $eventHealth.status
    data_dir = $resolvedDataDir
    rules_dir = $installedRules
    ocr_runtime_root = $installedOCRRuntime
    legacy_script_api_token_removed = $legacyScriptAPITokenRemoved
    capture_archive = $captureArchive
    log_file = $logFile
    runtime_log_file = $runtimeLogFile
    wgc_trace = $WGCTrace
    crash_dump_dir = $dumpDir
    crash_dump_registry = $dumpRegistryPath
    listen = $Listen
    process_id = $process.Id
    session_id = $process.SessionId
    health = $health.status
    firewall_changed = $false
    service_created = $false
} | ConvertTo-Json
