using System.Diagnostics;

namespace PpOcr.DirectML;

public static class Program
{
    public static int Main(string[] arguments)
    {
        try
        {
            if (arguments.Length > 0 && arguments[0] is "--text-regions-worker" or "--text-regions-validate-only")
            {
                var worker = arguments[0] == "--text-regions-worker";
                var parsedTextRegions = TextRegionsRuntimeArguments.Parse(arguments[1..]);
                var textRegionsConfig = TextRegionsRuntimeConfig.Load(parsedTextRegions.ConfigPath);
                var textRegionCharacters = textRegionsConfig.ValidateArtifacts(
                    parsedTextRegions.DetectionModelPath,
                    parsedTextRegions.RecognitionModelPath,
                    parsedTextRegions.CharactersPath);
                if (!worker)
                {
                    Console.WriteLine(System.Text.Json.JsonSerializer.Serialize(new
                    {
                        status = "ok",
                        runtime = TextRegionsRuntimeConfig.RuntimeId,
                        pipeline = TextRegionsRuntimeConfig.Pipeline,
                        detectionArtifactId = textRegionsConfig.DetectionModel.ArtifactId,
                        recognitionArtifactId = textRegionsConfig.RecognitionModel.ArtifactId,
                        characterCount = textRegionCharacters.Count,
                        provider = "DirectML",
                        adapterIndex = 0,
                    }));
                    return 0;
                }
                var textRegionsLoadTimer = Stopwatch.StartNew();
                using var detector = new TextRegionDetector(parsedTextRegions.DetectionModelPath, textRegionsConfig);
                using var textRegionsRecognizer = new TextLineRecognizer(
                    parsedTextRegions.RecognitionModelPath,
                    new RuntimeConfig(textRegionsConfig.RecognitionModel, textRegionsConfig.Characters, textRegionsConfig.Device),
                    textRegionCharacters);
                textRegionsLoadTimer.Stop();
                return TextRegionsWorkerProtocol.Run(
                    Console.OpenStandardInput(),
                    Console.OpenStandardOutput(),
                    textRegionsConfig,
                    detector,
                    textRegionsRecognizer,
                    Environment.ProcessId,
                    Math.Round(textRegionsLoadTimer.Elapsed.TotalMilliseconds, 2));
            }
            var parsed = RuntimeArguments.Parse(arguments);
            var config = RuntimeConfig.Load(parsed.ConfigPath);
            var characters = config.ValidateArtifacts(parsed.ModelPath, parsed.CharactersPath);
            if (parsed.ValidateOnly)
            {
                Console.WriteLine(System.Text.Json.JsonSerializer.Serialize(new
                {
                    status = "ok",
                    runtime = RuntimeConfig.RuntimeId,
                    pipeline = RuntimeConfig.Pipeline,
                    artifactId = config.Model.ArtifactId,
                    characterCount = characters.Count,
                    provider = "DirectML",
                    adapterIndex = 0,
                }));
                return 0;
            }

            if (parsed.Worker)
            {
                var workerLoadTimer = Stopwatch.StartNew();
                using var workerRecognizer = new TextLineRecognizer(parsed.ModelPath, config, characters);
                workerLoadTimer.Stop();
                return WorkerProtocol.Run(
                    Console.OpenStandardInput(),
                    Console.OpenStandardOutput(),
                    config,
                    workerRecognizer,
                    Environment.ProcessId,
                    Math.Round(workerLoadTimer.Elapsed.TotalMilliseconds, 2));
            }

            var request = RecognitionRequest.Load(parsed.RequestPath!, parsed.FrameRoot!);
            var region = request.ReadRegion();
            var totalTimer = Stopwatch.StartNew();
            var loadTimer = Stopwatch.StartNew();
            using var recognizer = new TextLineRecognizer(parsed.ModelPath, config, characters);
            loadTimer.Stop();
            var result = recognizer.Recognize(region);
            totalTimer.Stop();
            ResponseWriter.WriteNewAtomic(parsed.ResponsePath!, new
            {
                schemaVersion = 1,
                status = "ok",
                requestId = request.RequestId,
                runtime = RuntimeConfig.RuntimeId,
                pipeline = RuntimeConfig.Pipeline,
                provider = "DirectML",
                adapterIndex = 0,
                completedAt = Contract.FormatTimestamp(DateTimeOffset.UtcNow),
                model = new
                {
                    artifactId = config.Model.ArtifactId,
                    filename = config.Model.Filename,
                    sha256 = config.Model.Sha256,
                    characterCount = characters.Count,
                },
                region = new
                {
                    artifactId = request.Region.ArtifactId,
                    capturedAt = Contract.FormatTimestamp(request.Region.CapturedAt),
                    width = region.Width,
                    height = region.Height,
                    rgbSha256 = region.RgbSha256,
                },
                recognition = new
                {
                    text = result.Text,
                    confidence = result.Confidence,
                },
                timing = new
                {
                    modelLoadMs = Math.Round(loadTimer.Elapsed.TotalMilliseconds, 2),
                    preprocessMs = result.PreprocessMs,
                    inferenceMs = result.InferenceMs,
                    postprocessMs = result.PostprocessMs,
                    totalMs = Math.Round(totalTimer.Elapsed.TotalMilliseconds, 2),
                    inputWidth = result.InputWidth,
                    inputHeight = config.Model.InputHeight,
                },
            });
            Console.WriteLine(System.Text.Json.JsonSerializer.Serialize(new
            {
                status = "ok",
                requestId = request.RequestId,
                response = parsed.ResponsePath,
            }));
            return 0;
        }
        catch (Exception exception)
        {
            Console.Error.WriteLine($"[FATAL] {exception.Message}");
            return 1;
        }
    }
}
