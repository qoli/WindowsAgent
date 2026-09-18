// Package installerscripts exposes the repository-owned Windows installers to
// AssistGUI. The PowerShell files remain the single source of installation
// semantics; AssistGUI only supplies their inputs and orchestration.
package installerscripts

import _ "embed"

//go:embed install-windows-capture-agent.ps1
var InstallWindowsCaptureAgent string

//go:embed sync-windows-agent-rule.ps1
var SyncWindowsAgentRule string

//go:embed install-windows-watchdog.ps1
var InstallWindowsWatchdog string

//go:embed deploy-windows-binaries.ps1
var DeployWindowsBinaries string
