Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$stage = [IO.Path]::GetFullPath($env:WINDOWSAGENT_RELEASE_STAGE)
$catalogPath = [IO.Path]::GetFullPath($env:WINDOWSAGENT_RELEASE_CATALOG)
$dataDir = [IO.Path]::GetFullPath($env:WINDOWSAGENT_INSTALL_DATA_DIR)
if (-not (Test-Path -LiteralPath $stage -PathType Container)) { throw "release stage directory does not exist" }
if (-not (Test-Path -LiteralPath $catalogPath -PathType Leaf)) { throw "release catalog does not exist" }
if (-not $env:LOCALAPPDATA) { throw "LOCALAPPDATA is required" }
$expectedRoot = [IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"))
if ($dataDir -cne $expectedRoot) { throw "install data directory must equal the current user's WindowsAgent directory" }

$catalog = Get-Content -LiteralPath $catalogPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
if ([int]$catalog.schemaVersion -ne 1 -or [string]$catalog.target -cne "windows-amd64") { throw "release catalog identity is invalid" }
$selected = @($catalog.artifacts | Where-Object {
    $_.class -ceq "bootstrap" -or $_.class -ceq "runtime-required" -or $_.role -ceq "tailscale-adapter"
})
if ($selected.Count -eq 0) { throw "release catalog selected no base install artifacts" }
foreach ($artifact in $selected) {
    $name = [string]$artifact.name
    if ([IO.Path]::GetFileName($name) -cne $name -or -not $name.EndsWith(".exe", [StringComparison]::Ordinal)) {
        throw "release artifact name is unsafe: $name"
    }
    $source = Join-Path $stage $name
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw "staged release artifact is missing: $name" }
    if ((Get-Item -LiteralPath $source).Length -ne [long]$artifact.bytes) { throw "staged release artifact size mismatch: $name" }
    if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$artifact.sha256) {
        throw "staged release artifact SHA-256 mismatch: $name"
    }
}
foreach ($metadataName in @("windowsagent-release.json", "SHA256SUMS")) {
    $metadataSource = Join-Path $stage $metadataName
    if (-not (Test-Path -LiteralPath $metadataSource -PathType Leaf)) { throw "staged release metadata is missing: $metadataName" }
}

$agentTask = "gameGuide Windows Capture Agent"
$eventTask = "gameGuide Windows Event Stream"
$watchdogTask = "gameGuide Windows Watchdog"
$agentDescription = "gameGuide Go WGC screenshot agent; interactive-user session required"
$eventDescription = "gameGuide durable local event stream; interactive-user session required"
$watchdogDescription = "gameGuide external process watchdog; no automatic self-recovery"
$previousTaskXML = @{}
foreach ($entry in @(@($agentTask, $agentDescription), @($eventTask, $eventDescription))) {
    $task = Get-ScheduledTask -TaskName $entry[0] -ErrorAction SilentlyContinue
    if ($null -ne $task) {
        if ($task.Description -cne $entry[1]) { throw "scheduled task '$($entry[0])' is not owned by WindowsAgent" }
        $previousTaskXML[$entry[0]] = Export-ScheduledTask -TaskName $entry[0]
    }
}
$watchdog = Get-ScheduledTask -TaskName $watchdogTask -ErrorAction SilentlyContinue
$watchdogWasRunning = $false
if ($null -ne $watchdog) {
    if ($watchdog.Description -cne $watchdogDescription) { throw "scheduled task '$watchdogTask' is not owned by WindowsAgent" }
    $watchdogWasRunning = $watchdog.State -ceq 'Running'
}

$binDir = Join-Path $dataDir "bin"
$rulesDir = Join-Path $dataDir "Rules"
$logDir = Join-Path $dataDir "logs"
$eventDir = Join-Path $dataDir "events"
$tokenFile = Join-Path $dataDir "event-api.token"
foreach ($directory in @($dataDir, $binDir, $rulesDir, $logDir, $eventDir)) {
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
}
$createdToken = $false
if (Test-Path -LiteralPath $tokenFile -PathType Leaf) {
    $token = [IO.File]::ReadAllText($tokenFile)
    if ($token.Length -lt 32 -or $token.Length -gt 4096 -or $token.Trim() -cne $token) {
        throw "existing event API token must be 32-4096 characters without surrounding whitespace"
    }
} else {
    $tokenBytes = New-Object byte[] 32
    $random = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $random.GetBytes($tokenBytes) } finally { $random.Dispose() }
    $token = [Convert]::ToBase64String($tokenBytes)
    [IO.File]::WriteAllText($tokenFile, $token, [Text.UTF8Encoding]::new($false))
    $createdToken = $true
}

