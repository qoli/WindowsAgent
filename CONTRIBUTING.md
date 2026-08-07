# Contributing

Issues and focused pull requests are welcome.

## Development requirements

- Go 1.23 or newer
- macOS, Linux, or Windows for platform-independent tests
- Windows 10 1903+ amd64 for WGC runtime validation
- .NET 8 SDK for ScreenParser or PP-OCR DirectML runtime changes

Run before submitting a change:

```bash
gofmt -w $(find cmd internal -name '*.go')
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
mkdir -p .build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o .build/windows-capture-agent.exe \
  ./cmd/windows-capture-agent
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o .build/windows-observer.exe \
  ./cmd/windows-observer
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o .build/windows-observation-script-runner.exe \
  ./cmd/windows-observation-script-runner
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o .build/windows-observation-job.exe \
  ./cmd/windows-observation-job
```

Capture and observation-runtime changes should also be verified in a signed-in
interactive Windows session. Include only privacy-minimized metadata and
failure codes in the pull request; do not attach private screenshots, save
files, memory contents, inventory results, or logs containing sensitive local
paths.

PP-OCR runtime changes must additionally run
`PpOcr.DirectML.ContractTests` on Windows and validate the exact prepared model
with CPU execution-provider fallback disabled. Exercise worker initialization,
multiple framed recognition calls, and shutdown in one process. Performance
evidence must split model load, preprocessing, inference, postprocessing, and
external wall time; do not commit the private RGB test region or OCR response.

New or changed packages under `Rules/<Executable.exe>/Scripts/` must satisfy
the [`Script Package development contract`](docs/script-development-contract.md),
including real package-loader tests, explicit source ordering, manifest file
declarations, schema validation, and signed-in Windows evidence.

Changes to Agent-facing guidance should also be exercised through the
[`OpenCode black-box acceptance contract`](docs/testing/opencode-black-box-acceptance-contract.md).
Record the exact OpenCode executable/version and model, inspect tool events
rather than final prose, and enforce one launcher attempt.

## Design boundaries

- Do not add an alternate screenshot backend as a silent fallback.
- Keep each Windows capability behind an explicit package and API boundary.
- Keep observation calls finite and single-shot; do not add polling, file
  watching, or a general remote observer endpoint.
- Keep the Go observation command capability-neutral. It may route a declared
  runtime, bind the owning Rule process, validate input schemas and resource
  grants, snapshot a package, and broker primitives; it must not name or
  allowlist a game capability.
- A trusted Script Package may load only a `nativeLibraries` alias whose
  package-relative artifact is declared in the package manifest.
- Keep the Script Runner FFI provider-neutral. Game-specific exports, structs,
  return codes, and conversion logic belong in the owning `main.star`, never
  in the Observer, Job Host, or generic Go FFI.
- Preserve stable JSON error codes.
- Do not weaken manifest permission, schema, process-identity, path, resource,
  or single-job boundaries; add privilege escalation, process-memory writes,
  or broaden remote-control behavior without a documented threat model and
  review.
- Never run WGC capture as a traditional Session 0 service.
