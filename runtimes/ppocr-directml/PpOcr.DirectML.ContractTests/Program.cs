using Microsoft.ML.OnnxRuntime.Tensors;
using PpOcr.DirectML;

static void Equal<T>(T actual, T expected, string name)
{
    if (!EqualityComparer<T>.Default.Equals(actual, expected))
    {
        throw new Exception($"{name}: expected={expected} actual={actual}");
    }
}

var region = new CapturedRegion(2, 1, new byte[] { 0, 127, 255, 255, 127, 0 }, new string('0', 64));
var tensor = TextLineRecognizer.Preprocess(region, 48, 96);
Equal(tensor.Dimensions.SequenceEqual(new[] { 1, 3, 48, 96 }), true, "preprocess shape");
if (Math.Abs(tensor[0, 0, 24, 0] - (-1f)) > 0.0001f || Math.Abs(tensor[0, 2, 24, 0] - 1f) > 0.0001f)
{
    throw new Exception("preprocess RGB normalization is incorrect");
}

var output = new DenseTensor<float>(new[] { 1, 6, 4 });
output[0, 0, 0] = 0.9f;
output[0, 1, 1] = 0.8f;
output[0, 2, 1] = 0.7f;
output[0, 3, 0] = 0.9f;
output[0, 4, 2] = 0.6f;
output[0, 5, 0] = 0.9f;
var decoded = TextLineRecognizer.DecodeCtc(output, new[] { "A", "B", "C" });
Equal(decoded.Text, "AB", "CTC text");
if (Math.Abs(decoded.Confidence - 0.7) > 0.0001)
{
    throw new Exception($"CTC confidence: expected=0.7 actual={decoded.Confidence}");
}

try
{
    TextLineRecognizer.DecodeCtc(new DenseTensor<float>(new[] { 1, 1, 3 }), new[] { "A", "B", "C" });
    throw new Exception("invalid CTC output shape was accepted");
}
catch (ContractException exception) when (exception.Message.Contains("output shape", StringComparison.Ordinal))
{
}

try
{
    TextLineRecognizer.Preprocess(new CapturedRegion(2, 1, new byte[5], new string('0', 64)), 48, 96);
    throw new Exception("invalid RGB byte length was accepted");
}
catch (ContractException exception) when (exception.Message.Contains("buffer length", StringComparison.Ordinal))
{
}

try
{
    RuntimeArguments.Parse(new[] { "--unknown", "value" });
    throw new Exception("unknown runtime argument was accepted");
}
catch (ContractException exception) when (exception.Message.Contains("unknown argument", StringComparison.Ordinal))
{
}

var workerRgb = new byte[] { 1, 2, 3, 4, 5, 6 };
var workerSha = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(workerRgb)).ToLowerInvariant();
using var workerDocument = System.Text.Json.JsonDocument.Parse(System.Text.Json.JsonSerializer.Serialize(new
{
    requestId = "request-1",
    artifactId = "artifact-1",
    capturedAt = "2026-08-07T01:02:03.123456Z",
    width = 2,
    height = 1,
    rgbBase64 = Convert.ToBase64String(workerRgb),
    sha256 = workerSha,
}));
var workerRequest = WorkerProtocol.ParseRecognition(workerDocument.RootElement);
Equal(workerRequest.Region.RgbSha256, workerSha, "worker RGB SHA-256");

var fullSizeRgb = new byte[800 * 80 * 3];
var fullSizeSha = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(fullSizeRgb)).ToLowerInvariant();
using var fullSizeDocument = System.Text.Json.JsonDocument.Parse(System.Text.Json.JsonSerializer.Serialize(new
{
    requestId = "request-full-size",
    artifactId = "artifact-full-size",
    capturedAt = "2026-08-07T01:02:03.123456Z",
    width = 800,
    height = 80,
    rgbBase64 = Convert.ToBase64String(fullSizeRgb),
    sha256 = fullSizeSha,
}));
var fullSizeRequest = WorkerProtocol.ParseRecognition(fullSizeDocument.RootElement);
Equal(fullSizeRequest.Region.Rgb.Length, fullSizeRgb.Length, "full-size worker RGB length");

