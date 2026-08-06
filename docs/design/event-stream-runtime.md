# Event Stream Runtime

## Status

**Partially landed.**

The repository now contains a strict append-only journal, authenticated local
append/replay/live-stream HTTP surface, `windows-event-stream.exe`, and an
interactive-user Scheduled Task installation path. Module lifecycle and
segment rotation are not yet implemented.

## Responsibility

The event stream is the shared durable timeline for independent Game modules,
mini-model reactions, actions, and high-level model readers. It assigns event
identity, commit time, and a contiguous global sequence. It does not interpret,
merge, summarize, or replace producer-owned event semantics.

The process listens only on an explicit loopback IP and requires one canonical
bearer token from an absolute token file for append and replay. `/healthz` is
the only unauthenticated endpoint.

## Current contract

`POST /v1/events` accepts one strict `AppendRequest`. The journal rejects
unknown fields, duplicate JSON keys, invalid identifiers, missing foreground
revision, non-UTC observation time, invalid payload JSON, and oversized
records. It assigns `schemaVersion`, `sequence`, `eventId`, and `committedAt`.

`GET /v1/events?after=<sequence>&limit=<count>` returns ordered events plus the
next cursor and current last sequence. A cursor ahead of the journal is an
explicit conflict. Journal startup rejects malformed, non-newline-terminated,
oversized, unknown-field, or non-contiguous records; it never truncates or
repairs them automatically.

`GET /v1/events/stream?after=<sequence>` replays any committed events after the
cursor and then waits for newly committed records as NDJSON. It does not emit
invented heartbeat or summary events.

Each successful append is flushed and synchronized before it is acknowledged.
A write or sync failure poisons the writer instance so later operations cannot
pretend the journal remains authoritative.

## Deferred

- JSONL segment rotation and expired-cursor semantics;
- Game session creation and closure records;
- module credentials issued by the Windows Agent control plane;
- crash-injection acceptance beyond managed task restart.
