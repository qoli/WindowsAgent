using System.Diagnostics;
using System.Text.Json;
using PpOcr.DirectML;

var values = new Dictionary<string, string>(StringComparer.Ordinal);
for (var index = 0; index < args.Length; index++)
{
    var argument = args[index];
    if (argument is not ("--config" or "--model" or "--characters" or "--request" or "--frame-root" or "--iterations"))
    {
        throw new ContractException($"unknown argument: {argument}");
    }
    if (values.ContainsKey(argument) || ++index == args.Length)
    {
        throw new ContractException($"duplicate argument or missing value: {argument}");
    }
    values.Add(argument, args[index]);
}
var required = new[] { "--config", "--model", "--characters", "--request", "--frame-root", "--iterations" };
var missing = required.Where(key => !values.ContainsKey(key)).ToArray();
if (missing.Length > 0)
{
    throw new ContractException($"missing required arguments: {string.Join(", ", missing)}");
}
if (!int.TryParse(values["--iterations"], out var iterations) || iterations is < 1 or > 100)
{
    throw new ContractException("--iterations must be an integer from 1 through 100");
}

var config = RuntimeConfig.Load(values["--config"]);
var characters = config.ValidateArtifacts(values["--model"], values["--characters"]);
var request = RecognitionRequest.Load(values["--request"], values["--frame-root"]);
var region = request.ReadRegion();
var loadTimer = Stopwatch.StartNew();
using var recognizer = new TextLineRecognizer(values["--model"], config, characters);
loadTimer.Stop();
var runs = new List<object>(iterations);
for (var sequence = 1; sequence <= iterations; sequence++)
{
    var timer = Stopwatch.StartNew();
    var result = recognizer.Recognize(region);
    timer.Stop();
    runs.Add(new
    {
        sequence,
        wallMs = Math.Round(timer.Elapsed.TotalMilliseconds, 2),
        preprocessMs = result.PreprocessMs,
        inferenceMs = result.InferenceMs,
        postprocessMs = result.PostprocessMs,
        text = result.Text,
        confidence = result.Confidence,
    });
}
Console.WriteLine(JsonSerializer.Serialize(new
{
    schemaVersion = 1,
    runtime = RuntimeConfig.RuntimeId,
    pipeline = RuntimeConfig.Pipeline,
    provider = "DirectML",
    adapterIndex = 0,
    modelLoadMs = Math.Round(loadTimer.Elapsed.TotalMilliseconds, 2),
    iterations,
    runs,
}));
