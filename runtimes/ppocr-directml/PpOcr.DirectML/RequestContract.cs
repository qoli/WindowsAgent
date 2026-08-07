using System.Security.Cryptography;
using System.Text.Json;

namespace PpOcr.DirectML;

public sealed record CapturedRegion(int Width, int Height, byte[] Rgb, string RgbSha256);

public sealed record RegionArtifact(
    string ArtifactId,
    DateTimeOffset CapturedAt,
    string RgbPath,
    string Sha256,
    int Width,
    int Height);

public sealed record RecognitionRequest(string RequestId, RegionArtifact Region)
{
    public static RecognitionRequest Load(string path, string frameRoot)
    {
        var canonicalRoot = Contract.RequireExistingAbsoluteDirectory(frameRoot, "frame root");
        using var document = Contract.ReadStrictJson(path, "request path");
        var root = Contract.RequireObject(document.RootElement, "request");
        Contract.RequireProperties(root, "request", "schemaVersion", "requestId", "region");
        if (Contract.RequireInt(root, "schemaVersion") != 1)
        {
            throw new ContractException("request.schemaVersion must equal 1");
        }
        var requestId = Contract.RequireIdentifier(Contract.RequireString(root, "requestId"), "request.requestId");
        var value = Contract.RequireObject(root.GetProperty("region"), "request.region");
        Contract.RequireProperties(value, "request.region", "artifactId", "capturedAt", "rgbPath", "sha256", "width", "height");
        var rgbPath = Contract.RequireExistingAbsoluteFile(Contract.RequireString(value, "rgbPath"), "request.region.rgbPath");
        var relative = Path.GetRelativePath(canonicalRoot, rgbPath);
        if (Path.IsPathRooted(relative) || relative == ".." || relative.StartsWith(".." + Path.DirectorySeparatorChar, StringComparison.Ordinal))
        {
            throw new ContractException("request.region.rgbPath must be below the declared frame root");
        }
        var region = new RegionArtifact(
            Contract.RequireIdentifier(Contract.RequireString(value, "artifactId"), "request.region.artifactId"),
            Contract.RequireTimestamp(Contract.RequireString(value, "capturedAt"), "request.region.capturedAt"),
            rgbPath,
            Contract.RequireSha256(Contract.RequireString(value, "sha256"), "request.region.sha256"),
            Contract.RequireIntRange(value, "width", 1, 4096, "request.region.width"),
            Contract.RequireIntRange(value, "height", 1, 1024, "request.region.height"));
        return new RecognitionRequest(requestId, region);
    }

    public CapturedRegion ReadRegion()
    {
        var expected = checked((long)Region.Width * Region.Height * 3);
        var rgb = File.ReadAllBytes(Region.RgbPath);
        if (rgb.LongLength != expected)
        {
            throw new ContractException($"request.region RGB byte length mismatch: expected={expected} actual={rgb.LongLength}");
        }
        var actual = Convert.ToHexString(SHA256.HashData(rgb)).ToLowerInvariant();
        if (!StringComparer.Ordinal.Equals(actual, Region.Sha256))
        {
            throw new ContractException($"request.region sha256 mismatch: expected={Region.Sha256} actual={actual}");
        }
        return new CapturedRegion(Region.Width, Region.Height, rgb, actual);
    }
}

public static class ResponseWriter
{
    public static void WriteNewAtomic(string path, object response)
    {
        if (!Path.IsPathFullyQualified(path))
        {
            throw new ContractException($"response path must be absolute: {path}");
        }
        var canonical = Path.GetFullPath(path);
        var parent = Path.GetDirectoryName(canonical);
        if (string.IsNullOrEmpty(parent) || !Directory.Exists(parent))
        {
            throw new ContractException($"response parent must be an existing directory: {parent}");
        }
        if (File.Exists(canonical))
        {
            throw new ContractException($"response path must not already exist: {canonical}");
        }
        var temporary = canonical + ".tmp-" + Guid.NewGuid().ToString("N");
        try
        {
            File.WriteAllBytes(temporary, JsonSerializer.SerializeToUtf8Bytes(response));
            File.Move(temporary, canonical, false);
        }
        finally
        {
            if (File.Exists(temporary))
            {
                File.Delete(temporary);
            }
        }
    }
}
