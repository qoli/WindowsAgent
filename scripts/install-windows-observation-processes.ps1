# Installs resident Evidence and Visual Log control services as independent interactive-user processes.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$EvidenceExecutablePath,

    [Parameter(Mandatory = $true)]
    [string]$VisualLogExecutablePath,

    [Parameter(Mandatory = $true)]
    [uri]$VisualLogModelBaseURL,

    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [string]$EvidenceTaskName = "gameGuide Windows Evidence Recorder",
    [string]$VisualLogTaskName = "gameGuide Windows Visual Log",
    [string]$EvidenceListen = "127.0.0.1:8792",
    [string]$VisualLogListen = "127.0.0.1:8789",
    [timespan]$Timeout = ([timespan]::FromSeconds(20))
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$evidenceDescription = "gameGuide independent resident Evidence control service; interactive-user session required"
$visualLogDescription = "gameGuide independent resident Visual Log control service; interactive-user session required"

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

function Assert-LoopbackListen {
    param([Parameter(Mandatory = $true)][string]$Value, [Parameter(Mandatory = $true)][string]$Label)
    $match = [regex]::Match($Value, "^127\.0\.0\.1:([0-9]{1,5})$")
    if (-not $match.Success) {
        throw "$Label must use explicit loopback form 127.0.0.1:<port>"
    }
    $port = [int]$match.Groups[1].Value
    if ($port -lt 1 -or $port -gt 65535) {
        throw "$Label port must be between 1 and 65535"
    }
    return $port
}

function Assert-RegularTokenFile {
    param([Parameter(Mandatory = $true)][string]$Path, [Parameter(Mandatory = $true)][string]$Label)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Label does not exist: $Path"
    }
    $item = Get-Item -LiteralPath $Path -Force
    if ($item.PSIsContainer -or $item.Length -lt 1 -or $item.Length -gt 4096) {
        throw "$Label must be a regular file between 1 and 4096 bytes: $Path"
    }
}

function Stop-OwnedTaskProcess {
    param(
        [Parameter(Mandatory = $true)][string]$TaskName,
        [Parameter(Mandatory = $true)][string]$Description,
        [string[]]$PreviousDescriptions = @(),
        [Parameter(Mandatory = $true)][string]$ProcessName,
        [Parameter(Mandatory = $true)][string]$ExecutablePath
    )
    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($task) {
        if ($task.Description -ne $Description -and $task.Description -notin $PreviousDescriptions) {
            throw "scheduled task '$TaskName' is not owned by the expected process"
        }
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    }
    $deadline = [DateTime]::UtcNow.Add($Timeout)
    do {
        $running = @(Get-Process -Name $ProcessName -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -eq $ExecutablePath })
        if ($running.Count -eq 0) { return }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "$ProcessName did not stop before the deployment deadline"
}

function Wait-HealthyProcess {
    param(
        [Parameter(Mandatory = $true)][string]$TaskName,
        [Parameter(Mandatory = $true)][string]$ProcessName,
        [Parameter(Mandatory = $true)][string]$ExecutablePath,
        [Parameter(Mandatory = $true)][uri]$HealthURI
    )
    Start-ScheduledTask -TaskName $TaskName
    $deadline = [DateTime]::UtcNow.Add($Timeout)
    do {
        $process = Get-Process -Name $ProcessName -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -eq $ExecutablePath } |
            Select-Object -First 1
        $health = $null
        try {
            $health = Invoke-RestMethod -Uri $HealthURI -TimeoutSec 2
        } catch {
            $health = $null
        }
        if ($process -and $process.SessionId -ne 0 -and $health -and $health.status -eq "ok") {
            return $process
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    throw "scheduled task '$TaskName' did not become healthy at $HealthURI"
}

if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}
if ($Timeout -le [timespan]::Zero) {
    throw "Timeout must be positive"
}
$evidencePort = Assert-LoopbackListen -Value $EvidenceListen -Label "EvidenceListen"
$visualLogPort = Assert-LoopbackListen -Value $VisualLogListen -Label "VisualLogListen"
if ($evidencePort -eq $visualLogPort) {
    throw "EvidenceListen and VisualLogListen must use different ports"
}
if ($VisualLogModelBaseURL.Scheme -notin @("http", "https") -or -not $VisualLogModelBaseURL.Host) {
    throw "VisualLogModelBaseURL must be an absolute HTTP or HTTPS URL"
}

