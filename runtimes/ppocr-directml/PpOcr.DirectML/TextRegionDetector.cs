using System.Diagnostics;
using Microsoft.ML.OnnxRuntime;
using Microsoft.ML.OnnxRuntime.Tensors;

namespace PpOcr.DirectML;

public readonly record struct PointD(double X, double Y)
{
    public static PointD operator -(PointD left, PointD right) => new(left.X - right.X, left.Y - right.Y);
}

public sealed record DetectedQuadrilateral(
    IReadOnlyList<PointD> Points,
    double Confidence);

public sealed record DetectionResult(
    IReadOnlyList<DetectedQuadrilateral> Regions,
    int InputWidth,
    int InputHeight,
    double PreprocessMs,
    double InferenceMs,
    double PostprocessMs);

public sealed class TextRegionDetector : IDisposable
{
    private static readonly double[] Mean = [0.485, 0.456, 0.406];
    private static readonly double[] StandardDeviation = [0.229, 0.224, 0.225];

    private readonly TextRegionsRuntimeConfig _config;
    private readonly InferenceSession _session;

    public TextRegionDetector(string modelPath, TextRegionsRuntimeConfig config)
    {
        _config = config;
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
            throw new ContractException($"initialize pinned PP-OCR detection ONNX model with DirectML adapter 0: {exception.Message}", exception);
        }
        options.Dispose();
        ValidateModelContract();
    }

    public DetectionResult Detect(CapturedRegion region)
    {
        var timer = Stopwatch.StartNew();
        var prepared = Preprocess(region, _config.Detection.InputWidth);
        timer.Stop();
        var preprocessMs = timer.Elapsed.TotalMilliseconds;

        timer.Restart();
        Tensor<float> output;
        try
        {
            var inputs = new[] { NamedOnnxValue.CreateFromTensor(_config.DetectionModel.InputName, prepared.Tensor) };
            using var outputs = _session.Run(inputs, new[] { _config.DetectionModel.OutputName });
            var sessionOutput = outputs.Single().AsTensor<float>();
            output = new DenseTensor<float>(sessionOutput.ToArray(), sessionOutput.Dimensions.ToArray());
            timer.Stop();
        }
        catch (Exception exception)
        {
            throw new ContractException($"PP-OCR detection DirectML inference failed: {exception.Message}", exception);
        }
        var inferenceMs = timer.Elapsed.TotalMilliseconds;

        timer.Restart();
        var regions = Postprocess(
            output,
            region.Width,
            region.Height,
            prepared.Scale,
            _config.Detection);
        timer.Stop();
        return new DetectionResult(
            regions,
            prepared.Width,
            prepared.Height,
            Math.Round(preprocessMs, 2),
            Math.Round(inferenceMs, 2),
            Math.Round(timer.Elapsed.TotalMilliseconds, 2));
    }

    public static (DenseTensor<float> Tensor, int Width, int Height, double Scale) Preprocess(CapturedRegion region, int inputWidth)
    {
        if (region.Width <= 0 || region.Height <= 0 || region.Rgb.Length != checked(region.Width * region.Height * 3))
        {
            throw new ContractException("detection RGB buffer length does not match positive dimensions");
        }
        if (inputWidth <= 0 || inputWidth % 32 != 0)
        {
            throw new ContractException("detection input width must be positive and divisible by 32");
        }
        var scale = (double)inputWidth / region.Width;
        var contentHeight = Math.Max(1, (int)Math.Round(region.Height * scale));
        var inputHeight = checked((contentHeight + 31) / 32 * 32);
        if (inputHeight > 2048)
        {
            throw new ContractException($"detection input height {inputHeight} exceeds 2048");
        }
        var tensor = new DenseTensor<float>(new[] { 1, 3, inputHeight, inputWidth });
        for (var channel = 0; channel < 3; channel++)
        {
            var black = (float)(-Mean[channel] / StandardDeviation[channel]);
            for (var y = 0; y < inputHeight; y++)
            {
                for (var x = 0; x < inputWidth; x++)
                {
                    tensor[0, channel, y, x] = black;
                }
            }
        }
        var scaleX = (double)region.Width / inputWidth;
        var scaleY = (double)region.Height / contentHeight;
        for (var y = 0; y < contentHeight; y++)
        {
            var sourceY = (y + 0.5) * scaleY - 0.5;
            var yBase = (int)Math.Floor(sourceY);
            var y0 = Math.Clamp(yBase, 0, region.Height - 1);
            var y1 = Math.Clamp(yBase + 1, 0, region.Height - 1);
            var weightY = sourceY - Math.Floor(sourceY);
            for (var x = 0; x < inputWidth; x++)
            {
                var sourceX = (x + 0.5) * scaleX - 0.5;
                var xBase = (int)Math.Floor(sourceX);
                var x0 = Math.Clamp(xBase, 0, region.Width - 1);
                var x1 = Math.Clamp(xBase + 1, 0, region.Width - 1);
                var weightX = sourceX - Math.Floor(sourceX);
                for (var tensorChannel = 0; tensorChannel < 3; tensorChannel++)
                {
                    var rgbChannel = tensorChannel switch { 0 => 2, 1 => 1, _ => 0 };
                    var topLeft = region.Rgb[(y0 * region.Width + x0) * 3 + rgbChannel];
                    var topRight = region.Rgb[(y0 * region.Width + x1) * 3 + rgbChannel];
                    var bottomLeft = region.Rgb[(y1 * region.Width + x0) * 3 + rgbChannel];
                    var bottomRight = region.Rgb[(y1 * region.Width + x1) * 3 + rgbChannel];
                    var top = topLeft + (topRight - topLeft) * weightX;
                    var bottom = bottomLeft + (bottomRight - bottomLeft) * weightX;
                    var value = (top + (bottom - top) * weightY) / 255.0;
                    tensor[0, tensorChannel, y, x] = (float)((value - Mean[tensorChannel]) / StandardDeviation[tensorChannel]);
                }
            }
        }
        return (tensor, inputWidth, inputHeight, scale);
    }

    public static IReadOnlyList<DetectedQuadrilateral> Postprocess(
        Tensor<float> output,
        int sourceWidth,
        int sourceHeight,
        double scale,
        DetectionSettings settings)
    {
        var dimensions = output.Dimensions.ToArray();
        if (dimensions.Length != 4 || dimensions[0] != 1 || dimensions[1] != 1 || dimensions[2] <= 0 || dimensions[3] <= 0)
        {
            throw new ContractException($"detection output shape must equal [1,1,H,W], actual=[{string.Join(',', dimensions)}]");
        }
        if (sourceWidth <= 0 || sourceHeight <= 0 || !double.IsFinite(scale) || scale <= 0)
        {
            throw new ContractException("detection postprocess source geometry is invalid");
        }
        var height = dimensions[2];
        var width = dimensions[3];
        var visited = new bool[checked(width * height)];
        var queue = new int[visited.Length];
        var results = new List<DetectedQuadrilateral>();
        for (var y = 0; y < height && results.Count < settings.MaxCandidates; y++)
        {
            for (var x = 0; x < width && results.Count < settings.MaxCandidates; x++)
            {
                var index = y * width + x;
                var initial = output[0, 0, y, x];
                if (!float.IsFinite(initial))
                {
                    throw new ContractException($"detection output contains non-finite score at {x},{y}");
                }
                if (visited[index] || initial <= settings.PixelThreshold)
                {
                    continue;
                }
                var head = 0;
                var tail = 0;
                queue[tail++] = index;
                visited[index] = true;
                var points = new List<PointD>();
                while (head < tail)
                {
                    var current = queue[head++];
                    var currentX = current % width;
                    var currentY = current / width;
                    points.Add(new PointD(currentX, currentY));
                    for (var offsetY = -1; offsetY <= 1; offsetY++)
                    {
                        for (var offsetX = -1; offsetX <= 1; offsetX++)
                        {
                            if (offsetX == 0 && offsetY == 0)
                            {
                                continue;
                            }
                            var neighborX = currentX + offsetX;
                            var neighborY = currentY + offsetY;
                            if (neighborX < 0 || neighborX >= width || neighborY < 0 || neighborY >= height)
                            {
                                continue;
                            }
                            var neighbor = neighborY * width + neighborX;
                            if (visited[neighbor])
                            {
                                continue;
                            }
                            var score = output[0, 0, neighborY, neighborX];
                            if (!float.IsFinite(score))
                            {
                                throw new ContractException($"detection output contains non-finite score at {neighborX},{neighborY}");
                            }
                            if (score > settings.PixelThreshold)
                            {
                                visited[neighbor] = true;
                                queue[tail++] = neighbor;
                            }
                        }
                    }
                }
                if (points.Count < 3)
                {
                    continue;
                }
                var rectangle = MinimumAreaRectangle(points);
                if (Math.Min(rectangle.Width, rectangle.Height) < 3)
                {
                    continue;
                }
                var confidence = RectangleScore(output, rectangle);
                if (confidence < settings.BoxThreshold)
                {
                    continue;
                }
                var distance = rectangle.Width * rectangle.Height * settings.UnclipRatio /
                    (2 * (rectangle.Width + rectangle.Height));
                var expanded = rectangle with
                {
                    Width = rectangle.Width + 2 * distance,
                    Height = rectangle.Height + 2 * distance,
                };
                if (Math.Min(expanded.Width, expanded.Height) < 5)
                {
                    continue;
                }
                var mapped = OrderClockwise(expanded.Corners()
                    .Select(point => new PointD(
                        Math.Clamp(point.X / scale, 0, sourceWidth - 1),
                        Math.Clamp(point.Y / scale, 0, sourceHeight - 1)))
                    .ToArray());
                results.Add(new DetectedQuadrilateral(mapped, Math.Round(confidence, 6)));
            }
        }
        return results
            .OrderBy(region => region.Points.Min(point => point.Y))
            .ThenBy(region => region.Points.Min(point => point.X))
            .ToArray();
    }

    public static CapturedRegion Rectify(CapturedRegion source, IReadOnlyList<PointD> points, int width, int height)
    {
        if (points.Count != 4 || width <= 0 || height <= 0)
        {
            throw new ContractException("text quadrilateral and positive output dimensions are required");
        }
        var ordered = OrderClockwise(points.ToArray());
        var rgb = new byte[checked(width * height * 3)];
        for (var y = 0; y < height; y++)
        {
            var v = (y + 0.5) / height;
            for (var x = 0; x < width; x++)
            {
                var u = (x + 0.5) / width;
                var top = Lerp(ordered[0], ordered[1], u);
                var bottom = Lerp(ordered[3], ordered[2], u);
                var sample = Lerp(top, bottom, v);
                SampleBilinear(source, sample.X, sample.Y, rgb, (y * width + x) * 3);
            }
        }
        var digest = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(rgb)).ToLowerInvariant();
        return new CapturedRegion(width, height, rgb, digest);
    }

    private readonly record struct OrientedRectangle(
        PointD Center,
        PointD AxisX,
        PointD AxisY,
        double Width,
        double Height)
    {
        public IReadOnlyList<PointD> Corners()
        {
            var halfWidth = Width / 2;
            var halfHeight = Height / 2;
            return
            [
                Add(Center, AxisX, -halfWidth, AxisY, -halfHeight),
                Add(Center, AxisX, halfWidth, AxisY, -halfHeight),
                Add(Center, AxisX, halfWidth, AxisY, halfHeight),
                Add(Center, AxisX, -halfWidth, AxisY, halfHeight),
            ];
        }
    }

    private static OrientedRectangle MinimumAreaRectangle(IReadOnlyList<PointD> points)
    {
        var hull = ConvexHull(points);
        if (hull.Count < 2)
        {
            throw new ContractException("detection component does not define a rectangle");
        }
        OrientedRectangle? best = null;
        var bestArea = double.PositiveInfinity;
        for (var index = 0; index < hull.Count; index++)
        {
            var start = hull[index];
            var end = hull[(index + 1) % hull.Count];
            var deltaX = end.X - start.X;
            var deltaY = end.Y - start.Y;
            var length = Math.Sqrt(deltaX * deltaX + deltaY * deltaY);
            if (length <= 0)
            {
                continue;
            }
            var axisX = new PointD(deltaX / length, deltaY / length);
            var axisY = new PointD(-axisX.Y, axisX.X);
            var minX = double.PositiveInfinity;
            var maxX = double.NegativeInfinity;
            var minY = double.PositiveInfinity;
            var maxY = double.NegativeInfinity;
            foreach (var point in hull)
            {
                var projectedX = Dot(point, axisX);
                var projectedY = Dot(point, axisY);
                minX = Math.Min(minX, projectedX);
                maxX = Math.Max(maxX, projectedX);
                minY = Math.Min(minY, projectedY);
                maxY = Math.Max(maxY, projectedY);
            }
            var width = maxX - minX + 1;
            var height = maxY - minY + 1;
            var area = width * height;
            if (area < bestArea)
            {
                var center = new PointD(
                    axisX.X * ((minX + maxX) / 2) + axisY.X * ((minY + maxY) / 2),
                    axisX.Y * ((minX + maxX) / 2) + axisY.Y * ((minY + maxY) / 2));
                best = new OrientedRectangle(center, axisX, axisY, width, height);
                bestArea = area;
            }
        }
        return best ?? throw new ContractException("detection component minimum rectangle is unavailable");
    }

    private static double RectangleScore(Tensor<float> output, OrientedRectangle rectangle)
    {
        var dimensions = output.Dimensions.ToArray();
        var height = dimensions[2];
        var width = dimensions[3];
        var corners = rectangle.Corners();
        var minimumX = Math.Max(0, (int)Math.Floor(corners.Min(point => point.X)));
        var maximumX = Math.Min(width - 1, (int)Math.Ceiling(corners.Max(point => point.X)));
        var minimumY = Math.Max(0, (int)Math.Floor(corners.Min(point => point.Y)));
        var maximumY = Math.Min(height - 1, (int)Math.Ceiling(corners.Max(point => point.Y)));
        var total = 0.0;
        var count = 0;
        for (var y = minimumY; y <= maximumY; y++)
        {
            for (var x = minimumX; x <= maximumX; x++)
            {
                var relative = new PointD(x - rectangle.Center.X, y - rectangle.Center.Y);
                if (Math.Abs(Dot(relative, rectangle.AxisX)) <= rectangle.Width / 2 &&
                    Math.Abs(Dot(relative, rectangle.AxisY)) <= rectangle.Height / 2)
                {
                    total += output[0, 0, y, x];
                    count++;
                }
            }
        }
        return count == 0 ? 0 : total / count;
    }

    private static IReadOnlyList<PointD> ConvexHull(IReadOnlyList<PointD> points)
    {
        var sorted = points.Distinct().OrderBy(point => point.X).ThenBy(point => point.Y).ToArray();
        if (sorted.Length <= 1)
        {
            return sorted;
        }
        var lower = new List<PointD>();
        foreach (var point in sorted)
        {
            while (lower.Count >= 2 && Cross(lower[^1] - lower[^2], point - lower[^1]) <= 0)
            {
                lower.RemoveAt(lower.Count - 1);
            }
            lower.Add(point);
        }
        var upper = new List<PointD>();
        for (var index = sorted.Length - 1; index >= 0; index--)
        {
            var point = sorted[index];
            while (upper.Count >= 2 && Cross(upper[^1] - upper[^2], point - upper[^1]) <= 0)
            {
                upper.RemoveAt(upper.Count - 1);
            }
            upper.Add(point);
        }
        lower.RemoveAt(lower.Count - 1);
        upper.RemoveAt(upper.Count - 1);
        lower.AddRange(upper);
        return lower;
    }

    private static IReadOnlyList<PointD> OrderClockwise(IReadOnlyList<PointD> points)
    {
        if (points.Count != 4)
        {
            throw new ContractException("quadrilateral must contain exactly four points");
        }
        var topLeft = points.MinBy(point => point.X + point.Y);
        var bottomRight = points.MaxBy(point => point.X + point.Y);
        var topRight = points.MinBy(point => point.Y - point.X);
        var bottomLeft = points.MaxBy(point => point.Y - point.X);
        return [topLeft, topRight, bottomRight, bottomLeft];
    }

    private static void SampleBilinear(CapturedRegion source, double x, double y, byte[] target, int targetOffset)
    {
        var xBase = (int)Math.Floor(x);
        var yBase = (int)Math.Floor(y);
        var x0 = Math.Clamp(xBase, 0, source.Width - 1);
        var x1 = Math.Clamp(xBase + 1, 0, source.Width - 1);
        var y0 = Math.Clamp(yBase, 0, source.Height - 1);
        var y1 = Math.Clamp(yBase + 1, 0, source.Height - 1);
        var weightX = x - Math.Floor(x);
        var weightY = y - Math.Floor(y);
        for (var channel = 0; channel < 3; channel++)
        {
            var topLeft = source.Rgb[(y0 * source.Width + x0) * 3 + channel];
            var topRight = source.Rgb[(y0 * source.Width + x1) * 3 + channel];
            var bottomLeft = source.Rgb[(y1 * source.Width + x0) * 3 + channel];
            var bottomRight = source.Rgb[(y1 * source.Width + x1) * 3 + channel];
            var top = topLeft + (topRight - topLeft) * weightX;
            var bottom = bottomLeft + (bottomRight - bottomLeft) * weightX;
            target[targetOffset + channel] = (byte)Math.Clamp((int)Math.Round(top + (bottom - top) * weightY), 0, 255);
        }
    }

    private static PointD Lerp(PointD left, PointD right, double amount) =>
        new(left.X + (right.X - left.X) * amount, left.Y + (right.Y - left.Y) * amount);

    private static PointD Add(PointD center, PointD axisX, double amountX, PointD axisY, double amountY) =>
        new(center.X + axisX.X * amountX + axisY.X * amountY, center.Y + axisX.Y * amountX + axisY.Y * amountY);

    private static double Dot(PointD left, PointD right) => left.X * right.X + left.Y * right.Y;
    private static double Cross(PointD left, PointD right) => left.X * right.Y - left.Y * right.X;

    private void ValidateModelContract()
    {
        if (!_session.InputMetadata.TryGetValue(_config.DetectionModel.InputName, out var input))
        {
            throw new ContractException($"detection ONNX model is missing declared input: {_config.DetectionModel.InputName}");
        }
        var inputDimensions = input.Dimensions.ToArray();
        if (inputDimensions.Length != 4 || inputDimensions[1] != 3)
        {
            throw new ContractException("detection ONNX input shape must equal [N,3,H,W]");
        }
        if (!_session.OutputMetadata.TryGetValue(_config.DetectionModel.OutputName, out var output))
        {
            throw new ContractException($"detection ONNX model is missing declared output: {_config.DetectionModel.OutputName}");
        }
        var outputDimensions = output.Dimensions.ToArray();
        if (outputDimensions.Length != 4 || outputDimensions[1] != 1)
        {
            throw new ContractException("detection ONNX output shape must equal [N,1,H,W]");
        }
    }

    public void Dispose() => _session.Dispose();
}
