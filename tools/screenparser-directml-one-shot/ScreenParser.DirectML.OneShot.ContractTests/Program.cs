using ScreenParser.DirectML;
using ScreenParser.DirectML.OneShot;

var failures = new List<string>();

Run("valid fp16 diagnostic spec passes", () =>
{
    using var fixture = Fixture.Create("fp16");
    var spec = DiagnosticSpec.Load(fixture.SpecPath);
    DiagnosticSpec.ValidateArtifacts(spec);
    Equal("fp16", spec.Model.Precision);
    Equal(2, spec.Frame.Width);
    Equal(6L, new FileInfo(spec.Frame.RgbPath).Length);
});

Run("unknown properties are rejected", () =>
{
    using var fixture = Fixture.Create("fp32", ",\"fallbackPrecision\":\"fp32\"");
    Throws<ContractException>(() => DiagnosticSpec.Load(fixture.SpecPath), "unknown fields: fallbackPrecision");
});

Run("unsupported precision is rejected", () =>
{
    using var fixture = Fixture.Create("fp8");
    Throws<ContractException>(() => DiagnosticSpec.Load(fixture.SpecPath), "must equal fp32, fp16, or int8");
});

Run("model digest mismatch is rejected", () =>
{
    using var fixture = Fixture.Create("fp16");
    File.AppendAllText(fixture.ModelPath, "changed");
    var spec = DiagnosticSpec.Load(fixture.SpecPath);
    Throws<ContractException>(() => DiagnosticSpec.ValidateArtifacts(spec), "model sha256 mismatch");
});

Run("RGB byte length mismatch is rejected", () =>
{
    using var fixture = Fixture.Create("fp16");
    File.WriteAllBytes(fixture.RgbPath, [0, 1, 2]);
    fixture.RewriteRgbHash();
    var spec = DiagnosticSpec.Load(fixture.SpecPath);
    Throws<ContractException>(() => DiagnosticSpec.ValidateArtifacts(spec), "frame RGB byte length mismatch");
});

if (failures.Count > 0)
{
    Console.Error.WriteLine(string.Join(Environment.NewLine, failures));
    return 1;
}
Console.WriteLine("5 ScreenParser DirectML one-shot contract tests passed");
return 0;

void Run(string name, Action test)
{
    try
    {
        test();
        Console.WriteLine($"PASS {name}");
    }
    catch (Exception exception)
    {
        failures.Add($"FAIL {name}: {exception.Message}");
    }
}

static void Equal<T>(T expected, T actual) where T : notnull
{
    if (!EqualityComparer<T>.Default.Equals(expected, actual))
    {
        throw new Exception($"expected={expected} actual={actual}");
    }
}

static void Throws<T>(Action action, string message) where T : Exception
{
    try
    {
        action();
    }
    catch (T exception) when (exception.Message.Contains(message, StringComparison.Ordinal))
    {
        return;
    }
    throw new Exception($"expected {typeof(T).Name} containing {message}");
}

sealed class Fixture : IDisposable
{
    private readonly string _precision;
    private readonly string _rootInjection;

    private Fixture(string root, string precision, string rootInjection)
    {
        Root = root;
        _precision = precision;
        _rootInjection = rootInjection;
        ModelPath = Path.Combine(root, "model.onnx");
        RgbPath = Path.Combine(root, "frame.rgb");
        SpecPath = Path.Combine(root, "spec.json");
    }

    public string Root { get; }
    public string ModelPath { get; }
    public string RgbPath { get; }
    public string SpecPath { get; }

    public static Fixture Create(string precision, string rootInjection = "")
    {
        var root = Path.Combine(Path.GetTempPath(), "screenparser-one-shot-tests", Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(root);
        var fixture = new Fixture(root, precision, rootInjection);
        File.WriteAllText(fixture.ModelPath, "model");
        File.WriteAllBytes(fixture.RgbPath, [0, 1, 2, 3, 4, 5]);
        fixture.WriteSpec();
        return fixture;
    }

    public void RewriteRgbHash() => WriteSpec();

    private void WriteSpec()
    {
        var modelHash = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(File.ReadAllBytes(ModelPath))).ToLowerInvariant();
        var rgbHash = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(File.ReadAllBytes(RgbPath))).ToLowerInvariant();
        File.WriteAllText(SpecPath, $$"""
        {
          "schemaVersion": 1,
          "model": {
            "artifactId": "screenparser-test",
            "filename": {{System.Text.Json.JsonSerializer.Serialize(ModelPath)}},
            "sha256": "{{modelHash}}",
            "precision": "{{_precision}}",
            "opset": 20,
            "inputName": "images",
            "outputName": "output0",
            "inputWidth": 1280,
            "inputHeight": 1280,
            "labels": ["button", "text"]
          },
          "frame": {
            "rgbPath": {{System.Text.Json.JsonSerializer.Serialize(RgbPath)}},
            "sha256": "{{rgbHash}}",
            "width": 2,
            "height": 1
          },
          "inference": {
            "confidence": 0.1,
            "iou": 0.1,
            "maxDetections": 700,
            "warmupRuns": 1,
            "measuredRuns": 3
          }{{_rootInjection}}
        }
        """);
    }

    public void Dispose() => Directory.Delete(Root, true);
}
