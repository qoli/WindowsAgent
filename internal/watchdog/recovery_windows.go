//go:build windows

package watchdog

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type WindowsTaskRecoverer struct{}

const restartTaskScript = `$ErrorActionPreference = 'Stop'
$name = $env:WINDOWS_WATCHDOG_TASK_NAME
$expectedDescription = $env:WINDOWS_WATCHDOG_TASK_DESCRIPTION
if ([String]::IsNullOrWhiteSpace($name) -or [String]::IsNullOrWhiteSpace($expectedDescription)) {
    throw "watchdog task identity environment is missing"
}
$task = Get-ScheduledTask -TaskName $name -ErrorAction Stop
if ($task.Description -ne $expectedDescription) {
    throw "scheduled task description mismatch"
}
if ($task.State -eq 'Running') {
    Stop-ScheduledTask -TaskName $name -ErrorAction Stop
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 200
        $task = Get-ScheduledTask -TaskName $name -ErrorAction Stop
    } while ($task.State -eq 'Running' -and [DateTime]::UtcNow -lt $deadline)
    if ($task.State -eq 'Running') {
        throw "scheduled task did not stop"
    }
}
Start-ScheduledTask -TaskName $name -ErrorAction Stop
`

func (WindowsTaskRecoverer) RestartScheduledTask(ctx context.Context, recovery RecoveryConfig) error {
	command := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", restartTaskScript)
	command.Env = append(os.Environ(),
		"WINDOWS_WATCHDOG_TASK_NAME="+recovery.ScheduledTaskName,
		"WINDOWS_WATCHDOG_TASK_DESCRIPTION="+recovery.ExpectedTaskDescription,
	)
	var stderr bytes.Buffer
	command.Stdout = nil
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("restart scheduled task %q: %s", recovery.ScheduledTaskName, detail)
	}
	return nil
}
