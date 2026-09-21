#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
for argument in "$@"; do
  case "$argument" in
    --agent-url|--agent-url=*|--ssh-host|--ssh-host=*|--ssh-user|--ssh-user=*|\
    --ssh-port|--ssh-port=*|--ssh-timeout|--ssh-timeout=*|\
    --restart-timeout|--restart-timeout=*|--recovery-python|--recovery-python=*)
      printf 'error: %s is owned by the single-host capture adapter\n' "$argument" >&2
      exit 2
      ;;
  esac
done
# shellcheck disable=SC1091
source "$script_dir/resolve-pc.sh"

capture_helper="$WINDOWS_AGENT_HARNESS_ROOT/../gameGuide/tools/pc_screenshot/capture_go_agent.py"
if [[ ! -f "$capture_helper" ]]; then
  printf 'error: bundled Windows PC capture helper is unavailable: %s\n' "$capture_helper" >&2
  exit 1
fi

exec python3 "$capture_helper" \
  --json \
  --agent-url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --no-auto-restart \
  "$@"
