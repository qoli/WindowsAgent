# Read and verify a Windows text file

Read one caller-selected existing small UTF-8 file using the installed Agent's
Windows token. The input path must be authorized for inspection. No mutation,
process launch, desktop interaction, or cleanup is required.

Stat the path, reject a directory with EXPECTED_FILE, then read the text and
emit a progress event. Missing files, access denial, and non-UTF-8 content are
terminal host failures. Return verified=true only after the read succeeds,
together with the Unicode code-point count and elapsed runtime milliseconds.
Do not return private file content in the durable output.

The postcondition is successful reading of this file at execution time, not
application state or future file stability. For a practical experiment, compare
the count with an independently obtained fixture expectation and retain the
terminal invocation evidence. Local preflight does not establish that the
Windows file exists. Keep inputs and experiment notes outside this package.
