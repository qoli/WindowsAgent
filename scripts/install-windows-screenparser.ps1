# Installs the finite ScreenParser DirectML Action without a background task.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$RulePath,

    [Parameter(Mandatory = $true)]
    [string]$ModelPath,

    [Parameter(Mandatory = $true)]
    [string]$RuntimeBundlePath,

    [string]$AgentDataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"),
    [string]$LegacyLoopTaskName,
    [string]$LegacyReducerTaskName
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

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

function Assert-CanonicalSha256 {
    param([Parameter(Mandatory = $true)][string]$Value, [Parameter(Mandatory = $true)][string]$Name)
    if ($Value -cnotmatch '^[0-9a-f]{64}$') {
        throw "$Name must be 64 lowercase hexadecimal characters"
    }
}

function Remove-OwnedScheduledTask {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Description
    )
    $task = Get-ScheduledTask -TaskName $Name -ErrorAction SilentlyContinue
    if (-not $task) {
        return $false
    }
    if ($task.Description -cne $Description) {
        throw "scheduled task '$Name' exists but is not owned by the retired ScreenParser pipeline"
    }
    Stop-ScheduledTask -TaskName $Name -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $Name -Confirm:$false -ErrorAction Stop
    return $true
}

function Wait-NoProcess {
    param([Parameter(Mandatory = $true)][string]$Name)
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $processes = @(Get-Process -Name $Name -ErrorAction SilentlyContinue)
        if ($processes.Count -eq 0) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "retired background process did not exit: $Name"
}

if (-not [Environment]::Is64BitOperatingSystem) {
    throw "ScreenParser DirectML requires 64-bit Windows"
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
$sourceManifest = Join-Path $sourceRule "Actions\screenparser\manifest.json"
if (-not (Test-Path -LiteralPath $sourceManifest -PathType Leaf)) {
    throw "ScreenParser manifest does not exist: $sourceManifest"
}
$manifest = Get-Content -LiteralPath $sourceManifest -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
Assert-ExactProperties $manifest "manifest" @("schemaVersion", "moduleId", "kind", "runtime", "targetExecutable", "model", "inference")
if ($manifest.schemaVersion -ne 1 -or $manifest.kind -cne "action" -or $manifest.runtime -cne "screenparser-onnx-dml-v1") {
    throw "ScreenParser manifest must declare schemaVersion=1, kind=action, runtime=screenparser-onnx-dml-v1"
}
if ($manifest.targetExecutable -cne $targetExecutable) {
    throw "ScreenParser manifest targetExecutable does not match RulePath: $($manifest.targetExecutable)"
}
Assert-ExactProperties $manifest.model "manifest.model" @("artifactId", "format", "filename", "sha256", "precision", "opset", "inputName", "outputName", "inputWidth", "inputHeight", "labels", "source")
Assert-ExactProperties $manifest.inference "manifest.inference" @("confidence", "iou", "maxDetections", "device")
if ($manifest.model.format -cne "onnx" -or $manifest.model.precision -cne "fp16" -or $manifest.model.opset -gt 20) {
    throw "ScreenParser model must be an fp16 ONNX artifact with opset no greater than 20"
}
Assert-CanonicalSha256 $manifest.model.sha256 "manifest.model.sha256"
if ($manifest.inference.device -cne "directml:0") {
    throw "ScreenParser manifest inference.device must equal directml:0"
}

$sourceModel = [IO.Path]::GetFullPath($ModelPath)
if (-not (Test-Path -LiteralPath $sourceModel -PathType Leaf)) {
    throw "ScreenParser ONNX model does not exist: $sourceModel"
}
if ((Split-Path -Leaf $sourceModel) -cne $manifest.model.filename) {
    throw "ScreenParser model filename must equal manifest.model.filename: $($manifest.model.filename)"
}
$modelHash = (Get-FileHash -LiteralPath $sourceModel -Algorithm SHA256).Hash.ToLowerInvariant()
if ($modelHash -cne $manifest.model.sha256) {
    throw "ScreenParser ONNX sha256 mismatch: expected=$($manifest.model.sha256) actual=$modelHash"
}

$sourceRuntimeBundle = [IO.Path]::GetFullPath($RuntimeBundlePath)
if (-not (Test-Path -LiteralPath $sourceRuntimeBundle -PathType Container)) {
    throw "RuntimeBundlePath must be an existing directory: $sourceRuntimeBundle"
}
$sourceRuntimeManifest = Join-Path $sourceRuntimeBundle "runtime-artifact.json"
if (-not (Test-Path -LiteralPath $sourceRuntimeManifest -PathType Leaf)) {
    throw "runtime artifact manifest does not exist: $sourceRuntimeManifest"
}
$runtimeArtifact = Get-Content -LiteralPath $sourceRuntimeManifest -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
Assert-ExactProperties $runtimeArtifact "runtime-artifact" @("schemaVersion", "runtimeId", "filename", "sha256", "bytes", "architecture", "subsystem", "selfContained", "targetFramework", "onnxRuntimeDirectML")
if ($runtimeArtifact.schemaVersion -ne 1 -or
    $runtimeArtifact.runtimeId -cne "screenparser-onnx-dml-v1" -or
    $runtimeArtifact.filename -cne "ScreenParser.DirectML.exe" -or
    $runtimeArtifact.architecture -cne "win-x64" -or
    $runtimeArtifact.subsystem -cne "console" -or
    $runtimeArtifact.selfContained -ne $true -or
    $runtimeArtifact.targetFramework -cne "net8.0-windows" -or
    $runtimeArtifact.onnxRuntimeDirectML -cne "1.24.4") {
    throw "runtime artifact manifest does not describe the pinned on-demand ScreenParser DirectML runtime"
}
Assert-CanonicalSha256 $runtimeArtifact.sha256 "runtime-artifact.sha256"
$sourceRuntime = Join-Path $sourceRuntimeBundle $runtimeArtifact.filename
if (-not (Test-Path -LiteralPath $sourceRuntime -PathType Leaf)) {
    throw "runtime executable does not exist: $sourceRuntime"
}
if ((Get-Item -LiteralPath $sourceRuntime).Length -ne $runtimeArtifact.bytes) {
    throw "runtime executable byte length does not match runtime-artifact.json"
}
$runtimeHash = (Get-FileHash -LiteralPath $sourceRuntime -Algorithm SHA256).Hash.ToLowerInvariant()
if ($runtimeHash -cne $runtimeArtifact.sha256) {
    throw "runtime executable sha256 mismatch: expected=$($runtimeArtifact.sha256) actual=$runtimeHash"
}

& $sourceRuntime --config $sourceManifest --model $sourceModel --validate-only | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "ScreenParser DirectML Action validation failed with exit code $LASTEXITCODE"
}

