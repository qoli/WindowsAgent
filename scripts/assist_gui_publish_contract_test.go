package installerscripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistGUIIsFrameworkDependentAndUsesOfficialPrerequisiteUX(t *testing.T) {
	project := readContractFile(t, filepath.Join("..", "ui", "windows-assist-gui", "WindowsAssistGUI.csproj"))
	for _, required := range []string{
		"<WindowsPackageType>None</WindowsPackageType>",
		"<SelfContained>false</SelfContained>",
		"<FrameworkReference Include=\"Microsoft.WindowsDesktop.App\" />",
		"IncludeProjectPriInFrameworkDependentPublish",
		"<ResolvedFileToPublish Include=\"$(ProjectPriFullPath)\"",
	} {
		if !strings.Contains(project, required) {
			t.Fatalf("AssistGUI project is missing framework-dependent contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"<WindowsAppSDKSelfContained>",
		"<PublishSingleFile>",
		"<IncludeAllContentForSelfExtract>",
		"<WindowsAppSdkBootstrapInitialize>",
		"<WindowsAppSDKBootstrapAutoInitializeOptions_",
	} {
		if strings.Contains(project, forbidden) {
			t.Fatalf("AssistGUI project overrides the official framework/bootstrap path with %q", forbidden)
		}
	}

	build := readContractFile(t, "build-windows-assist-gui.ps1")
	for _, required := range []string{"--self-contained false", "windows-assist-gui.pri", "selfContained = $false", "singleFile = $false"} {
		if !strings.Contains(build, required) {
			t.Fatalf("AssistGUI build script is missing %q", required)
		}
	}

	workflow := readContractFile(t, filepath.Join("..", ".github", "workflows", "release.yml"))
	for _, required := range []string{
		"-OutputDir .release/assist-gui",
		"Copy-Item -Path .release/assist-gui/* -Destination $bootstrap -Recurse",
		"Copy-Item .release/windows-assist-backend.exe $bootstrap",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("release workflow is missing framework-dependent payload contract %q", required)
		}
	}

	uiRoot := filepath.Join("..", "ui", "windows-assist-gui")
	err := filepath.WalkDir(uiRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".cs") {
			return nil
		}
		source := readContractFile(t, path)
		for _, forbidden := range []string{
			"MessageBox",
			"MddBootstrap",
			"WindowsAppRuntimeInstall",
			"dotnet.microsoft.com",
			"aka.ms/dotnet",
		} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("AssistGUI source %s implements forbidden prerequisite behavior %q", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk AssistGUI source: %v", err)
	}
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
