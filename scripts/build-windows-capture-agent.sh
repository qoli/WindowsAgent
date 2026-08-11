#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "${script_dir}/.." && pwd)"
output_dir="${repository_root}/.build"

if [[ $# -gt 0 ]]; then
  if [[ $# -ne 2 || "$1" != "--output-dir" ]]; then
    echo "usage: $0 [--output-dir <directory>]" >&2
    exit 2
  fi
  output_dir="$2"
fi

mkdir -p "${output_dir}"
output_dir="$(cd "${output_dir}" && pwd)"

(
  cd "${repository_root}"
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-H=windowsgui" \
    -o "${output_dir}/windows-capture-agent.exe" \
    ./cmd/windows-capture-agent
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-capture-agent-console.exe" \
    ./cmd/windows-capture-agent
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -o "${output_dir}/windows-action-check.exe" \
    ./cmd/windows-action-check
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
)

python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-capture-agent.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-capture-agent-console.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-action-check.exe" --expect console
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-action-osd.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-watchdog.exe" --expect gui
python3 "${script_dir}/verify-windows-pe-subsystem.py" \
  "${output_dir}/windows-visual-log.exe" --expect gui
