param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9_.-]+$')]
    [string]$RuleId
)

$ErrorActionPreference = 'Stop'
$rulesRoot = [IO.Path]::GetFullPath(
    (Join-Path $env:LOCALAPPDATA 'gameGuide\windows-capture-agent\Rules')
)
$configPath = [IO.Path]::GetFullPath(
    (Join-Path $rulesRoot "$RuleId\VisualLog\config.json")
)
$requiredPrefix = $rulesRoot.TrimEnd('\') + '\'
if (-not $configPath.StartsWith($requiredPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Visual Log configuration escaped the installed Rules directory'
}
if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    throw "Visual Log configuration is unavailable for matched Rule $RuleId"
}
Get-Content -LiteralPath $configPath -Raw -Encoding UTF8
