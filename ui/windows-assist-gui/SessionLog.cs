using System.Text;
using System.Text.Json;

namespace WindowsAgent.AssistGui;

internal sealed class SessionLog
{
    public const string FileName = "windows-assist-gui.log";

    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web);

    private readonly object _gate = new();
    private readonly StreamWriter _writer;

    private SessionLog(StreamWriter writer)
    {
        _writer = writer;
    }

    public static SessionLog Create()
    {
        var executablePath = Environment.ProcessPath
            ?? throw new InvalidOperationException("Could not determine the AssistGUI executable path for logging.");
        var executableDirectory = Path.GetDirectoryName(executablePath)
            ?? throw new InvalidOperationException("Could not determine the AssistGUI executable directory for logging.");
        var logPath = Path.Combine(executableDirectory, FileName);
        var stream = new FileStream(logPath, FileMode.Create, FileAccess.Write, FileShare.Read);
        var writer = new StreamWriter(stream, new UTF8Encoding(encoderShouldEmitUTF8Identifier: false))
        {
            AutoFlush = true,
        };
        return new SessionLog(writer);
    }

    public void Info(string eventName, object? details = null) => Write("info", eventName, details);

    public void Warning(string eventName, object? details = null) => Write("warning", eventName, details);

    public void Error(
        string eventName,
        string code,
        string message,
        string? details = null,
        Exception? exception = null) =>
        Write("error", eventName, new
        {
            code,
            message,
            details,
            exceptionType = exception?.GetType().FullName,
            exceptionMessage = exception?.Message,
            stackTrace = exception?.StackTrace,
        });

    private void Write(string level, string eventName, object? details)
    {
        var line = JsonSerializer.Serialize(
            new LogEntry(DateTimeOffset.UtcNow, level, eventName, details),
            JsonOptions);
        lock (_gate)
        {
            _writer.WriteLine(line);
        }
    }

    private sealed record LogEntry(
        DateTimeOffset Timestamp,
        string Level,
        string Event,
        object? Details);
}