$transaction = [Guid]::NewGuid().ToString("N")
$backupDir = Join-Path (Join-Path $dataDir "backups") $transaction
New-Item -ItemType Directory -Path $backupDir -Force | Out-Null
$previousFiles = @{}
$transactionFileNames = @($selected | ForEach-Object { [string]$_.name }) + @("windowsagent-release.json", "SHA256SUMS")
foreach ($name in $transactionFileNames) {
    $destination = Join-Path $binDir $name
    if (Test-Path -LiteralPath $destination -PathType Leaf) {
        $backup = Join-Path $backupDir $name
        Copy-Item -LiteralPath $destination -Destination $backup
        $previousFiles[$name] = $backup
    }
}

function Quote-Native([string]$value) {
    if ($value -notmatch '[\s"]') { return $value }
    return '"' + ($value -replace '(\\*)"', '$1$1\"' -replace '(\\+)$', '$1$1') + '"'
}

function Wait-Health([string]$uri, [string]$label) {
    $deadline = [DateTime]::UtcNow.AddSeconds(25)
    do {
        try {
            $health = Invoke-RestMethod -Method Get -Uri $uri -TimeoutSec 2
            if ($health.status -ceq "ok") { return }
        } catch {}
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "$label did not become healthy at $uri"
}

function Confirm-ListenerOwner([int]$port, [string]$expectedPath, [string]$label) {
    $listeners = @(Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction Stop)
    if ($listeners.Count -ne 1) { throw "$label must own exactly one TCP listener on port $port" }
    $process = Get-Process -Id $listeners[0].OwningProcess -ErrorAction Stop
    if ([IO.Path]::GetFullPath($process.Path) -cne [IO.Path]::GetFullPath($expectedPath)) {
        throw "$label listener is owned by an unexpected executable"
    }
    if ([int]$process.SessionId -eq 0) { throw "$label is running in Session 0 instead of the interactive user session" }
}

function Wait-ExecutableExit([string]$expectedPath) {
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    $processName = [IO.Path]::GetFileNameWithoutExtension($expectedPath)
    do {
        $running = @(Get-Process -Name $processName -ErrorAction SilentlyContinue | Where-Object {
            $_.Path -and ([IO.Path]::GetFullPath($_.Path) -ceq [IO.Path]::GetFullPath($expectedPath))
        })
        if ($running.Count -eq 0) { return }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "installed executable is still running: $expectedPath"
}

try {
    if ($null -ne $watchdog) {
        Stop-ScheduledTask -TaskName $watchdogTask -ErrorAction SilentlyContinue
        Wait-ExecutableExit (Join-Path $binDir "windows-watchdog.exe")
    }
    foreach ($taskName in @($agentTask, $eventTask)) {
        if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
            Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
        }
    }
    Start-Sleep -Milliseconds 500
    foreach ($artifact in $selected) {
        Wait-ExecutableExit (Join-Path $binDir ([string]$artifact.name))
    }
    foreach ($artifact in $selected) {
        $name = [string]$artifact.name
        $source = Join-Path $stage $name
        $destination = Join-Path $binDir $name
        Copy-Item -LiteralPath $source -Destination $destination -Force
        if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$artifact.sha256) {
            throw "installed release artifact SHA-256 mismatch: $name"
        }
    }
    Copy-Item -LiteralPath (Join-Path $stage "windowsagent-release.json") -Destination (Join-Path $binDir "windowsagent-release.json") -Force
    Copy-Item -LiteralPath (Join-Path $stage "SHA256SUMS") -Destination (Join-Path $binDir "SHA256SUMS") -Force

    $identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    if (-not $identity) { throw "could not resolve current Windows identity" }
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
    $principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
    $settings = New-ScheduledTaskSettingsSet `
        -ExecutionTimeLimit ([timespan]::Zero) `
        -StartWhenAvailable `
        -AllowStartIfOnBatteries `
        -DontStopIfGoingOnBatteries `
        -MultipleInstances IgnoreNew

    $eventExe = Join-Path $binDir "windows-event-stream.exe"
    $eventArgs = @(
        "--listen", "127.0.0.1:8788",
        "--data-dir", $eventDir,
        "--token-file", $tokenFile,
        "--log-file", (Join-Path $logDir "event-stream.jsonl")
    ) | ForEach-Object { Quote-Native ([string]$_) }
    $eventAction = New-ScheduledTaskAction -Execute $eventExe -Argument ($eventArgs -join " ")
    $eventDefinition = New-ScheduledTask -Action $eventAction -Trigger $trigger -Principal $principal -Settings $settings -Description $eventDescription
    Register-ScheduledTask -TaskName $eventTask -InputObject $eventDefinition -Force | Out-Null

    $agentExe = Join-Path $binDir "windows-capture-agent.exe"
    $agentArgs = @(
        "--listen", "0.0.0.0:8787",
        "--data-dir", $dataDir,
        "--rules-dir", $rulesDir,
        "--event-api-url", "http://127.0.0.1:8788",
        "--event-token-file", $tokenFile,
        "--log-file", (Join-Path $logDir "agent.jsonl"),
        "--runtime-log-file", (Join-Path $logDir "runtime-stderr.log")
    ) | ForEach-Object { Quote-Native ([string]$_) }
    $agentAction = New-ScheduledTaskAction -Execute $agentExe -Argument ($agentArgs -join " ")
    $agentDefinition = New-ScheduledTask -Action $agentAction -Trigger $trigger -Principal $principal -Settings $settings -Description $agentDescription
    Register-ScheduledTask -TaskName $agentTask -InputObject $agentDefinition -Force | Out-Null

    Start-ScheduledTask -TaskName $eventTask
    Wait-Health "http://127.0.0.1:8788/healthz" "Event Stream"
    Confirm-ListenerOwner 8788 $eventExe "Event Stream"
    Start-ScheduledTask -TaskName $agentTask
    Wait-Health "http://127.0.0.1:8787/healthz" "Capture Agent"
    Confirm-ListenerOwner 8787 $agentExe "Capture Agent"
    if ($watchdogWasRunning) {
        Start-ScheduledTask -TaskName $watchdogTask
    }
    Remove-Item -LiteralPath $backupDir -Recurse -Force -ErrorAction SilentlyContinue
    [ordered]@{status="INSTALLED"; version=[string]$catalog.version; transaction=$transaction; dataDir=$dataDir} | ConvertTo-Json -Compress
} catch {
    $failure = $_
    $rollbackErrors = [Collections.Generic.List[string]]::new()
    if ($null -ne $watchdog) {
        try {
            Stop-ScheduledTask -TaskName $watchdogTask -ErrorAction SilentlyContinue
            Wait-ExecutableExit (Join-Path $binDir "windows-watchdog.exe")
        } catch {
            $rollbackErrors.Add("stop watchdog before rollback: $($_.Exception.Message)")
        }
    }
    foreach ($taskName in @($agentTask, $eventTask)) {
        try {
            if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
                Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
                Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
            }
        } catch {
            $rollbackErrors.Add("remove replacement task ${taskName}: $($_.Exception.Message)")
        }
        if ($previousTaskXML.ContainsKey($taskName)) {
            try {
                Register-ScheduledTask -TaskName $taskName -Xml $previousTaskXML[$taskName] -Force | Out-Null
            } catch {
                $rollbackErrors.Add("restore task ${taskName}: $($_.Exception.Message)")
            }
        }
    }
    foreach ($name in $transactionFileNames) {
        $destination = Join-Path $binDir $name
        try {
            if ($previousFiles.ContainsKey($name)) {
                Copy-Item -LiteralPath $previousFiles[$name] -Destination $destination -Force
            } else {
                Remove-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
            }
        } catch {
            $rollbackErrors.Add("restore ${name}: $($_.Exception.Message)")
        }
    }
    if ($createdToken) {
        try { Remove-Item -LiteralPath $tokenFile -Force -ErrorAction Stop } catch {
            $rollbackErrors.Add("remove new event token: $($_.Exception.Message)")
        }
    }
    foreach ($taskName in $previousTaskXML.Keys) {
        try { Start-ScheduledTask -TaskName $taskName -ErrorAction Stop } catch {
            $rollbackErrors.Add("restart task ${taskName}: $($_.Exception.Message)")
        }
    }
    if ($watchdogWasRunning) {
        try { Start-ScheduledTask -TaskName $watchdogTask -ErrorAction Stop } catch {
            $rollbackErrors.Add("restart task ${watchdogTask}: $($_.Exception.Message)")
        }
    }
    if ($rollbackErrors.Count -eq 0) {
        Remove-Item -LiteralPath $backupDir -Recurse -Force -ErrorAction SilentlyContinue
    } else {
        throw "installation failed: $($failure.Exception.Message); rollback also failed: $($rollbackErrors -join '; ')"
    }
    throw $failure
}
