using System.Diagnostics;
using System.Text;
using System.Text.Json;

namespace WindowsAgent.AssistGui.Backend;

internal sealed class BackendClient
{
    private const string BackendFileName = "windows-assist-backend.exe";

    public async Task<ResultEvent> ExecuteAsync(
        BackendRequest request,
        Action<BackendEvent> onEvent,
        CancellationToken cancellationToken = default)
    {
        ArgumentNullException.ThrowIfNull(request);
        ArgumentNullException.ThrowIfNull(onEvent);

        var backendPath = ResolveSiblingBackendPath();
        var startInfo = new ProcessStartInfo
        {
            FileName = backendPath,
            UseShellExecute = false,
            CreateNoWindow = true,
            RedirectStandardInput = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            StandardInputEncoding = new UTF8Encoding(encoderShouldEmitUTF8Identifier: false),
            StandardOutputEncoding = Encoding.UTF8,
            StandardErrorEncoding = Encoding.UTF8,
        };

        using var process = new Process { StartInfo = startInfo };
        try
        {
            if (!process.Start())
            {
                throw new BackendProtocolException("WindowsAgent setup backend did not start.");
            }
        }
        catch (Exception exception) when (exception is not BackendProtocolException)
        {
            throw new BackendProtocolException($"Could not start WindowsAgent setup backend at '{backendPath}'.", exception);
        }

        try
        {
            await using (process.StandardInput)
            {
                var requestLine = JsonSerializer.Serialize(request, BackendProtocol.JsonOptions);
                await process.StandardInput.WriteLineAsync(requestLine.AsMemory(), cancellationToken);
            }

            var stderrTask = process.StandardError.ReadToEndAsync(cancellationToken);
            ResultEvent? result = null;
            ErrorEvent? error = null;
            long lastSequence = 0;

            while (await process.StandardOutput.ReadLineAsync(cancellationToken) is { } line)
            {
                var backendEvent = BackendEventParser.Parse(line);
                if (!string.Equals(backendEvent.RequestId, request.RequestId, StringComparison.Ordinal))
                {
                    throw new BackendProtocolException("Backend event requestId does not match the request.");
                }

                if (backendEvent.Sequence <= lastSequence)
                {
                    throw new BackendProtocolException("Backend event sequence is not strictly increasing.");
                }

                if (result is not null || error is not null)
                {
                    throw new BackendProtocolException("Backend emitted an event after its terminal event.");
                }

                lastSequence = backendEvent.Sequence;
                onEvent(backendEvent);

                if (backendEvent is ResultEvent resultEvent)
                {
                    result = resultEvent;
                }
                else if (backendEvent is ErrorEvent errorEvent)
                {
                    error = errorEvent;
                }
            }

            await process.WaitForExitAsync(cancellationToken);
            var stderr = await stderrTask;
            if (error is not null)
            {
                throw new BackendOperationException(error.Code, error.Message, error.Details);
            }

            if (result is null)
            {
                var suffix = string.IsNullOrWhiteSpace(stderr) ? string.Empty : $" Backend diagnostics: {stderr.Trim()}";
                throw new BackendProtocolException($"Backend exited without a terminal result (exit code {process.ExitCode}).{suffix}");
            }

            if (process.ExitCode != 0)
            {
                throw new BackendProtocolException($"Backend returned success but exited with code {process.ExitCode}.");
            }

            return result;
        }
        catch
        {
            TryKill(process);
            throw;
        }
    }

    private static string ResolveSiblingBackendPath()
    {
        var guiPath = Environment.ProcessPath;
        if (string.IsNullOrWhiteSpace(guiPath))
        {
            throw new BackendProtocolException("Could not determine the AssistGUI executable path.");
        }

        var directory = Path.GetDirectoryName(guiPath)
            ?? throw new BackendProtocolException("Could not determine the AssistGUI executable directory.");
        var backendPath = Path.Combine(directory, BackendFileName);
        if (!File.Exists(backendPath))
        {
            throw new BackendProtocolException($"Required sibling backend is missing: {backendPath}");
        }

        return backendPath;
    }

    private static void TryKill(Process process)
    {
        try
        {
            if (!process.HasExited)
            {
                process.Kill(entireProcessTree: true);
            }
        }
        catch (InvalidOperationException)
        {
        }
    }
}

internal sealed class BackendOperationException : Exception
{
    public BackendOperationException(string code, string message, string? details)
        : base(message)
    {
        Code = code;
        Details = details;
    }

    public string Code { get; }

    public string? Details { get; }
}
