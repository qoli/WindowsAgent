Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not $env:LOCALAPPDATA) { throw "LOCALAPPDATA is required" }
$dataDir = [IO.Path]::GetFullPath($env:WINDOWSAGENT_INSTALL_DATA_DIR)
$expectedRoot = [IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA "gameGuide\windows-capture-agent"))
if ($dataDir -cne $expectedRoot) { throw "Watchdog data directory must equal the current user's WindowsAgent directory" }
$setting = [string]$env:WINDOWSAGENT_WATCHDOG_START_AT_LOGON
if ($setting -cnotin @("true", "false")) { throw "Watchdog start-at-logon setting must be true or false" }
$startAtLogon = $setting -ceq "true"
$executable = Join-Path $dataDir "bin\windows-watchdog.exe"
$config = Join-Path $dataDir "watchdog\config.json"
foreach ($path in @($executable, $config)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "installed Watchdog input is missing: $path" }
}
& (Join-Path $PSScriptRoot "install-windows-watchdog.ps1") -ExecutablePath $executable -ConfigPath $config -DataDir $dataDir -StartAtLogon:$startAtLogon
