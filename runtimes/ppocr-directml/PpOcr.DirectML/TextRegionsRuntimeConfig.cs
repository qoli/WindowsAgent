using System.Text.Json;

namespace PpOcr.DirectML;

public sealed record DetectionModel(
    string ArtifactId,
    string Filename,
    string Sha256,
    string InputName,
    string OutputName);

public sealed record DetectionSettings(
    int InputWidth,
    double PixelThreshold,
    double BoxThreshold,
    int MaxCandidates,
    double UnclipRatio);

public sealed record TextRegionsRuntimeConfig(
    DetectionModel DetectionModel,
    RecognitionModel RecognitionModel,
    CharacterDictionary Characters,
    DetectionSettings Detection,
    string Device)
{
    public const string RuntimeId = "ppocr-onnx-dml-text-regions-v1";
    public const string Pipeline = "text-region-detection-recognition";

    public static TextRegionsRuntimeConfig Load(string path)
    {
        using var document = Contract.ReadStrictJson(path, "text regions config path");
        var root = Contract.RequireObject(document.RootElement, "config");
        Contract.RequireProperties(
            root, "config", "schemaVersion", "runtime", "pipeline", "detectionModel",
            "recognitionModel", "characters", "detection", "inference");
        RequireEqual(Contract.RequireInt(root, "schemaVersion"), 1, "config.schemaVersion");
        RequireEqual(Contract.RequireString(root, "runtime"), RuntimeId, "config.runtime");
        RequireEqual(Contract.RequireString(root, "pipeline"), Pipeline, "config.pipeline");

        var detectionModelValue = Contract.RequireObject(root.GetProperty("detectionModel"), "config.detectionModel");
        Contract.RequireProperties(
            detectionModelValue, "config.detectionModel", "artifactId", "format", "filename", "sha256",
            "opset", "inputName", "outputName");
        RequireEqual(Contract.RequireString(detectionModelValue, "format"), "onnx", "config.detectionModel.format");
        RequireEqual(Contract.RequireInt(detectionModelValue, "opset"), 14, "config.detectionModel.opset");
        var detectionModel = new DetectionModel(
            Contract.RequireIdentifier(Contract.RequireString(detectionModelValue, "artifactId"), "config.detectionModel.artifactId"),
            CanonicalFilename(Contract.RequireString(detectionModelValue, "filename"), ".onnx", "config.detectionModel.filename"),
            Contract.RequireSha256(Contract.RequireString(detectionModelValue, "sha256"), "config.detectionModel.sha256"),
            Contract.RequireIdentifier(Contract.RequireString(detectionModelValue, "inputName"), "config.detectionModel.inputName"),
            Contract.RequireIdentifier(Contract.RequireString(detectionModelValue, "outputName"), "config.detectionModel.outputName"));

        var recognitionModelValue = Contract.RequireObject(root.GetProperty("recognitionModel"), "config.recognitionModel");
        Contract.RequireProperties(
            recognitionModelValue, "config.recognitionModel", "artifactId", "format", "filename", "sha256",
            "opset", "inputName", "outputName", "inputHeight", "inputWidth", "classCount");
        RequireEqual(Contract.RequireString(recognitionModelValue, "format"), "onnx", "config.recognitionModel.format");
        RequireEqual(Contract.RequireInt(recognitionModelValue, "opset"), 11, "config.recognitionModel.opset");
        var recognitionModel = new RecognitionModel(
            Contract.RequireIdentifier(Contract.RequireString(recognitionModelValue, "artifactId"), "config.recognitionModel.artifactId"),
            CanonicalFilename(Contract.RequireString(recognitionModelValue, "filename"), ".onnx", "config.recognitionModel.filename"),
            Contract.RequireSha256(Contract.RequireString(recognitionModelValue, "sha256"), "config.recognitionModel.sha256"),
            Contract.RequireIdentifier(Contract.RequireString(recognitionModelValue, "inputName"), "config.recognitionModel.inputName"),
            Contract.RequireIdentifier(Contract.RequireString(recognitionModelValue, "outputName"), "config.recognitionModel.outputName"),
            Contract.RequireIntRange(recognitionModelValue, "inputHeight", 48, 48, "config.recognitionModel.inputHeight"),
            Contract.RequireIntRange(recognitionModelValue, "inputWidth", 480, 480, "config.recognitionModel.inputWidth"),
            Contract.RequireIntRange(recognitionModelValue, "classCount", 2, 100000, "config.recognitionModel.classCount"));

        var charactersValue = Contract.RequireObject(root.GetProperty("characters"), "config.characters");
        Contract.RequireProperties(charactersValue, "config.characters", "filename", "sha256", "count", "blankIndex");
        var characters = new CharacterDictionary(
            CanonicalFilename(Contract.RequireString(charactersValue, "filename"), ".json", "config.characters.filename"),
            Contract.RequireSha256(Contract.RequireString(charactersValue, "sha256"), "config.characters.sha256"),
            Contract.RequireIntRange(charactersValue, "count", 1, 99999, "config.characters.count"),
            Contract.RequireIntRange(charactersValue, "blankIndex", 0, 0, "config.characters.blankIndex"));
        if (recognitionModel.ClassCount != characters.Count + 1)
        {
            throw new ContractException("config.recognitionModel.classCount must equal config.characters.count plus the CTC blank class");
        }

        var detectionValue = Contract.RequireObject(root.GetProperty("detection"), "config.detection");
        Contract.RequireProperties(
            detectionValue, "config.detection", "inputWidth", "pixelThreshold", "boxThreshold",
            "maxCandidates", "unclipRatio");
        var detection = new DetectionSettings(
            Contract.RequireIntRange(detectionValue, "inputWidth", 32, 2048, "config.detection.inputWidth"),
            RequireDoubleRange(detectionValue, "pixelThreshold", 0.01, 0.99, "config.detection.pixelThreshold"),
            RequireDoubleRange(detectionValue, "boxThreshold", 0.01, 0.99, "config.detection.boxThreshold"),
            Contract.RequireIntRange(detectionValue, "maxCandidates", 1, 3000, "config.detection.maxCandidates"),
            RequireDoubleRange(detectionValue, "unclipRatio", 1.0, 4.0, "config.detection.unclipRatio"));
        if (detection.InputWidth % 32 != 0)
        {
            throw new ContractException("config.detection.inputWidth must be divisible by 32");
        }

        var inference = Contract.RequireObject(root.GetProperty("inference"), "config.inference");
        Contract.RequireProperties(inference, "config.inference", "device");
        var device = Contract.RequireString(inference, "device");
        RequireEqual(device, "directml:0", "config.inference.device");
        return new TextRegionsRuntimeConfig(detectionModel, recognitionModel, characters, detection, device);
    }

    public IReadOnlyList<string> ValidateArtifacts(string detectionModelPath, string recognitionModelPath, string charactersPath)
    {
        foreach (var (path, expectedName, expectedHash, label) in new[]
        {
            (detectionModelPath, DetectionModel.Filename, DetectionModel.Sha256, "detection model"),
            (recognitionModelPath, RecognitionModel.Filename, RecognitionModel.Sha256, "recognition model"),
            (charactersPath, Characters.Filename, Characters.Sha256, "characters"),
        })
        {
            Contract.RequireExistingAbsoluteFile(path, label + " path");
            RequireEqual(Path.GetFileName(path), expectedName, label + " filename");
            Contract.RequireFileHash(path, expectedHash, label);
        }
        using var document = Contract.ReadStrictJson(charactersPath, "characters path");
        if (document.RootElement.ValueKind != JsonValueKind.Array || document.RootElement.GetArrayLength() != Characters.Count)
        {
            throw new ContractException($"characters must be a JSON array containing exactly {Characters.Count} entries");
        }
        var result = new List<string>(Characters.Count);
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (var value in document.RootElement.EnumerateArray())
        {
            if (value.ValueKind != JsonValueKind.String)
            {
                throw new ContractException("characters entries must be strings");
            }
            var character = Contract.CanonicalString(value.GetString(), "characters entry", allowSpace: true);
            if (!seen.Add(character))
            {
                throw new ContractException($"characters contains duplicate entry: {character}");
            }
            result.Add(character);
        }
        return result;
    }

    private static string CanonicalFilename(string value, string extension, string name)
    {
        if (Path.GetFileName(value) != value || !value.EndsWith(extension, StringComparison.Ordinal))
        {
            throw new ContractException($"{name} must be one canonical {extension} filename");
        }
        return value;
    }

    private static double RequireDoubleRange(JsonElement value, string property, double minimum, double maximum, string name)
    {
        if (!value.GetProperty(property).TryGetDouble(out var result) || !double.IsFinite(result) || result < minimum || result > maximum)
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
