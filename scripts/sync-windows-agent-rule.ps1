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
$allowedDescriptorFields = @("schemaVersion", "description", "actions", "registrations")
foreach ($property in $descriptor.PSObject.Properties) {
    if ($allowedDescriptorFields -notcontains $property.Name) {
        throw "Rule plugin rule.json contains unknown field: $($property.Name)"
    }
}
if ($descriptor.schemaVersion -ne 3) {
    throw "Rule plugin schemaVersion must equal 3"
}
if ([String]::IsNullOrWhiteSpace([string]$descriptor.description) -or `
    ([string]$descriptor.description).Trim() -ne [string]$descriptor.description) {
    throw "Rule plugin description must be non-empty and canonical"
}
if ($null -eq $descriptor.actions -or $null -eq $descriptor.registrations) {
    throw "Rule plugin actions and registrations registries are required"
}
foreach ($actionProperty in $descriptor.actions.PSObject.Properties) {
    $action = $actionProperty.Value
    foreach ($property in $action.PSObject.Properties) {
        if (@("path", "runtime", "registrableAs") -notcontains $property.Name) {
            throw "Rule plugin action '$($actionProperty.Name)' contains unknown field: $($property.Name)"
        }
    }
    $relativePath = [string]$action.path
    $runtime = [string]$action.runtime
    if ([String]::IsNullOrWhiteSpace($actionProperty.Name) -or `
        [String]::IsNullOrWhiteSpace($runtime)) {
        throw "Rule plugin action ID and runtime must be non-empty"
    }
    if ($null -eq $action.registrableAs) {
        throw "Rule plugin action '$($actionProperty.Name)' registrableAs is required"
    }
    $seenRegistrationTypes = @{}
    foreach ($registrationType in @($action.registrableAs)) {
        if (@("monitor", "reaction") -notcontains [string]$registrationType) {
            throw "Rule plugin action '$($actionProperty.Name)' has unsupported registrableAs value: $registrationType"
        }
        if ($seenRegistrationTypes.ContainsKey([string]$registrationType)) {
            throw "Rule plugin action '$($actionProperty.Name)' has duplicate registrableAs value: $registrationType"
        }
        $seenRegistrationTypes[[string]$registrationType] = $true
    }
    if (-not $relativePath.StartsWith("Actions/", [StringComparison]::Ordinal) -or `
        $relativePath.Contains([string][IO.Path]::DirectorySeparatorChar) -or `
        $relativePath.Contains(":") -or `
        $relativePath.Split("/") -contains "..") {
        throw "Rule plugin action path is not canonical: $relativePath"
    }
    $actionPath = Join-Path $sourceRule ($relativePath.Replace("/", [string][IO.Path]::DirectorySeparatorChar))
    if (-not (Test-Path -LiteralPath $actionPath -PathType Container)) {
        throw "Rule plugin action directory does not exist: $relativePath"
    }
    if (-not (Test-Path -LiteralPath (Join-Path $actionPath "manifest.json") -PathType Leaf)) {
        throw "Rule plugin action is missing manifest.json: $relativePath"
    }
}
foreach ($registrationProperty in $descriptor.registrations.PSObject.Properties) {
    $registration = $registrationProperty.Value
    $registrationID = [string]$registrationProperty.Name
    if ([String]::IsNullOrWhiteSpace($registrationID) -or `
        $registrationID.Trim() -ne $registrationID -or $registrationID.Contains("\")) {
        throw "Rule plugin registration ID is not canonical: $registrationID"
    }
    $registrationFields = @($registration.PSObject.Properties.Name)
    foreach ($field in $registrationFields) {
        if (@("type", "action", "input", "monitor", "reaction") -notcontains $field) {
            throw "Rule plugin registration '$registrationID' contains unknown field: $field"
        }
    }
    foreach ($requiredField in @("type", "action", "input")) {
        if ($registrationFields -notcontains $requiredField) {
            throw "Rule plugin registration '$registrationID' is missing $requiredField"
        }
    }
    $registrationType = [string]$registration.type
    if (@("monitor", "reaction") -notcontains $registrationType) {
        throw "Rule plugin registration '$registrationID' has unsupported type: $registrationType"
    }
    $actionProperty = $descriptor.actions.PSObject.Properties[[string]$registration.action]
    if ($null -eq $actionProperty) {
        throw "Rule plugin registration '$registrationID' references unknown action: $($registration.action)"
    }
    $action = $actionProperty.Value
    if (@($action.registrableAs) -notcontains $registrationType) {
        throw "Rule plugin registration '$registrationID' type is not declared by action '$($registration.action)'"
    }
    if ($null -eq $registration.input -or -not ($registration.input -is [PSCustomObject])) {
        throw "Rule plugin registration '$registrationID' input must be an object"
    }
    if ($registrationType -eq "monitor") {
        if ($registrationFields -notcontains "monitor" -or $null -eq $registration.monitor -or `
            $registrationFields -contains "reaction") {
            throw "Rule plugin monitor registration '$registrationID' requires monitor and forbids reaction"
        }
        $monitorFields = @($registration.monitor.PSObject.Properties.Name)
        if (($monitorFields | Where-Object { @("intervalMs", "emit") -notcontains $_ }).Count -ne 0 -or `
            $monitorFields -notcontains "intervalMs" -or $monitorFields -notcontains "emit") {
            throw "Rule plugin monitor registration '$registrationID' has an invalid monitor trigger"
        }
        if ([long]$registration.monitor.intervalMs -le 0 -or $null -eq $registration.monitor.emit) {
            throw "Rule plugin monitor registration '$registrationID' intervalMs and emit are required"
        }
        $emitFields = @($registration.monitor.emit.PSObject.Properties.Name)
        if ($emitFields.Count -ne 2 -or $emitFields -notcontains "stream" -or $emitFields -notcontains "eventType" -or `
            [String]::IsNullOrWhiteSpace([string]$registration.monitor.emit.stream) -or `
            [String]::IsNullOrWhiteSpace([string]$registration.monitor.emit.eventType)) {
            throw "Rule plugin monitor registration '$registrationID' emit is invalid"
        }
    } else {
        if ($registrationFields -notcontains "reaction" -or $null -eq $registration.reaction -or `
            $registrationFields -contains "monitor") {
            throw "Rule plugin reaction registration '$registrationID' requires reaction and forbids monitor"
        }
        $reactionFields = @($registration.reaction.PSObject.Properties.Name)
        if (($reactionFields | Where-Object { @("stream", "eventType", "match") -notcontains $_ }).Count -ne 0 -or `
            $reactionFields -notcontains "stream" -or $reactionFields -notcontains "eventType" -or `
            $reactionFields -notcontains "match" -or `
            [String]::IsNullOrWhiteSpace([string]$registration.reaction.stream) -or `
            [String]::IsNullOrWhiteSpace([string]$registration.reaction.eventType) -or `
            $null -eq $registration.reaction.match -or -not ($registration.reaction.match -is [PSCustomObject])) {
            throw "Rule plugin reaction registration '$registrationID' trigger is invalid"
        }
        foreach ($matchProperty in $registration.reaction.match.PSObject.Properties) {
            if ([String]::IsNullOrWhiteSpace([string]$matchProperty.Name)) {
                throw "Rule plugin reaction registration '$registrationID' match field is invalid"
            }
            try {
                [void][regex]::new([string]$matchProperty.Value)
            } catch {
                throw "Rule plugin reaction registration '$registrationID' match regex is invalid: $($matchProperty.Name)"
            }
        }
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
