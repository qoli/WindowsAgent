using System.Security.Cryptography;
using System.Text.Json;

namespace ScreenParser.DirectML;

public sealed class ContractException(string message, Exception? inner = null) : Exception(message, inner);

public sealed record SourceModel(
    string Repository,
    string Revision,
    string Filename,
    string Sha256,
    string License);

public sealed record OnnxModel(
    string ArtifactId,
    string Format,
    string Filename,
    string Sha256,
    string Precision,
    int Opset,
    string InputName,
    string OutputName,
    int InputWidth,
    int InputHeight,
    IReadOnlyList<string> Labels,
    SourceModel Source);

public sealed record InferenceSettings(
    double Confidence,
    double Iou,
    int MaxDetections,
    string Device);

public sealed record RuntimeConfig(
    string ModuleId,
    string TargetExecutable,
    OnnxModel Model,
    InferenceSettings Inference)
{
    public const string RuntimeId = "screenparser-onnx-dml-v1";

    public static RuntimeConfig Load(string path)
    {
        if (!Path.IsPathFullyQualified(path) || !File.Exists(path))
        {
            throw new ContractException($"config path must be an existing absolute file: {path}");
        }

        JsonDocument document;
        try
        {
            document = JsonDocument.Parse(
                File.ReadAllBytes(path),
                new JsonDocumentOptions { AllowTrailingCommas = false, CommentHandling = JsonCommentHandling.Disallow });
        }
        catch (Exception exception) when (exception is IOException or JsonException)
        {
            throw new ContractException($"config must be strict JSON: {exception.Message}", exception);
        }

        using (document)
        {
            var root = RequireObject(document.RootElement, "config");
            RequireProperties(root, "config", "schemaVersion", "moduleId", "kind", "runtime", "targetExecutable", "model", "inference");
            RequireEqual(RequireInt(root, "schemaVersion"), 1, "config.schemaVersion");
            RequireEqual(RequireString(root, "kind"), "action", "config.kind");
            RequireEqual(RequireString(root, "runtime"), RuntimeId, "config.runtime");

            var moduleId = RequireIdentifier(RequireString(root, "moduleId"), "config.moduleId");
            var targetExecutable = RequireString(root, "targetExecutable");
            if (Path.GetFileName(targetExecutable) != targetExecutable || !targetExecutable.EndsWith(".exe", StringComparison.OrdinalIgnoreCase))
            {
                throw new ContractException("config.targetExecutable must be one executable filename ending in .exe");
            }

            var model = ParseModel(RequireObject(root.GetProperty("model"), "config.model"));
            var inference = ParseInference(RequireObject(root.GetProperty("inference"), "config.inference"));
            return new RuntimeConfig(moduleId, targetExecutable, model, inference);
        }
    }

    public static void ValidateModel(string path, OnnxModel model)
    {
        if (!Path.IsPathFullyQualified(path) || !File.Exists(path))
        {
            throw new ContractException($"model path must be an existing absolute file: {path}");
        }
        var actual = Convert.ToHexString(SHA256.HashData(File.ReadAllBytes(path))).ToLowerInvariant();
        if (!StringComparer.Ordinal.Equals(actual, model.Sha256))
        {
            throw new ContractException($"model sha256 mismatch: expected={model.Sha256} actual={actual}");
        }
    }

    private static OnnxModel ParseModel(JsonElement value)
    {
        RequireProperties(value, "config.model", "artifactId", "format", "filename", "sha256", "precision", "opset", "inputName", "outputName", "inputWidth", "inputHeight", "labels", "source");
        var artifactId = RequireIdentifier(RequireString(value, "artifactId"), "config.model.artifactId");
        var format = RequireString(value, "format");
        RequireEqual(format, "onnx", "config.model.format");
        var filename = RequireString(value, "filename");
        if (Path.GetFileName(filename) != filename || !filename.EndsWith(".onnx", StringComparison.Ordinal))
        {
            throw new ContractException("config.model.filename must be one canonical .onnx filename");
        }
        var sha256 = RequireSha256(RequireString(value, "sha256"), "config.model.sha256");
        var precision = RequireString(value, "precision");
        RequireEqual(precision, "fp16", "config.model.precision");
        var opset = RequireIntRange(value, "opset", 12, 20, "config.model.opset");
        var inputName = RequireIdentifier(RequireString(value, "inputName"), "config.model.inputName", false);
        var outputName = RequireIdentifier(RequireString(value, "outputName"), "config.model.outputName", false);
        var inputWidth = RequireIntRange(value, "inputWidth", 320, 4096, "config.model.inputWidth");
        var inputHeight = RequireIntRange(value, "inputHeight", 320, 4096, "config.model.inputHeight");
        if (inputWidth != inputHeight || inputWidth % 32 != 0)
        {
            throw new ContractException("config.model input dimensions must be equal and divisible by 32");
        }

        var labelElement = value.GetProperty("labels");
        if (labelElement.ValueKind != JsonValueKind.Array || labelElement.GetArrayLength() is < 1 or > 512)
        {
            throw new ContractException("config.model.labels must be an array containing between 1 and 512 labels");
        }
        var labels = new List<string>(labelElement.GetArrayLength());
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (var element in labelElement.EnumerateArray())
        {
            if (element.ValueKind != JsonValueKind.String)
            {
                throw new ContractException("config.model.labels entries must be strings");
            }
            var label = CanonicalString(element.GetString(), "config.model.labels entry");
            if (!seen.Add(label))
            {
                throw new ContractException($"config.model.labels contains duplicate label: {label}");
            }
            labels.Add(label);
        }

        var sourceValue = RequireObject(value.GetProperty("source"), "config.model.source");
        RequireProperties(sourceValue, "config.model.source", "repository", "revision", "filename", "sha256", "license");
        var source = new SourceModel(
            RequireString(sourceValue, "repository"),
            RequireString(sourceValue, "revision"),
            RequireString(sourceValue, "filename"),
            RequireSha256(RequireString(sourceValue, "sha256"), "config.model.source.sha256"),
            RequireString(sourceValue, "license"));
        return new OnnxModel(artifactId, format, filename, sha256, precision, opset, inputName, outputName, inputWidth, inputHeight, labels, source);
    }

    private static InferenceSettings ParseInference(JsonElement value)
    {
        RequireProperties(value, "config.inference", "confidence", "iou", "maxDetections", "device");
        var settings = new InferenceSettings(
            RequireDoubleRange(value, "confidence", 0.001, 1.0, "config.inference.confidence"),
            RequireDoubleRange(value, "iou", 0.001, 1.0, "config.inference.iou"),
            RequireIntRange(value, "maxDetections", 1, 2000, "config.inference.maxDetections"),
            RequireString(value, "device"));
        RequireEqual(settings.Device, "directml:0", "config.inference.device");
        return settings;
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

    private static string RequireString(JsonElement value, string property) =>
        CanonicalString(value.GetProperty(property).ValueKind == JsonValueKind.String ? value.GetProperty(property).GetString() : null, $"{property}");

    private static string CanonicalString(string? value, string name)
    {
        if (string.IsNullOrEmpty(value) || value.Trim() != value || value.Length > 4096)
        {
            throw new ContractException($"{name} must be a non-empty canonical string");
        }
        return value;
    }

    private static string RequireIdentifier(string value, string name, bool structured = true)
    {
        if (value.Length > 256 || value.Any(character => !(char.IsAsciiLetterOrDigit(character) || "-_".Contains(character) || (structured && "/.".Contains(character)))))
        {
            throw new ContractException($"{name} contains unsupported characters or exceeds 256 characters");
        }
        return value;
    }

    private static string RequireSha256(string value, string name)
    {
        if (value.Length != 64 || value.Any(character => !char.IsAsciiHexDigit(character) || char.IsAsciiLetterUpper(character)))
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

    private static double RequireDoubleRange(JsonElement value, string property, double minimum, double maximum, string name)
    {
        if (!value.GetProperty(property).TryGetDouble(out var result) || !double.IsFinite(result) || result < minimum || result > maximum)
        {
            throw new ContractException($"{name} must be between {minimum} and {maximum}");
        }
        return result;
    }

    private static void RequireEqual<T>(T actual, T expected, string name) where T : notnull
    {
        if (!EqualityComparer<T>.Default.Equals(actual, expected))
        {
            throw new ContractException($"{name} must equal {expected}");
        }
    }
}
