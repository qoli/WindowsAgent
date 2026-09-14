# SFTP Filesystem Runtime

## Status

**Landed as `windows-sftp-v1`.** The dedicated runtime, persistent host-key
initialization, SFTP-only SSH surface, loopback health contract, installer,
Watchdog lifecycle, build, and transactional binary inventory are implemented
and accepted in a signed-in Windows installation.

## Responsibility

`windows-sftp.exe` is WindowsAgent's filesystem data plane for a supervising
agent on another machine. It is separate from `windows-capture-agent.exe` and
from the Starlark execution plane:

```text
macOS agent -- SFTP :2022 --> windows-sftp.exe --> Windows filesystem
macOS agent -- HTTP :8787 --> windows-capture-agent.exe --> executions/Starlark/actions
```

SFTP filesystem access does not require an interactive desktop. The installed
Scheduled Task nevertheless uses the current Windows identity with `RunLevel
Highest`, so filesystem operations use that elevated process token and normal
Windows filesystem semantics. This does not promise access through exclusive
file locks or ACLs that the process token and enabled privileges cannot satisfy.

## SSH and SFTP contract

The default SFTP listener is `0.0.0.0:2022`. Client authentication uses the SSH
`none` method: there is no account credential or password configuration. The
protocol username is the fixed label `windowsagent`; it does not select or
impersonate a Windows account.

Only SSH `session` channels carrying the `sftp` subsystem are accepted. Shell,
exec, PTY, agent forwarding, TCP forwarding, and other global or channel
requests are rejected. There is no SSH command-execution fallback.

The SFTP server exposes the Windows filesystem using the upstream
`pkg/sftp` Windows drive-root behavior. Virtual `/` enumerates available
Windows drives. The runtime does not add a chroot, allowed-root list, virtual
home directory, or per-connection filesystem identity.

## Host identity and health

SSH transport still requires a server host key even though clients are not
authenticated. Installation explicitly runs:

```text
windows-sftp.exe host-key init --path <absolute-path>
```

Initialization creates one Ed25519 private key without overwriting an existing
path. Normal runtime startup requires that exact persistent key and fails if it
is missing or malformed; it never generates a replacement during startup.

The default status listener is loopback-only `127.0.0.1:8793`. `GET /healthz`
returns `status`, runtime ID, bound SFTP listener, fixed username, client-auth
mode, and public host-key fingerprint. It does not expose private key material
or filesystem paths. Health becomes available only after both listeners bind.

## Installation and lifecycle

`scripts/install-windows-sftp.ps1` requires an elevated Administrator session,
installs the GUI-subsystem executable under the WindowsAgent data directory,
initializes or preserves the host key, registers an owned hidden Scheduled Task
for that identity with `RunLevel Highest`, and verifies the process and
loopback health response. It does not alter Windows Firewall.

`WatchdogManaged` is the default. The installer returns the complete `sftp`
Watchdog target declaration but does not rewrite Watchdog configuration or
teach the runtime about its lifecycle owner. The operator must merge that
declaration into the environment-owned Watchdog configuration and reinstall
or revalidate the Watchdog. `Standalone` is an explicit development mode with
an AtLogOn trigger and task-level restart policy.

The complete transactional binary deployment includes `windows-sftp.exe` only
after the installed Watchdog graph maps its owned Scheduled Task. A missing
mapping, missing host key, listener collision, invalid health response, or
binary/hash mismatch is terminal; no OpenSSH, SMB, password-authentication, or
filesystem-runtime fallback is selected.

## Live acceptance

Signed-in Windows acceptance proved the installed binary hash and GUI PE
subsystem, `Highest` interactive task principal, Watchdog recovery, persistent
host-key fingerprint across a process restart, SFTP `none` login as
`windowsagent`, root drive enumeration, upload, download, stat, rename, and
deletion. A different username, shell, exec, PTY, and TCP forwarding were
rejected. A 64 MiB upload and download preserved the exact SHA-256 digest while
the Agent HTTP and Event Web health surfaces remained available.
