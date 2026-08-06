using System.Globalization;
using System.Security.Cryptography;
using System.Text.Json;

namespace ScreenParser.DirectML;

public sealed record CapturedFrame(int Width, int Height, byte[] Rgb, string RgbSha256);

public sealed record FrameArtifact(
    string ArtifactId,
    DateTimeOffset CapturedAt,
    string RgbPath,
    string Sha256,
    int Width,
    int Height);

public sealed record PreprocessRequest(
    string RequestId,
    string TargetExecutable,
    FrameArtifact Frame)
{
    private const string TimestampFormat = "yyyy-MM-dd'T'HH:mm:ss.ffffff'Z'";

    public static PreprocessRequest Load(string path, string frameRoot, RuntimeConfig config)
    {
        RequireExistingAbsoluteFile(path, "request path");
        var canonicalFrameRoot = RequireExistingAbsoluteDirectory(frameRoot, "frame root");
        JsonDocument document;
        try
        {
            document = JsonDocument.Parse(
                File.ReadAllBytes(path),
                new JsonDocumentOptions { AllowTrailingCommas = false, CommentHandling = JsonCommentHandling.Disallow });
        }
        catch (Exception exception) when (exception is IOException or JsonException)
        {
            throw new ContractException($"request must be strict JSON: {exception.Message}", exception);
        }

        using (document)
        {
            var root = RequireObject(document.RootElement, "request");
            RequireProperties(root, "request", "schemaVersion", "requestId", "targetExecutable", "frame");
            RequireEqual(RequireInt(root, "schemaVersion"), 1, "request.schemaVersion");
            var requestId = RequireIdentifier(RequireString(root, "requestId"), "request.requestId");
            var targetExecutable = RequireString(root, "targetExecutable");
            RequireEqual(targetExecutable, config.TargetExecutable, "request.targetExecutable");
            var frame = ParseFrame(RequireObject(root.GetProperty("frame"), "request.frame"), canonicalFrameRoot);
            return new PreprocessRequest(requestId, targetExecutable, frame);
        }
    }

    public CapturedFrame ReadFrame()
    {
        var expectedBytes = checked((long)Frame.Width * Frame.Height * 3);
        var rgb = File.ReadAllBytes(Frame.RgbPath);
        var actualBytes = rgb.LongLength;
        if (actualBytes != expectedBytes)
        {
            throw new ContractException($"request.frame RGB byte length mismatch: expected={expectedBytes} actual={actualBytes}");
        }
        var actualHash = Convert.ToHexString(SHA256.HashData(rgb)).ToLowerInvariant();
        if (!StringComparer.Ordinal.Equals(actualHash, Frame.Sha256))
        {
            throw new ContractException($"request.frame sha256 mismatch: expected={Frame.Sha256} actual={actualHash}");
        }
        return new CapturedFrame(Frame.Width, Frame.Height, rgb, actualHash);
    }

    public static string FormatTimestamp(DateTimeOffset value) => value.UtcDateTime.ToString(TimestampFormat, CultureInfo.InvariantCulture);

    private static FrameArtifact ParseFrame(JsonElement value, string frameRoot)
    {
        RequireProperties(value, "request.frame", "artifactId", "capturedAt", "rgbPath", "sha256", "width", "height");
        var artifactId = RequireIdentifier(RequireString(value, "artifactId"), "request.frame.artifactId");
        var capturedAtValue = RequireString(value, "capturedAt");
        if (!DateTimeOffset.TryParseExact(capturedAtValue, TimestampFormat, CultureInfo.InvariantCulture, DateTimeStyles.AssumeUniversal, out var capturedAt) ||
            capturedAt.Offset != TimeSpan.Zero)
        {
            throw new ContractException($"request.frame.capturedAt must use {TimestampFormat}");
        }
        var rgbPath = RequireExistingAbsoluteFile(RequireString(value, "rgbPath"), "request.frame.rgbPath");
        var relative = Path.GetRelativePath(frameRoot, rgbPath);
        if (Path.IsPathRooted(relative) || relative == ".." || relative.StartsWith(".." + Path.DirectorySeparatorChar, StringComparison.Ordinal))
        {
            throw new ContractException("request.frame.rgbPath must be below the declared frame root");
        }
        return new FrameArtifact(
            artifactId,
            capturedAt,
            rgbPath,
            RequireSha256(RequireString(value, "sha256"), "request.frame.sha256"),
            RequireIntRange(value, "width", 1, 16384, "request.frame.width"),
            RequireIntRange(value, "height", 1, 16384, "request.frame.height"));
    }

    private static JsonElement RequireObject(JsonElement value, string name)
    {
        if (value.ValueKind != JsonValueKind.Object)
        {
            throw new ContractException($"{name} must be an object");
        }
        return value;
    }

    private static void RequireProperties(JsonElement value, string name, params string[] required)
    {
        var expected = required.ToHashSet(StringComparer.Ordinal);
        var actual = new HashSet<string>(StringComparer.Ordinal);
        foreach (var property in value.EnumerateObject())
        {
            if (!actual.Add(property.Name))
            {
                throw new ContractException($"{name} contains duplicate property: {property.Name}");
            }
        }
        var missing = expected.Except(actual, StringComparer.Ordinal).Order().ToArray();
        var unknown = actual.Except(expected, StringComparer.Ordinal).Order().ToArray();
        if (missing.Length > 0)
        {
            throw new ContractException($"{name} missing required fields: {string.Join(", ", missing)}");
        }
        if (unknown.Length > 0)
        {
            throw new ContractException($"{name} has unknown fields: {string.Join(", ", unknown)}");
        }
    }

    private static string RequireString(JsonElement value, string property)
    {
        var propertyValue = value.GetProperty(property);
        if (propertyValue.ValueKind != JsonValueKind.String || propertyValue.GetString() is not { } result ||
            result.Length is < 1 or > 4096 || result.Trim() != result)
        {
            throw new ContractException($"{property} must be a non-empty canonical string");
        }
        return result;
    }

    private static string RequireIdentifier(string value, string name)
    {
        if (value.Length > 256 || value.Any(character => !(char.IsAsciiLetterOrDigit(character) || "-_.:/".Contains(character))))
        {
            throw new ContractException($"{name} contains unsupported characters or exceeds 256 characters");
        }
        return value;
    }

    private static string RequireSha256(string value, string name)
    {
        if (value.Length != 64 || value.Any(character => !(char.IsAsciiDigit(character) || character is >= 'a' and <= 'f')))
        {
            throw new ContractException($"{name} must be 64 lowercase hexadecimal characters");
        }
        return value;
    }

    private static int RequireInt(JsonElement value, string property)
    {
        if (!value.GetProperty(property).TryGetInt32(out var result))
        {
            throw new ContractException($"{property} must be an integer");
        }
        return result;
    }

    private static int RequireIntRange(JsonElement value, string property, int minimum, int maximum, string name)
    {
        var result = RequireInt(value, property);
        if (result < minimum || result > maximum)
        {
            throw new ContractException($"{name} must be between {minimum} and {maximum}");
        }
        return result;
    }

    private static string RequireExistingAbsoluteFile(string path, string name)
    {
        if (!Path.IsPathFullyQualified(path) || !File.Exists(path))
        {
            throw new ContractException($"{name} must be an existing absolute file: {path}");
        }
        return Path.GetFullPath(path);
    }

    private static string RequireExistingAbsoluteDirectory(string path, string name)
    {
        if (!Path.IsPathFullyQualified(path) || !Directory.Exists(path))
        {
            throw new ContractException($"{name} must be an existing absolute directory: {path}");
        }
        return Path.GetFullPath(path);
    }

    private static void RequireEqual<T>(T actual, T expected, string name) where T : notnull
    {
        if (!EqualityComparer<T>.Default.Equals(actual, expected))
        {
            throw new ContractException($"{name} must equal {expected}");
        }
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