$sourceEvidence = [IO.Path]::GetFullPath($EvidenceExecutablePath)
$sourceVisualLog = [IO.Path]::GetFullPath($VisualLogExecutablePath)
foreach ($source in @($sourceEvidence, $sourceVisualLog)) {
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
        throw "source executable does not exist: $source"
    }
    if ((Get-WindowsPESubsystem -Path $source) -ne 2) {
        throw "independent process executable must use PE Windows GUI subsystem 2: $source"
    }
}

$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$installedEvidence = Join-Path $resolvedDataDir "windows-evidence-recorder.exe"
$installedVisualLog = Join-Path $resolvedDataDir "windows-visual-log.exe"
$evidenceConfig = Join-Path $resolvedDataDir "Rules\EliteDangerous64.exe\Evidence\config.json"
$visualLogConfig = Join-Path $resolvedDataDir "Rules\EliteDangerous64.exe\VisualLog\config.json"
$evidenceToken = Join-Path $resolvedDataDir "evidence.token"
$visualLogToken = Join-Path $resolvedDataDir "visual-log-control.token"
$eventToken = Join-Path $resolvedDataDir "event-api.token"
$modelToken = Join-Path $resolvedDataDir "omlx-api.key"
foreach ($config in @($evidenceConfig, $visualLogConfig)) {
    if (-not (Test-Path -LiteralPath $config -PathType Leaf)) {
        throw "installed Rule configuration does not exist: $config"
    }
}
Assert-RegularTokenFile -Path $evidenceToken -Label "Evidence token"
Assert-RegularTokenFile -Path $visualLogToken -Label "Visual Log control token"
Assert-RegularTokenFile -Path $eventToken -Label "event stream token"
Assert-RegularTokenFile -Path $modelToken -Label "model API key"

Stop-OwnedTaskProcess -TaskName $VisualLogTaskName -Description $visualLogDescription `
    -PreviousDescriptions @("gameGuide independent on-demand Visual Log; interactive-user session required") `
    -ProcessName "windows-visual-log" -ExecutablePath $installedVisualLog
Stop-OwnedTaskProcess -TaskName $EvidenceTaskName -Description $evidenceDescription `
    -PreviousDescriptions @("gameGuide independent finite Evidence recorder; interactive-user session required") `
    -ProcessName "windows-evidence-recorder" -ExecutablePath $installedEvidence

