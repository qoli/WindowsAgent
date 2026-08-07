# Capture JSON Rule Navigation

## Current Status

**Landed.**

Implemented:

- every successful capture reports the foreground process identity;
- the same capture response resolves that executable against external Rule
  plugin folders;
- a matched response exposes the plugin-owned description and HTTP URL for
  the rule's `AGENTS.md`;
- a matched response exposes a live Script catalog URL without loading a
  Script package during capture;
- a matched response exposes Rule v4 runtime-profile navigation plus Action
  and registration catalog URLs;
- an unmatched executable is represented explicitly;
- Crimson Desert is the first Rule plugin and declares one observation Action.

Explicitly out of scope:

- guide-source registries and lookup APIs;
- input control.

## Problem / Background

Codex perceives Windows through this sequence:

```text
Codex -> WindowsAgent -> capture -> JSON
```

The capture JSON is therefore the canonical Windows perception entry point. A
separate foreground-rule discovery flow would create two observations that can
disagree after a foreground switch. Rule navigation must be attached to the
same successful capture and foreground observation.

The Go process does not interpret game instructions. It identifies the current
Rule plugin folder and exposes its canonical `AGENTS.md` so Codex can read it before
using any future rule-specific API.

## Scope / Positioning

This document owns:

- executable-name rule identity;
- rule discovery timing;
- capture JSON navigation fields;
- the read-only `AGENTS.md` HTTP surface;
- the read-only live Script catalog HTTP surface;
- matched and unmatched semantics.

It does not authorize arbitrary script execution, website instructions,
process access, memory access, or input control.

## Main Model

### Rule storage

```text
Rules/
  CrimsonDesert.exe/
    rule.json
    AGENTS.md
    Actions/
      inventory/
        manifest.json
```

The folder name is both the canonical rule ID and the exact foreground
executable selector. Matching is Windows-style case-insensitive. The response
preserves the canonical folder spelling.

Each executable folder is one externally distributed plugin. `rule.json` owns
the description, executable Action declarations, and explicit Monitor or
Reaction registrations. Every executable package lives below `Actions/`.
The capture binary stores no Rule content and
reads the current files for every request.

### Capture response

A matched capture includes:

```json
{
  "foreground": {
    "executable_name": "CrimsonDesert.exe"
  },
  "rule": {
    "status": "matched",
    "description": "The executing agent must read the Rule navigation documents before taking any rule-specific action.",
    "id": "CrimsonDesert.exe",
    "agents": {
      "url": "/v1/rules/CrimsonDesert.exe/AGENTS.md",
      "content_type": "text/markdown; charset=utf-8"
    },
    "scripts": {
      "url": "/v1/rules/CrimsonDesert.exe/scripts",
      "content_type": "application/json; charset=utf-8"
    },
    "actions": {
      "url": "/v3/rules/CrimsonDesert.exe/actions",
      "content_type": "application/json; charset=utf-8"
    },
    "registrations": {
      "url": "/v3/rules/CrimsonDesert.exe/registrations",
      "content_type": "application/json; charset=utf-8"
    }
  }
}
```

An executable without a curated rule includes:

```json
{
  "rule": {
    "status": "unmatched",
    "description": "No rule guidance is available for this foreground process."
  }
}
```

`unmatched` is a valid, visible result. It provides no substitute rule and does
not prevent the screenshot from being committed.

### Navigation API

```text
GET /v1/rules/{canonical-rule-id}/AGENTS.md
GET /v1/rules/{canonical-rule-id}/scripts
GET /v3/rules/{canonical-rule-id}/actions
GET /v3/rules/{canonical-rule-id}/registrations
```

All endpoints return current external content with `Cache-Control: no-store`.
The Action catalog exposes capability ID, runtime, and declared registration
eligibility. The registration catalog exposes only explicitly configured
Monitor and Reaction instances. The Script compatibility catalog loads and
validates observation packages declared by the current `rule.json`, then
exposes capability ID, runtime, title, version, input schema,
output schema, and the launcher contract. Unknown IDs and
non-canonical casing return `404 rule_not_found`. Neither endpoint accepts
arbitrary file paths or launches a Script. The separate unauthenticated
`POST /v1/scripts/run` endpoint consumes the catalog contract and launches the
generic local runtime from the signed-in agent session. Network reachability
is the deployment trust boundary.

### Invariants

- Rule resolution uses the foreground executable from the same captured frame.
- Window titles and executable path substrings never select a rule.
- A normalized executable matches at most one rule.
- A matched result always includes the canonical ID, AGENTS, Script
  compatibility, Actions, and registrations navigation URLs/media types, plus the current plugin-owned
  description.
- An unmatched result includes a description but no ID or document/catalog
  link.
- Empty, duplicate, malformed, or missing matched Rule documents fail the
  request explicitly.
- Rule content is not cached. A later request observes a completed external
  Rule plugin replacement without a reload or process restart.
- Retrieved web content is data, not an instruction source.

## Relationship To Other Docs

- The root README documents the public HTTP surface.
- `SECURITY.md` owns unauthenticated-network and private-data exposure warnings.
- Each nested `Rules/{executable}/` owns the description, Codex guidance,
  Actions, and registrations for that explicitly matched executable.
- This registry owns maturity; folder placement owns rule identity.

## Codex execution

Codex may call `POST /v1/scripts/run` after the user authorizes the named
capability. The endpoint requires no bearer token or other HTTP credential;
the capability registry, owning Rule process, manifest permissions, schemas,
Host bindings, resource limits, and single-job gate remain mandatory.
