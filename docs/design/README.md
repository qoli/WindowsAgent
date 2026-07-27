# WindowsAgent Design Registry

This directory is the status view for maintained WindowsAgent design documents.
Implementation and tests remain the source of truth when a document drifts.

| Design | Status | Responsibility |
| --- | --- | --- |
| [Capture JSON rule navigation](capture-json-rule-navigation.md) | Landed | Load an external executable-scoped Rule plugin for each capture and navigate to its current guidance. |
| [Scripted observation job model](observation-job-model.md) | Landed | Resolve any registered windows-observation-v1 capability and bind one package, runner, observer, owning Rule process, Host resources, and terminal result. |
| [Observation script package](observation-script-package.md) | Landed | Validate external Starlark logic, task documentation, input/output schemas, Observer permissions, native DLL artifacts, and limits. |
| [Windows observer protocol](observation-worker-protocol.md) | Partially landed | Unify finite read-only memory and file calls behind framed process boundaries. |
| [Script Runner native-library FFI](native-library-ffi.md) | Landed | Load package-owned DLL aliases and execute package-owned ABIs through a provider-neutral Windows amd64 FFI. |

Status meanings:

- **Landed:** the core end-to-end contract is implemented and verified.
- **Partially landed:** a real path exists, but meaningful designed capability
  remains deferred.
- **Draft:** the document is mainly a proposal without an end-to-end path.
- **Retired:** retained only as historical context.
