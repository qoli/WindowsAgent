# PC environment contract

Use the source-compatible env file at:

```text
${WINDOWS_AGENT_PC_ENV:-$HOME/.config/windowsagent/pc.env}
```

Load it without printing its contents:

```bash
pc_env="${WINDOWS_AGENT_PC_ENV:-$HOME/.config/windowsagent/pc.env}"
set -a
source "$pc_env"
set +a
```

## Required fields

```text
WINDOWS_AGENT_PC_NAME
WINDOWS_AGENT_HTTP_ORIGIN
WINDOWS_AGENT_REPO
WINDOWS_AGENT_CAPTURE_HELPER
WINDOWS_AGENT_SFTP_HOST
WINDOWS_AGENT_SFTP_PORT
WINDOWS_AGENT_SFTP_USER
WINDOWS_AGENT_SFTP_HOST_KEY_SHA256
WINDOWS_AGENT_SFTP_KNOWN_HOSTS
WINDOWS_AGENT_SFTP_STAGING_DIR
WINDOWS_AGENT_WINDOWS_STAGING_DIR
```

- `WINDOWS_AGENT_HTTP_ORIGIN` is the trusted-LAN or private-overlay origin of
  the Capture Agent, including scheme and port.
- `WINDOWS_AGENT_REPO` is the local checkout containing the console clients.
- `WINDOWS_AGENT_CAPTURE_HELPER` is the maintained local fresh-capture helper.
- `WINDOWS_AGENT_SFTP_STAGING_DIR` is the SFTP spelling of the private staging
  directory, such as a path below `/c:/...`.
- `WINDOWS_AGENT_WINDOWS_STAGING_DIR` is the native Windows spelling of that
  same directory for `windows-exec ps1`.

The SFTP and Windows staging values must identify the same directory. The env
owns this mapping because it depends on the configured Windows identity.

## Optional fields

```text
WINDOWS_AGENT_EVENT_WEB_ORIGIN
WINDOWS_AGENT_ADMIN_SSH_HOST
WINDOWS_AGENT_PI_SSH_HOST
```

Use Event Web only when its health or UI is relevant. Use administrative SSH
only for an explicitly authorized deployment or diagnostic workflow; it is not
an execution fallback. `WINDOWS_AGENT_PI_SSH_HOST` is the OpenSSH destination
used only to tunnel the loopback delegated Pi API and PI WEB. Do not infer it
from the administrative target when it is absent, even if one deployment uses
the same Windows account.

No password, private key, bearer token, or host-key private material belongs in
this file. Capability-specific tokens may remain in their existing private
operator files and should be read only by the workflow that requires them.
