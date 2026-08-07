using System.Text.Json;

namespace PpOcr.DirectML;

public sealed record RecognitionModel(
    string ArtifactId,
    string Filename,
    string Sha256,
    string InputName,
    string OutputName,
    int InputHeight,
    int InputWidth,
    int ClassCount);

public sealed record CharacterDictionary(string Filename, string Sha256, int Count, int BlankIndex);

public sealed record RuntimeConfig(RecognitionModel Model, CharacterDictionary Characters, string Device)
{
    public const string RuntimeId = "ppocr-onnx-dml-v1";
    public const string Pipeline = "text-line-recognition";

    public static RuntimeConfig Load(string path)
    {
        using var document = Contract.ReadStrictJson(path, "config path");
        var root = Contract.RequireObject(document.RootElement, "config");
        Contract.RequireProperties(root, "config", "schemaVersion", "runtime", "pipeline", "model", "characters", "inference");
        RequireEqual(Contract.RequireInt(root, "schemaVersion"), 1, "config.schemaVersion");
        RequireEqual(Contract.RequireString(root, "runtime"), RuntimeId, "config.runtime");
        RequireEqual(Contract.RequireString(root, "pipeline"), Pipeline, "config.pipeline");

        var modelValue = Contract.RequireObject(root.GetProperty("model"), "config.model");
        Contract.RequireProperties(
            modelValue, "config.model", "artifactId", "format", "filename", "sha256", "opset",
            "inputName", "outputName", "inputHeight", "inputWidth", "classCount");
        RequireEqual(Contract.RequireString(modelValue, "format"), "onnx", "config.model.format");
        RequireEqual(Contract.RequireInt(modelValue, "opset"), 11, "config.model.opset");
        var model = new RecognitionModel(
            Contract.RequireIdentifier(Contract.RequireString(modelValue, "artifactId"), "config.model.artifactId"),
            CanonicalFilename(Contract.RequireString(modelValue, "filename"), ".onnx", "config.model.filename"),
            Contract.RequireSha256(Contract.RequireString(modelValue, "sha256"), "config.model.sha256"),
            Contract.RequireIdentifier(Contract.RequireString(modelValue, "inputName"), "config.model.inputName"),
            Contract.RequireIdentifier(Contract.RequireString(modelValue, "outputName"), "config.model.outputName"),
            Contract.RequireIntRange(modelValue, "inputHeight", 16, 512, "config.model.inputHeight"),
            Contract.RequireIntRange(modelValue, "inputWidth", 16, 3200, "config.model.inputWidth"),
            Contract.RequireIntRange(modelValue, "classCount", 2, 100000, "config.model.classCount"));
        RequireEqual(model.InputHeight, 48, "config.model.inputHeight");

        var charactersValue = Contract.RequireObject(root.GetProperty("characters"), "config.characters");
        Contract.RequireProperties(charactersValue, "config.characters", "filename", "sha256", "count", "blankIndex");
        var characters = new CharacterDictionary(
            CanonicalFilename(Contract.RequireString(charactersValue, "filename"), ".json", "config.characters.filename"),
            Contract.RequireSha256(Contract.RequireString(charactersValue, "sha256"), "config.characters.sha256"),
            Contract.RequireIntRange(charactersValue, "count", 1, 99999, "config.characters.count"),
            Contract.RequireIntRange(charactersValue, "blankIndex", 0, 0, "config.characters.blankIndex"));
        if (model.ClassCount != characters.Count + 1)
        {
            throw new ContractException("config.model.classCount must equal config.characters.count plus the CTC blank class");
        }

        var inference = Contract.RequireObject(root.GetProperty("inference"), "config.inference");
        Contract.RequireProperties(inference, "config.inference", "device");
        var device = Contract.RequireString(inference, "device");
        RequireEqual(device, "directml:0", "config.inference.device");
        return new RuntimeConfig(model, characters, device);
    }

    public IReadOnlyList<string> ValidateArtifacts(string modelPath, string charactersPath)
    {
        Contract.RequireExistingAbsoluteFile(modelPath, "model path");
        Contract.RequireExistingAbsoluteFile(charactersPath, "characters path");
        RequireEqual(Path.GetFileName(modelPath), Model.Filename, "model filename");
        RequireEqual(Path.GetFileName(charactersPath), Characters.Filename, "characters filename");
        Contract.RequireFileHash(modelPath, Model.Sha256, "model");
        Contract.RequireFileHash(charactersPath, Characters.Sha256, "characters");
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

    private static void RequireEqual<T>(T actual, T expected, string name) where T : notnull
    {
        if (!EqualityComparer<T>.Default.Equals(actual, expected))
        {
            throw new ContractException($"{name} must equal {expected}");
        }
    }
}
