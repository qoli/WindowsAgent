# Contributing

Issues and focused pull requests are welcome.

## Development requirements

- Go 1.23 or newer
- macOS, Linux, or Windows for platform-independent tests
- Windows 10 1903+ amd64 for WGC runtime validation

Run before submitting a change:

```bash
gofmt -w $(find cmd internal -name '*.go')
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
mkdir -p .build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o .build/windows-capture-agent.exe \
  ./cmd/windows-capture-agent
```

Capture changes should also be verified in a signed-in interactive Windows
session. Include relevant HTTP metadata and failure codes in the pull request;
do not attach private screenshots or logs containing sensitive local paths.

## Design boundaries

- Do not add an alternate screenshot backend as a silent fallback.
- Keep each Windows capability behind an explicit package and API boundary.
- Preserve stable JSON error codes.
- Do not add authentication, privilege escalation, process-memory writes, or
  remote-control behavior without a documented threat model and review.
- Never run WGC capture as a traditional Session 0 service.
