namespace WindowsAgent.AssistGui;

internal static class HarnessSetupPrompt
{
    internal static IReadOnlyList<string> Hosts(
        IReadOnlyList<string> lanEndpoints,
        string? tailscaleIpv4,
        string? tailscaleIpv6)
    {
        var hosts = new List<string>();
        foreach (var endpoint in lanEndpoints)
        {
            if (Uri.TryCreate(endpoint, UriKind.Absolute, out var uri) &&
                uri.Scheme == Uri.UriSchemeHttp && !string.IsNullOrWhiteSpace(uri.Host))
            {
                AddHost(hosts, uri.Host.Trim('[', ']'));
            }
        }
        AddHost(hosts, tailscaleIpv4);
        AddHost(hosts, tailscaleIpv6);
        return hosts;
    }

    internal static string Build(string host) =>
        $"""
        Initialize WindowsAgent for this Harness.

        WindowsAgent host:
        {host}

        Install or locate the official WindowsAgent user-facing Skills from https://github.com/qoli/WindowsAgent/releases/latest/download/windowsagent-user-skills.zip if they are not already available. Create the local WindowsAgent PC configuration using this host. Then use the installed use-windows-pc Skill to validate WindowsAgent health, capture the current desktop, and verify the normal read-only Windows control path.

        Do not stop after describing setup steps. Complete the Harness initialization and report the acceptance result.
        """;

    private static void AddHost(List<string> hosts, string? candidate)
    {
        if (!string.IsNullOrWhiteSpace(candidate) && !hosts.Contains(candidate, StringComparer.OrdinalIgnoreCase))
        {
            hosts.Add(candidate);
        }
    }
}
