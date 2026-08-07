using System.Buffers.Binary;
using System.Diagnostics;
using System.Security.Cryptography;
using System.Text.Json;

namespace PpOcr.DirectML;

public sealed record WorkerTextRegionsRequest(
    string RequestId,
    string ArtifactId,
    DateTimeOffset CapturedAt,
    CapturedRegion Region);

public static class TextRegionsWorkerProtocol
{
    public const int ProtocolVersion = 1;
    public const int MaxFrameBytes = 8 << 20;

    public static int Run(
        Stream input,
        Stream output,
        TextRegionsRuntimeConfig config,
        TextRegionDetector detector,
        TextLineRecognizer recognizer,
        int processId,
        double modelLoadMs)
    {
        var initialized = false;
        while (true)
        {
            using var document = ReadFrame(input);
            var root = Contract.RequireObject(document.RootElement, "text regions worker request");
            Contract.RequireProperties(root, "text regions worker request", "schemaVersion", "id", "method", "params");
            if (Contract.RequireInt(root, "schemaVersion") != ProtocolVersion)
            {
                throw new ContractException($"text regions worker request.schemaVersion must equal {ProtocolVersion}");
            }
            var id = Contract.RequireIdentifier(Contract.RequireString(root, "id"), "text regions worker request.id");
            var method = Contract.RequireIdentifier(Contract.RequireString(root, "method"), "text regions worker request.method");
            var parameters = Contract.RequireObject(root.GetProperty("params"), "text regions worker request.params");

            if (!initialized)
            {
                if (method != "initialize")
                {
                    throw new ContractException("first text regions worker request method must equal initialize");
                }
                Contract.RequireProperties(parameters, "text regions worker request.params");
                WriteFrame(output, new
                {
                    schemaVersion = ProtocolVersion,
                    id,
                    ok = true,
                    result = new
                    {
                        runtime = TextRegionsRuntimeConfig.RuntimeId,
                        pipeline = TextRegionsRuntimeConfig.Pipeline,
                        provider = "DirectML",
                        adapterIndex = 0,
                        processId,
                        modelLoadMs,
                        detectionModel = new
                        {
                            artifactId = config.DetectionModel.ArtifactId,
                            filename = config.DetectionModel.Filename,
                            sha256 = config.DetectionModel.Sha256,
                            inputWidth = config.Detection.InputWidth,
                        },
                        recognitionModel = new
                        {
                            artifactId = config.RecognitionModel.ArtifactId,
                            filename = config.RecognitionModel.Filename,
                            sha256 = config.RecognitionModel.Sha256,
                            inputWidth = config.RecognitionModel.InputWidth,
                            inputHeight = config.RecognitionModel.InputHeight,
                        },
                    },
                });
                initialized = true;
                continue;
            }

            if (method == "shutdown")
            {
                Contract.RequireProperties(parameters, "text regions worker request.params");
                WriteFrame(output, new
                {
                    schemaVersion = ProtocolVersion,
                    id,
                    ok = true,
                    result = new { state = "stopped" },
                });
                return 0;
            }
            if (method != "detectRecognize")
            {
                throw new ContractException($"unsupported text regions worker request method: {method}");
            }

            var request = ParseRequest(parameters);
            try
            {
                var total = Stopwatch.StartNew();
                var detection = detector.Detect(request.Region);
                var regions = new List<object>(detection.Regions.Count);
                var recognitionPreprocessMs = 0.0;
                var recognitionInferenceMs = 0.0;
                var recognitionPostprocessMs = 0.0;
                foreach (var detected in detection.Regions)
                {
                    var line = TextRegionDetector.Rectify(
                        request.Region,
                        detected.Points,
                        config.RecognitionModel.InputWidth,
                        config.RecognitionModel.InputHeight);
                    var recognition = recognizer.Recognize(line);
                    recognitionPreprocessMs += recognition.PreprocessMs;
                    recognitionInferenceMs += recognition.InferenceMs;
                    recognitionPostprocessMs += recognition.PostprocessMs;
                    regions.Add(new
                    {
                        points = detected.Points.Select(point => new
                        {
                            x = Math.Round(point.X, 2),
                            y = Math.Round(point.Y, 2),
                        }),
                        detectionConfidence = detected.Confidence,
                        text = recognition.Text,
                        recognitionConfidence = recognition.Confidence,
                    });
                }
                total.Stop();
                WriteFrame(output, new
                {
                    schemaVersion = ProtocolVersion,
                    id,
                    ok = true,
                    result = new
                    {
                        requestId = request.RequestId,
                        completedAt = Contract.FormatTimestamp(DateTimeOffset.UtcNow),
                        evidence = new
                        {
                            artifactId = request.ArtifactId,
                            capturedAt = Contract.FormatTimestamp(request.CapturedAt),
                            width = request.Region.Width,
                            height = request.Region.Height,
                            rgbSha256 = request.Region.RgbSha256,
                        },
                        models = new
                        {
                            detectionArtifactId = config.DetectionModel.ArtifactId,
                            recognitionArtifactId = config.RecognitionModel.ArtifactId,
                            provider = "DirectML",
                            adapterIndex = 0,
                            detectionInputWidth = detection.InputWidth,
                            detectionInputHeight = detection.InputHeight,
                            recognitionInputWidth = config.RecognitionModel.InputWidth,
                            recognitionInputHeight = config.RecognitionModel.InputHeight,
                        },
                        timing = new
                        {
                            detectionPreprocessMs = detection.PreprocessMs,
                            detectionInferenceMs = detection.InferenceMs,
                            detectionPostprocessMs = detection.PostprocessMs,
                            recognitionPreprocessMs = Math.Round(recognitionPreprocessMs, 2),
                            recognitionInferenceMs = Math.Round(recognitionInferenceMs, 2),
                            recognitionPostprocessMs = Math.Round(recognitionPostprocessMs, 2),
                            totalMs = Math.Round(total.Elapsed.TotalMilliseconds, 2),
                        },
                        regions,
                    },
                });
            }
            catch (Exception exception)
            {
                WriteFrame(output, new
                {
                    schemaVersion = ProtocolVersion,
                    id,
                    ok = false,
                    error = new { code = "OCR_TEXT_REGIONS_FAILED", message = exception.Message },
                });
                return 1;
            }
        }
    }

