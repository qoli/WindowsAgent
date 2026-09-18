#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "${script_dir}/.." && pwd)"
output_dir="${repository_root}/.build"
release_version="${WINDOWSAGENT_VERSION:-dev}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir)
      [[ $# -ge 2 ]] || { echo "error: --output-dir requires a value" >&2; exit 2; }
      output_dir="$2"
      shift 2
      ;;
    --version)
      [[ $# -ge 2 ]] || { echo "error: --version requires a value" >&2; exit 2; }
      release_version="$2"
      shift 2
      ;;
    *)
      echo "usage: $0 [--output-dir <directory>] [--version <version>]" >&2
      exit 2
      ;;
  esac
done

mkdir -p "${output_dir}"
output_dir="$(cd "${output_dir}" && pwd)"

(
  cd "${repository_root}"
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui -X main.version=${release_version}" \
    -o "${output_dir}/windows-assist-gui.exe" \
    ./cmd/windows-assist-gui
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui -X main.version=${release_version}" \
    -o "${output_dir}/windows-capture-agent.exe" \
    ./cmd/windows-capture-agent
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-X main.version=${release_version}" \
    -o "${output_dir}/windows-capture-agent-console.exe" \
    ./cmd/windows-capture-agent
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-wgc-worker.exe" \
    ./cmd/windows-wgc-worker
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-action-check.exe" \
    ./cmd/windows-action-check
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-starlark-check.exe" \
    ./cmd/windows-starlark-check
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-starlark-invoke.exe" \
    ./cmd/windows-starlark-invoke
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-exec.exe" \
    ./cmd/windows-exec
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-key.exe" \
    ./cmd/windows-key
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-agent-pi.exe" \
    ./cmd/windows-agent-pi
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-agent-pi-component.exe" \
    ./cmd/windows-agent-pi-component
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-action-osd.exe" \
    ./cmd/windows-action-osd
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-watchdog.exe" \
    ./cmd/windows-watchdog
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-visual-log.exe" \
    ./cmd/windows-visual-log
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-evidence-recorder.exe" \
    ./cmd/windows-evidence-recorder
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-event-stream.exe" \
    ./cmd/windows-event-stream
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-event-web.exe" \
    ./cmd/windows-event-web
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-sftp.exe" \
    ./cmd/windows-sftp
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-observation-job.exe" \
    ./cmd/windows-observation-job
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-observation-script-runner.exe" \
    ./cmd/windows-observation-script-runner
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-observer.exe" \
    ./cmd/windows-observer
)

(
  cd "${repository_root}/runtimes/tailscale-adapter"
  GODEBUG=http2client=0 GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -mod=readonly -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-tailscale-adapter.exe" .
)

python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-assist-gui.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-capture-agent.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-capture-agent-console.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-wgc-worker.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-action-check.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-starlark-check.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-starlark-invoke.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-exec.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-key.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-agent-pi.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-agent-pi-component.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-action-osd.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-watchdog.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-visual-log.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-evidence-recorder.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-event-stream.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-event-web.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-sftp.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-tailscale-adapter.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-observation-job.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-observation-script-runner.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-observer.exe" --expect console

(
  cd "${repository_root}"
  go run ./cmd/windows-release-catalog \
    --input-dir "${output_dir}" \
    --version "${release_version}" \
    --output-json "${output_dir}/windowsagent-release.json" \
    --output-sums "${output_dir}/SHA256SUMS"
)
