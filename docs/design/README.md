# WindowsAgent Design Registry

This directory is the status view for maintained WindowsAgent design documents.
Implementation and tests remain the source of truth when a document drifts.

| Design | Status | Responsibility |
| --- | --- | --- |
| [Capture JSON rule navigation](capture-json-rule-navigation.md) | Landed | Use the capture response as Codex's Windows perception entry point and navigate from its foreground process to trusted rule APIs. |
| [Scripted observation job model](observation-job-model.md) | Landed | Bound one trusted package, one runner, one observer, process limits, and one terminal JSON result. |
| [Observation script package](observation-script-package.md) | Landed | Digest-pin Starlark logic, task documentation, Observer permissions, native DLL artifacts, and output schema. |
| [Windows observer protocol](observation-worker-protocol.md) | Partially landed | Unify finite read-only memory and file calls behind framed process boundaries. |
| [Script Runner native-library FFI](native-library-ffi.md) | Landed | Load package-owned DLL aliases and execute package-owned ABIs through a provider-neutral Windows amd64 FFI. |

Status meanings:

- **Landed:** the core end-to-end contract is implemented and verified.
- **Partially landed:** a real path exists, but meaningful designed capability
  remains deferred.
- **Draft:** the document is mainly a proposal without an end-to-end path.
- **Retired:** retained only as historical context.
