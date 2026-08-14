# Validates and transactionally publishes one complete WindowsAgent Rules tree.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$PayloadRoot,
    [bool]$PruneUnknown = $false,
    [bool]$ValidateOnly = $false,
    [ValidateRange(5, 300)][int]$TimeoutSeconds = 45
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$taskName = "gameGuide Windows Capture Agent"
$taskDescription = "gameGuide Go WGC screenshot agent; interactive-user session required"
$manifestName = "manifest.json"
$checkerName = "windows-action-check.exe"
$executorName = "apply-windows-rules.ps1"

function Get-Sha256 {
    param([Parameter(Mandatory = $true)][string]$Path)
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Assert-Artifact {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)]$Declaration,
        [Parameter(Mandatory = $true)][string]$Name
    )
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "payload artifact is missing: $Name"
    }
    $file = Get-Item -LiteralPath $Path
    if ([long]$file.Length -ne [long]$Declaration.bytes) {
        throw "payload artifact byte length mismatch: $Name"
    }
    $actual = Get-Sha256 -Path $Path
    if ($actual -cne [string]$Declaration.sha256) {
        throw "payload artifact SHA-256 mismatch: $Name"
    }
}

function Get-TreeInventory {
    param([Parameter(Mandatory = $true)][string]$Root)
    $resolved = [IO.Path]::GetFullPath($Root)
    if (-not (Test-Path -LiteralPath $resolved -PathType Container)) {
        throw "Rules tree does not exist: $resolved"
    }
    $files = @{}
    $seen = @{}
    $prefix = $resolved.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    foreach ($item in @(Get-ChildItem -LiteralPath $resolved -File -Recurse | Sort-Object FullName)) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Rules tree contains a reparse point: $($item.FullName)"
        }
        if (-not $item.FullName.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Rules file escaped the expected root: $($item.FullName)"
        }
        $relative = $item.FullName.Substring($prefix.Length).Replace("\", "/")
        $folded = $relative.ToLowerInvariant()
        if ($seen.ContainsKey($folded)) {
            throw "Rules tree contains a case-insensitive path collision: $relative"
        }
        $seen[$folded] = $true
        $files[$relative] = [pscustomobject]@{
            path = $relative
            bytes = [long]$item.Length
            sha256 = Get-Sha256 -Path $item.FullName
        }
    }
    [string[]]$paths = @($files.Keys)
    [Array]::Sort($paths, [StringComparer]::OrdinalIgnoreCase)
    return @($paths | ForEach-Object { $files[$_] })
}

function Get-TreeHash {
    param([Parameter(Mandatory = $true)][object[]]$Files)
    $builder = [Text.StringBuilder]::new()
    foreach ($file in $Files) {
        [void]$builder.Append([string]$file.path)
        [void]$builder.Append([char]0)
        [void]$builder.Append(([long]$file.bytes).ToString([Globalization.CultureInfo]::InvariantCulture))
        [void]$builder.Append([char]0)
        [void]$builder.Append([string]$file.sha256)
        [void]$builder.Append("`n")
    }
    $algorithm = [Security.Cryptography.SHA256]::Create()
    try {
        $bytes = [Text.Encoding]::UTF8.GetBytes($builder.ToString())
        return ([BitConverter]::ToString($algorithm.ComputeHash($bytes))).Replace("-", "").ToLowerInvariant()
    } finally {
        $algorithm.Dispose()
    }
}

function Assert-Inventory {
    param(
        [Parameter(Mandatory = $true)][object[]]$Expected,
        [Parameter(Mandatory = $true)][object[]]$Actual,
        [Parameter(Mandatory = $true)][string]$Label
    )
    if ($Expected.Count -ne $Actual.Count) {
        throw "$Label file count mismatch: expected=$($Expected.Count) actual=$($Actual.Count)"
    }
    for ($index = 0; $index -lt $Expected.Count; $index++) {
        $left = $Expected[$index]
        $right = $Actual[$index]
        if ([string]$left.path -cne [string]$right.path -or
            [long]$left.bytes -ne [long]$right.bytes -or
            [string]$left.sha256 -cne [string]$right.sha256) {
            throw "$Label inventory mismatch at '$($left.path)'"
        }
    }
}

