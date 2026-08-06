using Microsoft.ML.OnnxRuntime.Tensors;
using ScreenParser.DirectML;

var failures = new List<string>();

Run("strict manifest parses", () =>
{
    using var fixture = ManifestFixture.Create();
    var config = RuntimeConfig.Load(fixture.ManifestPath);
    Equal("directml:0", config.Inference.Device);
    Equal(RuntimeConfig.RuntimeId, "screenparser-onnx-dml-v1");
});

Run("unknown fallback is rejected", () =>
{
    using var fixture = ManifestFixture.Create("\n  ,\"fallbackDevice\":\"cpu\"");
    Throws<ContractException>(() => RuntimeConfig.Load(fixture.ManifestPath), "unknown fields: fallbackDevice");
});

Run("CPU device is rejected", () =>
{
    using var fixture = ManifestFixture.Create().Replace("directml:0", "cpu");
    Throws<ContractException>(() => RuntimeConfig.Load(fixture.ManifestPath), "must equal directml:0");
});

Run("legacy loop contract is rejected", () =>
{
    using var fixture = ManifestFixture.Create().Replace("\"inference\":", "\"loop\":");
    Throws<ContractException>(() => RuntimeConfig.Load(fixture.ManifestPath), "missing required fields: inference");
});

Run("FP32 model precision is rejected", () =>
{
    using var fixture = ManifestFixture.Create().Replace("\"precision\": \"fp16\"", "\"precision\": \"fp32\"");
    Throws<ContractException>(() => RuntimeConfig.Load(fixture.ManifestPath), "config.model.precision must equal fp16");
});

Run("duplicate JSON key is rejected", () =>
{
    using var fixture = ManifestFixture.Create("\n  ,\"schemaVersion\":1");
    Throws<ContractException>(() => RuntimeConfig.Load(fixture.ManifestPath), "duplicate property: schemaVersion");
});

Run("model digest mismatch is rejected", () =>
{
    using var fixture = ManifestFixture.Create();
    var config = RuntimeConfig.Load(fixture.ManifestPath);
    File.WriteAllText(fixture.ModelPath, "wrong artifact");
    Throws<ContractException>(() => RuntimeConfig.ValidateModel(fixture.ModelPath, config.Model), "model sha256 mismatch");
});

Run("valid on-demand frame request parses", () =>
{
    using var fixture = ManifestFixture.Create();
    var config = RuntimeConfig.Load(fixture.ManifestPath);
    var request = PreprocessRequest.Load(fixture.RequestPath, fixture.FrameRoot, config);
    var frame = request.ReadFrame();
    Equal("request-test", request.RequestId);
    Equal(2, frame.Width);
    Equal(6, frame.Rgb.Length);
});

Run("request target mismatch is rejected", () =>
{
    using var fixture = ManifestFixture.Create();
    fixture.ReplaceRequest("target.exe", fixture.FramePath);
    var config = RuntimeConfig.Load(fixture.ManifestPath);
    Throws<ContractException>(() => PreprocessRequest.Load(fixture.RequestPath, fixture.FrameRoot, config), "request.targetExecutable must equal msedge.exe");
});

Run("frame outside declared root is rejected", () =>
{
    using var fixture = ManifestFixture.Create();
    fixture.ReplaceRequest("msedge.exe", fixture.OutsideFramePath);
    var config = RuntimeConfig.Load(fixture.ManifestPath);
    Throws<ContractException>(() => PreprocessRequest.Load(fixture.RequestPath, fixture.FrameRoot, config), "must be below the declared frame root");
});

Run("frame digest mismatch is rejected", () =>
{
    using var fixture = ManifestFixture.Create();
    var config = RuntimeConfig.Load(fixture.ManifestPath);
    var request = PreprocessRequest.Load(fixture.RequestPath, fixture.FrameRoot, config);
    var bytes = File.ReadAllBytes(fixture.FramePath);
    bytes[0] ^= 0xff;
    File.WriteAllBytes(fixture.FramePath, bytes);
    Throws<ContractException>(() => request.ReadFrame(), "request.frame sha256 mismatch");
});

Run("legacy event arguments are rejected", () =>
{
    using var fixture = ManifestFixture.Create();
    Throws<ContractException>(
        () => RuntimeArguments.Parse(["--config", fixture.ManifestPath, "--model", fixture.ModelPath, "--event-url", "http://127.0.0.1:8788/v1/events"]),
        "unknown argument: --event-url");
});

Run("validate-only rejects run arguments", () =>
{
    using var fixture = ManifestFixture.Create();
    Throws<ContractException>(
        () => RuntimeArguments.Parse(["--config", fixture.ManifestPath, "--model", fixture.ModelPath, "--request", fixture.RequestPath, "--validate-only"]),
        "--validate-only must not include run arguments: --request");
});

Run("preprocess uses canonical letterbox", () =>
{
    var frame = new CapturedFrame(2, 1, [255, 0, 0, 0, 255, 0], new string('0', 64));
    var (tensor, scale, padX, padY) = YoloDetector.Preprocess(frame, 4, 4);
    Equal(2.0, scale);
    Equal(0.0, padX);
    Equal(1.0, padY);
    Near(114f / 255f, tensor[0, 0, 0, 0]);
    Near(1f, tensor[0, 0, 1, 0]);
    Near(0f, tensor[0, 1, 1, 0]);
    Near(0.75f, tensor[0, 0, 1, 1]);
    Near(0.25f, tensor[0, 1, 1, 1]);
});

