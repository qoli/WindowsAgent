# Elite Dangerous filesystem nav-route

`elite-dangerous/filesystem/nav-route` is a finite query Action whose sole information source is
`NavRoute.json` under the current Windows user's resolved Saved Games known
folder at `Frontier Developments/Elite Dangerous`.

The caller supplies no filename or path. The package performs exactly one
bounded strict-JSON read of `NavRoute.json`, requires the event discriminator to
be one of `NavRoute`, `NavRouteClear`, requires a valid source timestamp, and returns
only its declared top-level fields. After 15000 ms freshness is UNKNOWN because a single read cannot prove continuous event coverage.

If Frontier has not produced this optional file, the successful result is
`state: ABSENT`, `freshness: UNKNOWN`, and `data: null`. An invalid or
missing Saved Games root, symlink or reparse point, malformed or duplicate-key
JSON, wrong event, invalid timestamp, oversized file, or a file changed during
the read fails explicitly.

This Action never reads another Frontier JSON file, Player Journal, screen
capture, OCR, process memory, network API, cache, or previous result. There is
no fallback. Nested Frontier arrays and objects may contain local player or
mission information and must not be persisted or published without an explicit
user-owned privacy boundary.
