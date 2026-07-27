# WindowsAgent Design Registry

This directory is the status view for maintained WindowsAgent design documents.
Implementation and tests remain the source of truth when a document drifts.

| Design | Status | Responsibility |
| --- | --- | --- |
| [Capture JSON rule navigation](capture-json-rule-navigation.md) | Landed | Use the capture response as Codex's Windows perception entry point and navigate from its foreground process to trusted rule APIs. |
| [Scripted observation job model](observation-job-model.md) | Partially landed | Bound one package, runner, observer, limits, and one terminal JSON result. |
| [Observation script package](observation-script-package.md) | Partially landed | Digest-pin Starlark task logic, human task documentation, permissions, and output schema. |
| [Windows observer protocol](observation-worker-protocol.md) | Partially landed | Unify finite read-only memory and file calls behind framed process boundaries. |

Status meanings:

- **Landed:** the core end-to-end contract is implemented and verified.
- **Partially landed:** a real path exists, but meaningful designed capability
  remains deferred.
- **Draft:** the document is mainly a proposal without an end-to-end path.
- **Retired:** retained only as historical context.
