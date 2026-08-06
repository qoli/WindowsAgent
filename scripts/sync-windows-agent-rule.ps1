# Publishes one externally distributed Rule plugin without restarting WindowsAgent.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$SourceRulePath,

    [Parameter(Mandatory = $true)]
    [string]$DestinationRulesDir
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$sourceRule = [IO.Path]::GetFullPath($SourceRulePath)
$destinationRoot = [IO.Path]::GetFullPath($DestinationRulesDir)
if (-not (Test-Path -LiteralPath $sourceRule -PathType Container)) {
    throw "source Rule plugin directory does not exist: $sourceRule"
}
$ruleID = Split-Path -Leaf $sourceRule
if (-not $ruleID.EndsWith(".exe", [StringComparison]::OrdinalIgnoreCase)) {
    throw "source Rule plugin folder must end in .exe: $ruleID"
}
if ($ruleID.Contains("/") -or $ruleID.Contains([string][IO.Path]::DirectorySeparatorChar)) {
    throw "source Rule plugin folder must be one executable name: $ruleID"
}

$ruleJSONPath = Join-Path $sourceRule "rule.json"
$agentsPath = Join-Path $sourceRule "AGENTS.md"
if (-not (Test-Path -LiteralPath $ruleJSONPath -PathType Leaf)) {
    throw "Rule plugin is missing rule.json: $sourceRule"
}
if (-not (Test-Path -LiteralPath $agentsPath -PathType Leaf)) {
    throw "Rule plugin is missing AGENTS.md: $sourceRule"
}
if ([String]::IsNullOrWhiteSpace([IO.File]::ReadAllText($agentsPath))) {
    throw "Rule plugin AGENTS.md is empty: $sourceRule"
}

try {
    $descriptor = Get-Content -LiteralPath $ruleJSONPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
} catch {
    throw "Rule plugin rule.json is invalid JSON: $($_.Exception.Message)"
}
$allowedDescriptorFields = @("schemaVersion", "description", "modules")
foreach ($property in $descriptor.PSObject.Properties) {
    if ($allowedDescriptorFields -notcontains $property.Name) {
        throw "Rule plugin rule.json contains unknown field: $($property.Name)"
    }
}
if ($descriptor.schemaVersion -ne 2) {
    throw "Rule plugin schemaVersion must equal 2"
}
if ([String]::IsNullOrWhiteSpace([string]$descriptor.description) -or `
    ([string]$descriptor.description).Trim() -ne [string]$descriptor.description) {
    throw "Rule plugin description must be non-empty and canonical"
}
if ($null -eq $descriptor.modules) {
    throw "Rule plugin modules registry is required"
}
foreach ($moduleProperty in $descriptor.modules.PSObject.Properties) {
    $module = $moduleProperty.Value
    foreach ($property in $module.PSObject.Properties) {
        if (@("kind", "path", "runtime") -notcontains $property.Name) {
            throw "Rule plugin module '$($moduleProperty.Name)' contains unknown field: $($property.Name)"
        }
    }
    $kind = [string]$module.kind
    $relativePath = [string]$module.path
    $runtime = [string]$module.runtime
    if ([String]::IsNullOrWhiteSpace($moduleProperty.Name) -or `
        [String]::IsNullOrWhiteSpace($runtime)) {
        throw "Rule plugin module ID and runtime must be non-empty"
    }
    if (@("query", "preprocessor", "loop", "reactor", "action") -notcontains $kind) {
        throw "Rule plugin module kind is unsupported: $kind"
    }
    $requiredPrefix = "Modules/"
    if ($kind -eq "reactor") {
        $requiredPrefix = "Reactors/"
    } elseif ($kind -eq "action") {
        $requiredPrefix = "Actions/"
    }
    if (-not $relativePath.StartsWith($requiredPrefix, [StringComparison]::Ordinal) -or `
        $relativePath.Contains([string][IO.Path]::DirectorySeparatorChar) -or `
        $relativePath.Contains(":") -or `
        $relativePath.Split("/") -contains "..") {
        throw "Rule plugin module path is not canonical for kind '$kind': $relativePath"
    }
    $modulePath = Join-Path $sourceRule ($relativePath.Replace("/", [string][IO.Path]::DirectorySeparatorChar))
    if (-not (Test-Path -LiteralPath $modulePath -PathType Container)) {
        throw "Rule plugin module directory does not exist: $relativePath"
    }
    if (-not (Test-Path -LiteralPath (Join-Path $modulePath "manifest.json") -PathType Leaf)) {
        throw "Rule plugin module is missing manifest.json: $relativePath"
    }
}

New-Item -ItemType Directory -Path $destinationRoot -Force | Out-Null
$transactionID = [Guid]::NewGuid().ToString("N")
$destinationParent = Split-Path -Parent $destinationRoot
$transactionRoot = Join-Path $destinationParent (".rule-sync-" + $transactionID)
$incoming = Join-Path $transactionRoot "incoming"
$previous = Join-Path $transactionRoot "previous"
$destination = Join-Path $destinationRoot $ruleID

New-Item -ItemType Directory -Path $transactionRoot | Out-Null
$movedPrevious = $false
try {
    Copy-Item -LiteralPath $sourceRule -Destination $incoming -Recurse
    if (Test-Path -LiteralPath $destination) {
        [IO.Directory]::Move($destination, $previous)
        $movedPrevious = $true
    }
    [IO.Directory]::Move($incoming, $destination)
} catch {
    if ($movedPrevious -and -not (Test-Path -LiteralPath $destination) -and `
        (Test-Path -LiteralPath $previous)) {
        [IO.Directory]::Move($previous, $destination)
        $movedPrevious = $false
    }
    throw "Rule plugin replacement failed: $($_.Exception.Message)"
} finally {
    if (-not $movedPrevious -and (Test-Path -LiteralPath $transactionRoot)) {
        Remove-Item -LiteralPath $transactionRoot -Recurse -Force
    }
}
if ($movedPrevious) {
    Remove-Item -LiteralPath $transactionRoot -Recurse -Force
}

[ordered]@{
    rule_id = $ruleID
    source = $sourceRule
    destination = $destination
    scheduled_task_restarted = $false
} | ConvertTo-Json
