using System.Buffers.Binary;
using System.Diagnostics;
using System.Security.Cryptography;
using System.Text.Json;

namespace PpOcr.DirectML;

public sealed record WorkerRecognitionRequest(
    string RequestId,
    string ArtifactId,
    DateTimeOffset CapturedAt,
    CapturedRegion Region);

public static class WorkerProtocol
{
    public const int ProtocolVersion = 1;
    public const int MaxFrameBytes = 512 << 10;

    public static int Run(
        Stream input,
        Stream output,
        RuntimeConfig config,
        TextLineRecognizer recognizer,
        int processId,
        double modelLoadMs)
    {
        var initialized = false;
        while (true)
        {
            using var document = ReadFrame(input);
            var root = Contract.RequireObject(document.RootElement, "worker request");
            Contract.RequireProperties(root, "worker request", "schemaVersion", "id", "method", "params");
            if (Contract.RequireInt(root, "schemaVersion") != ProtocolVersion)
            {
                throw new ContractException($"worker request.schemaVersion must equal {ProtocolVersion}");
            }
            var id = Contract.RequireIdentifier(Contract.RequireString(root, "id"), "worker request.id");
            var method = Contract.RequireIdentifier(Contract.RequireString(root, "method"), "worker request.method");
            var parameters = Contract.RequireObject(root.GetProperty("params"), "worker request.params");

            if (!initialized)
            {
                if (method != "initialize")
                {
                    throw new ContractException("first worker request method must equal initialize");
                }
                Contract.RequireProperties(parameters, "worker request.params");
                WriteFrame(output, new
                {
                    schemaVersion = ProtocolVersion,
                    id,
                    ok = true,
                    result = new
                    {
                        runtime = RuntimeConfig.RuntimeId,
                        pipeline = RuntimeConfig.Pipeline,
                        provider = "DirectML",
                        adapterIndex = 0,
                        processId,
                        modelLoadMs,
                        model = new
                        {
                            artifactId = config.Model.ArtifactId,
                            filename = config.Model.Filename,
                            sha256 = config.Model.Sha256,
                            inputWidth = config.Model.InputWidth,
                            inputHeight = config.Model.InputHeight,
                        },
                    },
                });
                initialized = true;
                continue;
            }

            if (method == "shutdown")
            {
                Contract.RequireProperties(parameters, "worker request.params");
                WriteFrame(output, new
                {
                    schemaVersion = ProtocolVersion,
                    id,
                    ok = true,
                    result = new { state = "stopped" },
                });
                return 0;
            }
            if (method != "recognize")
            {
                throw new ContractException($"unsupported worker request method: {method}");
            }

            var request = ParseRecognition(parameters);
            try
            {
                var timer = Stopwatch.StartNew();
                var recognition = recognizer.Recognize(request.Region);
                timer.Stop();
                WriteFrame(output, new
                {
                    schemaVersion = ProtocolVersion,
                    id,
                    ok = true,
                    result = new
                    {
                        requestId = request.RequestId,
                        completedAt = Contract.FormatTimestamp(DateTimeOffset.UtcNow),
                        text = recognition.Text,
                        confidence = recognition.Confidence,
                        evidence = new
                        {
                            artifactId = request.ArtifactId,
                            capturedAt = Contract.FormatTimestamp(request.CapturedAt),
                            width = request.Region.Width,
                            height = request.Region.Height,
                            rgbSha256 = request.Region.RgbSha256,
                        },
                        model = new
                        {
                            artifactId = config.Model.ArtifactId,
                            provider = "DirectML",
                            adapterIndex = 0,
                            inputWidth = recognition.InputWidth,
                            inputHeight = config.Model.InputHeight,
                        },
                        timing = new
                        {
                            preprocessMs = recognition.PreprocessMs,
                            inferenceMs = recognition.InferenceMs,
                            postprocessMs = recognition.PostprocessMs,
                            totalMs = Math.Round(timer.Elapsed.TotalMilliseconds, 2),
                        },
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
                    error = new { code = "OCR_RECOGNITION_FAILED", message = exception.Message },
                });
                return 1;
            }
        }
    }

    public static WorkerRecognitionRequest ParseRecognition(JsonElement value)
    {
        Contract.RequireProperties(
            value, "worker request.params", "requestId", "artifactId", "capturedAt",
            "width", "height", "rgbBase64", "sha256");
        var requestId = Contract.RequireIdentifier(Contract.RequireString(value, "requestId"), "worker request.params.requestId");
        var artifactId = Contract.RequireIdentifier(Contract.RequireString(value, "artifactId"), "worker request.params.artifactId");
        var capturedAt = Contract.RequireTimestamp(Contract.RequireString(value, "capturedAt"), "worker request.params.capturedAt");
        var width = Contract.RequireIntRange(value, "width", 1, 4096, "worker request.params.width");
        var height = Contract.RequireIntRange(value, "height", 1, 1024, "worker request.params.height");
        var expectedSha256 = Contract.RequireSha256(Contract.RequireString(value, "sha256"), "worker request.params.sha256");
        var base64Element = value.GetProperty("rgbBase64");
        if (base64Element.ValueKind != JsonValueKind.String || string.IsNullOrEmpty(base64Element.GetString()))
        {
            throw new ContractException("worker request.params.rgbBase64 must be non-empty canonical base64");
        }
        var base64 = base64Element.GetString()!;
        byte[] rgb;
        try
        {
            rgb = Convert.FromBase64String(base64);
        }
        catch (FormatException exception)
        {
            throw new ContractException("worker request.params.rgbBase64 must be canonical base64", exception);
        }
        if (!StringComparer.Ordinal.Equals(Convert.ToBase64String(rgb), base64))
        {
            throw new ContractException("worker request.params.rgbBase64 must be canonical base64");
        }
        var expectedBytes = checked(width * height * 3);
        if (rgb.Length != expectedBytes)
        {
            throw new ContractException($"worker RGB byte length mismatch: expected={expectedBytes} actual={rgb.Length}");
        }
        var actualSha256 = Convert.ToHexString(SHA256.HashData(rgb)).ToLowerInvariant();
        if (!StringComparer.Ordinal.Equals(actualSha256, expectedSha256))
        {
            throw new ContractException($"worker RGB sha256 mismatch: expected={expectedSha256} actual={actualSha256}");
        }
        return new WorkerRecognitionRequest(
            requestId, artifactId, capturedAt,
            new CapturedRegion(width, height, rgb, actualSha256));
    }

    public static JsonDocument ReadFrame(Stream input)
    {
        var header = new byte[4];
        ReadExactly(input, header);
        var length = BinaryPrimitives.ReadInt32LittleEndian(header);
        if (length is <= 0 or > MaxFrameBytes)
        {
            throw new ContractException($"worker frame length must be from 1 through {MaxFrameBytes}");
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
            throw new ContractException($"worker frame must contain strict JSON: {exception.Message}", exception);
        }
    }

    public static void WriteFrame(Stream output, object value)
    {
        var body = JsonSerializer.SerializeToUtf8Bytes(value);
        if (body.Length is <= 0 or > MaxFrameBytes)
        {
            throw new ContractException($"worker response exceeds {MaxFrameBytes} bytes");
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
                throw new EndOfStreamException("worker input ended before a complete frame was read");
            }
            offset += count;
        }
    }
}
