//go:build windows

package assistinstall

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeVerifierReadsActualProcessToken(t *testing.T) {
	script := []byte(verifyRuntimeScript)
	// The native query is exercised against a real process regardless of whether
	// the Windows test runner itself is elevated or in an interactive session.
	script = append(script, []byte(`
$self = [WindowsAgent.AssistSetup.ProcessIdentity]::Read([uint32]$PID)
$current = [Security.Principal.WindowsIdentity]::GetCurrent()
try {
    if ($self.ProcessId -ne $PID -or $self.SessionId -ne [Diagnostics.Process]::GetCurrentProcess().SessionId -or $self.UserSid -cne $current.User.Value) {
        throw "native token identity does not match the actual test process"
    }
    $principal = [Security.Principal.WindowsPrincipal]::new($current)
    $isAdmin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if ($self.Elevated -ne $isAdmin) { throw "token elevation disagrees with enabled administrator membership" }
    function Assert-Rejected([scriptblock]$Operation, [string]$Message) {
        $caught = $false
        try { & $Operation } catch {
            if (-not $_.Exception.Message.Contains($Message)) { throw }
            $caught = $true
        }
        if (-not $caught) { throw "expected verifier rejection: $Message" }
    }
    Assert-Rejected { [WindowsAgent.AssistSetup.ProcessIdentity]::Read(0) } "open Capture Agent process"
    Assert-Rejected {
        Assert-AssistProcessIdentity $self $self.ExecutablePath ($self.SessionId + 1) $self.UserSid
    } "interactive session"
    if ($self.SessionId -ne 0) {
        Assert-Rejected {
            Assert-AssistProcessIdentity $self $self.ExecutablePath $self.SessionId "S-1-0-0"
        } "installing user"
        if ($self.Elevated) {
            Assert-AssistProcessIdentity $self $self.ExecutablePath $self.SessionId $self.UserSid
        } else {
            Assert-Rejected {
                Assert-AssistProcessIdentity $self $self.ExecutablePath $self.SessionId $self.UserSid
            } "not elevated"
        }
    } else {
        Assert-Rejected {
            Assert-AssistProcessIdentity $self $self.ExecutablePath $self.SessionId $self.UserSid
        } "interactive session"
    }
    Assert-Rejected {
        Assert-AssistProcessIdentity $self (Join-Path $env:TEMP "not-the-capture-agent.exe") $self.SessionId $self.UserSid
    } "installed artifact"
} finally { $current.Dispose() }
`)...)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := runInstaller(ctx, Request{}, string(script)); err != nil {
		t.Fatal(err)
	}
}
