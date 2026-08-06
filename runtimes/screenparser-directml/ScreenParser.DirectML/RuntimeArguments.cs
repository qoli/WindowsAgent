namespace ScreenParser.DirectML;

public sealed record RuntimeArguments(
    string ConfigPath,
    string ModelPath,
    string? RequestPath,
    string? FrameRoot,
    string? ResponsePath,
    bool ValidateOnly)
{
    public static RuntimeArguments Parse(string[] arguments)
    {
        var values = new Dictionary<string, string>(StringComparer.Ordinal);
        var validateOnly = false;
        for (var index = 0; index < arguments.Length; index++)
        {
            var argument = arguments[index];
            if (argument == "--validate-only")
            {
                if (validateOnly)
                {
                    throw new ContractException("duplicate argument: --validate-only");
                }
                validateOnly = true;
                continue;
            }
            if (argument is not ("--config" or "--model" or "--request" or "--frame-root" or "--response"))
            {
                throw new ContractException($"unknown argument: {argument}");
            }
            if (values.ContainsKey(argument))
            {
                throw new ContractException($"duplicate argument: {argument}");
            }
            if (++index == arguments.Length)
            {
                throw new ContractException($"missing value for argument: {argument}");
            }
            values.Add(argument, arguments[index]);
        }

        var commonRequired = new[] { "--config", "--model" };
        var missingCommon = commonRequired.Where(key => !values.ContainsKey(key)).ToArray();
        if (missingCommon.Length > 0)
        {
            throw new ContractException($"missing required arguments: {string.Join(", ", missingCommon)}");
        }

        var runArguments = new[] { "--request", "--frame-root", "--response" };
        if (validateOnly)
        {
            var unexpected = runArguments.Where(values.ContainsKey).ToArray();
            if (unexpected.Length > 0)
            {
                throw new ContractException($"--validate-only must not include run arguments: {string.Join(", ", unexpected)}");
            }
            return new RuntimeArguments(values["--config"], values["--model"], null, null, null, true);
        }

        var missingRun = runArguments.Where(key => !values.ContainsKey(key)).ToArray();
        if (missingRun.Length > 0)
        {
            throw new ContractException($"missing required arguments: {string.Join(", ", missingRun)}");
        }
        if (Path.GetFullPath(values["--request"]).Equals(Path.GetFullPath(values["--response"]), StringComparison.OrdinalIgnoreCase))
        {
            throw new ContractException("request and response paths must differ");
        }
        return new RuntimeArguments(
            values["--config"],
            values["--model"],
            values["--request"],
            values["--frame-root"],
            values["--response"],
            false);
    }
}
