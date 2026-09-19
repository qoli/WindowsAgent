using System.Text.Json;
using System.Text.Json.Serialization;

namespace WindowsAgent.AssistGui.Backend;

internal static class BackendProtocol
{
    public const string Version = "windows-assist-v1";

    public static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web)
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        UnmappedMemberHandling = JsonUnmappedMemberHandling.Disallow,
        PropertyNameCaseInsensitive = false,
    };
}

internal static class BackendCommands
{
    public const string Inspect = "inspect";
    public const string Install = "install";
    public const string Update = "update";
    public const string Repair = "repair";
    public const string Uninstall = "uninstall";
    public const string Start = "start";
    public const string Stop = "stop";
    public const string ConfigureWatchdog = "configure-watchdog";
    public const string StartTailscale = "start-tailscale";
    public const string StopTailscale = "stop-tailscale";

    private static readonly HashSet<string> Known =
    [
        Inspect,
        Install,
        Update,
        Repair,
        Uninstall,
        Start,
        Stop,
        ConfigureWatchdog,
        StartTailscale,
        StopTailscale,
    ];

    public static bool IsKnown(string command) => Known.Contains(command);
}

internal sealed record BackendRequest(
    [property: JsonPropertyName("protocol")] string Protocol,
    [property: JsonPropertyName("requestId")] string RequestId,
    [property: JsonPropertyName("command")] string Command,
    [property: JsonPropertyName("clientProcessId")] int ClientProcessId,
    [property: JsonPropertyName("settings")] BackendSettings? Settings)
{
    public static BackendRequest Create(string command, BackendSettings? settings = null)
    {
        if (!BackendCommands.IsKnown(command))
        {
            throw new ArgumentOutOfRangeException(nameof(command), command, "Unknown backend command.");
        }

        return new BackendRequest(
            BackendProtocol.Version,
            Guid.NewGuid().ToString("D"),
            command,
            Environment.ProcessId,
            settings);
    }
}

internal sealed record BackendSettings(
    [property: JsonPropertyName("watchdogStartAtSignIn")] bool? WatchdogStartAtSignIn = null,
    [property: JsonPropertyName("tailscaleAuthKey")] string? TailscaleAuthKey = null);

internal abstract record BackendEvent(string RequestId, long Sequence);

internal sealed record ProgressEvent(string RequestId, long Sequence, string Phase, string Message)
    : BackendEvent(RequestId, Sequence);

internal sealed record SnapshotEvent(string RequestId, long Sequence, AgentSnapshot Snapshot)
    : BackendEvent(RequestId, Sequence);

internal sealed record ResultEvent(
    string RequestId,
    long Sequence,
    string Message,
    bool CloseGui,
    AgentSnapshot? Snapshot)
    : BackendEvent(RequestId, Sequence);

internal sealed record ErrorEvent(string RequestId, long Sequence, string Code, string Message, string? Details)
    : BackendEvent(RequestId, Sequence);

internal sealed class AgentSnapshot
{
    [JsonRequired]
    [JsonPropertyName("installed")]
    public bool Installed { get; init; }

    [JsonRequired]
    [JsonPropertyName("version")]
    public string? Version { get; init; }

    [JsonRequired]
    [JsonPropertyName("capture")]
    public ProcessSnapshot Capture { get; init; } = null!;

    [JsonRequired]
    [JsonPropertyName("watchdog")]
    public WatchdogSnapshot Watchdog { get; init; } = null!;

    [JsonRequired]
    [JsonPropertyName("lanEndpoints")]
    public IReadOnlyList<string> LanEndpoints { get; init; } = null!;

    [JsonRequired]
    [JsonPropertyName("tailscale")]
    public TailscaleSnapshot Tailscale { get; init; } = null!;
}

internal sealed class ProcessSnapshot
{
    [JsonRequired]
    [JsonPropertyName("running")]
    public bool Running { get; init; }
}

internal sealed class WatchdogSnapshot
{
    [JsonRequired]
    [JsonPropertyName("installed")]
    public bool Installed { get; init; }

    [JsonRequired]
    [JsonPropertyName("running")]
    public bool Running { get; init; }

    [JsonRequired]
    [JsonPropertyName("startAtSignIn")]
    public bool StartAtSignIn { get; init; }
}

internal sealed class TailscaleSnapshot
{
    [JsonRequired]
    [JsonPropertyName("status")]
    public string Status { get; init; } = string.Empty;

    [JsonRequired]
    [JsonPropertyName("ipv4")]
    public string? Ipv4 { get; init; }

    [JsonRequired]
    [JsonPropertyName("ipv6")]
    public string? Ipv6 { get; init; }
}
