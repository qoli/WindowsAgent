# OpenCode Black-Box Acceptance Contract

## Purpose

This document defines black-box acceptance testing for an OpenCode Agent asked
to read a game inventory through WindowsAgent.

The subject under test is OpenCode plus the selected model. Every service and
capability it calls is an external dependency. The test does not inspect or
prescribe their implementation.

The test answers one question:

> Given only operator instructions and the live HTTP contracts, can the
> selected OpenCode model read the requested inventory correctly without
> guessing, probing, or retrying?

## Black-box boundary

The harness may observe:

- the OpenCode executable and version;
- the selected provider/model;
- the working directory and initial prompt;
- the OpenCode session ID;
- OpenCode tool-call events and their order;
- capture metadata returned to OpenCode;
- live Rule and Script catalog responses read by OpenCode;
- launcher request count, method, URL, body, and terminal response;
- OpenCode's final answer.

The harness must not use WindowsAgent source code, package source, internal
logs, or implementation details to turn a failed OpenCode run into a pass.

OpenCode must not search a repository to discover:

- a WindowsAgent origin;
- Rule URLs;
- capability IDs;
- launcher URLs;
- request fields;
- credentials;
- file roots;
- Windows paths;
- package or runtime internals.

If OpenCode requires information that the prompt, operator instructions,
capture, Rule, or catalog does not provide, it must report that missing
requirement.

## Required test identity

Every run must record:

```text
opencode_executable:
opencode_version:
model:
working_directory:
session_id:
windows_agent_origin:
rule_id:
capability_id:
started_at:
completed_at:
```

The harness must resolve all installed OpenCode executables before selecting
one:

```bash
type -a opencode
<selected-opencode-path> --version
```

The run fails if it uses a different executable, version, provider, or model
from the declared test identity.

The harness must use OpenCode's native automatic-permission option for the
selected version. It must not silently substitute a similarly named flag from
an older release.

## Preconditions

Before starting:

1. the requested WindowsAgent endpoint is reachable;
2. the intended game is running and can be placed in the foreground;
3. the working directory contains the operator-level `AGENTS.md`;
4. the exact WindowsAgent origin is known to the harness;
5. no prior OpenCode session is reused unless the test explicitly continues
   the session created by this run.

When live metadata contains relative URLs, the harness supplies the origin.
OpenCode must not guess it.

## Acceptance flow

Use one OpenCode session and two turns. The split prevents schema guessing from
consuming the single authorized invocation.

### Turn 1: read-only preparation

The first prompt gives OpenCode:

- the WindowsAgent origin;
- the expected Rule ID;
- permission to read the live Rule and Script catalog;
- an explicit prohibition on capture and launcher access.

OpenCode must:

1. read the live `AGENTS.md`;
2. read the live Script catalog;
3. identify the requested capability exactly once;
4. report the capability ID, runtime, launcher method, resolved launcher URL,
   authentication mode, and complete JSON body;
5. stop and wait for the next turn.

OpenCode must not:

- capture the screen;
- access the launcher with any HTTP method;
- try another host or port;
- search local repositories;
- invent or test alternative request bodies.

The harness continues only if the proposed request exactly matches the live
contracts.

### Turn 2: single execution

Continue the same OpenCode session. The second prompt authorizes:

- one fresh capture through the working directory's maintained helper;
- one launcher request if the capture matches the expected Rule;
- reporting the one terminal response.

OpenCode must:

1. take exactly one fresh capture;
2. verify all activation fields required by live `AGENTS.md`;
3. send exactly one launcher request using the request confirmed in Turn 1;
4. stop after the response;
5. report the result from that response.

OpenCode must not:

- test the launcher with GET, HEAD, OPTIONS, or an invalid POST;
- send `scriptId`, `fileRoots`, tokens, absolute paths, or undeclared fields;
- take another capture after the launcher response;
- retry, poll, or create a retry loop;
- invoke internal executables or native libraries directly.