using var nonCanonicalBase64Document = System.Text.Json.JsonDocument.Parse(System.Text.Json.JsonSerializer.Serialize(new
{
    requestId = "request-noncanonical",
    artifactId = "artifact-noncanonical",
    capturedAt = "2026-08-07T01:02:03.123456Z",
    width = 2,
    height = 1,
    rgbBase64 = Convert.ToBase64String(workerRgb) + "\n",
    sha256 = workerSha,
}));
try
{
    WorkerProtocol.ParseRecognition(nonCanonicalBase64Document.RootElement);
    throw new Exception("non-canonical worker base64 was accepted");
}
catch (ContractException exception) when (exception.Message.Contains("canonical base64", StringComparison.Ordinal))
{
}

using var frame = new MemoryStream();
WorkerProtocol.WriteFrame(frame, new { schemaVersion = 1, id = "frame-1", ok = true });
frame.Position = 0;
using var decodedFrame = WorkerProtocol.ReadFrame(frame);
Equal(decodedFrame.RootElement.GetProperty("id").GetString(), "frame-1", "worker frame ID");

try
{
    RuntimeArguments.Parse(new[] { "--config", "a", "--model", "b", "--characters", "c", "--worker", "--request", "d" });
    throw new Exception("worker mode accepted one-shot arguments");
}
catch (ContractException exception) when (exception.Message.Contains("must not include run arguments", StringComparison.Ordinal))
{
}

try
{
    TextRegionsRuntimeArguments.Parse(new[] { "--unknown", "value" });
    throw new Exception("unknown text regions runtime argument was accepted");
}
catch (ContractException exception) when (exception.Message.Contains("unknown text regions worker argument", StringComparison.Ordinal))
{
}

var textRegionsRgb = new byte[640 * 300 * 3];
var textRegionsSha = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(textRegionsRgb)).ToLowerInvariant();
using var textRegionsDocument = System.Text.Json.JsonDocument.Parse(System.Text.Json.JsonSerializer.Serialize(new
{
    requestId = "regions-full-size",
    artifactId = "regions-artifact",
    capturedAt = "2026-08-08T01:02:03.123456Z",
    width = 640,
    height = 300,
    rgbBase64 = Convert.ToBase64String(textRegionsRgb),
    sha256 = textRegionsSha,
}));
var textRegionsRequest = TextRegionsWorkerProtocol.ParseRequest(textRegionsDocument.RootElement);
Equal(textRegionsRequest.Region.Rgb.Length, textRegionsRgb.Length, "text regions RGB length");

var detectionOutput = new DenseTensor<float>(new[] { 1, 1, 32, 64 });
for (var y = 10; y < 18; y++)
{
    for (var x = 15; x < 45; x++)
    {
        detectionOutput[0, 0, y, x] = 0.9f;
    }
}
var detectedRegions = TextRegionDetector.Postprocess(
    detectionOutput,
    64,
    32,
    1,
    new DetectionSettings(64, 0.2, 0.45, 8, 1.4));
Equal(detectedRegions.Count, 1, "synthetic detection region count");
Equal(detectedRegions[0].Points.Count, 4, "synthetic detection point count");

detectionOutput[0, 0, 0, 0] = float.NaN;
try
{
    TextRegionDetector.Postprocess(detectionOutput, 64, 32, 1, new DetectionSettings(64, 0.2, 0.45, 8, 1.4));
    throw new Exception("non-finite detection output was accepted");
}
catch (ContractException exception) when (exception.Message.Contains("non-finite", StringComparison.Ordinal))
{
}

var rectified = TextRegionDetector.Rectify(
    new CapturedRegion(2, 2, new byte[] { 255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255 }, new string('0', 64)),
    new[] { new PointD(0, 0), new PointD(1, 0), new PointD(1, 1), new PointD(0, 1) },
    480,
    48);
Equal(rectified.Rgb.Length, 480 * 48 * 3, "rectified RGB length");
Equal(rectified.RgbSha256.Length, 64, "rectified RGB SHA length");

Console.WriteLine("15 PP-OCR DirectML contract tests passed");
