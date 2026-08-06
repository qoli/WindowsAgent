using System.Diagnostics;
using Microsoft.ML.OnnxRuntime;
using Microsoft.ML.OnnxRuntime.Tensors;

namespace ScreenParser.DirectML;

public sealed record BoundingBox(double Left, double Top, double Right, double Bottom);

public sealed record Detection(int ClassId, string Label, double Confidence, BoundingBox Box);

public sealed record InferenceResult(IReadOnlyList<Detection> Detections, double DurationMs);

internal sealed record Candidate(int ClassId, double Confidence, double CenterX, double CenterY, double Width, double Height);

public sealed class YoloDetector : IDisposable
{
    private readonly RuntimeConfig _config;
    private readonly InferenceSession _session;

    public YoloDetector(string modelPath, RuntimeConfig config)
    {
        _config = config;
        var options = new SessionOptions
        {
            GraphOptimizationLevel = GraphOptimizationLevel.ORT_ENABLE_ALL,
            ExecutionMode = ExecutionMode.ORT_SEQUENTIAL,
            EnableMemoryPattern = false,
        };
        options.AppendExecutionProvider_DML(0);
        try
        {
            _session = new InferenceSession(modelPath, options);
        }
        catch (Exception exception)
        {
            options.Dispose();
            throw new ContractException($"initialize pinned ONNX model with DirectML adapter 0: {exception.Message}", exception);
        }
        options.Dispose();

        if (!_session.InputMetadata.TryGetValue(config.Model.InputName, out var input))
        {
            throw new ContractException($"ONNX model is missing declared input: {config.Model.InputName}");
        }
        var expectedInput = new[] { 1, 3, config.Model.InputHeight, config.Model.InputWidth };
        if (!input.Dimensions.SequenceEqual(expectedInput))
        {
            throw new ContractException($"ONNX input shape mismatch: expected=[{string.Join(',', expectedInput)}] actual=[{string.Join(',', input.Dimensions)}]");
        }
        if (!_session.OutputMetadata.TryGetValue(config.Model.OutputName, out var output))
        {
            throw new ContractException($"ONNX model is missing declared output: {config.Model.OutputName}");
        }
        if (output.Dimensions.Length != 3 || output.Dimensions[0] != 1 || output.Dimensions[1] != config.Model.Labels.Count + 4 || output.Dimensions[2] < 1)
        {
            throw new ContractException($"ONNX output shape mismatch: expected=[1,{config.Model.Labels.Count + 4},N] actual=[{string.Join(',', output.Dimensions)}]");
        }
    }

    public InferenceResult Infer(CapturedFrame frame)
    {
        var timer = Stopwatch.StartNew();
        var (tensor, scale, padX, padY) = Preprocess(frame, _config.Model.InputWidth, _config.Model.InputHeight);
        var inputs = new[] { NamedOnnxValue.CreateFromTensor(_config.Model.InputName, tensor) };
        try
        {
            using var outputs = _session.Run(inputs, new[] { _config.Model.OutputName });
            var output = outputs.Single().AsTensor<float>();
            var detections = Decode(
                output,
                _config.Model.Labels,
                _config.Inference.Confidence,
                _config.Inference.Iou,
                _config.Inference.MaxDetections,
                frame.Width,
                frame.Height,
                scale,
                padX,
                padY);
            timer.Stop();
            return new InferenceResult(detections, Math.Round(timer.Elapsed.TotalMilliseconds, 2));
        }
        catch (ContractException)
        {
            throw;
        }
        catch (Exception exception)
        {
            throw new ContractException($"DirectML inference failed: {exception.Message}", exception);
        }
    }

    public static (DenseTensor<float> Tensor, double Scale, double PadX, double PadY) Preprocess(CapturedFrame frame, int inputWidth, int inputHeight)
    {
        if (frame.Rgb.Length != checked(frame.Width * frame.Height * 3))
        {
            throw new ContractException("captured RGB buffer length does not match its dimensions");
        }
        var scale = Math.Min((double)inputWidth / frame.Width, (double)inputHeight / frame.Height);
        var resizedWidth = (int)Math.Round(frame.Width * scale, MidpointRounding.AwayFromZero);
        var resizedHeight = (int)Math.Round(frame.Height * scale, MidpointRounding.AwayFromZero);
        var padX = (inputWidth - resizedWidth) / 2.0;
        var padY = (inputHeight - resizedHeight) / 2.0;
        var leftPad = (int)Math.Round(padX - 0.1, MidpointRounding.ToEven);
        var topPad = (int)Math.Round(padY - 0.1, MidpointRounding.ToEven);
        var tensor = new DenseTensor<float>(new[] { 1, 3, inputHeight, inputWidth });
        tensor.Buffer.Span.Fill(114f / 255f);

        for (var y = 0; y < resizedHeight; y++)
        {
            var sourceY = (y + 0.5) / scale - 0.5;
            var sourceY0 = (int)Math.Floor(sourceY);
            var sourceY1 = sourceY0 + 1;
            var weightY = sourceY - sourceY0;
            sourceY0 = Math.Clamp(sourceY0, 0, frame.Height - 1);
            sourceY1 = Math.Clamp(sourceY1, 0, frame.Height - 1);
            var destinationY = y + topPad;
            for (var x = 0; x < resizedWidth; x++)
            {
                var sourceX = (x + 0.5) / scale - 0.5;
                var sourceX0 = (int)Math.Floor(sourceX);
                var sourceX1 = sourceX0 + 1;
                var weightX = sourceX - sourceX0;
                sourceX0 = Math.Clamp(sourceX0, 0, frame.Width - 1);
                sourceX1 = Math.Clamp(sourceX1, 0, frame.Width - 1);
                var destinationX = x + leftPad;
                for (var channel = 0; channel < 3; channel++)
                {
                    var topLeft = frame.Rgb[(sourceY0 * frame.Width + sourceX0) * 3 + channel];
                    var topRight = frame.Rgb[(sourceY0 * frame.Width + sourceX1) * 3 + channel];
                    var bottomLeft = frame.Rgb[(sourceY1 * frame.Width + sourceX0) * 3 + channel];
                    var bottomRight = frame.Rgb[(sourceY1 * frame.Width + sourceX1) * 3 + channel];
                    var top = topLeft + (topRight - topLeft) * weightX;
                    var bottom = bottomLeft + (bottomRight - bottomLeft) * weightX;
                    tensor[0, channel, destinationY, destinationX] = (float)((top + (bottom - top) * weightY) / 255.0);
                }
            }
        }
        return (tensor, scale, padX, padY);
    }

