[CmdletBinding()]
param(
    [string]$OutputDir = (Join-Path $PSScriptRoot "..\.build"),
    [string]$Version = "dev"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ($env:OS -cne "Windows_NT") {
    throw "WinUI AssistGUI must be built on Windows"
}
if ($Version -cnotmatch '^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$') {
    throw "AssistGUI version is not canonical: $Version"
}
$managedVersion = "0.0.0"
if ($Version -cmatch '^([0-9]+\.[0-9]+\.[0-9]+)(?:[-+].*)?$') {
    $managedVersion = $Matches[1]
}

$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$project = Join-Path $repositoryRoot "ui\windows-assist-gui\WindowsAssistGUI.csproj"
if (-not (Test-Path -LiteralPath $project -PathType Leaf)) {
    throw "WinUI AssistGUI project is missing: $project"
}

$resolvedOutput = [IO.Path]::GetFullPath($OutputDir)
New-Item -ItemType Directory -Path $resolvedOutput -Force | Out-Null
$publishDir = Join-Path ([IO.Path]::GetTempPath()) ("windows-assist-gui-publish-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $publishDir -Force | Out-Null

try {
    & dotnet publish $project `
        --configuration Release `
        --runtime win-x64 `
        --self-contained true `
        "-p:Platform=x64" `
        "-p:Version=$managedVersion" `
        "-p:InformationalVersion=$Version" `
        --output $publishDir
    if ($LASTEXITCODE -ne 0) {
        throw "dotnet publish failed with exit code $LASTEXITCODE"
    }

    $publishedExecutable = Join-Path $publishDir "windows-assist-gui.exe"
    if (-not (Test-Path -LiteralPath $publishedExecutable -PathType Leaf)) {
        throw "WinUI publish did not produce windows-assist-gui.exe"
    }
    $destination = Join-Path $resolvedOutput "windows-assist-gui.exe"
    Copy-Item -LiteralPath $publishedExecutable -Destination $destination -Force

    & python (Join-Path $PSScriptRoot "verify-windows-pe-subsystem.py") $destination --expect gui
    if ($LASTEXITCODE -ne 0) {
        throw "WinUI AssistGUI PE subsystem verification failed"
    }

    [ordered]@{
        executable = $destination
        version = $Version
        runtime = "win-x64"
        selfContained = $true
        singleFile = $true
    } | ConvertTo-Json -Compress
} finally {
    Remove-Item -LiteralPath $publishDir -Recurse -Force -ErrorAction SilentlyContinue
}
