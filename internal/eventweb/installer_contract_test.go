package eventweb

import (
	"os"
	"strings"
	"testing"
)

func TestInstallerPreservesWindowlessInteractiveLoopbackContract(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-windows-event-web.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`[string]$WebListen = "127.0.0.1:8790"`,
		`[string]$StartupMode = "Standalone"`,
		`Event Web executable must use PE Windows GUI subsystem 2`,
		`New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited`,
		`if ($process.MainWindowHandle -ne 0)`,
		`event-web-api.token`,
		`Assert-EventListen -Value $EventListen`,
		`Assert-WebListen -Value $WebListen`,
		`WebListen must use loopback or an RFC1918 private LAN address`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer is missing contract fragment %q", required)
		}
	}
	if strings.Contains(script, "New-NetFirewallRule") {
		t.Fatal("installer must not expose the Event Web listener or alter Windows Firewall")
	}
}
