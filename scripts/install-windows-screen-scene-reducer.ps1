# Installs one executable-scoped deterministic screen scene reducer task.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$RulePath,

    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [string]$AgentDataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [string]$EventBaseURL = "http://127.0.0.1:8788",
    [string]$TaskName
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

throw "screen-scene-reducer is retired: ScreenParser is now an on-demand preprocessor and does not publish a raw detection event stream"

function Assert-ExactProperties {
    param(
        [Parameter(Mandatory = $true)]$Value,
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string[]]$Properties
    )
    $actual = @($Value.PSObject.Properties.Name)
    $missing = @($Properties | Where-Object { $_ -notin $actual })
    $unknown = @($actual | Where-Object { $_ -notin $Properties })
    if ($missing.Count -gt 0) {
        throw "$Name missing required fields: $($missing -join ', ')"
    }
    if ($unknown.Count -gt 0) {
        throw "$Name has unknown fields: $($unknown -join ', ')"
    }
}

function ConvertTo-NativeQuotedArgument {
    param([Parameter(Mandatory = $true)][string]$Value)
    if ($Value.Contains('"')) {
        throw "task arguments must not contain a double quote: $Value"
    }
    return '"' + $Value + '"'
}

function Assert-WindowsGUISubsystem {
    param([Parameter(Mandatory = $true)][string]$Path)
    $bytes = [IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt 256 -or $bytes[0] -ne 0x4d -or $bytes[1] -ne 0x5a) {
        throw "screen scene reducer executable is not a valid PE image: $Path"
    }
    $peOffset = [BitConverter]::ToInt32($bytes, 0x3c)
    if ($peOffset -lt 0 -or $peOffset + 94 -gt $bytes.Length -or
        $bytes[$peOffset] -ne 0x50 -or $bytes[$peOffset + 1] -ne 0x45 -or
        $bytes[$peOffset + 2] -ne 0 -or $bytes[$peOffset + 3] -ne 0) {
        throw "screen scene reducer executable has an invalid PE header: $Path"
    }
    $optionalHeader = $peOffset + 24
    $subsystem = [BitConverter]::ToUInt16($bytes, $optionalHeader + 68)
    if ($subsystem -ne 2) {
        throw "screen scene reducer must use the Windows GUI subsystem to avoid stealing foreground focus"
    }
}

if (-not [Environment]::Is64BitOperatingSystem) {
    throw "screen scene reducer requires 64-bit Windows"
}
if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}

$sourceRule = [IO.Path]::GetFullPath($RulePath)
if (-not (Test-Path -LiteralPath $sourceRule -PathType Container)) {
    throw "RulePath must be an existing directory: $sourceRule"
}
$targetExecutable = Split-Path -Leaf $sourceRule
if (-not $targetExecutable.EndsWith(".exe", [StringComparison]::OrdinalIgnoreCase)) {
    throw "RulePath leaf must be a canonical executable name ending in .exe"
}
$sourceManifest = Join-Path $sourceRule "Reactors\screen-scene\manifest.json"
if (-not (Test-Path -LiteralPath $sourceManifest -PathType Leaf)) {
    throw "screen scene reducer manifest does not exist: $sourceManifest"
}
$manifest = Get-Content -LiteralPath $sourceManifest -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
Assert-ExactProperties $manifest "manifest" @("schemaVersion", "moduleId", "kind", "runtime", "targetExecutable", "input", "output", "reducer")
Assert-ExactProperties $manifest.input "manifest.input" @("moduleId", "stream", "parsedType", "lifecycleType", "failureType")
Assert-ExactProperties $manifest.output "manifest.output" @("stream", "sceneChangedType", "sceneStableType", "foregroundChangedType", "sourceFailureType")
Assert-ExactProperties $manifest.reducer "manifest.reducer" @("positionQuantum", "changeThreshold", "stableIntervalMs", "maxRegions")
if ($manifest.schemaVersion -ne 1 -or $manifest.kind -cne "reactor" -or $manifest.runtime -cne "screen-scene-reducer-v1") {
    throw "screen scene reducer manifest must declare schemaVersion=1, kind=reactor, runtime=screen-scene-reducer-v1"
}
if ($manifest.targetExecutable -cne $targetExecutable) {
    throw "screen scene reducer targetExecutable does not match RulePath"
}
if ($manifest.input.stream -ceq $manifest.output.stream) {
    throw "screen scene reducer input and output streams must differ"
}
if ($manifest.reducer.positionQuantum -le 0 -or $manifest.reducer.positionQuantum -gt 0.25 -or
    $manifest.reducer.changeThreshold -le 0 -or $manifest.reducer.changeThreshold -gt 1 -or
    $manifest.reducer.stableIntervalMs -lt 1000 -or $manifest.reducer.stableIntervalMs -gt 86400000 -or
    $manifest.reducer.maxRegions -lt 1 -or $manifest.reducer.maxRegions -gt 64) {
    throw "screen scene reducer parameters are outside the supported contract"
}

