#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: deploy-windows-agent.sh [--host <ssh-host>] [--allow-dirty]
                               [--validate-only] [--timeout-seconds <5-300>]

Builds every deployed WindowsAgent binary on macOS, stops the installed
Watchdog and its current configured targets, replaces only their binaries,
then starts the Watchdog and verifies hashes and health. --validate-only
performs the complete local build, upload, payload check, installed mapping,
Task-action, process/session, and HTTP-probe preflight without stopping or
replacing the installed runtime.
EOF
}

ssh_host="Ronnie-PC"
allow_dirty=0
validate_only=0
timeout_seconds=45
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
    --validate-only)
      validate_only=1
      shift
      ;;
    --timeout-seconds)
      [[ $# -ge 2 && "$2" =~ ^[0-9]+$ && "$2" -ge 5 && "$2" -le 300 ]] || {
        echo "error: --timeout-seconds requires an integer from 5 through 300" >&2
        exit 2
      }
      timeout_seconds="$2"
      shift 2
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

if [[ "$ssh_host" == -* || ! "$ssh_host" =~ ^[A-Za-z0-9_.@-]+$ ]]; then
  echo "error: --host must be a plain SSH host name" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
cd "$repo_root"

if [[ -n "$(git status --porcelain --untracked-files=all)" && "$allow_dirty" -ne 1 ]]; then
  echo "error: worktree is modified; commit/stash it or pass --allow-dirty" >&2
  exit 1
fi

for command_name in go python3 ssh scp zip shasum mktemp awk find mkdir cp; do
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
payload_sha256="$(shasum -a 256 "${release_dir}/SHA256SUMS" | awk '{print $1}')"
remote_stage="windowsagent-deploy-${deployment_id}"
local_receipt_dir="${repo_root}/.build/binary-deployments"
mkdir -p "$local_receipt_dir"
local_receipt="${local_receipt_dir}/${deployment_id}.json"
write_local_failure_receipt() {
  phase="$1"
  detail="$2"
  retained="$3"
  python3 -c 'import datetime, json, pathlib, sys; path=pathlib.Path(sys.argv[1]); receipt={"schema_version":1,"deployment_id":sys.argv[2],"payload_sha256":sys.argv[3],"status":"FAILED","phase":sys.argv[4],"error":sys.argv[5],"staging_retained":sys.argv[6]=="true","remote_stage":sys.argv[7],"completed_at":datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00","Z")}; path.write_text(json.dumps(receipt, indent=2, sort_keys=True)+"\n", encoding="utf-8")' \
    "$local_receipt" "$deployment_id" "$payload_sha256" "$phase" "$detail" "$retained" "$remote_stage"
}
if ! ssh -o BatchMode=yes "$ssh_host" \
  "powershell.exe -NoProfile -NonInteractive -Command \"\
    \$stage=Join-Path \$env:USERPROFILE '${remote_stage}'; \
    if(Test-Path -LiteralPath \$stage){throw 'remote staging directory already exists'}; \
    New-Item -ItemType Directory -Path \$stage -ErrorAction Stop | Out-Null\""; then
  write_local_failure_receipt "create_remote_stage" "remote staging directory creation failed" "false"
  echo "error: could not create remote staging; receipt: ${local_receipt}" >&2
  exit 1
fi
if ! scp -q "$archive" "${ssh_host}:${remote_stage}/windowsagent-binaries.zip"; then
  write_local_failure_receipt "upload" "binary archive upload failed" "true"
  echo "error: binary upload failed; receipt: ${local_receipt}; staging retained at ${remote_stage}" >&2
  exit 1
fi

validate_literal='$false'
if [[ "$validate_only" -eq 1 ]]; then
  validate_literal='$true'
fi
remote_output="${release_dir}/remote-receipt.json"
set +e
ssh -o BatchMode=yes "$ssh_host" \
  "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command \"\
    \$ErrorActionPreference='Stop'; \
    \$stage=Join-Path \$env:USERPROFILE '${remote_stage}'; \
    Expand-Archive -LiteralPath (Join-Path \$stage 'windowsagent-binaries.zip') -DestinationPath \$stage -Force; \
    & (Join-Path \$stage 'deploy-windows-binaries.ps1') \
      -PayloadRoot \$stage \
      -DeploymentId '${deployment_id}' \
      -PayloadSha256 '${payload_sha256}' \
      -ValidateOnly:${validate_literal} \
      -TimeoutSeconds ${timeout_seconds}\"" >"$remote_output"
remote_status=$?
set -e

if ! python3 -c 'import json, pathlib, sys; source=pathlib.Path(sys.argv[1]); target=pathlib.Path(sys.argv[2]); expected=sys.argv[3]; receipt=json.loads(source.read_text(encoding="utf-8")); assert receipt.get("deployment_id") == expected; target.write_text(json.dumps(receipt, indent=2, sort_keys=True)+"\n", encoding="utf-8")' \
  "$remote_output" "$local_receipt" "$deployment_id"; then
  write_local_failure_receipt "remote_executor" "remote executor returned no valid matching JSON receipt" "true"
  echo "error: remote binary deployment returned no valid matching JSON receipt; staging retained at ${remote_stage}" >&2
  exit 1
fi

if [[ "$remote_status" -ne 0 ]]; then
  echo "error: remote binary deployment failed; receipt: ${local_receipt}; staging retained at ${remote_stage}" >&2
  python3 -m json.tool "$local_receipt"
  exit "$remote_status"
fi

expected_status="SUCCEEDED"
if [[ "$validate_only" -eq 1 ]]; then
  expected_status="VALIDATED"
fi
if ! python3 -c 'import json, sys; receipt=json.load(open(sys.argv[1], encoding="utf-8")); assert receipt.get("status") == sys.argv[2]' \
  "$local_receipt" "$expected_status"; then
  echo "error: remote binary deployment receipt did not report ${expected_status}; staging retained at ${remote_stage}" >&2
  exit 1
fi

if ssh -o BatchMode=yes "$ssh_host" \
  "powershell.exe -NoProfile -NonInteractive -Command \"\
    \$stage=Join-Path \$env:USERPROFILE '${remote_stage}'; \
    Remove-Item -LiteralPath \$stage -Recurse -Force -ErrorAction Stop\""; then
  python3 -c 'import json, pathlib, sys; path=pathlib.Path(sys.argv[1]); receipt=json.loads(path.read_text(encoding="utf-8")); receipt["staging_retained"]=False; path.write_text(json.dumps(receipt, indent=2, sort_keys=True)+"\n", encoding="utf-8")' "$local_receipt"
else
  python3 -c 'import json, pathlib, sys; path=pathlib.Path(sys.argv[1]); receipt=json.loads(path.read_text(encoding="utf-8")); receipt["cleanup_warning"]="remote staging cleanup failed"; path.write_text(json.dumps(receipt, indent=2, sort_keys=True)+"\n", encoding="utf-8")' "$local_receipt"
  echo "warning: deployment succeeded but remote staging cleanup failed: ${remote_stage}" >&2
fi

python3 -m json.tool "$local_receipt"
