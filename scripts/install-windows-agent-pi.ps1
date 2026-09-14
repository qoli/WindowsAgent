# Installs the independent PI WEB delegated-agent runtime for the current user.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ExecutablePath,
    [Parameter(Mandatory = $true)][string]$ComponentExecutablePath,
    [Parameter(Mandatory = $true)][string]$RuntimeBundlePath,
    [Parameter(Mandatory = $true)][string]$PiWebConfigPath,
    [string]$NodePath,
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "gameGuide\windows-agent-pi"),
    [string]$SessionDaemonListen = "127.0.0.1:8503",
    [string]$WebListen = "127.0.0.1:8504",
    [string]$AgentListen = "127.0.0.1:8791",
    [string]$SessionDaemonTaskName = "gameGuide Windows Agent Pi Session Daemon",
    [string]$WebTaskName = "gameGuide Windows Agent Pi Web",
    [string]$AgentTaskName = "gameGuide Windows Agent Pi",
    [timespan]$Timeout = ([timespan]::FromSeconds(30))
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$sessionDaemonDescription = "gameGuide windows-agent-pi PI WEB session daemon; interactive current-user token"
$webDescription = "gameGuide windows-agent-pi PI WEB browser and session projection; interactive current-user token"
$agentDescription = "gameGuide windows-agent-pi authenticated delegated-task control plane; interactive current-user token"

function Get-WindowsPESubsystem {
    param([Parameter(Mandatory = $true)][string]$Path)
    $bytes = [IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt 64 -or $bytes[0] -ne 0x4D -or $bytes[1] -ne 0x5A) {
        throw "executable does not have a valid DOS header: $Path"
    }
    $peOffset = [BitConverter]::ToInt32($bytes, 0x3C)
    $optionalOffset = $peOffset + 24
    if ($peOffset -lt 0 -or $optionalOffset + 0x46 -gt $bytes.Length -or
        $bytes[$peOffset] -ne 0x50 -or $bytes[$peOffset + 1] -ne 0x45 -or
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

function Copy-VerifiedFile {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$Label
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
        throw "$Label source does not exist: $Source"
    }
    if ([IO.Path]::GetFullPath($Source) -ine [IO.Path]::GetFullPath($Destination)) {
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
    }
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $Source).Hash -cne
        (Get-FileHash -Algorithm SHA256 -LiteralPath $Destination).Hash) {
        throw "installed $Label hash differs from source"
    }
}

function Resolve-LoopbackEndpoint {
    param([Parameter(Mandatory = $true)][string]$Value, [Parameter(Mandatory = $true)][string]$Label)
    $match = [regex]::Match($Value, "^127\.0\.0\.1:([0-9]{1,5})$")
    if (-not $match.Success) {
        throw "$Label must use explicit loopback form 127.0.0.1:<port>"
    }
    $port = [int]$match.Groups[1].Value
    if ($port -lt 1 -or $port -gt 65535) {
        throw "$Label port must be between 1 and 65535"
    }
    return [pscustomobject]@{ Listen = $Value; Port = $port; Health = "http://127.0.0.1:$port" }
}

function Assert-OwnedTask {
    param([Parameter(Mandatory = $true)][string]$Name, [Parameter(Mandatory = $true)][string]$Description)
    $task = Get-ScheduledTask -TaskName $Name -ErrorAction SilentlyContinue
    if ($task -and $task.Description -cne $Description) {
        throw "scheduled task '$Name' exists but is not owned by windows-agent-pi"
    }
    return $task
}

function Stop-OwnedTask {
    param([Parameter(Mandatory = $true)][string]$Name, [Parameter(Mandatory = $true)][string]$Description)
    $task = Assert-OwnedTask -Name $Name -Description $Description
    if ($task) {
        Stop-ScheduledTask -TaskName $Name -ErrorAction SilentlyContinue
        $deadline = [DateTime]::UtcNow.Add($Timeout)
        do {
            $state = (Get-ScheduledTask -TaskName $Name -ErrorAction Stop).State.ToString()
            if ($state -cne "Running") { return }
            Start-Sleep -Milliseconds 200
        } while ([DateTime]::UtcNow -lt $deadline)
        throw "scheduled task '$Name' did not stop before installation deadline"
    }
}

function Wait-ProcessPathStopped {
    param([Parameter(Mandatory = $true)][string]$Path)
    $deadline = [DateTime]::UtcNow.Add($Timeout)
    do {
        $running = @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -ieq $Path })
        if ($running.Count -eq 0) { return }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "process did not stop before installation deadline: $Path"
}

