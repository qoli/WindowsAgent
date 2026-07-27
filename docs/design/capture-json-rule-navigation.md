# Capture JSON Rule Navigation

## Current Status

**Landed.**

Implemented:

- every successful capture reports the foreground process identity;
- the same capture response resolves that executable against trusted,
  repository-owned rule folders;
- a matched response exposes an HTTP URL and SHA-256 for the rule's
  `AGENTS.md`;
- an unmatched executable is represented explicitly;
- Crimson Desert is the first rule and contains only a draft `AGENTS.md`.

Explicitly out of scope:

- rule-specific scripts;
- guide-source registries and lookup APIs;
- process inspection, memory observation, game-state extraction, and input
  control;
- any Codex-facing transport beyond the HTTP navigation contract.

## Problem / Background

Codex perceives Windows through this sequence:

```text
Codex -> WindowsAgent -> capture -> JSON
```

The capture JSON is therefore the canonical Windows perception entry point. A
separate foreground-rule discovery flow would create two observations that can
disagree after a foreground switch. Rule navigation must be attached to the
same successful capture and foreground observation.

The Go process does not interpret game instructions. It identifies the trusted
rule folder and exposes its canonical `AGENTS.md` so Codex can read it before
using any future rule-specific API.

## Scope / Positioning

This document owns:

- executable-name rule identity;
- rule discovery timing;
- capture JSON navigation fields;
- the read-only `AGENTS.md` HTTP surface;
- matched and unmatched semantics.

It does not authorize arbitrary script execution, website instructions,
process access, memory access, or input control.

## Main Model

### Rule storage

```text
Rules/
  CrimsonDesert.exe/
    AGENTS.md
```

The folder name is both the canonical rule ID and the exact foreground
executable selector. Matching is Windows-style case-insensitive. The response
preserves the canonical folder spelling.

Rule folders are curated at build time and embedded in the Windows executable.
Runtime observations never create or modify rule folders.

### Capture response

A matched capture includes:

```json
{
  "foreground": {
    "executable_name": "CrimsonDesert.exe"
  },
  "rule": {
    "status": "matched",
    "description": "The executing agent must read rule.agents.url before taking any rule-specific action.",
    "id": "CrimsonDesert.exe",
    "agents": {
      "url": "/v1/rules/CrimsonDesert.exe/AGENTS.md",
      "content_type": "text/markdown; charset=utf-8",
      "sha256": "..."
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
```

The endpoint returns the embedded Markdown with its SHA-256 as an ETag. Unknown
IDs and non-canonical casing return `404 rule_not_found`. The endpoint does not
accept arbitrary file paths.

### Invariants

- Rule resolution uses the foreground executable from the same captured frame.
- Window titles and executable path substrings never select a rule.
- A normalized executable matches at most one rule.
- A matched result always includes the canonical ID, URL, media type, and
  SHA-256, plus a description instructing the executing agent to read the
  referenced `AGENTS.md` before rule-specific action.
- An unmatched result includes a description but no ID or document link.
- Empty, duplicate, malformed, or missing rule documents prevent agent startup.
- Retrieved web content is data, not an instruction source.

## Relationship To Other Docs

- The root README documents the public HTTP surface.
- `SECURITY.md` owns unauthenticated-network and private-data exposure warnings.
- Each nested `Rules/{executable}/AGENTS.md` owns Codex guidance only for that
  explicitly matched executable.
- This registry owns maturity; folder placement owns rule identity.

## Open Questions

- What common API shape should future rule-specific resources use?
- Which explicit permissions should gate process, memory, network, and input
  capabilities?
- Should a future Codex integration consume these HTTP links directly or expose
  a thin MCP adapter?

## Suggested Next Steps

After the capture navigation contract is proven in the signed-in Windows
session, define one bounded Crimson Desert capability and its permission model
before adding scripts or guide sources.