$sourceExecutable = [IO.Path]::GetFullPath($ExecutablePath)
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    throw "screen scene reducer executable does not exist: $sourceExecutable"
}
Assert-WindowsGUISubsystem $sourceExecutable
$resolvedAgentDataDir = [IO.Path]::GetFullPath($AgentDataDir)
$eventMatch = [regex]::Match($EventBaseURL, '^http://127\.0\.0\.1:([0-9]{1,5})$')
if (-not $eventMatch.Success) {
    throw "EventBaseURL must use exact http://127.0.0.1:<port> form"
}
$eventPort = [int]$eventMatch.Groups[1].Value
if ($eventPort -lt 1 -or $eventPort -gt 65535) {
    throw "EventBaseURL port must be between 1 and 65535"
}
$tokenFile = Join-Path $resolvedAgentDataDir "event-api.token"
if (-not (Test-Path -LiteralPath $tokenFile -PathType Leaf)) {
    throw "event API token does not exist: $tokenFile"
}
try {
    $health = Invoke-RestMethod -Method Get -Uri ($EventBaseURL + "/healthz") -TimeoutSec 3
} catch {
    throw "event stream health check failed before reducer installation: $($_.Exception.Message)"
}
if (-not $health -or $health.status -ne "ok") {
    throw "event stream is not healthy before reducer installation"
}

if (-not $TaskName) {
    $TaskName = "gameGuide Screen Scene Reducer " + $targetExecutable
}
$taskDescription = "gameGuide deterministic screen scene reducer for $targetExecutable"
$existingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existingTask) {
    if ($existingTask.Description -ne $taskDescription) {
        throw "scheduled task '$TaskName' exists but is not owned by this screen scene reducer"
    }
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}