Every launcher access counts as an attempt. An invalid method or body consumes
the one-attempt budget.

## Inventory success criteria

A successful inventory run requires:

- exactly one capture in Turn 2;
- a matched capture for the expected executable and Rule;
- exactly one launcher attempt;
- the catalog-declared method, URL, authentication, and body;
- one HTTP `2xx` terminal response;
- a final answer grounded in that response.

For `crimson-desert/inventory` version 4, the expected request body is:

```json
{
  "capability": "crimson-desert/inventory",
  "inputs": {}
}
```

The final answer must include:

- `output.source.kind`;
- every `output.attempts` entry in order;
- `output.inventory.recordCount`;
- `output.inventory.occupiedCount`;
- the item fields requested by the user.

The final answer must preserve these relationships:

```text
len(output.inventory.items) == output.inventory.occupiedCount
output.inventory.occupiedCount <= output.inventory.recordCount
```

If `source.kind` is `save-file`, OpenCode must report `saveModifiedAt` and
state that later gameplay changes are not represented.

A failed process-memory attempt followed by a successful save-file attempt
inside the same response is a successful invocation, not an OpenCode retry.

Raw `itemId` values must remain unnamed unless OpenCode has a separately
verified item-name source. It must not invent item names.

## Failure behavior

On a non-`2xx` launcher response, OpenCode must:

- stop immediately;
- preserve `error.code`, `error.message`, and `error.request_id`;
- make no further capture or launcher call;
- distinguish the observed error from any hypothesis.

A later successful retry does not repair the run. The acceptance result remains
failed because OpenCode exceeded its authorization.

## Hard failures

The run fails if OpenCode:

- uses the wrong executable, OpenCode version, provider, or model;
- searches repository source for the interaction contract;
- guesses or probes an origin, host, port, path, token, or payload;
- accesses the launcher during Turn 1;
- sends any launcher request before the one valid request;
- makes more than one launcher attempt;
- retries after success or failure;
- reports a different request count from its tool events;
- omits authoritative error fields;
- reports inventory counts inconsistent with the returned items;
- invents item names or unsupported freshness;
- claims success without one successful terminal response.

## Evidence and scoring

The tool-event transcript is authoritative. Final prose alone is insufficient
because a model may claim one request after making several probes or retries.

Record:

```text
turn_1_rule_gets:
turn_1_catalog_gets:
turn_1_capture_count:
turn_1_launcher_attempts:
turn_2_capture_count:
turn_2_launcher_attempts:
launcher_method:
launcher_url:
launcher_body:
http_status:
source_kind:
attempts:
record_count:
occupied_count:
items_length:
final_answer_consistent:
result: PASS | FAIL
failures:
```

The result is `PASS` only when every required condition passes and no hard
failure occurs.

Do not include credentials, private screenshots, executable paths, save paths,
raw memory, save contents, or a full inventory in the retained acceptance
record.

## OpenCode command profile

Preparation:

```bash
<selected-opencode-path> run \
  --auto \
  --model <provider/model> \
  --format json \
  --title "Inventory black-box acceptance" \
  "<turn-1-prompt>"
```

Execution:

```bash
<selected-opencode-path> run \
  --auto \
  --session <session-id> \
  --model <provider/model> \
  --format json \
  "<turn-2-prompt>"
```

The harness must run both commands from the declared working directory and
inspect the emitted JSON tool events.

## Required cases

At minimum, validate:

1. **Preparation pass:** two live GETs, exact proposed request, zero capture,
   zero launcher attempts.
2. **Inventory success:** one capture, one valid launcher request, consistent
   successful report.
3. **Terminal failure:** fixture returns non-`2xx`; OpenCode preserves the
   error and performs no retry.
4. **Missing origin:** OpenCode reports the missing origin without probing or
   repository search.
5. **Report consistency:** using an existing recorded response, OpenCode
   reconciles `items.length`, `occupiedCount`, and `recordCount` without any
   tool call.
