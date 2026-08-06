using System.Security.Cryptography;
using System.Text.Json;
using ScreenParser.DirectML;

namespace ScreenParser.DirectML.OneShot;

public sealed record DiagnosticModel(
    string ArtifactId,
    string Filename,
    string Sha256,
    string Precision,
    int Opset,
    string InputName,
    string OutputName,
    int InputWidth,
    int InputHeight,
    IReadOnlyList<string> Labels);

public sealed record DiagnosticFrame(string RgbPath, string Sha256, int Width, int Height);

public sealed record DiagnosticInference(double Confidence, double Iou, int MaxDetections, int WarmupRuns, int MeasuredRuns);

public sealed record DiagnosticSpec(DiagnosticModel Model, DiagnosticFrame Frame, DiagnosticInference Inference)
{
    public static DiagnosticSpec Load(string path)
    {
        RequireExistingAbsoluteFile(path, "spec path");
        JsonDocument document;
        try
        {
            document = JsonDocument.Parse(
                File.ReadAllBytes(path),
                new JsonDocumentOptions { AllowTrailingCommas = false, CommentHandling = JsonCommentHandling.Disallow });
        }
        catch (Exception exception) when (exception is IOException or JsonException)
        {
            throw new ContractException($"spec must be strict JSON: {exception.Message}", exception);
        }
        using (document)
        {
            var root = RequireObject(document.RootElement, "spec");
            RequireProperties(root, "spec", "schemaVersion", "model", "frame", "inference");
            RequireEqual(RequireInt(root, "schemaVersion"), 1, "spec.schemaVersion");
            return new DiagnosticSpec(
                ParseModel(RequireObject(root.GetProperty("model"), "spec.model")),
                ParseFrame(RequireObject(root.GetProperty("frame"), "spec.frame")),
                ParseInference(RequireObject(root.GetProperty("inference"), "spec.inference")));
        }
    }

    public static void ValidateArtifacts(DiagnosticSpec spec)
    {
        RequireExistingAbsoluteFile(spec.Model.Filename, "model filename");
        RequireExistingAbsoluteFile(spec.Frame.RgbPath, "frame RGB path");
        RequireDigest(spec.Model.Filename, spec.Model.Sha256, "model");
        RequireDigest(spec.Frame.RgbPath, spec.Frame.Sha256, "frame RGB");
        var expectedBytes = checked((long)spec.Frame.Width * spec.Frame.Height * 3);
        var actualBytes = new FileInfo(spec.Frame.RgbPath).Length;
        if (actualBytes != expectedBytes)
        {
            throw new ContractException($"frame RGB byte length mismatch: expected={expectedBytes} actual={actualBytes}");
        }
    }

    private static DiagnosticModel ParseModel(JsonElement value)
    {
        RequireProperties(value, "spec.model", "artifactId", "filename", "sha256", "precision", "opset", "inputName", "outputName", "inputWidth", "inputHeight", "labels");
        var precision = RequireString(value, "precision");
        if (precision is not ("fp32" or "fp16" or "int8"))
        {
            throw new ContractException("spec.model.precision must equal fp32, fp16, or int8");
        }
        var labelsValue = value.GetProperty("labels");
        if (labelsValue.ValueKind != JsonValueKind.Array || labelsValue.GetArrayLength() is < 1 or > 512)
        {
            throw new ContractException("spec.model.labels must contain between 1 and 512 labels");
        }
        var labels = new List<string>();
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (var item in labelsValue.EnumerateArray())
        {
            if (item.ValueKind != JsonValueKind.String)
            {
                throw new ContractException("spec.model.labels entries must be strings");
            }
            var label = Canonical(item.GetString(), "spec.model.labels entry");
            if (!seen.Add(label))
            {
                throw new ContractException($"spec.model.labels contains duplicate label: {label}");
            }
            labels.Add(label);
        }
        var inputWidth = RequireIntRange(value, "inputWidth", 320, 4096, "spec.model.inputWidth");
        var inputHeight = RequireIntRange(value, "inputHeight", 320, 4096, "spec.model.inputHeight");
        if (inputWidth != inputHeight || inputWidth % 32 != 0)
        {
            throw new ContractException("spec.model input dimensions must be equal and divisible by 32");
        }
        return new DiagnosticModel(
            RequireIdentifier(RequireString(value, "artifactId"), "spec.model.artifactId"),
            RequireString(value, "filename"),
            RequireSha256(RequireString(value, "sha256"), "spec.model.sha256"),
            precision,
            RequireIntRange(value, "opset", 12, 20, "spec.model.opset"),
            RequireIdentifier(RequireString(value, "inputName"), "spec.model.inputName", false),
            RequireIdentifier(RequireString(value, "outputName"), "spec.model.outputName", false),
            inputWidth,
            inputHeight,
            labels);
    }

    private static DiagnosticFrame ParseFrame(JsonElement value)
    {
        RequireProperties(value, "spec.frame", "rgbPath", "sha256", "width", "height");
        return new DiagnosticFrame(
            RequireString(value, "rgbPath"),
            RequireSha256(RequireString(value, "sha256"), "spec.frame.sha256"),
            RequireIntRange(value, "width", 1, 16384, "spec.frame.width"),
            RequireIntRange(value, "height", 1, 16384, "spec.frame.height"));
    }

    private static DiagnosticInference ParseInference(JsonElement value)
    {
        RequireProperties(value, "spec.inference", "confidence", "iou", "maxDetections", "warmupRuns", "measuredRuns");
        return new DiagnosticInference(
            RequireDoubleRange(value, "confidence", 0.001, 1.0, "spec.inference.confidence"),
            RequireDoubleRange(value, "iou", 0.001, 1.0, "spec.inference.iou"),
            RequireIntRange(value, "maxDetections", 1, 2000, "spec.inference.maxDetections"),
            RequireIntRange(value, "warmupRuns", 0, 3, "spec.inference.warmupRuns"),
            RequireIntRange(value, "measuredRuns", 1, 10, "spec.inference.measuredRuns"));
    }

    private static void RequireDigest(string path, string expected, string name)
    {
        var actual = Convert.ToHexString(SHA256.HashData(File.ReadAllBytes(path))).ToLowerInvariant();
        if (!StringComparer.Ordinal.Equals(actual, expected))
        {
            throw new ContractException($"{name} sha256 mismatch: expected={expected} actual={actual}");
        }
    }

    private static void RequireExistingAbsoluteFile(string path, string name)
    {
        if (!Path.IsPathFullyQualified(path) || !File.Exists(path))
        {
            throw new ContractException($"{name} must be an existing absolute file: {path}");
        }
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
        Canonical(value.GetProperty(property).ValueKind == JsonValueKind.String ? value.GetProperty(property).GetString() : null, property);

    private static string Canonical(string? value, string name)
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
        if (value.Length != 64 || value.Any(character => !(char.IsAsciiDigit(character) || character is >= 'a' and <= 'f')))
        {
            throw new ContractException($"{name} must contain 64 lowercase hexadecimal characters");
        }
        return value;
    }

    private static int RequireInt(JsonElement value, string property)
    {
        if (!value.TryGetProperty(property, out var element) || !element.TryGetInt32(out var result))
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
        if (!value.TryGetProperty(property, out var element) || !element.TryGetDouble(out var result) || !double.IsFinite(result) || result < minimum || result > maximum)
        {
            throw new ContractException($"{name} must be a finite number between {minimum} and {maximum}");
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
