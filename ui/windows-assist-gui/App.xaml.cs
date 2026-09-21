using Microsoft.UI.Xaml;

namespace WindowsAgent.AssistGui;

public partial class App : Application
{
    private const string SingleInstanceName = @"Local\WindowsAgent.AssistGUI";

    private Window? _window;
    private SessionLog? _log;
    private readonly Mutex? _singleInstanceMutex;
    private readonly bool _alreadyRunning;

    public App()
    {
        _singleInstanceMutex = new Mutex(initiallyOwned: true, SingleInstanceName, out var createdNew);
        _alreadyRunning = !createdNew;
        UnhandledException += (_, args) =>
            _log?.Error("unhandled-exception", "GUI_UNHANDLED_EXCEPTION", args.Message, exception: args.Exception);
        InitializeComponent();
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        if (_alreadyRunning)
        {
            _singleInstanceMutex?.Dispose();
            Exit();
            return;
        }

        _log = SessionLog.Create();
        _log.Info("session-start", new
        {
            processId = Environment.ProcessId,
            executable = Environment.ProcessPath,
        });
        _window = new MainWindow(_log);
        _window.Activate();
    }
}
