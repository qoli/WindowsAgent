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

func TestAssistGUIIsPerMonitorDPIAware(t *testing.T) {
	manifest := readContractFile(t, filepath.Join("..", "ui", "windows-assist-gui", "app.manifest"))
	for _, required := range []string{
		`<dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/PM</dpiAware>`,
		`<dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2, PerMonitor</dpiAwareness>`,
	} {
		if !strings.Contains(manifest, required) {
			t.Fatalf("AssistGUI manifest is missing per-monitor DPI contract %q", required)
		}
	}
}

func TestAssistGUIUsesOneTruncatedSessionLogBesideTheExecutable(t *testing.T) {
	logger := readContractFile(t, filepath.Join("..", "ui", "windows-assist-gui", "SessionLog.cs"))
	for _, required := range []string{
		`public const string FileName = "windows-assist-gui.log"`,
		`Environment.ProcessPath`,
		`Path.GetDirectoryName(executablePath)`,
		`new FileStream(logPath, FileMode.Create, FileAccess.Write, FileShare.Read)`,
		`AutoFlush = true`,
		`new UTF8Encoding(encoderShouldEmitUTF8Identifier: false)`,
	} {
		if !strings.Contains(logger, required) {
			t.Fatalf("AssistGUI session log is missing contract %q", required)
		}
	}

	app := readContractFile(t, filepath.Join("..", "ui", "windows-assist-gui", "App.xaml.cs"))
	if strings.Index(app, "if (_alreadyRunning)") > strings.Index(app, "_log = SessionLog.Create()") {
		t.Fatal("AssistGUI must not truncate the active session log when a second instance exits")
	}

	window := readContractFile(t, filepath.Join("..", "ui", "windows-assist-gui", "MainWindow.xaml.cs"))
	for _, required := range []string{
		`_log.Info("command-start"`,
		`_log.Info("backend-progress"`,
		`_log.Info("command-result"`,
		`_log.Error("operation-error"`,
		`authKeyProvided = !string.IsNullOrWhiteSpace(settings?.TailscaleAuthKey)`,
	} {
		if !strings.Contains(window, required) {
			t.Fatalf("AssistGUI session log is missing behavior or error event %q", required)
		}
	}
}

func TestAssistGUIUsesTheCompactGroupedSettingsVisualContract(t *testing.T) {
	project := readContractFile(t, filepath.Join("..", "ui", "windows-assist-gui", "WindowsAssistGUI.csproj"))
	if strings.Contains(project, "CommunityToolkit.WinUI.Controls.SettingsControls") {
		t.Fatal("AssistGUI must not retain the unused SettingsControls dependency after adopting native grouped rows")
	}

	xaml := readContractFile(t, filepath.Join("..", "ui", "windows-assist-gui", "MainWindow.xaml"))
	for _, required := range []string{
		`<MicaBackdrop />`,
		`<TitleBar x:Name="AppTitleBar"`,
		`x:Name="RefreshButton"`,
		`<TextBlock Text="Refresh" />`,
		`<ScrollView x:Name="PageScroll"`,
		`<x:Double x:Key="Breakpoint640Plus">641</x:Double>`,
		`<Style x:Key="GroupedCardStyle" TargetType="Border">`,
		`<Style x:Key="SettingsRowStyle" TargetType="Grid">`,
		`BasedOn="{StaticResource BodyStrongTextBlockStyle}"`,
		`AutomationProperties.HeadingLevel="Level1"`,
		`x:Name="MaintenanceButton"`,
		`x:Name="UpdateMenuItem"`,
		`Text="Reinstall"`,
		`x:Name="UninstallMenuItem"`,
		`x:Name="RuntimeMenuButton"`,
		`x:Name="StartRuntimeMenuItem"`,
		`x:Name="StopRuntimeMenuItem"`,
		`<CheckBox x:Name="StartAtSignInCheckBox"`,
		`MinWidth="0"`,
		`HorizontalAlignment="Right"`,
		`x:Name="TailscaleDisconnectedPanel"`,
		`x:Name="TailscaleConnectedPanel"`,
		`<Grid x:Name="OperationOverlay"`,
		`Background="{ThemeResource SmokeFillColorDefaultBrush}"`,
		`<ItemsControl x:Name="ProgressItems" />`,
		`Visibility="Collapsed"`,
	} {
		if !strings.Contains(xaml, required) {
			t.Fatalf("AssistGUI XAML is missing compact grouped-settings contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"<NavigationView",
		"<TitleBar.RightHeader>",
		"toolkit:SettingsCard",
		"xmlns:toolkit=",
		"ApplicationPageBackgroundThemeBrush",
		"StartAtSignInToggle",
		"Apply startup setting",
		"Set up WindowsAgent",
		"Capture Agent",
		`x:Name="OperationPanel"`,
		`x:Name="OperationSectionHeader"`,
		"No operation in progress",
		"Setup operations are performed by the sibling",
	} {
		if strings.Contains(xaml, forbidden) {
			t.Fatalf("AssistGUI XAML regressed to the retired expanded settings pattern %q", forbidden)
		}
	}

	codeBehind := readContractFile(t, filepath.Join("..", "ui", "windows-assist-gui", "MainWindow.xaml.cs"))
	for _, required := range []string{
		`ResizeToEffectivePixels(InitialWidth, InitialHeight)`,
		`WinRT.Interop.WindowNative.GetWindowHandle(this)`,
		`GetDpiForWindow(windowHandle)`,
		`_snapshot.Tailscale.Status is "starting" or "online" or "stopping"`,
		`var showOperationOverlay = command != BackendCommands.Inspect`,
		`OperationOverlay.Visibility = showOperationOverlay ? Visibility.Visible : Visibility.Collapsed`,
		`if (command != BackendCommands.Inspect)`,
		`OperationOverlay.Visibility = Visibility.Collapsed`,
		`TailscaleDisconnectedPanel.Visibility = installed && !tailscaleRunning`,
		`TailscaleConnectedPanel.Visibility = installed && tailscaleRunning`,
		`StartAtSignInCheckBox.IsChecked == true`,
	} {
		if !strings.Contains(codeBehind, required) {
			t.Fatalf("AssistGUI code-behind is missing compact interaction contract %q", required)
		}
	}
	for _, forbidden := range []string{
		`_snapshot.Tailscale.Status != "disabled"`,
		"OperationPanel.Visibility",
		"StartAtSignInToggle",
	} {
		if strings.Contains(codeBehind, forbidden) {
			t.Fatalf("AssistGUI code-behind retains retired interaction %q", forbidden)
		}
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
