using System.Collections.ObjectModel;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using WindowsAgent.AssistGui.Backend;

namespace WindowsAgent.AssistGui;

public sealed partial class MainWindow : Window
{
    private readonly BackendClient _backend = new();
    private readonly ObservableCollection<string> _progress = [];
    private AgentSnapshot? _snapshot;
    private bool _operationActive;
    private bool _loaded;
    private bool _allowClose;

    public MainWindow()
    {
        InitializeComponent();
        AppWindow.Resize(new Windows.Graphics.SizeInt32(980, 760));
        AppWindow.Closing += (_, args) =>
        {
            if (_operationActive && !_allowClose)
            {
                args.Cancel = true;
                ShowNotice(InfoBarSeverity.Warning, "Wait for the current WindowsAgent operation to finish before closing AssistGUI.");
            }
        };
        ProgressItems.ItemsSource = _progress;
        Root.Loaded += Root_Loaded;
        RefreshControls();
    }

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

    private async void UpdateButton_Click(object sender, RoutedEventArgs e) =>
        await RunConfiguredCommandAsync(BackendCommands.Update, "Updating WindowsAgent");

    private async void RepairButton_Click(object sender, RoutedEventArgs e) =>
        await RunConfiguredCommandAsync(BackendCommands.Repair, "Repairing WindowsAgent");

    private async void UninstallButton_Click(object sender, RoutedEventArgs e)
    {
        if (!await ConfirmAsync(
                "Uninstall WindowsAgent?",
                "This removes the installed WindowsAgent runtime and its owned Scheduled Tasks. User data is preserved.",
                "Uninstall"))
        {
            return;
        }

        await RunCommandAsync(BackendCommands.Uninstall, null, "Uninstalling WindowsAgent");
    }

    private async void StartButton_Click(object sender, RoutedEventArgs e)
    {
        var authKey = EmptyToNull(AuthKeyBox.Password);
        await RunCommandAsync(
            BackendCommands.Start,
            authKey is null ? null : new BackendSettings(TailscaleAuthKey: authKey),
            "Starting WindowsAgent");
    }

    private async void StopButton_Click(object sender, RoutedEventArgs e)
    {
        if (!await ConfirmAsync(
                "Stop WindowsAgent?",
                "This stops all installed WindowsAgent runtime processes, including Tailscale access. The installation, Scheduled Tasks, settings, and user data are preserved. A remote connection may be interrupted.",
                "Stop WindowsAgent"))
        {
            return;
        }

        await RunCommandAsync(BackendCommands.Stop, null, "Stopping WindowsAgent");
    }

    private async void ApplyWatchdogButton_Click(object sender, RoutedEventArgs e) =>
        await RunCommandAsync(
            BackendCommands.ConfigureWatchdog,
            new BackendSettings(WatchdogStartAtSignIn: StartAtSignInCheckBox.IsChecked == true),
            "Updating Watchdog startup setting");

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

        _operationActive = true;
        NoticeBar.IsOpen = false;
        _progress.Clear();
        OperationTitle.Text = title;
        OperationProgress.IsActive = true;
        RefreshControls();

        try
        {
            var request = BackendRequest.Create(command, settings);
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

            ShowNotice(InfoBarSeverity.Success, result.Message);
            if (result.CloseGui)
            {
                _allowClose = true;
                Application.Current.Exit();
                return;
            }
        }
        catch (BackendOperationException exception)
        {
            ShowError(exception.Code, exception.Message, exception.Details);
        }
        catch (BackendProtocolException exception)
        {
            ShowError("BACKEND_PROTOCOL_ERROR", exception.Message, null);
        }
        catch (OperationCanceledException)
        {
            ShowError("OPERATION_CANCELLED", "The operation was cancelled.", null);
        }
        catch (Exception exception)
        {
            ShowError("GUI_OPERATION_FAILED", exception.Message, null);
        }
        finally
        {
            _operationActive = false;
            OperationProgress.IsActive = false;
            OperationTitle.Text = "No operation in progress";
            RefreshControls();
        }
    }

    private void HandleBackendEvent(BackendEvent backendEvent)
    {
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
        _snapshot = snapshot;
        InstalledValue.Text = snapshot.Installed ? "Installed" : "Not installed";
        VersionValue.Text = snapshot.Version ?? "—";
        CaptureValue.Text = snapshot.Capture.Running ? "Running" : "Stopped";
        WatchdogValue.Text = snapshot.Watchdog.Installed
            ? snapshot.Watchdog.Running ? "Running" : "Stopped"
            : "Not installed";
        if (snapshot.Watchdog.Installed)
        {
            StartAtSignInCheckBox.IsChecked = snapshot.Watchdog.StartAtSignIn;
        }
        LanEndpointsValue.Text = snapshot.LanEndpoints.Count == 0
            ? "None detected"
            : string.Join(Environment.NewLine, snapshot.LanEndpoints);
        TailscaleStatusValue.Text = Capitalize(snapshot.Tailscale.Status);

        var addresses = new[] { snapshot.Tailscale.Ipv4, snapshot.Tailscale.Ipv6 }
            .Where(value => !string.IsNullOrWhiteSpace(value));
        var addressText = string.Join(Environment.NewLine, addresses);
        TailscaleAddressesValue.Text = string.IsNullOrEmpty(addressText) ? "No Tailscale addresses" : addressText;
        RefreshControls();
    }

    private void RefreshControls()
    {
        var installed = _snapshot?.Installed == true;
        var watchdogRunning = _snapshot?.Watchdog.Running == true;
        var captureRunning = _snapshot?.Capture.Running == true;
        var tailscaleRunning = _snapshot is not null && _snapshot.Tailscale.Status != "disabled";
        var enabled = !_operationActive;

        InstallButton.IsEnabled = enabled && _snapshot is not null && !installed;
        UpdateButton.IsEnabled = enabled && installed;
        RepairButton.IsEnabled = enabled && installed;
        UninstallButton.IsEnabled = enabled && installed;
        StartButton.IsEnabled = enabled && installed && !watchdogRunning;
        StopButton.IsEnabled = enabled && installed && (watchdogRunning || captureRunning || tailscaleRunning);
        StartAtSignInCheckBox.IsEnabled = enabled && _snapshot is not null;
        ApplyWatchdogButton.IsEnabled = enabled && installed;
        AuthKeyBox.IsEnabled = enabled && installed;
        ConnectTailscaleButton.IsEnabled = enabled && installed && !string.IsNullOrWhiteSpace(AuthKeyBox.Password);
        DisconnectTailscaleButton.IsEnabled = enabled && installed && tailscaleRunning;
        RefreshButton.IsEnabled = enabled;
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

    private void ShowError(string code, string message, string? details)
    {
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

    private static string Capitalize(string value) =>
        value.Length == 0 ? value : char.ToUpperInvariant(value[0]) + value[1..];
}