$binDir = Join-Path $resolvedAgentDataDir "bin"
$installedRulesDir = Join-Path $resolvedAgentDataDir "Rules"
$installedRule = Join-Path $installedRulesDir $targetExecutable
$installedManifest = Join-Path $installedRule "Reactors\screen-scene\manifest.json"
$instanceRoot = Join-Path $resolvedAgentDataDir ("screen-reducer\instances\" + $targetExecutable)
$dataDir = Join-Path $instanceRoot "data"
$logDir = Join-Path $instanceRoot "logs"
$installedExecutable = Join-Path $binDir "windows-screen-scene-reducer.exe"
$stateFile = Join-Path $dataDir "state.json"
$statusFile = Join-Path $dataDir "status.json"
$logFile = Join-Path $logDir "screen-scene-reducer.jsonl"
New-Item -ItemType Directory -Path $binDir -Force | Out-Null
New-Item -ItemType Directory -Path $installedRulesDir -Force | Out-Null
New-Item -ItemType Directory -Path $dataDir -Force | Out-Null
New-Item -ItemType Directory -Path $logDir -Force | Out-Null

& (Join-Path $PSScriptRoot "sync-windows-agent-rule.ps1") `
    -SourceRulePath $sourceRule `
    -DestinationRulesDir $installedRulesDir | Out-Null
if (Test-Path -LiteralPath $installedExecutable -PathType Leaf) {
    $exitDeadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $ownedProcesses = @(
            Get-Process -Name "windows-screen-scene-reducer" -ErrorAction SilentlyContinue |
                Where-Object { $_.Path -ceq $installedExecutable }
        )
        if ($ownedProcesses.Count -eq 0) {
            break
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $exitDeadline)
    if ($ownedProcesses.Count -ne 0) {
        throw "installed screen scene reducer did not exit before replacement: $installedExecutable"
    }
}
if ($sourceExecutable -cne $installedExecutable) {
    Copy-Item -LiteralPath $sourceExecutable -Destination $installedExecutable -Force
}
if (-not (Test-Path -LiteralPath $stateFile -PathType Leaf)) {
    $initialState = [ordered]@{
        schemaVersion = 1
        moduleId = [string]$manifest.moduleId
        inputStream = [string]$manifest.input.stream
        cursor = 0
        lastOutputSequence = 0
        lastSummarySequence = 0
    }
    [IO.File]::WriteAllText(
        $stateFile,
        (($initialState | ConvertTo-Json -Compress) + "`n"),
        [Text.UTF8Encoding]::new($false)
    )
}

$runtimeArguments = @(
    "--config", (ConvertTo-NativeQuotedArgument $installedManifest),
    "--event-base-url", (ConvertTo-NativeQuotedArgument $EventBaseURL),
    "--token-file", (ConvertTo-NativeQuotedArgument $tokenFile),
    "--state-file", (ConvertTo-NativeQuotedArgument $stateFile),
    "--log-file", (ConvertTo-NativeQuotedArgument $logFile),
    "--status-file", (ConvertTo-NativeQuotedArgument $statusFile)
) -join " "
$validation = Start-Process -FilePath $installedExecutable -ArgumentList ($runtimeArguments + " --validate-only") -Wait -PassThru
if ($validation.ExitCode -ne 0) {
    throw "screen scene reducer contract validation failed with exit code $($validation.ExitCode)"
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
if (-not $identity) {
    throw "could not resolve the current Windows identity"
}
$action = New-ScheduledTaskAction -Execute $installedExecutable -Argument $runtimeArguments
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
$task = New-ScheduledTask -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description $taskDescription
Register-ScheduledTask -TaskName $TaskName -InputObject $task -Force | Out-Null
if (Test-Path -LiteralPath $statusFile -PathType Leaf) {
    Remove-Item -LiteralPath $statusFile -Force
}
Start-ScheduledTask -TaskName $TaskName

$deadline = [DateTime]::UtcNow.AddSeconds(30)
$status = $null
do {
    Start-Sleep -Milliseconds 250
    if (Test-Path -LiteralPath $statusFile -PathType Leaf) {
        try {
            $status = Get-Content -LiteralPath $statusFile -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
        } catch {
            $status = $null
        }
    }
    if ($status -and ($status.state -eq "active" -or $status.state -eq "failed")) {
        break
    }
} while ([DateTime]::UtcNow -lt $deadline)
if (-not $status -or $status.state -ne "active") {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    $hasError = $status -and @($status.PSObject.Properties.Name) -contains "error"
    $detail = if ($hasError) { $status.error } else { "status did not become active" }
    throw "screen scene reducer did not start: $detail; inspect $logFile"
}

[pscustomobject]@{
    taskName = $TaskName
    executable = $installedExecutable
    manifest = $installedManifest
    stateFile = $stateFile
    statusFile = $statusFile
    logFile = $logFile
    cursor = $status.cursor
    lastOutputSequence = $status.lastOutputSequence
}
