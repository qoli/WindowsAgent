# PC environment contract

Use the source-compatible env file at:

```text
${WINDOWS_AGENT_PC_ENV:-$HOME/.config/windowsagent/pc.env}
```

Load it through the skill-owned resolver rather than assembling transport
configuration from the file:

```bash
skill_root="${CODEX_HOME:-$HOME/.codex}/skills/use-windows-pc"
source "$skill_root/scripts/resolve-pc.sh"
```

## Required PC identity

```text
WINDOWS_AGENT_HOST
```

The value is one reachable trusted-LAN or private-overlay hostname, IPv4
address, or IPv6 address. Do not include a URL scheme, port, path, or SSH user.
The resolver derives the normal Capture Agent and SFTP endpoints, fixed SFTP
protocol username, and persistent local host-key
state. Those derived values are implementation details, not fields in this
file.

For an existing legacy file, run the bundled
`scripts/migrate-pc-env.sh` once. It accepts only the standard HTTP/SFTP
constants and one matching legacy host, preserves the optional fields below,
and keeps the file private with mode `0600`.

## Optional label and capability-specific transport

```text
WINDOWS_AGENT_PC_NAME
WINDOWS_AGENT_EVENT_WEB_ORIGIN
WINDOWS_AGENT_PI_SSH_HOST
```

`WINDOWS_AGENT_PC_NAME` is only a human-readable label. It never selects the
target and is not required to operate the PC.

The other optional values do not redefine PC identity. Event Web may have a
deployment-specific listener, while the delegated Pi tunnel may require an SSH
config alias or `user@host` destination. Use it only for delegated Pi and not
as an execution or file-plane fallback.

No password, private key, bearer token, pre-provisioned host-key fingerprint,
repository path, helper path, known-hosts path, port constant, protocol
username, or staging-path mapping belongs in this file. Capability-specific
tokens remain in their installer-owned private files and are read only by the
workflow that requires them.