    public static WorkerTextRegionsRequest ParseRequest(JsonElement value)
    {
        Contract.RequireProperties(
            value, "text regions worker request.params", "requestId", "artifactId", "capturedAt",
            "width", "height", "rgbBase64", "sha256");
        var requestId = Contract.RequireIdentifier(Contract.RequireString(value, "requestId"), "text regions worker request.params.requestId");
        var artifactId = Contract.RequireIdentifier(Contract.RequireString(value, "artifactId"), "text regions worker request.params.artifactId");
        var capturedAt = Contract.RequireTimestamp(Contract.RequireString(value, "capturedAt"), "text regions worker request.params.capturedAt");
        var width = Contract.RequireIntRange(value, "width", 1, 4096, "text regions worker request.params.width");
        var height = Contract.RequireIntRange(value, "height", 1, 2048, "text regions worker request.params.height");
        var expectedSha256 = Contract.RequireSha256(Contract.RequireString(value, "sha256"), "text regions worker request.params.sha256");
        var base64Element = value.GetProperty("rgbBase64");
        if (base64Element.ValueKind != JsonValueKind.String || string.IsNullOrEmpty(base64Element.GetString()))
        {
            throw new ContractException("text regions worker request.params.rgbBase64 must be non-empty canonical base64");
        }
        var base64 = base64Element.GetString()!;
        byte[] rgb;
        try
        {
            rgb = Convert.FromBase64String(base64);
        }
        catch (FormatException exception)
        {
            throw new ContractException("text regions worker request.params.rgbBase64 must be canonical base64", exception);
        }
        if (!StringComparer.Ordinal.Equals(Convert.ToBase64String(rgb), base64))
        {
            throw new ContractException("text regions worker request.params.rgbBase64 must be canonical base64");
        }
        var expectedBytes = checked(width * height * 3);
        if (rgb.Length != expectedBytes)
        {
            throw new ContractException($"text regions worker RGB byte length mismatch: expected={expectedBytes} actual={rgb.Length}");
        }
        var actualSha256 = Convert.ToHexString(SHA256.HashData(rgb)).ToLowerInvariant();
        if (!StringComparer.Ordinal.Equals(actualSha256, expectedSha256))
        {
            throw new ContractException($"text regions worker RGB sha256 mismatch: expected={expectedSha256} actual={actualSha256}");
        }
        return new WorkerTextRegionsRequest(
            requestId,
            artifactId,
            capturedAt,
            new CapturedRegion(width, height, rgb, actualSha256));
    }

    public static JsonDocument ReadFrame(Stream input)
    {
        var header = new byte[4];
        ReadExactly(input, header);
        var length = BinaryPrimitives.ReadInt32LittleEndian(header);
        if (length is <= 0 or > MaxFrameBytes)
        {
            throw new ContractException($"text regions worker frame length must be from 1 through {MaxFrameBytes}");
        }
        var body = new byte[length];
        ReadExactly(input, body);
        try
        {
            return JsonDocument.Parse(body, new JsonDocumentOptions
            {
                AllowTrailingCommas = false,
                CommentHandling = JsonCommentHandling.Disallow,
            });
        }
        catch (JsonException exception)
        {
            throw new ContractException($"text regions worker frame must contain strict JSON: {exception.Message}", exception);
        }
    }

    public static void WriteFrame(Stream output, object value)
    {
        var body = JsonSerializer.SerializeToUtf8Bytes(value);
        if (body.Length is <= 0 or > MaxFrameBytes)
        {
            throw new ContractException($"text regions worker response exceeds {MaxFrameBytes} bytes");
        }
        Span<byte> header = stackalloc byte[4];
        BinaryPrimitives.WriteInt32LittleEndian(header, body.Length);
        output.Write(header);
        output.Write(body);
        output.Flush();
    }

    private static void ReadExactly(Stream input, byte[] target)
    {
        var offset = 0;
        while (offset < target.Length)
        {
            var count = input.Read(target, offset, target.Length - offset);
            if (count == 0)
            {
                throw new EndOfStreamException("text regions worker input ended before a complete frame was read");
            }
            offset += count;
        }
    }
}
