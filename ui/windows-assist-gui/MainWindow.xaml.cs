using System.Collections.ObjectModel;
using System.Runtime.InteropServices;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Windows.ApplicationModel.DataTransfer;
using WindowsAgent.AssistGui.Backend;

namespace WindowsAgent.AssistGui;

public sealed partial class MainWindow : Window
{
    private const int InitialWidth = 720;
    private const int InitialHeight = 640;
    private readonly BackendClient _backend = new();
    private readonly SessionLog _log;
    private readonly ObservableCollection<string> _progress = [];
    private readonly ObservableCollection<string> _setupHosts = [];
    private AgentSnapshot? _snapshot;
    private bool _operationActive;
    private bool _updatingStartupCheckBox;
    private bool _loaded;
    private bool _allowClose;

    internal MainWindow(SessionLog log)
    {
        _log = log;
        InitializeComponent();
        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);
        ResizeToEffectivePixels(InitialWidth, InitialHeight);
        AppWindow.Closing += (_, args) =>
        {
            if (_operationActive && !_allowClose)
            {
                args.Cancel = true;
                _log.Warning("window-close-blocked", new { reason = "operation-active" });
                ShowNotice(InfoBarSeverity.Warning, "Wait for the current WindowsAgent operation to finish before closing AssistGUI.");
                return;
            }

            _log.Info("session-closing");
        };
        ProgressItems.ItemsSource = _progress;
        SetupHostComboBox.ItemsSource = _setupHosts;
        Root.Loaded += Root_Loaded;
        RefreshControls();
    }

    private void ResizeToEffectivePixels(int width, int height)
    {
        var windowHandle = WinRT.Interop.WindowNative.GetWindowHandle(this);
        var dpi = GetDpiForWindow(windowHandle);
        AppWindow.Resize(new Windows.Graphics.SizeInt32(
            checked((int)Math.Round(width * dpi / 96.0)),
            checked((int)Math.Round(height * dpi / 96.0))));
    }

    [DllImport("user32.dll")]
    private static extern uint GetDpiForWindow(IntPtr windowHandle);

    private async void Root_Loaded(object sender, RoutedEventArgs e)
    {
        if (_loaded)
        {
            return;
        }

        _loaded = true;
        await RunCommandAsync(BackendCommands.Inspect, null, "Checking WindowsAgent status");
    }

    private async void InstallButton_Click(object sender, RoutedEventArgs e) =>
        await RunConfiguredCommandAsync(BackendCommands.Install, "Installing WindowsAgent");

    private async void UpdateMenuItem_Click(object sender, RoutedEventArgs e) =>
        await RunConfiguredCommandAsync(BackendCommands.Update, "Updating WindowsAgent");

    private async void ReinstallMenuItem_Click(object sender, RoutedEventArgs e) =>
        await RunConfiguredCommandAsync(BackendCommands.Repair, "Reinstalling WindowsAgent");

    private async void UninstallMenuItem_Click(object sender, RoutedEventArgs e)
    {
        if (!await ConfirmAsync(
                "Uninstall WindowsAgent?",
                "This removes the installed WindowsAgent runtime and its owned Scheduled Tasks. User data is preserved.",
                "Uninstall"))
        {
            _log.Info("command-cancelled", new { command = BackendCommands.Uninstall });
            return;
        }

        await RunCommandAsync(BackendCommands.Uninstall, null, "Uninstalling WindowsAgent");
    }

    private async void StartRuntimeMenuItem_Click(object sender, RoutedEventArgs e)
    {
        var authKey = EmptyToNull(AuthKeyBox.Password);
        await RunCommandAsync(
            BackendCommands.Start,
            authKey is null ? null : new BackendSettings(TailscaleAuthKey: authKey),
            "Starting WindowsAgent");
    }

    private async void StopRuntimeMenuItem_Click(object sender, RoutedEventArgs e)
    {
        if (!await ConfirmAsync(
                "Stop WindowsAgent?",
                "This stops all installed WindowsAgent runtime processes, including Tailscale access. The installation, Scheduled Tasks, settings, and user data are preserved. A remote connection may be interrupted.",
                "Stop WindowsAgent"))
        {
            _log.Info("command-cancelled", new { command = BackendCommands.Stop });
            return;
        }

        await RunCommandAsync(BackendCommands.Stop, null, "Stopping WindowsAgent");
    }

    private async void StartAtSignInCheckBox_Click(object sender, RoutedEventArgs e)
    {
        if (_updatingStartupCheckBox || _operationActive || _snapshot is null || !_snapshot.Installed)
        {
            return;
        }

        await RunCommandAsync(
            BackendCommands.ConfigureWatchdog,
            new BackendSettings(WatchdogStartAtSignIn: StartAtSignInCheckBox.IsChecked == true),
            "Updating startup setting");

        if (_snapshot is not null)
        {
            SetStartAtSignIn(_snapshot.Watchdog.StartAtSignIn);
        }
    }

    private async void ConnectTailscaleButton_Click(object sender, RoutedEventArgs e)
    {
        var authKey = EmptyToNull(AuthKeyBox.Password);
        if (authKey is null)
        {
            ShowError("AUTH_KEY_REQUIRED", "Enter a Tailscale auth key before connecting.", null);
            return;
        }

        await RunCommandAsync(
            BackendCommands.StartTailscale,
            new BackendSettings(TailscaleAuthKey: authKey),
            "Connecting Tailscale");
    }

    private async void DisconnectTailscaleButton_Click(object sender, RoutedEventArgs e) =>
        await RunCommandAsync(BackendCommands.StopTailscale, null, "Disconnecting Tailscale");

    private async void RefreshButton_Click(object sender, RoutedEventArgs e) =>
        await RunCommandAsync(BackendCommands.Inspect, null, "Refreshing WindowsAgent status");

    private void AuthKeyBox_PasswordChanged(object sender, RoutedEventArgs e) => RefreshControls();

    private void SetupHostComboBox_SelectionChanged(object sender, SelectionChangedEventArgs e) => RefreshControls();

    private void CopySetupPromptButton_Click(object sender, RoutedEventArgs e)
    {
        if (_snapshot?.Installed != true || SetupHostComboBox.SelectedItem is not string host)
        {
            return;
        }

        var package = new DataPackage();
        package.SetText(HarnessSetupPrompt.Build(host));
        Clipboard.SetContent(package);
        Clipboard.Flush();
        ShowNotice(InfoBarSeverity.Success, "WindowsAgent setup prompt copied. Paste it into your Harness to finish initialization.");
    }

    private Task RunConfiguredCommandAsync(string command, string title) =>
        RunCommandAsync(
            command,
            new BackendSettings(WatchdogStartAtSignIn: StartAtSignInCheckBox.IsChecked == true),
            title);

    private async Task RunCommandAsync(string command, BackendSettings? settings, string title)
    {
        if (_operationActive)
        {
            return;
        }

        var showOperationOverlay = command != BackendCommands.Inspect;
        _operationActive = true;
        NoticeBar.IsOpen = false;
        _progress.Clear();
        OperationTitle.Text = title;
        OperationProgress.IsActive = showOperationOverlay;
        OperationOverlay.Visibility = showOperationOverlay ? Visibility.Visible : Visibility.Collapsed;
        RefreshControls();

        try
        {
            var request = BackendRequest.Create(command, settings);
            _log.Info("command-start", new
            {
                requestId = request.RequestId,
                command,
                watchdogStartAtSignIn = settings?.WatchdogStartAtSignIn,
                authKeyProvided = !string.IsNullOrWhiteSpace(settings?.TailscaleAuthKey),
            });
            var execution = _backend.ExecuteAsync(request, HandleBackendEvent);
            if (!string.IsNullOrWhiteSpace(settings?.TailscaleAuthKey))
            {
                // The secret has been handed to the stdin-only backend request;
                // do not retain it in the visible control for the operation's
                // remaining lifetime or after a backend failure.
                AuthKeyBox.Password = string.Empty;
            }

            var result = await execution;
            if (result.Snapshot is not null)
            {
                ApplySnapshot(result.Snapshot);
            }

            _log.Info("command-result", new
            {
                requestId = request.RequestId,
                command,
                result.Message,
                result.CloseGui,
                snapshot = result.Snapshot is null ? null : SnapshotSummary(result.Snapshot),
            });

            if (command != BackendCommands.Inspect)
            {
                ShowNotice(InfoBarSeverity.Success, result.Message);
            }
            if (result.CloseGui)
            {
                _allowClose = true;
                Application.Current.Exit();
                return;
            }
        }
        catch (BackendOperationException exception)
        {
            ShowError(exception.Code, exception.Message, exception.Details, exception);
        }
        catch (BackendProtocolException exception)
        {
            ShowError("BACKEND_PROTOCOL_ERROR", exception.Message, null, exception);
        }
        catch (OperationCanceledException exception)
        {
            ShowError("OPERATION_CANCELLED", "The operation was cancelled.", null, exception);
        }
        catch (Exception exception)
        {
            ShowError("GUI_OPERATION_FAILED", exception.Message, null, exception);
        }
        finally
        {
            _operationActive = false;
            OperationProgress.IsActive = false;
            OperationOverlay.Visibility = Visibility.Collapsed;
            RefreshControls();
        }
    }

    private void HandleBackendEvent(BackendEvent backendEvent)
    {
        switch (backendEvent)
        {
            case ProgressEvent progress:
                _log.Info("backend-progress", new
                {
                    requestId = progress.RequestId,
                    progress.Sequence,
                    progress.Phase,
                    progress.Message,
                });
                break;
            case SnapshotEvent snapshot:
                _log.Info("backend-snapshot", new
                {
                    requestId = snapshot.RequestId,
                    snapshot.Sequence,
                    snapshot = SnapshotSummary(snapshot.Snapshot),
                });
                break;
        }

        DispatcherQueue.TryEnqueue(() =>
        {
            switch (backendEvent)
            {
                case ProgressEvent progress:
                    _progress.Add($"{progress.Phase}: {progress.Message}");
                    break;
                case SnapshotEvent snapshot:
                    ApplySnapshot(snapshot.Snapshot);
                    break;
            }
        });
    }

    private void ApplySnapshot(AgentSnapshot snapshot)
    {
        var selectedHost = SetupHostComboBox.SelectedItem as string;
        _snapshot = snapshot;
        VersionValue.Text = snapshot.Version ?? "—";
        if (snapshot.Installed)
        {
            SetStartAtSignIn(snapshot.Watchdog.StartAtSignIn);
        }
        LanEndpointsValue.Text = snapshot.LanEndpoints.Count == 0
            ? "None detected"
            : string.Join(Environment.NewLine, snapshot.LanEndpoints);
        TailscaleStatusValue.Text = Capitalize(snapshot.Tailscale.Status);

        var addresses = new[] { snapshot.Tailscale.Ipv4, snapshot.Tailscale.Ipv6 }
            .Where(value => !string.IsNullOrWhiteSpace(value));
        var addressText = string.Join(Environment.NewLine, addresses);
        TailscaleAddressesValue.Text = string.IsNullOrEmpty(addressText) ? "No Tailscale addresses" : addressText;
        _setupHosts.Clear();
        foreach (var host in HarnessSetupPrompt.Hosts(
                     snapshot.LanEndpoints,
                     snapshot.Tailscale.Ipv4,
                     snapshot.Tailscale.Ipv6))
        {
            _setupHosts.Add(host);
        }
        SetupHostComboBox.SelectedItem = selectedHost is not null && _setupHosts.Contains(selectedHost)
            ? selectedHost
            : _setupHosts.FirstOrDefault();
        RefreshControls();
    }

    private void RefreshControls()
    {
        var installed = _snapshot?.Installed == true;
        var watchdogRunning = _snapshot?.Watchdog.Running == true;
        var captureRunning = _snapshot?.Capture.Running == true;
        var tailscaleRunning = _snapshot is not null && _snapshot.Tailscale.Status is "starting" or "online" or "stopping";
        var anyRuntimeRunning = watchdogRunning || captureRunning || tailscaleRunning;
        var enabled = !_operationActive;

        InstallButton.IsEnabled = enabled && _snapshot is not null && !installed;
        InstallButton.Visibility = _snapshot is not null && !installed ? Visibility.Visible : Visibility.Collapsed;
        MaintenanceButton.IsEnabled = enabled && installed;
        MaintenanceButton.Visibility = installed ? Visibility.Visible : Visibility.Collapsed;
        UpdateMenuItem.IsEnabled = enabled && installed;
        ReinstallMenuItem.IsEnabled = enabled && installed;
        UninstallMenuItem.IsEnabled = enabled && installed;

        RuntimeStatusValue.Text = _snapshot is null
            ? "Checking…"
            : !installed
                ? "Not installed"
                : anyRuntimeRunning ? "Running" : "Stopped";
        RuntimeMenuButton.IsEnabled = enabled && installed;
        StartRuntimeMenuItem.IsEnabled = enabled && installed && !anyRuntimeRunning;
        StartRuntimeMenuItem.Visibility = installed && !anyRuntimeRunning ? Visibility.Visible : Visibility.Collapsed;
        StopRuntimeMenuItem.IsEnabled = enabled && installed && anyRuntimeRunning;
        StopRuntimeMenuItem.Visibility = installed && anyRuntimeRunning ? Visibility.Visible : Visibility.Collapsed;

        StartAtSignInCheckBox.IsEnabled = enabled && _snapshot is not null;
        AuthKeyBox.IsEnabled = enabled && installed;
        ConnectTailscaleButton.IsEnabled = enabled && installed && !string.IsNullOrWhiteSpace(AuthKeyBox.Password);
        DisconnectTailscaleButton.IsEnabled = enabled && installed && tailscaleRunning;
        TailscaleDisconnectedPanel.Visibility = installed && !tailscaleRunning ? Visibility.Visible : Visibility.Collapsed;
        TailscaleConnectedPanel.Visibility = installed && tailscaleRunning ? Visibility.Visible : Visibility.Collapsed;
        SetupHostComboBox.IsEnabled = enabled && installed && _setupHosts.Count > 0;
        CopySetupPromptButton.IsEnabled = enabled && installed && SetupHostComboBox.SelectedItem is string;
        RefreshButton.IsEnabled = enabled;
    }

    private void SetStartAtSignIn(bool value)
    {
        _updatingStartupCheckBox = true;
        StartAtSignInCheckBox.IsChecked = value;
        _updatingStartupCheckBox = false;
    }

    private async Task<bool> ConfirmAsync(string title, string content, string primaryText)
    {
        var dialog = new ContentDialog
        {
            XamlRoot = Root.XamlRoot,
            Title = title,
            Content = content,
            PrimaryButtonText = primaryText,
            CloseButtonText = "Cancel",
            DefaultButton = ContentDialogButton.Close,
        };

        return await dialog.ShowAsync() == ContentDialogResult.Primary;
    }

    private void ShowError(string code, string message, string? details, Exception? exception = null)
    {
        _log.Error("operation-error", code, message, details, exception);
        var fullMessage = string.IsNullOrWhiteSpace(details)
            ? $"{code}: {message}"
            : $"{code}: {message}{Environment.NewLine}{details}";
        ShowNotice(InfoBarSeverity.Error, fullMessage);
    }

    private void ShowNotice(InfoBarSeverity severity, string message)
    {
        NoticeBar.Severity = severity;
        NoticeBar.Message = message;
        NoticeBar.IsOpen = true;
    }

    private static string? EmptyToNull(string value) => string.IsNullOrWhiteSpace(value) ? null : value;

    private static object SnapshotSummary(AgentSnapshot snapshot) => new
    {
        snapshot.Installed,
        snapshot.Version,
        captureRunning = snapshot.Capture.Running,
        watchdogInstalled = snapshot.Watchdog.Installed,
        watchdogRunning = snapshot.Watchdog.Running,
        snapshot.Watchdog.StartAtSignIn,
        lanEndpointCount = snapshot.LanEndpoints.Count,
        tailscaleStatus = snapshot.Tailscale.Status,
        tailscaleIpv4Assigned = !string.IsNullOrWhiteSpace(snapshot.Tailscale.Ipv4),
        tailscaleIpv6Assigned = !string.IsNullOrWhiteSpace(snapshot.Tailscale.Ipv6),
    };

    private static string Capitalize(string value) =>
        value.Length == 0 ? value : char.ToUpperInvariant(value[0]) + value[1..];
}
