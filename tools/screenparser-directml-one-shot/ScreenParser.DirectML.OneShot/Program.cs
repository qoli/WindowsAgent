using System.Diagnostics;
using System.Security.Cryptography;
using System.Text.Json;
using ScreenParser.DirectML;
using ScreenParser.DirectML.OneShot;

try
{
    if (args.Length != 2 || args[0] != "--spec")
    {
        throw new ContractException("usage: ScreenParser.DirectML.OneShot.exe --spec <absolute-json-path>");
    }
    var spec = DiagnosticSpec.Load(args[1]);
    DiagnosticSpec.ValidateArtifacts(spec);
    var rgb = File.ReadAllBytes(spec.Frame.RgbPath);
    var frame = new CapturedFrame(spec.Frame.Width, spec.Frame.Height, rgb, spec.Frame.Sha256);
    var placeholderSourceHash = new string('0', 64);
    var model = new OnnxModel(
        spec.Model.ArtifactId,
        "onnx",
        Path.GetFileName(spec.Model.Filename),
        spec.Model.Sha256,
        spec.Model.Precision,
        spec.Model.Opset,
        spec.Model.InputName,
        spec.Model.OutputName,
        spec.Model.InputWidth,
        spec.Model.InputHeight,
        spec.Model.Labels,
        new SourceModel("diagnostic-one-shot", "not-applicable", "not-applicable", placeholderSourceHash, "not-applicable"));
    var inferenceSettings = new InferenceSettings(
        spec.Inference.Confidence,
        spec.Inference.Iou,
        spec.Inference.MaxDetections,
        "directml:0");
    var config = new RuntimeConfig(
        "screenparser/diagnostic-one-shot",
        "diagnostic.exe",
        model,
        inferenceSettings);

    var process = Process.GetCurrentProcess();
    var workingSetBefore = process.WorkingSet64;
    var loadTimer = Stopwatch.StartNew();
    using var detector = new YoloDetector(spec.Model.Filename, config);
    loadTimer.Stop();
    process.Refresh();
    var workingSetAfterLoad = process.WorkingSet64;

    var warmup = new List<double>();
    for (var index = 0; index < spec.Inference.WarmupRuns; index++)
    {
        warmup.Add(detector.Infer(frame).DurationMs);
    }

    var measured = new List<RunMeasurement>();
    InferenceResult? final = null;
    for (var index = 0; index < spec.Inference.MeasuredRuns; index++)
    {
        final = detector.Infer(frame);
        measured.Add(new RunMeasurement(
            index + 1,
            final.DurationMs,
            final.Detections.Count,
            DetectionDigest(final.Detections)));
    }
    process.Refresh();
    var workingSetAfterInference = process.WorkingSet64;
    var detections = final!.Detections.Select(detection => new
    {
        classId = detection.ClassId,
        label = detection.Label,
        confidence = detection.Confidence,
        bboxPixels = new
        {
            left = detection.Box.Left,
            top = detection.Box.Top,
            right = detection.Box.Right,
            bottom = detection.Box.Bottom,
        },
    }).ToArray();
    var durations = measured.Select(value => value.EndToEndDurationMs).ToArray();
    Console.WriteLine(JsonSerializer.Serialize(new
    {
        schemaVersion = 1,
        status = "ok",
        provider = "DirectML",
        adapterIndex = 0,
        processId = Environment.ProcessId,
        model = new
        {
            artifactId = spec.Model.ArtifactId,
            filename = Path.GetFileName(spec.Model.Filename),
            sha256 = spec.Model.Sha256,
            precision = spec.Model.Precision,
            bytes = new FileInfo(spec.Model.Filename).Length,
        },
        frame = new
        {
            width = spec.Frame.Width,
            height = spec.Frame.Height,
            rgbSha256 = spec.Frame.Sha256,
            bytes = rgb.LongLength,
        },
        session = new
        {
            modelLoadMs = Math.Round(loadTimer.Elapsed.TotalMilliseconds, 2),
            workingSetBytesBefore = workingSetBefore,
            workingSetBytesAfterLoad = workingSetAfterLoad,
            workingSetBytesAfterInference = workingSetAfterInference,
        },
        warmup = new { count = warmup.Count, endToEndDurationMs = warmup },
        measured = measured.Select(value => new
        {
            index = value.Index,
            endToEndDurationMs = value.EndToEndDurationMs,
            detectionCount = value.DetectionCount,
            detectionSha256 = value.DetectionSha256,
        }),
        summary = new
        {
            count = durations.Length,
            minEndToEndDurationMs = durations.Min(),
            medianEndToEndDurationMs = Median(durations),
            maxEndToEndDurationMs = durations.Max(),
        },
        detections,
    }));
    return 0;
}
catch (Exception exception)
{
    Console.Error.WriteLine(JsonSerializer.Serialize(new { schemaVersion = 1, status = "error", error = exception.Message }));
    return 1;
}

static string DetectionDigest(IReadOnlyList<Detection> detections)
{
    var bytes = JsonSerializer.SerializeToUtf8Bytes(detections);
    return Convert.ToHexString(SHA256.HashData(bytes)).ToLowerInvariant();
}

static double Median(double[] values)
{
    var ordered = values.Order().ToArray();
    var middle = ordered.Length / 2;
    return ordered.Length % 2 == 0 ? (ordered[middle - 1] + ordered[middle]) / 2 : ordered[middle];
}

sealed record RunMeasurement(
    int Index,
    double EndToEndDurationMs,
    int DetectionCount,
    string DetectionSha256);