function Stop-OwnedPiChildProcesses {
    param(
        [Parameter(Mandatory = $true)][string]$Node,
        [Parameter(Mandatory = $true)][string]$Runtime,
        [Parameter(Mandatory = $true)][string]$Helper
    )
    $sessiondEntrypoint = Join-Path $Runtime "node_modules\@jmfederico\pi-web\dist\server\sessiond.js"
    $webEntrypoint = Join-Path $Runtime "node_modules\@jmfederico\pi-web\dist\server\index.js"
    $owned = @(Get-CimInstance Win32_Process | Where-Object {
        ($_.ExecutablePath -ieq $Node -and $_.CommandLine -and
            ($_.CommandLine.Contains($sessiondEntrypoint) -or $_.CommandLine.Contains($webEntrypoint))) -or
        $_.ExecutablePath -ieq $Helper
    })
    foreach ($process in $owned) {
        Stop-Process -Id $process.ProcessId -Force -ErrorAction Stop
    }
    $deadline = [DateTime]::UtcNow.Add($Timeout)
    foreach ($process in $owned) {
        while (Get-Process -Id $process.ProcessId -ErrorAction SilentlyContinue) {
            if ([DateTime]::UtcNow -ge $deadline) {
                throw "owned PI runtime process $($process.ProcessId) did not stop before installation deadline"
            }
            Start-Sleep -Milliseconds 200
        }
    }
}

function New-RuntimeTaskSettings {
    $arguments = @{
        AllowStartIfOnBatteries = $true
        DontStopIfGoingOnBatteries = $true
        StartWhenAvailable = $true
        Hidden = $true
        ExecutionTimeLimit = [timespan]::Zero
        MultipleInstances = "IgnoreNew"
    }
    $arguments.RestartCount = 3
    $arguments.RestartInterval = [timespan]::FromMinutes(1)
    return New-ScheduledTaskSettingsSet @arguments
}

function Register-RuntimeTask {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Description,
        [Parameter(Mandatory = $true)]$Action,
        [Parameter(Mandatory = $true)]$Principal,
        [Parameter(Mandatory = $true)]$Settings,
        [Parameter(Mandatory = $true)][string]$Identity
    )
    $taskArguments = @{
        Action = $Action
        Principal = $Principal
        Settings = $Settings
        Description = $Description
    }
    $taskArguments.Trigger = New-ScheduledTaskTrigger -AtLogOn -User $Identity
    Register-ScheduledTask -TaskName $Name -InputObject (New-ScheduledTask @taskArguments) -Force | Out-Null
}