if (-not $LegacyLoopTaskName) {
    $LegacyLoopTaskName = "gameGuide ScreenParser " + $targetExecutable
}
if (-not $LegacyReducerTaskName) {
    $LegacyReducerTaskName = "gameGuide Screen Scene Reducer " + $targetExecutable
}
$legacyLoopRemoved = Remove-OwnedScheduledTask `
    -Name $LegacyLoopTaskName `
    -Description "gameGuide ScreenParser DirectML loop for $targetExecutable; interactive-user session required"
$legacyReducerRemoved = Remove-OwnedScheduledTask `
    -Name $LegacyReducerTaskName `
    -Description "gameGuide deterministic screen scene reducer for $targetExecutable"
Wait-NoProcess -Name "ScreenParser.DirectML"
Wait-NoProcess -Name "windows-screen-scene-reducer"

$resolvedAgentDataDir = [IO.Path]::GetFullPath($AgentDataDir)
$screenParserRoot = Join-Path $resolvedAgentDataDir "screenparser"
$runtimeDir = Join-Path $screenParserRoot ("runtimes\" + $runtimeArtifact.runtimeId + "\" + $runtimeArtifact.sha256)
$modelDir = Join-Path $screenParserRoot ("models\" + $manifest.model.artifactId)
$installedRuntime = Join-Path $runtimeDir $runtimeArtifact.filename
$installedRuntimeManifest = Join-Path $runtimeDir "runtime-artifact.json"
$installedModel = Join-Path $modelDir $manifest.model.filename
$installedRulesDir = Join-Path $resolvedAgentDataDir "Rules"
$installedManifest = Join-Path $installedRulesDir ($targetExecutable + "\Actions\screenparser\manifest.json")
New-Item -ItemType Directory -Path $runtimeDir -Force | Out-Null
New-Item -ItemType Directory -Path $modelDir -Force | Out-Null
New-Item -ItemType Directory -Path $installedRulesDir -Force | Out-Null

if (Test-Path -LiteralPath $installedRuntime -PathType Leaf) {
    $installedRuntimeHash = (Get-FileHash -LiteralPath $installedRuntime -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($installedRuntimeHash -cne $runtimeArtifact.sha256) {
        throw "installed runtime artifact has conflicting content: $installedRuntime"
    }
} else {
    Copy-Item -LiteralPath $sourceRuntime -Destination $installedRuntime
}
if (Test-Path -LiteralPath $installedRuntimeManifest -PathType Leaf) {
    $installedArtifact = Get-Content -LiteralPath $installedRuntimeManifest -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if ($installedArtifact.sha256 -cne $runtimeArtifact.sha256 -or $installedArtifact.subsystem -cne "console") {
        throw "installed runtime manifest has conflicting content: $installedRuntimeManifest"
    }
} else {
    Copy-Item -LiteralPath $sourceRuntimeManifest -Destination $installedRuntimeManifest
}
if (Test-Path -LiteralPath $installedModel -PathType Leaf) {
    $installedModelHash = (Get-FileHash -LiteralPath $installedModel -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($installedModelHash -cne $manifest.model.sha256) {
        throw "installed model artifact has conflicting content: $installedModel"
    }
} else {
    Copy-Item -LiteralPath $sourceModel -Destination $installedModel
}

& (Join-Path $PSScriptRoot "sync-windows-agent-rule.ps1") `
    -SourceRulePath $sourceRule `
    -DestinationRulesDir $installedRulesDir | Out-Null

[ordered]@{
    mode = "on-demand"
    target_executable = $targetExecutable
    runtime = $installedRuntime
    runtime_id = $runtimeArtifact.runtimeId
    runtime_sha256 = $runtimeArtifact.sha256
    manifest = $installedManifest
    model = $installedModel
    model_artifact_id = $manifest.model.artifactId
    model_sha256 = $manifest.model.sha256
    legacy_loop_task_removed = $legacyLoopRemoved
    legacy_reducer_task_removed = $legacyReducerRemoved
    background_task_created = $false
    background_process_count = 0
    event_stream_required = $false
    python_installed = $false
    cuda_toolkit_installed = $false
    firewall_changed = $false
    service_created = $false
} | ConvertTo-Json
