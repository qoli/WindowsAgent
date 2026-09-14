# Prepares the pinned Node bundle and migrates model authorization into a dedicated Pi profile.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$RuntimeBundlePath,
    [Parameter(Mandatory = $true)][string]$SourceAuthPath,
    [Parameter(Mandatory = $true)][string]$SourceSettingsPath,
    [string]$NodePath,
    [string]$NpmPath,
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-agent-pi")
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Copy-VerifiedFile {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$Label
    )
    Copy-Item -LiteralPath $Source -Destination $Destination -Force
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $Source).Hash -cne
        (Get-FileHash -Algorithm SHA256 -LiteralPath $Destination).Hash) {
        throw "$Label copy hash mismatch"
    }
}

if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}
$runtimeDir = [IO.Path]::GetFullPath($RuntimeBundlePath)
$sourceAuth = [IO.Path]::GetFullPath($SourceAuthPath)
$sourceSettings = [IO.Path]::GetFullPath($SourceSettingsPath)
foreach ($required in @($sourceAuth, $sourceSettings, (Join-Path $runtimeDir "package.json"), (Join-Path $runtimeDir "package-lock.json"))) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "required preparation input does not exist: $required"
    }
}

$auth = Get-Content -LiteralPath $sourceAuth -Raw | ConvertFrom-Json -ErrorAction Stop
if ($auth -isnot [pscustomobject] -or @($auth.PSObject.Properties).Count -eq 0) {
    throw "source Pi authorization must be a non-empty JSON object"
}
$settings = Get-Content -LiteralPath $sourceSettings -Raw | ConvertFrom-Json -ErrorAction Stop
if ($settings -isnot [pscustomobject]) {
    throw "source Pi settings must be a JSON object"
}
foreach ($requiredSetting in @("defaultProvider", "defaultModel")) {
    if (-not ($settings.PSObject.Properties.Name -ccontains $requiredSetting) -or
        [string]::IsNullOrWhiteSpace([string]$settings.$requiredSetting)) {
        throw "source Pi settings are missing $requiredSetting"
    }
}

if (-not $NodePath) {
    $NodePath = (Get-Command node.exe -ErrorAction Stop).Source
}
if (-not $NpmPath) {
    $NpmPath = (Get-Command npm.cmd -ErrorAction Stop).Source
}
$resolvedNode = [IO.Path]::GetFullPath($NodePath)
$resolvedNpm = [IO.Path]::GetFullPath($NpmPath)
foreach ($required in @($resolvedNode, $resolvedNpm)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "required Node tool does not exist: $required"
    }
}
$nodeVersion = & $resolvedNode --version
if ($LASTEXITCODE -ne 0 -or $nodeVersion -notmatch '^v(\d+)\.(\d+)\.(\d+)$') {
    throw "could not resolve the installed Node version"
}
if ([int]$Matches[1] -lt 22 -or ([int]$Matches[1] -eq 22 -and [int]$Matches[2] -lt 19)) {
    throw "windows-agent-pi requires Node 22.19.0 or newer; found $nodeVersion"
}
$nodeDirectory = [IO.Path]::GetDirectoryName($resolvedNode)
$env:Path = $nodeDirectory + [IO.Path]::PathSeparator + $env:Path
$childNode = Get-Command node.exe -ErrorAction Stop
if ([IO.Path]::GetFullPath($childNode.Source) -ine $resolvedNode) {
    throw "Node child-process resolution differs from the verified executable"
}

$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$agentDir = Join-Path $resolvedDataDir "pi-agent"
$sessionDir = Join-Path $resolvedDataDir "sessions"
$piWebDataDir = Join-Path $resolvedDataDir "pi-web"
$helperPath = Join-Path $resolvedDataDir "helpers\pi-computer-use\windows-bridge.exe"
$installedAuth = Join-Path $agentDir "auth.json"
$installedSettings = Join-Path $agentDir "settings.json"
$piWebConfig = Join-Path $piWebDataDir "config.json"
New-Item -ItemType Directory -Force -Path @($agentDir, $sessionDir, $piWebDataDir, ([IO.Path]::GetDirectoryName($helperPath))) | Out-Null

Copy-VerifiedFile -Source $sourceAuth -Destination $installedAuth -Label "Pi authorization"
$migratedSettings = [ordered]@{
    defaultProvider = [string]$settings.defaultProvider
    defaultModel = [string]$settings.defaultModel
}
foreach ($optionalSetting in @("defaultThinkingLevel", "theme", "lastChangelogVersion")) {
    if ($settings.PSObject.Properties.Name -ccontains $optionalSetting) {
        $migratedSettings[$optionalSetting] = $settings.$optionalSetting
    }
}
[IO.File]::WriteAllText($installedSettings, ($migratedSettings | ConvertTo-Json -Depth 10), [Text.UTF8Encoding]::new($false))
if (-not (Test-Path -LiteralPath $piWebConfig -PathType Leaf)) {
    [IO.File]::WriteAllText($piWebConfig, "{}", [Text.UTF8Encoding]::new($false))
}

$env:PI_CODING_AGENT_DIR = $agentDir
$env:PI_CODING_AGENT_SESSION_DIR = $sessionDir
$env:PI_WEB_DATA_DIR = $piWebDataDir
$env:PI_WEB_CONFIG = $piWebConfig
$env:PI_COMPUTER_USE_WINDOWS_HELPER_PATH = $helperPath
Remove-Item Env:PI_COMPUTER_USE_ALLOW_BUILD -ErrorAction SilentlyContinue

Push-Location $runtimeDir
try {
    & $resolvedNpm ci
    if ($LASTEXITCODE -ne 0) {
        throw "npm ci failed with exit code $LASTEXITCODE"
    }
    & $resolvedNpm run pi:install-computer-use
    if ($LASTEXITCODE -ne 0) {
        throw "pinned pi-computer-use profile installation failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

$preparedSettings = Get-Content -LiteralPath $installedSettings -Raw | ConvertFrom-Json -ErrorAction Stop
$configuredPackages = @($preparedSettings.packages | Where-Object {
    if ($_ -is [string]) {
        return $_ -ceq "npm:@injaneity/pi-computer-use@0.5.1"
    }
    return $null -ne $_ -and
        ($_.PSObject.Properties.Name -ccontains "source") -and
        [string]$_.source -ceq "npm:@injaneity/pi-computer-use@0.5.1"
})
if ($configuredPackages.Count -ne 1 -or @($preparedSettings.packages).Count -ne 1) {
    throw "prepared Pi profile must contain only npm:@injaneity/pi-computer-use@0.5.1"
}
if (-not (Test-Path -LiteralPath $helperPath -PathType Leaf)) {
    throw "pinned pi-computer-use did not install its Windows helper: $helperPath"
}

[ordered]@{
    node_version = $nodeVersion
    runtime_dir = $runtimeDir
    runtime_lock_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $runtimeDir "package-lock.json")).Hash.ToLowerInvariant()
    pi_agent_dir = $agentDir
    pi_session_dir = $sessionDir
    pi_web_config = $piWebConfig
    default_provider = [string]$preparedSettings.defaultProvider
    default_model = [string]$preparedSettings.defaultModel
    authorization_provider_count = @($auth.PSObject.Properties).Count
    package = "npm:@injaneity/pi-computer-use@0.5.1"
    helper_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $helperPath).Hash.ToLowerInvariant()
} | ConvertTo-Json -Depth 5
