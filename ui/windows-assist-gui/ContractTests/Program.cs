using WindowsAgent.AssistGui;

var hosts = HarnessSetupPrompt.Hosts(
    ["http://192.168.1.20:8787", "http://[fd7a:115c:a1e0::1]:8787"],
    "100.64.0.20",
    "fd7a:115c:a1e0::1");
Require(hosts.SequenceEqual(["192.168.1.20", "fd7a:115c:a1e0::1", "100.64.0.20"]), "host candidates");

var prompt = HarnessSetupPrompt.Build("100.64.0.20");
foreach (var required in new[]
{
    "Initialize WindowsAgent for this Harness.",
    "100.64.0.20",
    "https://github.com/qoli/WindowsAgent/releases/latest/download/windowsagent-user-skills.zip",
    "use-windows-pc Skill",
    "validate WindowsAgent health",
    "capture the current desktop",
    "normal read-only Windows control path",
    "Do not stop after describing setup steps",
})
{
    Require(prompt.Contains(required, StringComparison.Ordinal), $"prompt clause {required}");
}
foreach (var forbidden in new[] { "8787", "2022", "SFTP", "repository", "pc.env" })
{
    Require(!prompt.Contains(forbidden, StringComparison.OrdinalIgnoreCase), $"forbidden prompt detail {forbidden}");
}

static void Require(bool condition, string label)
{
    if (!condition)
    {
        throw new InvalidOperationException($"AssistGUI Harness setup contract failed: {label}");
    }
}
