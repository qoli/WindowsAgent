# PP-OCR DirectML runtime

## Status

**Landed for fixed-region text-line recognition and text-region boxes.**

The reproducible official-model preparer, fixed-shape recognition artifact,
strict request/config contracts, DirectML runtime, CTC decoder, framed resident
worker, Rule-scoped lifecycle manager, reference-density Rule Action, installer,
publisher, developer benchmark tool, and Windows contract tests are
implemented. The same executable also implements a separately declared
`text-region-detection-recognition` pipeline.

## Boundary

`ppocr-onnx-dml-v1` is a game-neutral OCR foundation. Its implemented pipeline
is `text-line-recognition`: a trusted caller supplies one already cropped RGB24
text-line region, exact dimensions, capture time, artifact identity, and
SHA-256. In worker mode the runtime validates the pinned PP-OCRv6 small
recognition ONNX model and generated character dictionary once, then handles
strictly framed finite recognition requests through that same initialized
DirectML session.

The runtime does not capture the screen, choose a region, classify game state,
write the event stream, invoke another Action, or switch models. The
game-neutral text-regions worker accepts one caller-owned RGB24 region and
returns PP-OCR quadrilaterals, detection confidence, recognized text,
recognition confidence, provenance, and timings. The executable-specific
`ppocr-w480-text-v1` Action owns its
reviewed 1920x1080 reference rectangle and reference sampling choice. A Rule
runtime profile may keep the initialized worker alive only while that Rule is
active; reuse does not make the OCR runtime a Monitor.

The Elite Dangerous path is:

```text
1920x1080 reference rectangle
  -> centered 16:9 reference-density 400x40 WGC region capture
  -> RGB24 w480 text-line worker request
  -> raw text, confidence, provenance, model identity, and timing
```

The Action requires the returned reference sample to remain exactly 10:1, then
the model preprocessor scales it to 480x48. It returns no semantic `state`
field. The separately declared pure `elite-dangerous/flight-status` Action
accepts its complete raw result and decides whether text such as
`SUPERCRUISE`, docking, or FSD alignment reaches the reviewed confidence and
separation thresholds. It returns `UNKNOWN` when the evidence does not support
a finite state. Connecting repeated OCR results to events remains an explicit
registration concern.

The Elite Dangerous ship-status path uses a second profile and three finite
layers:

```text
reference-density 320x150 lower-right ROI
  -> resident PP-OCRv6 small detection + w480 recognition
  -> raw boxes plus same-line, same-frame left-context pixels
  -> pure MASS / LANDING / CARGO prefix classifier
  -> independent ON / OFF / UNKNOWN indicators
```

Boxes touching the ROI's left edge return an explicitly empty left context.
Only the game classifier interprets left context; a required label with missing
or ambiguous context becomes `UNKNOWN`.

The visual ship-speed path reuses the same resident profile but owns a separate
reference-density ROI:

```text
260x170 speed-number search ROI
  -> PP-OCR quadrilateral and recognition evidence
  -> game-specific geometry and digit-count checks
  -> unitless KNOWN displayValue or UNKNOWN
```

The classifier is intentionally fail-closed around reviewed `0/8` and `6/8`
confusions. The result is visual evidence only; Player Journal, `Status.json`,
and requested throttle are not fallback sources.

## Rule lifecycle

Rule schema version 5 declares worker residency independently from Action and
registration declarations:

```json
{
  "runtimeProfiles": {
    "ocr/w480": {
      "runtime": "ppocr-onnx-dml-worker-v1",
      "residency": "while-rule-active",
      "artifactId": "ppocrv6-small-rec-onnx-official-w480"
    },
    "ocr/text-regions": {
      "runtime": "ppocr-onnx-dml-text-regions-worker-v1",
      "residency": "while-rule-active",
      "artifactId": "ppocrv6-small-det-onnx-official"
    }
  }
}
```