Copy-Item -LiteralPath $sourceEvidence -Destination $installedEvidence -Force
Copy-Item -LiteralPath $sourceVisualLog -Destination $installedVisualLog -Force
foreach ($pair in @(@($sourceEvidence, $installedEvidence), @($sourceVisualLog, $installedVisualLog))) {
    if ((Get-FileHash -LiteralPath $pair[0] -Algorithm SHA256).Hash -ne `
        (Get-FileHash -LiteralPath $pair[1] -Algorithm SHA256).Hash) {
        throw "installed executable hash differs from source: $($pair[1])"
    }
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
if (-not $identity) {
    throw "could not resolve the current Windows identity"
}
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
$settingsArguments = @{
    AllowStartIfOnBatteries = $true
    DontStopIfGoingOnBatteries = $true
    StartWhenAvailable = $true
    Hidden = $true
    ExecutionTimeLimit = [timespan]::Zero
    MultipleInstances = "IgnoreNew"
}
$settings = New-ScheduledTaskSettingsSet @settingsArguments

$evidenceArguments = @(
    "--config", (ConvertTo-NativeQuotedArgument $evidenceConfig),
    "--listen", $EvidenceListen,
    "--data-dir", (ConvertTo-NativeQuotedArgument (Join-Path $resolvedDataDir "evidence-data")),
    "--token-file", (ConvertTo-NativeQuotedArgument $evidenceToken)
) -join " "
$evidenceTask = New-ScheduledTask `
    -Action (New-ScheduledTaskAction -Execute $installedEvidence -Argument $evidenceArguments) `
    -Principal $principal `
    -Settings $settings `
    -Description $evidenceDescription
Register-ScheduledTask -TaskName $EvidenceTaskName -InputObject $evidenceTask -Force | Out-Null

$visualLogArguments = @(
    "--config", (ConvertTo-NativeQuotedArgument $visualLogConfig),
    "--model-base-url", $VisualLogModelBaseURL.AbsoluteUri.TrimEnd('/'),
    "--model-api-key-file", (ConvertTo-NativeQuotedArgument $modelToken),
    "--event-base-url", "http://127.0.0.1:8788",
    "--event-token-file", (ConvertTo-NativeQuotedArgument $eventToken),
    "--control-listen", $VisualLogListen,
    "--control-token-file", (ConvertTo-NativeQuotedArgument $visualLogToken),
    "--log-file", (ConvertTo-NativeQuotedArgument (Join-Path $resolvedDataDir "visual-log.jsonl")),
    "--status-file", (ConvertTo-NativeQuotedArgument (Join-Path $resolvedDataDir "visual-log-status.json"))
) -join " "
$visualLogTask = New-ScheduledTask `
    -Action (New-ScheduledTaskAction -Execute $installedVisualLog -Argument $visualLogArguments) `
    -Principal $principal `
    -Settings $settings `
    -Description $visualLogDescription
Register-ScheduledTask -TaskName $VisualLogTaskName -InputObject $visualLogTask -Force | Out-Null

$evidenceProcess = Wait-HealthyProcess `
    -TaskName $EvidenceTaskName `
    -ProcessName "windows-evidence-recorder" `
    -ExecutablePath $installedEvidence `
    -HealthURI ([uri]("http://" + $EvidenceListen + "/healthz"))
$visualLogProcess = Wait-HealthyProcess `
    -TaskName $VisualLogTaskName `
    -ProcessName "windows-visual-log" `
    -ExecutablePath $installedVisualLog `
    -HealthURI ([uri]("http://" + $VisualLogListen + "/healthz"))

[ordered]@{
    evidence = [ordered]@{
        task_name = $EvidenceTaskName
        task_state = (Get-ScheduledTask -TaskName $EvidenceTaskName).State.ToString()
        trigger_count = @((Get-ScheduledTask -TaskName $EvidenceTaskName).Triggers | Where-Object { $null -ne $_ }).Count
        restart_count = [int](Get-ScheduledTask -TaskName $EvidenceTaskName).Settings.RestartCount
        executable = $installedEvidence
        sha256 = (Get-FileHash -LiteralPath $installedEvidence -Algorithm SHA256).Hash.ToLowerInvariant()
        process_id = $evidenceProcess.Id
        session_id = $evidenceProcess.SessionId
        health = "ok"
    }
    visual_log = [ordered]@{
        task_name = $VisualLogTaskName
        task_state = (Get-ScheduledTask -TaskName $VisualLogTaskName).State.ToString()
        trigger_count = @((Get-ScheduledTask -TaskName $VisualLogTaskName).Triggers | Where-Object { $null -ne $_ }).Count
        restart_count = [int](Get-ScheduledTask -TaskName $VisualLogTaskName).Settings.RestartCount
        executable = $installedVisualLog
        sha256 = (Get-FileHash -LiteralPath $installedVisualLog -Algorithm SHA256).Hash.ToLowerInvariant()
        process_id = $visualLogProcess.Id
        session_id = $visualLogProcess.SessionId
        health = "ok"
        model_base_url = $VisualLogModelBaseURL.AbsoluteUri.TrimEnd('/')
    }
    lifecycle_owner = "independent"
    watchdog_managed = $true
} | ConvertTo-Json -Depth 5