    public static IReadOnlyList<Detection> Decode(
        Tensor<float> output,
        IReadOnlyList<string> labels,
        double confidenceThreshold,
        double iouThreshold,
        int maximumDetections,
        int frameWidth,
        int frameHeight,
        double scale,
        double padX,
        double padY)
    {
        var dimensions = output.Dimensions.ToArray();
        if (dimensions.Length != 3 || dimensions[0] != 1 || dimensions[1] != labels.Count + 4 || dimensions[2] < 1)
        {
            throw new ContractException($"ONNX output shape must equal [1,{labels.Count + 4},N], actual=[{string.Join(',', dimensions)}]");
        }
        var candidates = new List<Candidate>();
        for (var anchor = 0; anchor < dimensions[2]; anchor++)
        {
            var bestClass = -1;
            var bestConfidence = double.NegativeInfinity;
            for (var classId = 0; classId < labels.Count; classId++)
            {
                var score = output[0, classId + 4, anchor];
                if (!float.IsFinite(score))
                {
                    throw new ContractException($"ONNX output contains non-finite class score at anchor {anchor}");
                }
                if (score > bestConfidence)
                {
                    bestConfidence = score;
                    bestClass = classId;
                }
            }
            if (bestConfidence < confidenceThreshold)
            {
                continue;
            }
            var centerX = output[0, 0, anchor];
            var centerY = output[0, 1, anchor];
            var width = output[0, 2, anchor];
            var height = output[0, 3, anchor];
            if (!new[] { centerX, centerY, width, height }.All(float.IsFinite) || width <= 0 || height <= 0)
            {
                throw new ContractException($"ONNX output contains invalid bounding box at anchor {anchor}");
            }
            candidates.Add(new Candidate(bestClass, bestConfidence, centerX, centerY, width, height));
        }

        candidates.Sort((left, right) => right.Confidence.CompareTo(left.Confidence));
        var selected = new List<Detection>(Math.Min(maximumDetections, candidates.Count));
        foreach (var candidate in candidates)
        {
            var box = ToOriginalBox(candidate, frameWidth, frameHeight, scale, padX, padY);
            if (box.Right <= box.Left || box.Bottom <= box.Top)
            {
                continue;
            }
            if (selected.Any(existing => existing.ClassId == candidate.ClassId && IntersectionOverUnion(existing.Box, box) > iouThreshold))
            {
                continue;
            }
            selected.Add(new Detection(candidate.ClassId, labels[candidate.ClassId], Math.Round(candidate.Confidence, 6), box));
            if (selected.Count == maximumDetections)
            {
                break;
            }
        }
        return selected
            .OrderBy(detection => detection.Box.Top)
            .ThenBy(detection => detection.Box.Left)
            .ThenBy(detection => detection.ClassId)
            .ToArray();
    }

    private static BoundingBox ToOriginalBox(Candidate candidate, int frameWidth, int frameHeight, double scale, double padX, double padY)
    {
        var left = Math.Clamp((candidate.CenterX - candidate.Width / 2 - padX) / scale, 0, frameWidth);
        var top = Math.Clamp((candidate.CenterY - candidate.Height / 2 - padY) / scale, 0, frameHeight);
        var right = Math.Clamp((candidate.CenterX + candidate.Width / 2 - padX) / scale, 0, frameWidth);
        var bottom = Math.Clamp((candidate.CenterY + candidate.Height / 2 - padY) / scale, 0, frameHeight);
        return new BoundingBox(Math.Round(left, 2), Math.Round(top, 2), Math.Round(right, 2), Math.Round(bottom, 2));
    }

    private static double IntersectionOverUnion(BoundingBox left, BoundingBox right)
    {
        var intersectionWidth = Math.Max(0, Math.Min(left.Right, right.Right) - Math.Max(left.Left, right.Left));
        var intersectionHeight = Math.Max(0, Math.Min(left.Bottom, right.Bottom) - Math.Max(left.Top, right.Top));
        var intersection = intersectionWidth * intersectionHeight;
        var leftArea = (left.Right - left.Left) * (left.Bottom - left.Top);
        var rightArea = (right.Right - right.Left) * (right.Bottom - right.Top);
        return intersection / (leftArea + rightArea - intersection);
    }

    public void Dispose() => _session.Dispose();
}
