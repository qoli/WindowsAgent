using System.Text.Json;
using System.Text.Json.Serialization;

namespace WindowsAgent.AssistGui.Backend;

internal static class BackendEventParser
{
    private static readonly HashSet<string> TailscaleStatuses =
    [
        "disabled",
        "starting",
        "online",
        "stopping",
        "error",
    ];

    public static BackendEvent Parse(string line)
    {
        if (string.IsNullOrWhiteSpace(line))
        {
            throw new BackendProtocolException("Backend emitted an empty JSONL event.");
        }

        RawEvent raw;
        try
        {
            raw = JsonSerializer.Deserialize<RawEvent>(line, BackendProtocol.JsonOptions)
                ?? throw new BackendProtocolException("Backend emitted a null event.");
        }
        catch (JsonException exception)
        {
            throw new BackendProtocolException("Backend emitted invalid JSONL.", exception);
        }

        ValidateEnvelope(raw);

        return raw.Type switch
        {
            "progress" => ParseProgress(raw),
            "snapshot" => ParseSnapshot(raw),
            "result" => ParseResult(raw),
            "error" => ParseError(raw),
            _ => throw new BackendProtocolException($"Backend emitted unknown event type '{raw.Type}'."),
        };
    }

    private static ProgressEvent ParseProgress(RawEvent raw)
    {
        RequireOnly(raw, allowPhase: true, allowMessage: true);
        return new ProgressEvent(raw.RequestId, raw.Sequence, Required(raw.Phase, "phase"), Required(raw.Message, "message"));
    }

    private static SnapshotEvent ParseSnapshot(RawEvent raw)
    {
        RequireOnly(raw, allowSnapshot: true);
        var snapshot = raw.Snapshot ?? throw Missing("snapshot");
        ValidateSnapshot(snapshot);
        return new SnapshotEvent(raw.RequestId, raw.Sequence, snapshot);
    }

    private static ResultEvent ParseResult(RawEvent raw)
    {
        RequireOnly(raw, allowMessage: true, allowSuccess: true, allowCloseGui: true, allowSnapshot: true);
        if (raw.Success is not true)
        {
            throw new BackendProtocolException("A result event must contain success=true; failures use an error event.");
        }

        if (raw.Snapshot is not null)
        {
            ValidateSnapshot(raw.Snapshot);
        }

        return new ResultEvent(
            raw.RequestId,
            raw.Sequence,
            Required(raw.Message, "message"),
            raw.CloseGui ?? false,
            raw.Snapshot);
    }

    private static ErrorEvent ParseError(RawEvent raw)
    {
        RequireOnly(raw, allowMessage: true, allowCode: true, allowDetails: true);
        return new ErrorEvent(
            raw.RequestId,
            raw.Sequence,
            Required(raw.Code, "code"),
            Required(raw.Message, "message"),
            raw.Details);
    }

    private static void ValidateEnvelope(RawEvent raw)
    {
        if (!string.Equals(raw.Protocol, BackendProtocol.Version, StringComparison.Ordinal))
        {
            throw new BackendProtocolException($"Unsupported backend protocol '{raw.Protocol}'.");
        }

        if (!Guid.TryParseExact(raw.RequestId, "D", out _))
        {
            throw new BackendProtocolException("Backend event requestId is not a canonical UUID.");
        }

        if (raw.Sequence < 1)
        {
            throw new BackendProtocolException("Backend event sequence must be positive.");
        }
    }

    private static void ValidateSnapshot(AgentSnapshot snapshot)
    {
        if (snapshot.Capture is null || snapshot.Watchdog is null || snapshot.LanEndpoints is null || snapshot.Tailscale is null)
        {
            throw new BackendProtocolException("Backend snapshot is incomplete.");
        }

        if (!snapshot.Installed && !string.IsNullOrEmpty(snapshot.Version))
        {
            throw new BackendProtocolException("A not-installed snapshot cannot report a version.");
        }

        if (snapshot.LanEndpoints.Any(string.IsNullOrWhiteSpace))
        {
            throw new BackendProtocolException("Backend snapshot contains an empty LAN endpoint.");
        }

        if (!TailscaleStatuses.Contains(snapshot.Tailscale.Status))
        {
            throw new BackendProtocolException($"Unknown Tailscale status '{snapshot.Tailscale.Status}'.");
        }
    }

    private static void RequireOnly(
        RawEvent raw,
        bool allowPhase = false,
        bool allowMessage = false,
        bool allowSnapshot = false,
        bool allowSuccess = false,
        bool allowCloseGui = false,
        bool allowCode = false,
        bool allowDetails = false)
    {
        Reject(raw.Phase, allowPhase, "phase");
        Reject(raw.Message, allowMessage, "message");
        Reject(raw.Snapshot, allowSnapshot, "snapshot");
        Reject(raw.Success, allowSuccess, "success");
        Reject(raw.CloseGui, allowCloseGui, "closeGui");
        Reject(raw.Code, allowCode, "code");
        Reject(raw.Details, allowDetails, "details");
    }

    private static void Reject(object? value, bool allowed, string property)
    {
        if (!allowed && value is not null)
        {
            throw new BackendProtocolException($"Event type contains forbidden property '{property}'.");
        }
    }

    private static string Required(string? value, string property)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            throw Missing(property);
        }

        return value;
    }

    private static BackendProtocolException Missing(string property) =>
        new($"Backend event is missing required property '{property}'.");

    private sealed class RawEvent
    {
        [JsonRequired]
        [JsonPropertyName("protocol")]
        public string Protocol { get; init; } = string.Empty;

        [JsonRequired]
        [JsonPropertyName("requestId")]
        public string RequestId { get; init; } = string.Empty;

        [JsonRequired]
        [JsonPropertyName("sequence")]
        public long Sequence { get; init; }

        [JsonRequired]
        [JsonPropertyName("type")]
        public string Type { get; init; } = string.Empty;

        [JsonPropertyName("phase")]
        public string? Phase { get; init; }

        [JsonPropertyName("message")]
        public string? Message { get; init; }

        [JsonPropertyName("snapshot")]
        public AgentSnapshot? Snapshot { get; init; }

        [JsonPropertyName("success")]
        public bool? Success { get; init; }

        [JsonPropertyName("closeGui")]
        public bool? CloseGui { get; init; }

        [JsonPropertyName("code")]
        public string? Code { get; init; }

        [JsonPropertyName("details")]
        public string? Details { get; init; }
    }
}

internal sealed class BackendProtocolException : Exception
{
    public BackendProtocolException(string message)
        : base(message)
    {
    }

    public BackendProtocolException(string message, Exception innerException)
        : base(message, innerException)
    {
    }
}
