namespace PpOcr.DirectML;

public sealed record RuntimeArguments(
    string ConfigPath,
    string ModelPath,
    string CharactersPath,
    string? RequestPath,
    string? FrameRoot,
    string? ResponsePath,
    bool ValidateOnly,
    bool Worker)
{
    public static RuntimeArguments Parse(string[] arguments)
    {
        var values = new Dictionary<string, string>(StringComparer.Ordinal);
        var validateOnly = false;
        var worker = false;
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
            if (argument == "--worker")
            {
                if (worker)
                {
                    throw new ContractException("duplicate argument: --worker");
                }
                worker = true;
                continue;
            }
            if (argument is not ("--config" or "--model" or "--characters" or "--request" or "--frame-root" or "--response"))
            {
                throw new ContractException($"unknown argument: {argument}");
            }
            if (!values.TryAdd(argument, string.Empty))
            {
                throw new ContractException($"duplicate argument: {argument}");
            }
            if (++index == arguments.Length)
            {
                throw new ContractException($"missing value for argument: {argument}");
            }
            values[argument] = arguments[index];
        }

        var common = new[] { "--config", "--model", "--characters" };
        var missing = common.Where(key => !values.ContainsKey(key)).ToArray();
        if (missing.Length > 0)
        {
            throw new ContractException($"missing required arguments: {string.Join(", ", missing)}");
        }
        var run = new[] { "--request", "--frame-root", "--response" };
        if (validateOnly && worker)
        {
            throw new ContractException("--validate-only and --worker are mutually exclusive");
        }
        if (validateOnly || worker)
        {
            var unexpected = run.Where(values.ContainsKey).ToArray();
            if (unexpected.Length > 0)
            {
                var mode = validateOnly ? "--validate-only" : "--worker";
                throw new ContractException($"{mode} must not include run arguments: {string.Join(", ", unexpected)}");
            }
            return new RuntimeArguments(
                values["--config"], values["--model"], values["--characters"],
                null, null, null, validateOnly, worker);
        }
        missing = run.Where(key => !values.ContainsKey(key)).ToArray();
        if (missing.Length > 0)
        {
            throw new ContractException($"missing required arguments: {string.Join(", ", missing)}");
        }
        if (Path.GetFullPath(values["--request"]).Equals(Path.GetFullPath(values["--response"]), StringComparison.OrdinalIgnoreCase))
        {
            throw new ContractException("request and response paths must differ");
        }
        return new RuntimeArguments(
            values["--config"], values["--model"], values["--characters"],
            values["--request"], values["--frame-root"], values["--response"], false, false);
    }
}
