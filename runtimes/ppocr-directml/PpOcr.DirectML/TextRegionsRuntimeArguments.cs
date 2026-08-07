namespace PpOcr.DirectML;

public sealed record TextRegionsRuntimeArguments(
    string ConfigPath,
    string DetectionModelPath,
    string RecognitionModelPath,
    string CharactersPath)
{
    public static TextRegionsRuntimeArguments Parse(string[] arguments)
    {
        var values = new Dictionary<string, string>(StringComparer.Ordinal);
        for (var index = 0; index < arguments.Length; index++)
        {
            var argument = arguments[index];
            if (argument is not ("--config" or "--detection-model" or "--recognition-model" or "--characters"))
            {
                throw new ContractException($"unknown text regions worker argument: {argument}");
            }
            if (!values.TryAdd(argument, string.Empty))
            {
                throw new ContractException($"duplicate text regions worker argument: {argument}");
            }
            if (++index == arguments.Length)
            {
                throw new ContractException($"missing value for text regions worker argument: {argument}");
            }
            values[argument] = arguments[index];
        }
        var required = new[] { "--config", "--detection-model", "--recognition-model", "--characters" };
        var missing = required.Where(key => !values.ContainsKey(key)).ToArray();
        if (missing.Length > 0)
        {
            throw new ContractException($"missing required text regions worker arguments: {string.Join(", ", missing)}");
        }
        return new TextRegionsRuntimeArguments(
            values["--config"], values["--detection-model"], values["--recognition-model"], values["--characters"]);
    }
}
