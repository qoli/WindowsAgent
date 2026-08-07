using System.Globalization;
using System.Security.Cryptography;
using System.Text.Json;

namespace PpOcr.DirectML;

public sealed class ContractException(string message, Exception? inner = null) : Exception(message, inner);

internal static class Contract
{
    public const string TimestampFormat = "yyyy-MM-dd'T'HH:mm:ss.ffffff'Z'";

    public static JsonDocument ReadStrictJson(string path, string label)
    {
        RequireExistingAbsoluteFile(path, label);
        try
        {
            return JsonDocument.Parse(
                File.ReadAllBytes(path),
                new JsonDocumentOptions { AllowTrailingCommas = false, CommentHandling = JsonCommentHandling.Disallow });
        }
        catch (Exception exception) when (exception is IOException or JsonException)
        {
            throw new ContractException($"{label} must be strict JSON: {exception.Message}", exception);
        }
    }

    public static JsonElement RequireObject(JsonElement value, string name)
    {
        if (value.ValueKind != JsonValueKind.Object)
        {
            throw new ContractException($"{name} must be an object");
        }
        return value;
    }

    public static void RequireProperties(JsonElement value, string name, params string[] required)
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

    public static string RequireString(JsonElement value, string property) =>
        CanonicalString(
            value.GetProperty(property).ValueKind == JsonValueKind.String ? value.GetProperty(property).GetString() : null,
            property);

    public static string CanonicalString(string? value, string name, bool allowSpace = false)
    {
        if (string.IsNullOrEmpty(value) || (!allowSpace && value.Trim() != value) || value.Length > 4096)
        {
            throw new ContractException($"{name} must be a non-empty canonical string");
        }
        return value;
    }

    public static string RequireIdentifier(string value, string name)
    {
        if (value.Length > 256 || value.Any(character => !(char.IsAsciiLetterOrDigit(character) || "-_.:/".Contains(character))))
        {
            throw new ContractException($"{name} contains unsupported characters or exceeds 256 characters");
        }
        return value;
    }

    public static string RequireSha256(string value, string name)
    {
        if (value.Length != 64 || value.Any(character => !(char.IsAsciiDigit(character) || character is >= 'a' and <= 'f')))
        {
            throw new ContractException($"{name} must be 64 lowercase hexadecimal characters");
        }
        return value;
    }

    public static int RequireInt(JsonElement value, string property)
    {
        if (!value.GetProperty(property).TryGetInt32(out var result))
        {
            throw new ContractException($"{property} must be an integer");
        }
        return result;
    }

    public static int RequireIntRange(JsonElement value, string property, int minimum, int maximum, string name)
    {
        var result = RequireInt(value, property);
        if (result < minimum || result > maximum)
        {
            throw new ContractException($"{name} must be between {minimum} and {maximum}");
        }
        return result;
    }

    public static string RequireExistingAbsoluteFile(string path, string name)
    {
        if (!Path.IsPathFullyQualified(path) || !File.Exists(path))
        {
            throw new ContractException($"{name} must be an existing absolute file: {path}");
        }
        return Path.GetFullPath(path);
    }

    public static string RequireExistingAbsoluteDirectory(string path, string name)
    {
        if (!Path.IsPathFullyQualified(path) || !Directory.Exists(path))
        {
            throw new ContractException($"{name} must be an existing absolute directory: {path}");
        }
        return Path.GetFullPath(path);
    }

    public static void RequireFileHash(string path, string expected, string name)
    {
        var actual = Convert.ToHexString(SHA256.HashData(File.ReadAllBytes(path))).ToLowerInvariant();
        if (!StringComparer.Ordinal.Equals(actual, expected))
        {
            throw new ContractException($"{name} sha256 mismatch: expected={expected} actual={actual}");
        }
    }

    public static DateTimeOffset RequireTimestamp(string value, string name)
    {
        if (!DateTimeOffset.TryParseExact(value, TimestampFormat, CultureInfo.InvariantCulture, DateTimeStyles.AssumeUniversal, out var result) ||
            result.Offset != TimeSpan.Zero)
        {
            throw new ContractException($"{name} must use {TimestampFormat}");
        }
        return result;
    }

    public static string FormatTimestamp(DateTimeOffset value) =>
        value.UtcDateTime.ToString(TimestampFormat, CultureInfo.InvariantCulture);
}