The foreground reconcile loop starts the worker when the owning Rule becomes
active and stops it when that Rule loses ownership. Initialization failure is
remembered for the rest of that Rule activation. The manager neither retries
silently nor changes provider or artifact. The runtime profile appears at
`GET /v4/rules/{rule-id}/runtimes`; it creates no timer or event subscription.

## Model preparation

PaddleOCR publishes separate ONNX detection and recognition packages for
PP-OCRv6 small. The preparer downloads both official archives, verifies their
pinned archive, ONNX, and YAML digests, extracts the 18,709-entry recognition
dictionary, and specializes the dynamic recognition input to one explicit
width:

```bash
python3 -m pip install -r tools/ppocr-model/requirements-build.in
python3 tools/ppocr-model/prepare.py \
  --output-dir /absolute/empty/output \
  --recognition-input-width 480
```

The `w480` specialization has input `[1,3,48,480]`. Its source is the official
PP-OCRv6 small recognition ONNX artifact with SHA-256
`5435fd747c9e0efe15a96d0b378d5bd157e9492ed8fd80edf08f30d02fa24634`;
the specialized artifact SHA-256 is
`5352753ebb51f62d5e37d66b3efb1268280edad0f0ffe4ac9a993b535dca3cd5`.
The transform changes only declared input dimensions and runs ONNX shape
inference and the full checker.

## DirectML and failure policy

The runtime pins ONNX Runtime DirectML 1.24.4, adapter 0, sequential execution,
disabled memory patterns, and `session.disable_cpu_ep_fallback=1`. The official
dynamic recognition model is rejected because ONNX Runtime assigns graph nodes
to the default CPU provider. The fixed `w480` graph initializes and runs with
CPU provider fallback disabled.

Unknown fields or arguments, path escape, missing artifacts, filename or hash
mismatch, malformed RGB length, incompatible aspect ratio, changed model I/O,
non-finite model output, malformed worker frame, unavailable DirectML, and any
required CPU provider assignment are terminal. The runtime never switches to
the dynamic model, CPU, PP-OCR tiny, a different width, or stretched input.

## Windows evidence

On 2026-08-07, the strict `w480` runtime recognized the reviewed Elite
Dangerous `800x80` Supercruise ROI as `SUPERCRUISE ASSIST ACTIVE` with confidence
`0.958969`. The exact RGB24 evidence SHA-256 was
`6dd02e520537d6476b843097ae173b19e9968cde73f016714ef911e5b271c3a3`.

One bounded ten-run same-session measurement produced identical text and
confidence in all ten runs. Model load was `1465.78 ms`; warm wall time ranged
from `28.67 ms` to `167.77 ms`, with median `108.26 ms`. Inference median was
`84.68 ms`. Separate-process cold runs were materially slower, so a reusable
bounded session is required before timer-driven use. These measurements are
device- and contention-specific and are not a portable performance guarantee.

The later flight-status calibration replayed 28 reviewed reference-density
crops five times through one resident worker, for 140 recognition calls. Every
image produced identical text and confidence across its five passes. Across
all calls, inference median was `12.29 ms` and p95 was `30.61 ms`; total worker
median was `30.76 ms` and p95 was `48.07 ms`. The separate semantic Action uses
`OCR confidence * phrase similarity >= 0.30` and a best-versus-runner-up margin
of at least `0.10`. In the reviewed set, the weakest accepted example scored
`0.3149`, while the strongest unknown interference scored `0.2382`.

On 2026-08-08, the text-regions worker processed the reviewed Elite Dangerous
ship-status crop at the configured 320x150 reference density. It returned
`MASS LOCKED`, `LANDING GEAR.`, and `CARGO SCOOP` as separate boxes with
recognition confidence `0.954942`, `0.989826`, and `0.998809`. Model load was
`862.16 ms`; the measured complete request was `698.28 ms`, including six line
recognitions. This is one device-specific validation sample, not a throughput
guarantee.

## Deferred

- add explicit registration execution for multi-frame confirmation and event
  emission;
- benchmark a separately declared PP-OCRv6 tiny artifact only if small remains
  too slow after session reuse.