Run("decode applies class-aware NMS and original coordinates", () =>
{
    var output = new DenseTensor<float>(new[] { 1, 6, 3 });
    SetCandidate(output, 0, 100, 100, 40, 40, 0.9f, 0.1f);
    SetCandidate(output, 1, 101, 101, 40, 40, 0.8f, 0.1f);
    SetCandidate(output, 2, 100, 100, 40, 40, 0.1f, 0.85f);
    var result = YoloDetector.Decode(output, ["button", "text"], 0.2, 0.5, 10, 200, 100, 1, 0, 50);
    Equal(2, result.Count);
    Equal("button", result[0].Label);
    Equal("text", result[1].Label);
    Equal(new BoundingBox(80, 30, 120, 70), result[0].Box);
});

if (failures.Count > 0)
{
    Console.Error.WriteLine(string.Join(Environment.NewLine, failures));
    return 1;
}
Console.WriteLine("15 ScreenParser DirectML contract tests passed");
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

static void SetCandidate(DenseTensor<float> tensor, int anchor, float x, float y, float width, float height, float first, float second)
{
    tensor[0, 0, anchor] = x;
    tensor[0, 1, anchor] = y;
    tensor[0, 2, anchor] = width;
    tensor[0, 3, anchor] = height;
    tensor[0, 4, anchor] = first;
    tensor[0, 5, anchor] = second;
}

static void Equal<T>(T expected, T actual) where T : notnull
{
    if (!EqualityComparer<T>.Default.Equals(expected, actual))
    {
        throw new Exception($"expected={expected} actual={actual}");
    }
}

static void Near(float expected, float actual)
{
    if (Math.Abs(expected - actual) > 0.0001f)
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

sealed class ManifestFixture : IDisposable
{
    private ManifestFixture(string root, string manifestPath, string modelPath, string frameRoot, string framePath, string outsideFramePath, string requestPath, string responsePath)
    {
        Root = root;
        ManifestPath = manifestPath;
        ModelPath = modelPath;
        FrameRoot = frameRoot;
        FramePath = framePath;
        OutsideFramePath = outsideFramePath;
        RequestPath = requestPath;
        ResponsePath = responsePath;
    }

    public string Root { get; }
    public string ManifestPath { get; }
    public string ModelPath { get; }
    public string FrameRoot { get; }
    public string FramePath { get; }
    public string OutsideFramePath { get; }
    public string RequestPath { get; }
    public string ResponsePath { get; }

    public static ManifestFixture Create(string rootInjection = "")
    {
        var root = Path.Combine(Path.GetTempPath(), "screenparser-directml-tests", Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(root);
        var modelPath = Path.Combine(root, "screenparser-v2.onnx");
        File.WriteAllText(modelPath, "model");
        var frameRoot = Path.Combine(root, "frames");
        Directory.CreateDirectory(frameRoot);
        var framePath = Path.Combine(frameRoot, "frame.rgb");
        var outsideFramePath = Path.Combine(root, "outside.rgb");
        File.WriteAllBytes(framePath, [255, 0, 0, 0, 255, 0]);
        File.WriteAllBytes(outsideFramePath, [255, 0, 0, 0, 255, 0]);
        var requestPath = Path.Combine(root, "request.json");
        var responsePath = Path.Combine(root, "response.json");
        var manifestPath = Path.Combine(root, "manifest.json");
        var sourceHash = new string('a', 64);
        var modelHash = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(File.ReadAllBytes(modelPath))).ToLowerInvariant();
        File.WriteAllText(manifestPath, $$"""
        {
          "schemaVersion": 1,
          "moduleId": "screenparser/ui-elements",
          "kind": "preprocessor",
          "runtime": "screenparser-onnx-dml-v1",
          "targetExecutable": "msedge.exe",
          "model": {
            "artifactId": "screenparser-v2-test",
            "format": "onnx",
            "filename": "screenparser-v2.onnx",
            "sha256": "{{modelHash}}",
            "precision": "fp16",
            "opset": 20,
            "inputName": "images",
            "outputName": "output0",
            "inputWidth": 1280,
            "inputHeight": 1280,
            "labels": ["button", "text"],
            "source": {
              "repository": "docling-project/ScreenParser",
              "revision": "f029e565f1206577402e43206454522075be3f72",
              "filename": "best.pt",
              "sha256": "{{sourceHash}}",
              "license": "Apache-2.0"
            }
          },
          "inference": {
            "confidence": 0.1,
            "iou": 0.1,
            "maxDetections": 700,
            "device": "directml:0"
          }{{rootInjection}}
        }
        """);
        var fixture = new ManifestFixture(root, manifestPath, modelPath, frameRoot, framePath, outsideFramePath, requestPath, responsePath);
        fixture.ReplaceRequest("msedge.exe", framePath);
        return fixture;
    }

    public ManifestFixture Replace(string oldValue, string newValue)
    {
        File.WriteAllText(ManifestPath, File.ReadAllText(ManifestPath).Replace(oldValue, newValue, StringComparison.Ordinal));
        return this;
    }

    public void ReplaceRequest(string targetExecutable, string framePath)
    {
        var frameHash = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(File.ReadAllBytes(framePath))).ToLowerInvariant();
        File.WriteAllText(RequestPath, $$"""
        {
          "schemaVersion": 1,
          "requestId": "request-test",
          "targetExecutable": "{{targetExecutable}}",
          "frame": {
            "artifactId": "frame-test",
            "capturedAt": "2026-08-07T00:00:00.000000Z",
            "rgbPath": {{System.Text.Json.JsonSerializer.Serialize(framePath)}},
            "sha256": "{{frameHash}}",
            "width": 2,
            "height": 1
          }
        }
        """);
    }

    public void Dispose() => Directory.Delete(Root, true);
}
