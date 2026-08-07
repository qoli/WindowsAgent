using System.Diagnostics;
using Microsoft.ML.OnnxRuntime;
using Microsoft.ML.OnnxRuntime.Tensors;

namespace PpOcr.DirectML;

public sealed record RecognitionResult(
    string Text,
    double Confidence,
    string Constraint,
    string RawText,
    double RawConfidence,
    string ConstrainedText,
    double ConstrainedConfidence,
    double RawConstraintMargin,
    int InputWidth,
    double PreprocessMs,
    double InferenceMs,
    double PostprocessMs);

public sealed class TextLineRecognizer : IDisposable
{
    private readonly RuntimeConfig _config;
    private readonly IReadOnlyList<string> _characters;
    private readonly InferenceSession _session;

    public TextLineRecognizer(string modelPath, RuntimeConfig config, IReadOnlyList<string> characters)
    {
        _config = config;
        _characters = characters;
        var options = new SessionOptions
        {
            GraphOptimizationLevel = GraphOptimizationLevel.ORT_ENABLE_ALL,
            ExecutionMode = ExecutionMode.ORT_SEQUENTIAL,
            EnableMemoryPattern = false,
        };
        options.AddSessionConfigEntry("session.disable_cpu_ep_fallback", "1");
        options.AppendExecutionProvider_DML(0);
        try
        {
            _session = new InferenceSession(modelPath, options);
        }
        catch (Exception exception)
        {
            options.Dispose();
            throw new ContractException($"initialize pinned PP-OCR ONNX model with DirectML adapter 0: {exception.Message}", exception);
        }
        options.Dispose();
        ValidateModelContract();
    }

    public RecognitionResult Recognize(CapturedRegion region, string constraint = "none")
    {
        var timer = Stopwatch.StartNew();
        var tensor = Preprocess(region, _config.Model.InputHeight, _config.Model.InputWidth);
        timer.Stop();
        var preprocessMs = timer.Elapsed.TotalMilliseconds;

        timer.Restart();
        Tensor<float> output;
        try
        {
            var inputs = new[] { NamedOnnxValue.CreateFromTensor(_config.Model.InputName, tensor) };
            using var outputs = _session.Run(inputs, new[] { _config.Model.OutputName });
            var sessionOutput = outputs.Single().AsTensor<float>();
            output = new DenseTensor<float>(sessionOutput.ToArray(), sessionOutput.Dimensions.ToArray());
            timer.Stop();
        }
        catch (Exception exception)
        {
            throw new ContractException($"PP-OCR DirectML inference failed: {exception.Message}", exception);
        }
        var inferenceMs = timer.Elapsed.TotalMilliseconds;

        timer.Restart();
        var decoded = DecodeCtc(output, _characters, constraint);
        timer.Stop();
        return new RecognitionResult(
            decoded.ConstrainedText,
            Math.Round(decoded.ConstrainedConfidence, 6),
            decoded.Constraint,
            decoded.RawText,
            Math.Round(decoded.RawConfidence, 6),
            decoded.ConstrainedText,
            Math.Round(decoded.ConstrainedConfidence, 6),
            Math.Round(Math.Max(0, decoded.RawConfidence - decoded.ConstrainedConfidence), 6),
            _config.Model.InputWidth,
            Math.Round(preprocessMs, 2),
            Math.Round(inferenceMs, 2),
            Math.Round(timer.Elapsed.TotalMilliseconds, 2));
    }

    public static DenseTensor<float> Preprocess(CapturedRegion region, int inputHeight, int inputWidth)
    {
        if (region.Rgb.Length != checked(region.Width * region.Height * 3))
        {
            throw new ContractException("captured RGB buffer length does not match its dimensions");
        }
        if (inputHeight < 1 || inputWidth < 1)
        {
            throw new ContractException("recognition input dimensions must be positive");
        }
        var tensor = new DenseTensor<float>(new[] { 1, 3, inputHeight, inputWidth });
        var contentWidth = Math.Min(inputWidth, Math.Max(1,
            (int)Math.Ceiling(inputHeight * ((double)region.Width / region.Height))));
        var scaleX = (double)region.Width / contentWidth;
        var scaleY = (double)region.Height / inputHeight;
        for (var y = 0; y < inputHeight; y++)
        {
            var sourceY = (y + 0.5) * scaleY - 0.5;
            var sourceY0 = (int)Math.Floor(sourceY);
            var sourceY1 = sourceY0 + 1;
            var y0 = Math.Clamp(sourceY0, 0, region.Height - 1);
            var y1 = Math.Clamp(sourceY1, 0, region.Height - 1);
            var weightY = sourceY - Math.Floor(sourceY);
            for (var x = 0; x < contentWidth; x++)
            {
                var sourceX = (x + 0.5) * scaleX - 0.5;
                var sourceX0 = (int)Math.Floor(sourceX);
                var sourceX1 = sourceX0 + 1;
                var x0 = Math.Clamp(sourceX0, 0, region.Width - 1);
                var x1 = Math.Clamp(sourceX1, 0, region.Width - 1);
                var weightX = sourceX - Math.Floor(sourceX);
                for (var channel = 0; channel < 3; channel++)
                {
                    var topLeft = region.Rgb[(y0 * region.Width + x0) * 3 + channel];
                    var topRight = region.Rgb[(y0 * region.Width + x1) * 3 + channel];
                    var bottomLeft = region.Rgb[(y1 * region.Width + x0) * 3 + channel];
                    var bottomRight = region.Rgb[(y1 * region.Width + x1) * 3 + channel];
                    var top = topLeft + (topRight - topLeft) * weightX;
                    var bottom = bottomLeft + (bottomRight - bottomLeft) * weightX;
                    tensor[0, channel, y, x] = (float)((top + (bottom - top) * weightY) / 127.5 - 1.0);
                }
            }
        }
        return tensor;
    }

