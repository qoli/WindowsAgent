package main

import "testing"

func TestParseComponentConfig(t *testing.T) {
	_, err := parseComponentConfig([]string{
		"--component", "sessiond", "--node", `C:\node.exe`, "--runtime-dir", `C:\runtime`,
		"--agent-dir", `C:\agent`, "--session-dir", `C:\sessions`, "--pi-web-data-dir", `C:\web`,
		"--pi-web-config", `C:\web\config.json`, "--computer-use-helper", `C:\helper.exe`, "--log-file", `C:\logs\sessiond.log`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseComponentConfig([]string{"--component", "other"}); err == nil {
		t.Fatal("invalid component accepted")
	}
}
