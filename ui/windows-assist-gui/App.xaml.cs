using Microsoft.UI.Xaml;

namespace WindowsAgent.AssistGui;

public partial class App : Application
{
    private const string SingleInstanceName = @"Local\WindowsAgent.AssistGUI";

    private Window? _window;
    private readonly Mutex? _singleInstanceMutex;
    private readonly bool _alreadyRunning;

    public App()
    {
        _singleInstanceMutex = new Mutex(initiallyOwned: true, SingleInstanceName, out var createdNew);
        _alreadyRunning = !createdNew;
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

        _window = new MainWindow();
        _window.Activate();
    }
}