    public sealed record CtcDecodeResult(
        string Constraint,
        string RawText,
        double RawConfidence,
        string ConstrainedText,
        double ConstrainedConfidence);

    public static CtcDecodeResult DecodeCtc(
        Tensor<float> output,
        IReadOnlyList<string> characters,
        string constraint = "none")
    {
        var dimensions = output.Dimensions.ToArray();
        if (dimensions.Length != 3 || dimensions[0] != 1 || dimensions[2] != characters.Count + 1)
        {
            throw new ContractException(
                $"recognition output shape must equal [1,T,{characters.Count + 1}], actual=[{string.Join(',', dimensions)}]");
        }
        if (constraint is not ("none" or "digits"))
        {
            throw new ContractException("recognition character constraint must equal none or digits");
        }
        var digitIndexes = new HashSet<int>();
        if (constraint == "digits")
        {
            for (var digit = 0; digit <= 9; digit++)
            {
                var index = -1;
                for (var characterIndex = 0; characterIndex < characters.Count; characterIndex++)
                {
                    if (characters[characterIndex] == digit.ToString())
                    {
                        index = characterIndex + 1;
                        break;
                    }
                }
                if (index < 0)
                {
                    throw new ContractException($"recognition character dictionary is missing digit {digit}");
                }
                digitIndexes.Add(index);
            }
        }

        var rawText = new System.Text.StringBuilder();
        var constrainedText = new System.Text.StringBuilder();
        var rawConfidence = 0.0;
        var constrainedConfidence = 0.0;
        var rawKept = 0;
        var constrainedKept = 0;
        var rawPrevious = -1;
        var constrainedPrevious = -1;
        for (var time = 0; time < dimensions[1]; time++)
        {
            var rawBestIndex = 0;
            var rawBestValue = output[0, time, 0];
            var constrainedBestIndex = 0;
            var constrainedBestValue = rawBestValue;
            if (!float.IsFinite(rawBestValue))
            {
                throw new ContractException($"recognition output contains non-finite score at time {time}, class 0");
            }
            for (var classIndex = 1; classIndex < dimensions[2]; classIndex++)
            {
                var value = output[0, time, classIndex];
                if (!float.IsFinite(value))
                {
                    throw new ContractException($"recognition output contains non-finite score at time {time}, class {classIndex}");
                }
                if (value > rawBestValue)
                {
                    rawBestValue = value;
                    rawBestIndex = classIndex;
                }
                if ((constraint == "none" || digitIndexes.Contains(classIndex)) && value > constrainedBestValue)
                {
                    constrainedBestValue = value;
                    constrainedBestIndex = classIndex;
                }
            }
            if (rawBestIndex != 0 && rawBestIndex != rawPrevious)
            {
                rawText.Append(characters[rawBestIndex - 1]);
                rawConfidence += rawBestValue;
                rawKept++;
            }
            if (constrainedBestIndex != 0 && constrainedBestIndex != constrainedPrevious)
            {
                constrainedText.Append(characters[constrainedBestIndex - 1]);
                constrainedConfidence += constrainedBestValue;
                constrainedKept++;
            }
            rawPrevious = rawBestIndex;
            constrainedPrevious = constrainedBestIndex;
        }
        return new CtcDecodeResult(
            constraint,
            rawText.ToString(),
            rawKept == 0 ? 0 : rawConfidence / rawKept,
            constrainedText.ToString(),
            constrainedKept == 0 ? 0 : constrainedConfidence / constrainedKept);
    }

    private void ValidateModelContract()
    {
        if (!_session.InputMetadata.TryGetValue(_config.Model.InputName, out var input))
        {
            throw new ContractException($"ONNX model is missing declared input: {_config.Model.InputName}");
        }
        var inputDimensions = input.Dimensions.ToArray();
        if (inputDimensions.Length != 4 || inputDimensions[0] != 1 || inputDimensions[1] != 3 ||
            inputDimensions[2] != _config.Model.InputHeight || inputDimensions[3] != _config.Model.InputWidth)
        {
            throw new ContractException($"ONNX input shape must equal [1,3,{_config.Model.InputHeight},{_config.Model.InputWidth}]");
        }
        if (!_session.OutputMetadata.TryGetValue(_config.Model.OutputName, out var output))
        {
            throw new ContractException($"ONNX model is missing declared output: {_config.Model.OutputName}");
        }
        var outputDimensions = output.Dimensions.ToArray();
        if (outputDimensions.Length != 3 || outputDimensions[2] != _config.Model.ClassCount)
        {
            throw new ContractException($"ONNX output shape must equal [N,T,{_config.Model.ClassCount}]");
        }
    }

    public void Dispose() => _session.Dispose();
}