function Wait-JsonHealth {
    param(
        [Parameter(Mandatory = $true)][string]$TaskName,
        [Parameter(Mandatory = $true)][string]$URI,
        [Parameter(Mandatory = $true)][scriptblock]$Accept
    )
    Start-ScheduledTask -TaskName $TaskName
    $deadline = [DateTime]::UtcNow.Add($Timeout)
    $lastFailure = "endpoint did not return a response matching the required health contract"
    do {
        try {
            $response = Invoke-RestMethod -Method Get -Uri $URI -TimeoutSec 2
            if (& $Accept $response) { return $response }
            $lastFailure = "endpoint response did not match the required health contract"
        } catch {
            $lastFailure = $_.Exception.Message
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "scheduled task '$TaskName' did not become healthy at ${URI}: $lastFailure"
}

if (-not $env:LOCALAPPDATA) {
    throw "LOCALAPPDATA is required"
}
if ($Timeout -le [timespan]::Zero) {
    throw "Timeout must be positive"
}
$sessionDaemonEndpoint = Resolve-LoopbackEndpoint -Value $SessionDaemonListen -Label "SessionDaemonListen"
$webEndpoint = Resolve-LoopbackEndpoint -Value $WebListen -Label "WebListen"
$agentEndpoint = Resolve-LoopbackEndpoint -Value $AgentListen -Label "AgentListen"
$ports = @($sessionDaemonEndpoint.Port, $webEndpoint.Port, $agentEndpoint.Port) | Select-Object -Unique
if ($ports.Count -ne 3) {
    throw "SessionDaemonListen, WebListen, and AgentListen must use different ports"
}

$sourceExecutable = [IO.Path]::GetFullPath($ExecutablePath)
$sourceComponentExecutable = [IO.Path]::GetFullPath($ComponentExecutablePath)
$sourceRuntime = [IO.Path]::GetFullPath($RuntimeBundlePath)
$sourceConfig = [IO.Path]::GetFullPath($PiWebConfigPath)
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    throw "windows-agent-pi executable does not exist: $sourceExecutable"
}
if ((Get-WindowsPESubsystem -Path $sourceExecutable) -ne 2) {
    throw "windows-agent-pi executable must use PE Windows GUI subsystem 2"
}
if (-not (Test-Path -LiteralPath $sourceComponentExecutable -PathType Leaf) -or
    (Get-WindowsPESubsystem -Path $sourceComponentExecutable) -ne 2) {
    throw "windows-agent-pi component owner must exist and use PE Windows GUI subsystem 2"
}
if (-not (Test-Path -LiteralPath $sourceConfig -PathType Leaf)) {
    throw "PI WEB config does not exist: $sourceConfig"
}
$null = Get-Content -LiteralPath $sourceConfig -Raw | ConvertFrom-Json -ErrorAction Stop

if (-not $NodePath) {
    $nodeCommand = Get-Command node.exe -ErrorAction Stop
    $NodePath = $nodeCommand.Source
}
$resolvedNode = [IO.Path]::GetFullPath($NodePath)
if (-not (Test-Path -LiteralPath $resolvedNode -PathType Leaf)) {
    throw "Node executable does not exist: $resolvedNode"
}
$nodeVersionText = (& $resolvedNode --version)
if ($LASTEXITCODE -ne 0 -or $nodeVersionText -notmatch '^v(\d+)\.(\d+)\.(\d+)$') {
    throw "could not resolve the installed Node version"
}
$nodeMajor = [int]$Matches[1]
$nodeMinor = [int]$Matches[2]
if ($nodeMajor -lt 22 -or ($nodeMajor -eq 22 -and $nodeMinor -lt 19)) {
    throw "windows-agent-pi requires Node 22.19.0 or newer; found $nodeVersionText"
}

$lockFile = Join-Path $sourceRuntime "package-lock.json"
$runtimePackageFile = Join-Path $sourceRuntime "package.json"
foreach ($required in @($lockFile, $runtimePackageFile)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "runtime bundle is missing required file: $required"
    }
}
$expectedPackages = [ordered]@{
    "@jmfederico/pi-web" = "1.202609.0"
    "@earendil-works/pi-ai" = "0.85.1"
    "@earendil-works/pi-agent-core" = "0.85.1"
    "@earendil-works/pi-coding-agent" = "0.85.1"
    "@injaneity/pi-computer-use" = "0.5.1"
    "typebox" = "1.3.7"
}
foreach ($packageName in $expectedPackages.Keys) {
    $packagePath = Join-Path $sourceRuntime ("node_modules\" + $packageName + "\package.json")
    if (-not (Test-Path -LiteralPath $packagePath -PathType Leaf)) {
        throw "runtime bundle is not installed; missing package: $packagePath"
    }
    $metadata = Get-Content -LiteralPath $packagePath -Raw | ConvertFrom-Json -ErrorAction Stop
    if ([string]$metadata.name -cne $packageName -or [string]$metadata.version -cne $expectedPackages[$packageName]) {
        throw "runtime package identity mismatch at $packagePath"
    }
}

$resolvedDataDir = [IO.Path]::GetFullPath($DataDir)
$binDir = Join-Path $resolvedDataDir "bin"
$runtimeRoot = Join-Path $resolvedDataDir "runtimes"
$agentDir = Join-Path $resolvedDataDir "pi-agent"
$sessionDir = Join-Path $resolvedDataDir "sessions"
$piWebDataDir = Join-Path $resolvedDataDir "pi-web"
$journalDir = Join-Path $resolvedDataDir "delegated-events"
$logDir = Join-Path $resolvedDataDir "logs"
$helperPath = Join-Path $resolvedDataDir "helpers\pi-computer-use\windows-bridge.exe"
$tokenFile = Join-Path $resolvedDataDir "delegated-api.token"
$installedConfig = Join-Path $piWebDataDir "config.json"
$installedExecutable = Join-Path $binDir "windows-agent-pi.exe"
$installedComponentExecutable = Join-Path $binDir "windows-agent-pi-component.exe"

$lockHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $lockFile).Hash.ToLowerInvariant()
$installedRuntime = Join-Path $runtimeRoot ("runtime-" + $lockHash.Substring(0, 16))
$computerUseSource = Join-Path $sourceRuntime "node_modules\@injaneity\pi-computer-use"
$prebuiltHelper = Join-Path $computerUseSource "prebuilt\windows\windows-bridge.exe"
if (-not (Test-Path -LiteralPath $prebuiltHelper -PathType Leaf)) {
    throw "pinned pi-computer-use package has no Windows prebuilt helper: $prebuiltHelper"
}
$agentSettings = Join-Path $agentDir "settings.json"
if (-not (Test-Path -LiteralPath $agentSettings -PathType Leaf)) {
    throw "dedicated Pi profile is not configured: $agentSettings; run the pinned pi:install-computer-use command with PI_CODING_AGENT_DIR set to $agentDir"
}
$settings = Get-Content -LiteralPath $agentSettings -Raw | ConvertFrom-Json -ErrorAction Stop
if (-not ($settings.PSObject.Properties.Name -ccontains "packages")) {
    throw "dedicated Pi profile settings must declare packages: $agentSettings"
}
$configuredComputerUse = @($settings.packages | Where-Object {
    if ($_ -is [string]) {
        return $_ -ceq "npm:@injaneity/pi-computer-use@0.5.1"
    }
    return $null -ne $_ -and
        ($_.PSObject.Properties.Name -ccontains "source") -and
        [string]$_.source -ceq "npm:@injaneity/pi-computer-use@0.5.1"
})
if ($configuredComputerUse.Count -ne 1) {
    throw "dedicated Pi profile must configure exactly npm:@injaneity/pi-computer-use@0.5.1"
}

