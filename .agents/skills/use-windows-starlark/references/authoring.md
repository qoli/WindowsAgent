# Package and host contract

## Five-file package

Use exactly these regular files, with no symlinks, extra inputs, nested package
wrapper, or undeclared artifacts:

```text
manifest.json
main.star
TASK.md
input.schema.json
output.schema.json
```

The starter manifest is:

```json
{
  "schemaVersion": 1,
  "version": 1,
  "title": "Read and verify a Windows text file",
  "entrypoint": "main.star",
  "taskDocument": "TASK.md",
  "inputSchema": "input.schema.json",
  "outputSchema": "output.schema.json",
  "files": ["main.star", "TASK.md", "input.schema.json", "output.schema.json"],
  "limits": {
    "wallTimeMs": 5000,
    "maxSteps": 100000,
    "maxResultBytes": 4096,
    "maxProcessOutputBytes": 4096
  }
}
```

`schemaVersion` must be 1; `version` and all four limits must be positive
integers. `title` must be nonempty without surrounding whitespace. Unknown
manifest fields are rejected. `files` contains exactly the other four files,
never `manifest.json`. These example limits fit a small read-only task: choose
limits appropriate to the actual task. Process-output bytes are limited per
stream; result bytes limit serialized JSON, not filesystem reads or memory.

Write a nonempty `TASK.md` describing preconditions, side effects, operation
order, expected result, failure meanings, cleanup, and independent verification.
Use strict JSON with no duplicate keys/trailing data. Schemas use JSON Schema
2020-12 by default; external schema resources are forbidden. Declare input and
output types, required fields, and `additionalProperties: false` for task-owned
objects so unexpected values fail explicitly. Invocation inputs must be an
object. Return JSON-compatible values matching the output schema.

## Starlark dialect

Define exactly one top-level `def main(ctx):`; the parameter must be named
`ctx`, with no defaults or extra parameters. Read `ctx.inputs["field"]`.
Process results and file metadata are dictionaries too, so use `r["exitCode"]`,
not `r.exitCode`. Keep operations inside `main` or helpers it calls.

This is Starlark, not Python: no imports, `load()`, exceptions/try/finally,
classes, or standard-library modules. `while` is enabled but must remain bounded
by the task and declared limits. There is no sleep API. Ordinary Starlark
builtins are available; the only host globals are `windows` and `task`.
`print()` is discarded: use `task.activity` for durable progress. Return only
JSON-compatible dictionaries with string keys, lists/tuples, strings, booleans,
None, signed 64-bit integers, or finite floats.

## Current host surface

All host calls take **named arguments only**, except the no-argument elapsed
clock. Defaults below describe optional arguments, not additional capabilities.
Use explicit Windows paths to avoid relying on the Agent's working directory;
JSON Windows backslashes must be escaped.

| Call | Result and behavior |
| --- | --- |
| `windows.process.run(executable=..., argv=[...], cwd="", env={}, stdin="")` | Synchronous dictionary: `pid`, `exitCode`, `stdout`, `stderr`, `durationMs`. `executable` and a list of string `argv` are required. Environment overrides map strings to strings and merge with the Agent environment. stdin/stdout/stderr are text; output must be UTF-8. |
| `windows.fs.read(path=...)` | UTF-8 text string; binary/non-UTF-8 content fails. |
| `windows.fs.write(path=..., content=...)` | Writes text, creates or truncates the file; parent must exist. Returns None. |
| `windows.fs.stat(path=...)` | Dictionary: `name`, `path`, `size`, `isDir`, `modifiedAt` (UTC timestamp string). Missing paths fail; this is not an exists predicate. |
| `windows.fs.list(path=...)` | List of those metadata dictionaries for immediate children. |
| `windows.fs.mkdir(path=..., parents=False)` | Creates directory; `parents=True` creates missing parents. Returns None. |
| `windows.fs.copy(source=..., destination=..., overwrite=False)` | Copies file/directory; rejects source symlinks. Returns None. |
| `windows.fs.move(source=..., destination=..., overwrite=False)` | Rename semantics, not an automatic cross-volume copy. Returns None. |
| `windows.fs.remove(path=..., recursive=False)` | Deletes file/empty directory, or recursively when explicitly selected. Missing target fails. Returns None. |
| `task.activity(message=..., level=...)` | Emits `action.activity`, returns its durable sequence. Level is `info`, `warning`, or `error`. |
| `task.elapsed_milliseconds()` | Nonnegative elapsed milliseconds. |
| `task.fail(code=..., message=...)` | Terminates with caller-owned code and stage `task.fail`; code contains only uppercase ASCII letters, digits, underscores. |

Activity/failure messages must be nonempty single lines, with no surrounding
whitespace, tabs, or newlines. Avoid putting secrets in durable messages.

A nonzero process exit is data: explicitly test `result["exitCode"]` and use
`task.fail` when the task considers it failure. Process start/wait/output,
filesystem, deadline, schema, and journal errors abort the invocation; do not
convert them into success or domain UNKNOWN. There is no shell command-string
API; pass executable and argv separately. A deliberately selected executable
is not a license to silently fall back to PowerShell, SSH, or another provider.

`overwrite=True` for copy/move removes the existing destination before the
operation; it is not atomic replacement or rollback. File operations may leave
partial changes. `windows.process.run` does not promise detached-process
ownership or whole descendant-tree cleanup; choose managed `windows-exec run`
when that ownership is needed. Limits and cancellation do not undo prior
mutations. This is general execution with the Agent token, not a Rule package's
permission-bounded Observer sandbox.
