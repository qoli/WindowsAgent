#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: deploy-windows-agent.sh [--host <ssh-host>] [--allow-dirty]

Builds every deployed WindowsAgent binary on macOS, stops the installed
Watchdog and its current configured targets, replaces only their binaries,
then starts the Watchdog and verifies hashes and health.
EOF
}

ssh_host="Ronnie-PC"
allow_dirty=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --host)
      [[ $# -ge 2 && -n "$2" ]] || { echo "error: --host requires a value" >&2; exit 2; }
      ssh_host="$2"
      shift 2
      ;;
    --allow-dirty)
      allow_dirty=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
cd "$repo_root"

if [[ -n "$(git status --porcelain --untracked-files=all)" && "$allow_dirty" -ne 1 ]]; then
  echo "error: worktree is modified; commit/stash it or pass --allow-dirty" >&2
  exit 1
fi

for command_name in go python3 ssh scp zip shasum mktemp awk find; do
  command -v "$command_name" >/dev/null || { echo "error: missing command: $command_name" >&2; exit 1; }
done

go test ./...
go run ./cmd/windows-action-check --rules-dir Rules
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...

release_dir="$(mktemp -d "${TMPDIR:-/tmp}/windowsagent-binaries.XXXXXX")"
cleanup() { find "$release_dir" -depth -delete 2>/dev/null || true; }
trap cleanup EXIT

"${script_dir}/build-windows-capture-agent.sh" --output-dir "$release_dir"

binaries=(
  windows-capture-agent.exe
  windows-wgc-worker.exe
  windows-event-stream.exe
  windows-action-osd.exe
  windows-watchdog.exe
  windows-observer.exe
  windows-observation-script-runner.exe
  windows-observation-job.exe
  windows-evidence-recorder.exe
  windows-visual-log.exe
)

: >"${release_dir}/SHA256SUMS"
for binary in "${binaries[@]}"; do
  [[ -f "${release_dir}/${binary}" ]] || { echo "error: build missing ${binary}" >&2; exit 1; }
  hash="$(shasum -a 256 "${release_dir}/${binary}" | awk '{print $1}')"
  printf '%s  %s\n' "$hash" "$binary" >>"${release_dir}/SHA256SUMS"
done
cp "${script_dir}/deploy-windows-binaries.ps1" "$release_dir/"

archive="${release_dir}/windowsagent-binaries.zip"
(
  cd "$release_dir"
  zip -X -q "$archive" SHA256SUMS deploy-windows-binaries.ps1 "${binaries[@]}"
)

deployment_id="$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
remote_stage="windowsagent-deploy-${deployment_id}"
ssh -o BatchMode=yes "$ssh_host" \
  "powershell.exe -NoProfile -NonInteractive -Command \"New-Item -ItemType Directory -Path (Join-Path \$env:USERPROFILE '${remote_stage}') -ErrorAction Stop | Out-Null\""
scp -q "$archive" "${ssh_host}:${remote_stage}/windowsagent-binaries.zip"

ssh -o BatchMode=yes "$ssh_host" \
  "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command \"\
    \$ErrorActionPreference='Stop'; \
    \$stage=Join-Path \$env:USERPROFILE '${remote_stage}'; \
    Expand-Archive -LiteralPath (Join-Path \$stage 'windowsagent-binaries.zip') -DestinationPath \$stage -Force; \
    & (Join-Path \$stage 'deploy-windows-binaries.ps1') -PayloadRoot \$stage; \
    if (\$LASTEXITCODE -ne 0) { exit \$LASTEXITCODE }; \
    Remove-Item -LiteralPath \$stage -Recurse -Force\""