function Get-TaskArgument {
    param(
        [Parameter(Mandatory = $true)][string]$Arguments,
        [Parameter(Mandatory = $true)][string]$Name
    )
    $escaped = [regex]::Escape($Name)
    $match = [regex]::Match($Arguments, "(?:^|\s)$escaped\s+(?:`"([^`"]+)`"|(\S+))")
    if (-not $match.Success) {
        throw "installed capture task does not declare $Name"
    }
    if ($match.Groups[1].Success) { return $match.Groups[1].Value }
    return $match.Groups[2].Value
}

function Get-LoopbackOrigin {
    param([Parameter(Mandatory = $true)][string]$Listen)
    $separator = $Listen.LastIndexOf(":")
    if ($separator -lt 0 -or $separator -eq $Listen.Length - 1) {
        throw "installed capture task has invalid --listen value: $Listen"
    }
    $port = 0
    if (-not [int]::TryParse($Listen.Substring($separator + 1), [ref]$port) -or $port -lt 1 -or $port -gt 65535) {
        throw "installed capture task has invalid --listen port: $Listen"
    }
    return "http://127.0.0.1:$port"
}

function Invoke-JSONGet {
    param([Parameter(Mandatory = $true)][string]$URI)
    $response = Invoke-WebRequest -Uri $URI -UseBasicParsing -TimeoutSec $TimeoutSeconds
    if ([int]$response.StatusCode -ne 200) {
        throw "live catalog request returned HTTP $($response.StatusCode): $URI"
    }
    return $response.Content | ConvertFrom-Json -ErrorAction Stop
}

function Get-SortedStrings {
    param([object[]]$Values)
    return @($Values | ForEach-Object { [string]$_ } | Sort-Object)
}

function Assert-SameStrings {
    param([object[]]$Expected, [object[]]$Actual, [string]$Label)
    $left = (Get-SortedStrings -Values $Expected) -join "`n"
    $right = (Get-SortedStrings -Values $Actual) -join "`n"
    if ($left -cne $right) {
        throw "$Label mismatch"
    }
}

function Assert-LiveCatalogs {
    param(
        [Parameter(Mandatory = $true)][string]$Origin,
        [Parameter(Mandatory = $true)][string]$RulesRoot,
        [Parameter(Mandatory = $true)][string[]]$RuleIDs
    )
    $health = Invoke-JSONGet -URI ($Origin + "/healthz")
    if ([string]$health.status -cne "ok") {
        throw "installed Agent health is not ok"
    }
    $catalogs = @()
    foreach ($ruleID in $RuleIDs) {
        $encodedRule = [Uri]::EscapeDataString($ruleID)
        $descriptorPath = Join-Path $RulesRoot ($ruleID + "\rule.json")
        $descriptor = Get-Content -LiteralPath $descriptorPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop

        $agentsResponse = Invoke-WebRequest -Uri ($Origin + "/v1/rules/$encodedRule/AGENTS.md") `
            -UseBasicParsing -TimeoutSec $TimeoutSeconds
        $expectedAgents = [IO.File]::ReadAllText((Join-Path $RulesRoot ($ruleID + "\AGENTS.md")))
        if ([int]$agentsResponse.StatusCode -ne 200 -or
            [string]$agentsResponse.Content -cne $expectedAgents) {
            throw "live AGENTS document is unavailable for Rule $ruleID"
        }

        $actions = Invoke-JSONGet -URI ($Origin + "/v3/rules/$encodedRule/actions")
        $expectedActions = @($descriptor.actions.PSObject.Properties |
            Where-Object {
                $null -eq $_.Value.PSObject.Properties["exposure"] -or
                    [string]$_.Value.PSObject.Properties["exposure"].Value -ne "internal"
            } |
            ForEach-Object { [string]$_.Name })
        Assert-SameStrings -Expected $expectedActions -Actual @($actions.actions | ForEach-Object { $_.id }) `
            -Label "live public Action catalog for Rule $ruleID"

        $scripts = Invoke-JSONGet -URI ($Origin + "/v1/rules/$encodedRule/scripts")
        $expectedScripts = @($descriptor.actions.PSObject.Properties |
            Where-Object {
                ($null -eq $_.Value.PSObject.Properties["exposure"] -or
                    [string]$_.Value.PSObject.Properties["exposure"].Value -ne "internal") -and
                    [string]$_.Value.runtime -eq "windows-observation-v1"
            } |
            ForEach-Object { [string]$_.Name })
        Assert-SameStrings -Expected $expectedScripts -Actual @($scripts.scripts | ForEach-Object { $_.id }) `
            -Label "live Script catalog for Rule $ruleID"

        $registrations = Invoke-JSONGet -URI ($Origin + "/v3/rules/$encodedRule/registrations")
        Assert-SameStrings -Expected @($descriptor.registrations.PSObject.Properties.Name) `
            -Actual @($registrations.registrations | ForEach-Object { $_.id }) `
            -Label "live registration catalog for Rule $ruleID"

        $runtimes = Invoke-JSONGet -URI ($Origin + "/v4/rules/$encodedRule/runtimes")
        Assert-SameStrings -Expected @($descriptor.runtimeProfiles.PSObject.Properties.Name) `
            -Actual @($runtimes.runtimes | ForEach-Object { $_.id }) `
            -Label "live runtime catalog for Rule $ruleID"

        [void](Invoke-JSONGet -URI ($Origin + "/v3/rules/$encodedRule/action-sequence-tool"))
        $catalogs += [pscustomobject]@{
            rule_id = $ruleID
            public_action_count = @($actions.actions).Count
            script_count = @($scripts.scripts).Count
            registration_count = @($registrations.registrations).Count
            runtime_count = @($runtimes.runtimes).Count
        }
    }
    return $catalogs
}

$payload = [IO.Path]::GetFullPath($PayloadRoot)
$manifestPath = Join-Path $payload $manifestName
$stagedRules = Join-Path $payload "Rules"
$checkerPath = Join-Path $payload $checkerName
$executorPath = Join-Path $payload $executorName
if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
    throw "Rule deployment manifest is missing"
}
$manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
if ([int]$manifest.schemaVersion -ne 1 -or
    [string]$manifest.deploymentId -notmatch '^[0-9a-f]{32}$' -or
    [string]$manifest.treeSha256 -notmatch '^[0-9a-f]{64}$') {
    throw "Rule deployment manifest identity is invalid"
}
$deploymentID = [string]$manifest.deploymentId
$expectedFiles = @($manifest.files)
if ($expectedFiles.Count -ne [int]$manifest.fileCount -or $expectedFiles.Count -eq 0) {
    throw "Rule deployment manifest file count is invalid"
}
[string[]]$expectedPaths = @($expectedFiles | ForEach-Object { [string]$_.path })
[string[]]$sortedExpectedPaths = @($expectedPaths)
[Array]::Sort($sortedExpectedPaths, [StringComparer]::OrdinalIgnoreCase)
for ($index = 0; $index -lt $expectedPaths.Count; $index++) {
    if ($expectedPaths[$index] -cne $sortedExpectedPaths[$index]) {
        throw "Rule deployment manifest file order is not canonical"
    }
}
Assert-Artifact -Path $checkerPath `
    -Declaration $manifest.artifacts.PSObject.Properties[$checkerName].Value -Name $checkerName
Assert-Artifact -Path $executorPath `
    -Declaration $manifest.artifacts.PSObject.Properties[$executorName].Value -Name $executorName
$stagedInventory = @(Get-TreeInventory -Root $stagedRules)
Assert-Inventory -Expected $expectedFiles -Actual $stagedInventory -Label "staged Rules"
$stagedTreeHash = Get-TreeHash -Files $stagedInventory
if ($stagedTreeHash -cne [string]$manifest.treeSha256) {
    throw "staged Rules tree SHA-256 mismatch"
}

$checkOutput = [string]::Join([Environment]::NewLine, @(& $checkerPath --rules-dir $stagedRules --json 2>&1))
$checkExitCode = $LASTEXITCODE
if ($checkExitCode -ne 0) {
    throw "staged Windows Rule check failed with exit $checkExitCode`: $checkOutput"
}
$remoteCheck = $checkOutput | ConvertFrom-Json -ErrorAction Stop
if ($remoteCheck.valid -ne $true) {
    throw "staged Windows Rule checker did not report valid=true"
}
foreach ($field in @("schemaVersion", "ruleCount", "actionCount", "dependencyCount")) {
    $localValue = $manifest.localCheck.PSObject.Properties[$field].Value
    $remoteValue = $remoteCheck.PSObject.Properties[$field].Value
    if ([long]$localValue -ne [long]$remoteValue) {
        throw "local and staged Windows Rule checker reports differ at $field"
    }
}

$task = Get-ScheduledTask -TaskName $taskName -ErrorAction Stop
if ([string]$task.Description -cne $taskDescription -or @($task.Actions).Count -ne 1) {
    throw "capture Scheduled Task ownership mismatch"
}
$taskArguments = [string]$task.Actions[0].Arguments
$agentPath = [IO.Path]::GetFullPath([string]$task.Actions[0].Execute)
if ([IO.Path]::GetFileName($agentPath) -cne "windows-capture-agent.exe") {
    throw "capture Scheduled Task executable identity mismatch"
}
$agentProcesses = @(Get-Process -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -eq $agentPath })
if ($agentProcesses.Count -ne 1 -or $agentProcesses[0].SessionId -eq 0) {
    throw "installed capture Agent is not one healthy interactive process"
}
$installedRules = [IO.Path]::GetFullPath((Get-TaskArgument -Arguments $taskArguments -Name "--rules-dir"))
if ([IO.Path]::GetFileName($installedRules) -cne "Rules" -or
    [string]::IsNullOrWhiteSpace((Split-Path -Parent $installedRules)) -or
    (Split-Path -Parent $installedRules) -ceq [IO.Path]::GetPathRoot($installedRules)) {
    throw "installed --rules-dir is not a bounded Rules directory"
}
if (Test-Path -LiteralPath $installedRules) {
    $installedRulesItem = Get-Item -LiteralPath $installedRules
    if (($installedRulesItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "installed Rules root must not be a reparse point"
    }
}
$listen = Get-TaskArgument -Arguments $taskArguments -Name "--listen"
$origin = Get-LoopbackOrigin -Listen $listen
[void](Invoke-JSONGet -URI ($origin + "/healthz"))

$sourceRuleIDs = @($manifest.rules | ForEach-Object { [string]$_ } | Sort-Object)
if ($sourceRuleIDs.Count -eq 0) { throw "Rule deployment manifest has no Rules" }
$sourceRuleDirectories = @(Get-ChildItem -LiteralPath $stagedRules -Directory | ForEach-Object { $_.Name } | Sort-Object)
Assert-SameStrings -Expected $sourceRuleIDs -Actual $sourceRuleDirectories -Label "staged Rule directory set"

$installedRuleIDs = @()
if (Test-Path -LiteralPath $installedRules -PathType Container) {
    $installedRuleIDs = @(Get-ChildItem -LiteralPath $installedRules -Directory | ForEach-Object { $_.Name } | Sort-Object)
    $installedRootFiles = @(Get-ChildItem -LiteralPath $installedRules -File)
    if ($installedRootFiles.Count -ne 0) {
        throw "installed Rules root contains unexpected files"
    }
}
$unknownRules = @($installedRuleIDs | Where-Object { $sourceRuleIDs -cnotcontains $_ })
if ($unknownRules.Count -ne 0 -and -not $PruneUnknown) {
    throw "installed Rules contain source-unknown plugins; rerun with --prune-unknown only if removal is intended: $($unknownRules -join ', ')"
}

if ($ValidateOnly) {
    [ordered]@{
        schema_version = 1
        deployment_id = $deploymentID
        validated_at = [DateTime]::UtcNow.ToString("o")
        git_revision = [string]$manifest.gitRevision
        git_dirty = [bool]$manifest.gitDirty
        rules_dir = $installedRules
        rule_count = $sourceRuleIDs.Count
        file_count = $expectedFiles.Count
        tree_sha256 = $stagedTreeHash
        local_check = $manifest.localCheck
        remote_check = $remoteCheck
        excluded_platform_files = @($manifest.excludedPlatformFiles)
        unknown_installed_rules = @($unknownRules)
        validated_only = $true
        published = $false
        task_restarted = $false
    } | ConvertTo-Json -Depth 20 -Compress
    exit 0
}

$destinationParent = Split-Path -Parent $installedRules
New-Item -ItemType Directory -Path $destinationParent -Force | Out-Null
$transactionRoot = Join-Path $destinationParent (".rules-deploy-" + $deploymentID)
$incoming = Join-Path $transactionRoot "incoming"
$previous = Join-Path $transactionRoot "previous"
$failed = Join-Path $transactionRoot "failed"
if (Test-Path -LiteralPath $transactionRoot) {
    throw "Rule deployment transaction already exists: $transactionRoot"
}
New-Item -ItemType Directory -Path $transactionRoot | Out-Null
Copy-Item -LiteralPath $stagedRules -Destination $incoming -Recurse
$incomingInventory = @(Get-TreeInventory -Root $incoming)
Assert-Inventory -Expected $expectedFiles -Actual $incomingInventory -Label "incoming Rules"

$movedPrevious = $false
$published = $false
try {
    if (Test-Path -LiteralPath $installedRules) {
        [IO.Directory]::Move($installedRules, $previous)
        $movedPrevious = $true
    }
    [IO.Directory]::Move($incoming, $installedRules)
    $published = $true

    $installedInventory = @(Get-TreeInventory -Root $installedRules)
    Assert-Inventory -Expected $expectedFiles -Actual $installedInventory -Label "installed Rules"
    $installedTreeHash = Get-TreeHash -Files $installedInventory
    if ($installedTreeHash -cne [string]$manifest.treeSha256) {
        throw "installed Rules tree SHA-256 mismatch"
    }
    $catalogs = @(Assert-LiveCatalogs -Origin $origin -RulesRoot $installedRules -RuleIDs $sourceRuleIDs)

    $captureResponse = Invoke-WebRequest -Uri ($origin + "/v1/captures") -Method Post `
        -ContentType "application/json" -Body '{"include_cursor":false,"profile":"native-jpeg"}' `
        -UseBasicParsing -TimeoutSec $TimeoutSeconds
    if ([int]$captureResponse.StatusCode -ne 201) {
        throw "fresh post-deployment capture returned HTTP $($captureResponse.StatusCode)"
    }
    $capture = $captureResponse.Content | ConvertFrom-Json -ErrorAction Stop
    $foregroundRuleExpected = $sourceRuleIDs -icontains [string]$capture.foreground.executable_name
    if ($foregroundRuleExpected -and
        ([string]$capture.rule.status -cne "matched" -or
            [string]$capture.rule.id -cne [string]$capture.foreground.executable_name)) {
        throw "fresh capture did not resolve the deployed foreground Rule"
    }

    $receiptRoot = Join-Path $destinationParent "deployments\rules"
    $receiptPath = Join-Path $receiptRoot ($deploymentID + ".json")
    $receipt = [ordered]@{
        schema_version = 1
        deployment_id = $deploymentID
        deployed_at = [DateTime]::UtcNow.ToString("o")
        git_revision = [string]$manifest.gitRevision
        git_dirty = [bool]$manifest.gitDirty
        rules_dir = $installedRules
        rule_count = $sourceRuleIDs.Count
        file_count = $expectedFiles.Count
        tree_sha256 = $installedTreeHash
        local_check = $manifest.localCheck
        remote_check = $remoteCheck
        catalogs = $catalogs
        fresh_capture = [ordered]@{
            id = [string]$capture.id
            foreground_executable = [string]$capture.foreground.executable_name
            rule_status = [string]$capture.rule.status
            rule_id = [string]$capture.rule.id
        }
        excluded_platform_files = @($manifest.excludedPlatformFiles)
        unknown_rules_removed = @($unknownRules)
        task_restarted = $false
        rollback_performed = $false
        backup_path = $(if ($movedPrevious) { $previous } else { $null })
        remote_receipt = $receiptPath
    }
    New-Item -ItemType Directory -Path $receiptRoot -Force | Out-Null
    $receiptTemporary = $receiptPath + ".tmp"
    $receiptJSON = $receipt | ConvertTo-Json -Depth 20
    [IO.File]::WriteAllText($receiptTemporary, $receiptJSON + "`n", [Text.UTF8Encoding]::new($false))
    Move-Item -LiteralPath $receiptTemporary -Destination $receiptPath -Force
    $receipt | ConvertTo-Json -Depth 20 -Compress
} catch {
    $failure = $_
    $rollbackError = $null
    try {
        if ($published -and (Test-Path -LiteralPath $installedRules)) {
            [IO.Directory]::Move($installedRules, $failed)
            $published = $false
        }
        if ($movedPrevious -and (Test-Path -LiteralPath $previous) -and
            -not (Test-Path -LiteralPath $installedRules)) {
            [IO.Directory]::Move($previous, $installedRules)
            $movedPrevious = $false
        }
        if (-not (Test-Path -LiteralPath $installedRules -PathType Container)) {
            throw "installed Rules directory is absent after rollback"
        }
        [void](Invoke-JSONGet -URI ($origin + "/healthz"))
    } catch {
        $rollbackError = $_
    }
    if ($null -ne $rollbackError) {
        throw "Rule deployment failed and rollback also failed; transaction retained at $transactionRoot`: deployment=$($failure.Exception.Message); rollback=$($rollbackError.Exception.Message)"
    }
    throw "Rule deployment failed and previous Rules were restored; transaction retained at $transactionRoot`: $($failure.Exception.Message)"
}
