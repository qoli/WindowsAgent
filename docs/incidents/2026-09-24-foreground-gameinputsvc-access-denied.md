# Foreground process query denied for GameInputSvc

Date: 2026-09-24 (UTC+08:00)

Status: Open; the immediate failure was observed, but the foreground transition was not reproduced.

## Impact and observed failure

On one signed-in Windows PC installed through AssistGUI, full capture returned
`foreground_process_unavailable` (HTTP 503). The WGC worker had acquired a frame
in approximately 11–38 ms; the failure occurred afterward, when foreground
identity resolution called `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` for
the PID returned by `GetForegroundWindow`. Windows returned `Access is denied`.
The inaccessible PID belonged to a `GameInputSvc.exe` child of the LocalSystem
GameInput service. The Agent health endpoint and structured process execution
remained available. No capture artifact or Rule identity was committed for the
failed request.

This was distinct from the separately diagnosed idle WGC frame timeout: the
frame arrived, and foreground process access was the terminal boundary.

## Comparison and recovery evidence

The affected installation reported Agent version 0.1.13. Its Capture Agent
Scheduled Task used `HighestAvailable`, and the running Agent had a High
integrity token in the signed-in interactive session. A comparison PC with an
older, conventional version 0.1.5 installation ran its Agent at Medium
integrity. Both PCs had a running LocalSystem GameInput service and a
`GameInputSvc.exe` child whose executable path was unavailable in process
inventory.

After the affected PC's foreground returned to an ordinary application,
captures succeeded again. A controlled comparison then launched a visible
PowerShell window on each PC while GameInput remained running. Four successive
captures on each PC succeeded and identified the actual foreground executable:
`powershell.exe` on the affected PC and `WindowsTerminal.exe` hosting PowerShell
on the comparison PC. Launching `powershell.exe` directly through detached
process start did not leave a visible window, so those attempts were not used
as foreground evidence. No GameInput service or account setting was changed.

## Assessment and unresolved question

The evidence does not support insufficient AssistGUI elevation: the failing
Agent ran at a higher integrity level than the comparison Agent. It also does
not establish PowerShell as the trigger. The immediate failure is bounded to
opening the service child after Windows reported that child as the foreground
window owner. Why Windows reported it as foreground while another application
appeared visible is unknown. The child's access policy and window properties
were not captured at the failure instant, so no DACL, protection-level, or
focus-transition cause is claimed.

The current implementation fails explicitly when it cannot obtain the
foreground executable path; it does not substitute a visible, previous, or
guessed process. A future recurrence needs correlated foreground-window
identity and window-state diagnostics at the same instant as the access error
before a behavioral change can be justified. This record documents the
incident and comparison only; it does not declare the issue fixed.
