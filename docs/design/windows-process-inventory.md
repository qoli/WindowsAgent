# Windows Process Inventory

## Status

**Landed as `windows-process-inventory-v1`.** The native Windows collector,
osquery-shaped process and service tables, `GET /v1/processes`, HTTP contract
tests, standalone Windows collector acceptance, and installed-Agent HTTP
acceptance are implemented.

## Responsibility

This runtime returns one current read-only inventory of Windows processes and
services. It owns collection, osquery-aligned field names, typed unavailable
values, one observation time, and the stable failure contract. It does not
interpret, group, monitor, start, stop, suspend, prioritize, or restart a
process or service.

Processes and services remain separate tables. When `services.pid` is nonzero,
a caller identifies the services hosted by a process with
`services.pid == processes.pid`. A zero service PID means SCM reports no
current hosting process and must not be joined to the PID 0 process row. The
runtime does not special-case `svchost.exe` or create a generic association
vocabulary.

## HTTP Contract

`GET /v1/processes` takes no query parameters and returns:

```json
{
  "schemaVersion": 1,
  "runtime": "windows-process-inventory-v1",
  "observedAt": "2026-09-15T00:00:00Z",
  "processes": [],
  "services": []
}
```

Process rows use the osquery names `pid`, `parent`, `name`, `path`, `cmdline`,
`state`, `start_time`, and `threads`, plus the Windows-specific `session_id`.
The process snapshot supplies PID, parent, name, and thread count. Fields that
require opening the process are JSON `null` when Windows denies access or the
process exits during collection; the base row is retained.

Service rows use the osquery names `pid`, `name`, `display_name`, `status`,
`service_type`, `start_type`, `path`, `module_path`, `description`,
`user_account`, `win32_exit_code`, and `service_exit_code`. The Service Control
Manager owns service identity, state, type, and PID. Configuration and registry
enrichment may be `null` for an individual inaccessible or deleted service.

The response is generated on demand with `Cache-Control: no-store`. It is not
written to the event journal, capture store, or runtime logs because command
lines can contain private data.

## Native Sources

The Windows implementation uses `CreateToolhelp32Snapshot` with
`Process32FirstW`/`Process32NextW`, `QueryFullProcessImageNameW`,
`NtQueryInformationProcess(ProcessCommandLineInformation)`,
`ProcessIdToSessionId`, `GetExitCodeProcess`, and `GetProcessTimes`.

Services come from `EnumServicesStatusEx`; optional configuration comes from
`QueryServiceConfigW`, `QueryServiceConfig2W`, and the service `ServiceDll`
registry value. The service status and type strings match the maintained
osquery Windows table. There is no PowerShell, WMI, `tasklist`, cached result,
external `osqueryi.exe`, or alternate-provider fallback.

## Failure Contract

Failure to create or walk the process snapshot, connect to the Service Control
Manager, or enumerate the service table fails the whole request with a stable
`process_inventory_*` error. Per-row access denial and process/service exit
during enrichment produce unavailable optional fields rather than discarding
an already observed base row.

The collector never enables `SeDebugPrivilege` or changes the Agent token. It
reports only information available to the installed Capture Agent identity.

## Security Boundary

The endpoint inherits the Capture Agent's unauthenticated trusted-network
boundary on port 8787. Raw command lines are returned to the caller and are not
redacted, logged, cached, or persisted. Do not expose the listener to the
public Internet.

## Deferred

- fields outside the v1 table subset, including CPU, memory, I/O, user-token,
  signature, open-handle, window, and network inventories;
- any process or service mutation, monitoring, history, grouping, or semantic
  interpretation.
