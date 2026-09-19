#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "${script_dir}/.." && pwd)"
output_dir="${repository_root}/.build"
release_version="${WINDOWSAGENT_VERSION:-dev}"
assist_gui_exe=""
skip_assist_gui=false
skip_catalog=false

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
    --assist-gui-exe)
      [[ $# -ge 2 ]] || { echo "error: --assist-gui-exe requires a value" >&2; exit 2; }
      assist_gui_exe="$2"
      shift 2
      ;;
    --skip-assist-gui)
      skip_assist_gui=true
      shift
      ;;
    --skip-catalog)
      skip_catalog=true
      shift
      ;;
    *)
      echo "usage: $0 [--output-dir <directory>] [--version <version>] [--assist-gui-exe <path> | --skip-assist-gui] [--skip-catalog]" >&2
      exit 2
      ;;
  esac
done

if [[ -n "${assist_gui_exe}" && "${skip_assist_gui}" == true ]]; then
  echo "error: --assist-gui-exe and --skip-assist-gui are mutually exclusive" >&2
  exit 2
fi
if [[ "${skip_assist_gui}" == true && "${skip_catalog}" != true ]]; then
  echo "error: --skip-assist-gui requires --skip-catalog because the release catalog is a complete-set contract" >&2
  exit 2
fi

mkdir -p "${output_dir}"
output_dir="$(cd "${output_dir}" && pwd)"

# A partial Go-only build must not leave stale files that look like a complete
# WinUI release set in a reused output directory.
if [[ "${skip_assist_gui}" == true ]]; then
  rm -f "${output_dir}/windows-assist-gui.exe"
fi
if [[ "${skip_catalog}" == true ]]; then
  rm -f "${output_dir}/windowsagent-release.json" "${output_dir}/SHA256SUMS"
fi

(
  cd "${repository_root}"
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-assist-backend.exe" \
    ./cmd/windows-assist-backend
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

if [[ -n "${assist_gui_exe}" ]]; then
  assist_gui_exe="$(cd "$(dirname "${assist_gui_exe}")" && pwd)/$(basename "${assist_gui_exe}")"
  [[ -f "${assist_gui_exe}" ]] || { echo "error: prebuilt AssistGUI does not exist: ${assist_gui_exe}" >&2; exit 1; }
  if [[ "${assist_gui_exe}" != "${output_dir}/windows-assist-gui.exe" ]]; then
    cp "${assist_gui_exe}" "${output_dir}/windows-assist-gui.exe"
  fi
elif [[ "${skip_assist_gui}" != true && ! -f "${output_dir}/windows-assist-gui.exe" ]]; then
  echo "error: WinUI AssistGUI is not built by the Go cross-build; pass --assist-gui-exe or use --skip-assist-gui --skip-catalog" >&2
  exit 1
fi

(
  cd "${repository_root}/runtimes/tailscale-adapter"
  GODEBUG=http2client=0 GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -mod=readonly -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-tailscale-adapter.exe" .
)

python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-assist-backend.exe" --expect gui
if [[ "${skip_assist_gui}" != true ]]; then
  python3 "${script_dir}/verify-windows-pe-subsystem.py" \
    "${output_dir}/windows-assist-gui.exe" --expect gui
fi
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

if [[ "${skip_catalog}" != true ]]; then
  (
    cd "${repository_root}"
    go run ./cmd/windows-release-catalog \
      --input-dir "${output_dir}" \
      --version "${release_version}" \
      --output-json "${output_dir}/windowsagent-release.json" \
      --output-sums "${output_dir}/SHA256SUMS"
  )
fi