foreach ($directory in @($binDir, $runtimeRoot, $sessionDir, $piWebDataDir, $journalDir, $logDir, ([IO.Path]::GetDirectoryName($helperPath)))) {
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
}
if (-not (Test-Path -LiteralPath $installedRuntime -PathType Container)) {
    $stagingRuntime = Join-Path $runtimeRoot ("staging-" + [Guid]::NewGuid().ToString("N"))
    try {
        New-Item -ItemType Directory -Path $stagingRuntime -Force | Out-Null
        Get-ChildItem -LiteralPath $sourceRuntime -Force | ForEach-Object {
            Copy-Item -LiteralPath $_.FullName -Destination $stagingRuntime -Recurse -Force
        }
        if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $stagingRuntime "package-lock.json")).Hash.ToLowerInvariant() -cne $lockHash) {
            throw "staged runtime lock hash differs from source"
        }
        Move-Item -LiteralPath $stagingRuntime -Destination $installedRuntime
    } catch {
        if (Test-Path -LiteralPath $stagingRuntime) {
            Remove-Item -LiteralPath $stagingRuntime -Recurse -Force
        }
        throw
    }
}
if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $installedRuntime "package-lock.json")).Hash.ToLowerInvariant() -cne $lockHash) {
    throw "installed runtime lock hash differs from source"
}
foreach ($packageName in $expectedPackages.Keys) {
    $packagePath = Join-Path $installedRuntime ("node_modules\" + $packageName + "\package.json")
    if (-not (Test-Path -LiteralPath $packagePath -PathType Leaf)) {
        throw "installed runtime is missing pinned package: $packagePath"
    }
    $metadata = Get-Content -LiteralPath $packagePath -Raw | ConvertFrom-Json -ErrorAction Stop
    if ([string]$metadata.name -cne $packageName -or [string]$metadata.version -cne $expectedPackages[$packageName]) {
        throw "installed runtime package identity mismatch at $packagePath"
    }
}

Stop-OwnedTask -Name $AgentTaskName -Description $agentDescription
Stop-OwnedTask -Name $WebTaskName -Description $webDescription
Stop-OwnedTask -Name $SessionDaemonTaskName -Description $sessionDaemonDescription
Stop-OwnedPiChildProcesses -Node $resolvedNode -Runtime $installedRuntime -Helper $helperPath
Wait-ProcessPathStopped -Path $installedExecutable
Wait-ProcessPathStopped -Path $installedComponentExecutable

Copy-VerifiedFile -Source $sourceExecutable -Destination $installedExecutable -Label "windows-agent-pi"
Copy-VerifiedFile -Source $sourceComponentExecutable -Destination $installedComponentExecutable -Label "PI WEB component owner"
Copy-VerifiedFile -Source $sourceConfig -Destination $installedConfig -Label "PI WEB config"
Copy-VerifiedFile -Source $prebuiltHelper -Destination $helperPath -Label "pi-computer-use helper"

if (-not (Test-Path -LiteralPath $tokenFile)) {
    $tokenBytes = New-Object byte[] 32
    $tokenRng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $tokenRng.GetBytes($tokenBytes)
        [IO.File]::WriteAllText($tokenFile, [Convert]::ToBase64String($tokenBytes))
    } finally {
        $tokenRng.Dispose()
    }
}
$tokenInfo = Get-Item -LiteralPath $tokenFile -Force
if ($tokenInfo.PSIsContainer -or $tokenInfo.Length -lt 32 -or $tokenInfo.Length -gt 4096) {
    throw "delegated task token must be a regular file between 32 and 4096 bytes: $tokenFile"
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
$taskSettings = New-RuntimeTaskSettings
$commonComponentArguments = @(
    "--node", (ConvertTo-NativeQuotedArgument $resolvedNode),
    "--runtime-dir", (ConvertTo-NativeQuotedArgument $installedRuntime),
    "--agent-dir", (ConvertTo-NativeQuotedArgument $agentDir),
    "--session-dir", (ConvertTo-NativeQuotedArgument $sessionDir),
    "--pi-web-data-dir", (ConvertTo-NativeQuotedArgument $piWebDataDir),
    "--pi-web-config", (ConvertTo-NativeQuotedArgument $installedConfig),
    "--computer-use-helper", (ConvertTo-NativeQuotedArgument $helperPath),
    "--sessiond-listen", (ConvertTo-NativeQuotedArgument $SessionDaemonListen),
    "--web-listen", (ConvertTo-NativeQuotedArgument $WebListen)
)
$sessionDaemonArguments = @($commonComponentArguments + @(
    "--component", "sessiond", "--log-file", (ConvertTo-NativeQuotedArgument (Join-Path $logDir "pi-web-sessiond.log"))
)) -join " "
$webArguments = @($commonComponentArguments + @(
    "--component", "web", "--log-file", (ConvertTo-NativeQuotedArgument (Join-Path $logDir "pi-web.log"))
)) -join " "
$agentArguments = @(
    "--listen", (ConvertTo-NativeQuotedArgument $AgentListen),
    "--data-dir", (ConvertTo-NativeQuotedArgument $journalDir),
    "--token-file", (ConvertTo-NativeQuotedArgument $tokenFile),
    "--pi-web-url", (ConvertTo-NativeQuotedArgument $webEndpoint.Health),
    "--log-file", (ConvertTo-NativeQuotedArgument (Join-Path $logDir "windows-agent-pi.jsonl"))
) -join " "

Register-RuntimeTask -Name $SessionDaemonTaskName -Description $sessionDaemonDescription `
    -Action (New-ScheduledTaskAction -Execute $installedComponentExecutable -Argument $sessionDaemonArguments) `
    -Principal $principal -Settings $taskSettings -Identity $identity
Register-RuntimeTask -Name $WebTaskName -Description $webDescription `
    -Action (New-ScheduledTaskAction -Execute $installedComponentExecutable -Argument $webArguments) `
    -Principal $principal -Settings $taskSettings -Identity $identity
Register-RuntimeTask -Name $AgentTaskName -Description $agentDescription `
    -Action (New-ScheduledTaskAction -Execute $installedExecutable -Argument $agentArguments) `
    -Principal $principal -Settings $taskSettings -Identity $identity

$sessionDaemonHealth = Wait-JsonHealth -TaskName $SessionDaemonTaskName -URI ($sessionDaemonEndpoint.Health + "/health") -Accept {
    param($value) $value -and $value.ok -eq $true -and $value.version -and $value.version.component -ceq "sessiond"
}
$webHealth = Wait-JsonHealth -TaskName $WebTaskName -URI ($webEndpoint.Health + "/api/machines/local/runtime?refresh=1") -Accept {
    param($value)
    $value -and
        $value.ok -eq $true -and
        $value.machineId -ceq "local" -and
        $value.packageName -ceq "@jmfederico/pi-web" -and
        $value.components.web.component -ceq "web" -and
        $value.components.web.available -eq $true -and
        $value.components.web.runtimeVersion -ceq "1.202609.0" -and
        $value.components.web.piVersion -ceq "0.85.1" -and
        $value.components.sessiond.component -ceq "sessiond" -and
        $value.components.sessiond.available -eq $true -and
        $value.components.sessiond.runtimeVersion -ceq "1.202609.0" -and
        $value.components.sessiond.piVersion -ceq "0.85.1" -and
        $value.components.sessiond.activeAgentProfile.dir -ieq $agentDir
}
$agentHealth = Wait-JsonHealth -TaskName $AgentTaskName -URI ($agentEndpoint.Health + "/healthz") -Accept {
    param($value) $value -and $value.status -ceq "ok" -and $value.runtime -ceq "windows-agent-pi"
}

$agentProcess = Get-Process -Name "windows-agent-pi" -ErrorAction Stop |
    Where-Object { $_.Path -ieq $installedExecutable } | Select-Object -First 1
if (-not $agentProcess -or $agentProcess.SessionId -eq 0) {
    throw "windows-agent-pi is not running in the signed-in interactive session"
}
$nodeProcesses = @(Get-CimInstance Win32_Process -Filter "Name = 'node.exe'" | Where-Object {
    $_.CommandLine -and ($_.CommandLine.Contains((Join-Path $installedRuntime "node_modules\@jmfederico\pi-web\dist\server\sessiond.js")) -or
        $_.CommandLine.Contains((Join-Path $installedRuntime "node_modules\@jmfederico\pi-web\dist\server\index.js")))
})
if ($nodeProcesses.Count -ne 2 -or @($nodeProcesses | Where-Object { $_.SessionId -eq 0 }).Count -ne 0) {
    throw "PI WEB session daemon and Web process must both run in the signed-in interactive session"
}
$componentProcesses = @(Get-CimInstance Win32_Process -Filter "Name = 'windows-agent-pi-component.exe'" | Where-Object {
    $_.ExecutablePath -ieq $installedComponentExecutable
})
if ($componentProcesses.Count -ne 2 -or @($componentProcesses | Where-Object { $_.SessionId -eq 0 }).Count -ne 0) {
    throw "both PI WEB component owners must run in the signed-in interactive session"
}

$registeredSessionDaemon = Get-ScheduledTask -TaskName $SessionDaemonTaskName -ErrorAction Stop
$registeredWeb = Get-ScheduledTask -TaskName $WebTaskName -ErrorAction Stop
$registeredAgent = Get-ScheduledTask -TaskName $AgentTaskName -ErrorAction Stop

[ordered]@{
    startup_mode = "Standalone"
    runtime_lock_sha256 = $lockHash
    runtime_dir = $installedRuntime
    node = $resolvedNode
    node_version = $nodeVersionText
    pi_agent_dir = $agentDir
    pi_session_dir = $sessionDir
    pi_web_data_dir = $piWebDataDir
    pi_web_config = $installedConfig
    pi_computer_use_helper = $helperPath
    pi_computer_use_helper_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $helperPath).Hash.ToLowerInvariant()
    component_owner = [ordered]@{
        executable = $installedComponentExecutable
        sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $installedComponentExecutable).Hash.ToLowerInvariant()
        process_ids = @($componentProcesses | ForEach-Object { $_.ProcessId })
        session_ids = @($componentProcesses | ForEach-Object { $_.SessionId } | Select-Object -Unique)
    }
    sessiond = [ordered]@{
        task_name = $SessionDaemonTaskName
        task_state = $registeredSessionDaemon.State.ToString()
        trigger_count = @($registeredSessionDaemon.Triggers | Where-Object { $null -ne $_ }).Count
        restart_count = [int]$registeredSessionDaemon.Settings.RestartCount
        listen = $SessionDaemonListen
        version = $sessionDaemonHealth.version
    }
    web = [ordered]@{
        task_name = $WebTaskName
        task_state = $registeredWeb.State.ToString()
        trigger_count = @($registeredWeb.Triggers | Where-Object { $null -ne $_ }).Count
        restart_count = [int]$registeredWeb.Settings.RestartCount
        url = $webEndpoint.Health
        health = $webHealth
    }
    agent = [ordered]@{
        task_name = $AgentTaskName
        task_state = $registeredAgent.State.ToString()
        trigger_count = @($registeredAgent.Triggers | Where-Object { $null -ne $_ }).Count
        restart_count = [int]$registeredAgent.Settings.RestartCount
        executable = $installedExecutable
        sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $installedExecutable).Hash.ToLowerInvariant()
        process_id = $agentProcess.Id
        session_id = $agentProcess.SessionId
        url = $agentEndpoint.Health
        token_file = $tokenFile
        health = $agentHealth
    }
} | ConvertTo-Json -Depth 10
