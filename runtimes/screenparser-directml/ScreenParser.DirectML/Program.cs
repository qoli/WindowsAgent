namespace ScreenParser.DirectML;

public static class Program
{
    public static int Main(string[] arguments)
    {
        try
        {
            var parsed = RuntimeArguments.Parse(arguments);
            var config = RuntimeConfig.Load(parsed.ConfigPath);
            RuntimeConfig.ValidateModel(parsed.ModelPath, config.Model);
            if (parsed.ValidateOnly)
            {
                Console.WriteLine(System.Text.Json.JsonSerializer.Serialize(new
                {
                    status = "ok",
                    moduleId = config.ModuleId,
                    targetExecutable = config.TargetExecutable,
                    runtime = RuntimeConfig.RuntimeId,
                    artifactId = config.Model.ArtifactId,
                }));
                return 0;
            }

            var request = PreprocessRequest.Load(parsed.RequestPath!, parsed.FrameRoot!, config);
            var frame = request.ReadFrame();
            var loadTimer = System.Diagnostics.Stopwatch.StartNew();
            using var detector = new YoloDetector(parsed.ModelPath, config);
            loadTimer.Stop();
            var inference = detector.Infer(frame);
            var detections = inference.Detections.Select(detection => new
            {
                classId = detection.ClassId,
                label = detection.Label,
                confidence = detection.Confidence,
                bboxPixels = new { left = detection.Box.Left, top = detection.Box.Top, right = detection.Box.Right, bottom = detection.Box.Bottom },
                bboxNormalized = new
                {
                    left = Math.Round(detection.Box.Left / frame.Width, 6),
                    top = Math.Round(detection.Box.Top / frame.Height, 6),
                    right = Math.Round(detection.Box.Right / frame.Width, 6),
                    bottom = Math.Round(detection.Box.Bottom / frame.Height, 6),
                },
            }).ToArray();
            ResponseWriter.WriteNewAtomic(parsed.ResponsePath!, new
            {
                schemaVersion = 1,
                status = "ok",
                requestId = request.RequestId,
                moduleId = config.ModuleId,
                targetExecutable = config.TargetExecutable,
                runtime = RuntimeConfig.RuntimeId,
                provider = "DirectML",
                adapterIndex = 0,
                completedAt = PreprocessRequest.FormatTimestamp(DateTimeOffset.UtcNow),
                model = new
                {
                    artifactId = config.Model.ArtifactId,
                    format = config.Model.Format,
                    filename = config.Model.Filename,
                    sha256 = config.Model.Sha256,
                    precision = config.Model.Precision,
                    source = new { repository = config.Model.Source.Repository, revision = config.Model.Source.Revision, sha256 = config.Model.Source.Sha256 },
                },
                frame = new
                {
                    artifactId = request.Frame.ArtifactId,
                    capturedAt = PreprocessRequest.FormatTimestamp(request.Frame.CapturedAt),
                    width = frame.Width,
                    height = frame.Height,
                    rgbSha256 = frame.RgbSha256,
                },
                inference = new
                {
                    modelLoadMs = Math.Round(loadTimer.Elapsed.TotalMilliseconds, 2),
                    durationMs = inference.DurationMs,
                    device = config.Inference.Device,
                    inputWidth = config.Model.InputWidth,
                    inputHeight = config.Model.InputHeight,
                    confidence = config.Inference.Confidence,
                    iou = config.Inference.Iou,
                },
                detectionCount = detections.Length,
                detections,
            });
            Console.WriteLine(System.Text.Json.JsonSerializer.Serialize(new { status = "ok", requestId = request.RequestId, response = parsed.ResponsePath }));
            return 0;
        }
        catch (Exception exception)
        {
            Console.Error.WriteLine($"[FATAL] {exception.Message}");
            return 1;
        }
    }
}
