# ScreenParser Loop Module

## Status

**Retired.**

This document records the former autonomous loop design and its acceptance
evidence. It is no longer an active runtime contract. The Palworld Rule now uses
the finite [ScreenParser Action](screenparser-action.md),
and installation removes the owned legacy ScreenParser and scene-reducer tasks.

The repository ships the independent self-contained .NET loop runtime, strict
manifest, Windows installer, Palworld executable Rule, contract tests, reproducible
runtime publisher, and a build-only exporter for the pinned official
ScreenParser v2 model. Model weights and generated ONNX bytes are deliberately
not committed. Generic automatic lifecycle management for every configured
Game Rule remains deferred.

## Boundary

ScreenParser v2 is an independent `loop` module enabled only by the current
Game executable's Rule config. Its process owns capture, its configured time
loop, inference, deterministic post-processing, and append to the event stream.
The Windows Agent control plane owns process start, stop, foreground/session
binding, and cleanup; it does not schedule individual inference ticks.

The package must explicitly declare model identity, weights location, interval,
capture target, confidence/class settings, event schema, and foreground-loss
behavior. Missing model/config state is terminal. No alternate model, capture
backend, or event sink is selected automatically.

The landed `screenparser-onnx-dml-v1` runtime validates the exact ONNX digest,
requires `directml:0`, observes the foreground executable, captures the primary
monitor through Win32 GDI, applies the fixed YOLO bilinear letterbox and
class-aware NMS contract, runs one inference every configured interval, and
directly appends to the event stream. It pauses while the owning executable is
not foreground and emits lifecycle events on foreground changes.

`screenparser.parsed` events include model provenance, original frame size and
RGB hash, inference latency, detection count, and ordered detections with class,
confidence, pixel coordinates, and normalized coordinates. ScreenParser is a
detector only: it never invents OCR or visible text. A JSONL process log and
atomically replaced `status.json` expose task health without replacing the
durable event journal.

Capture, DirectML initialization, model-load, inference, and event-append failures are terminal.
The process attempts one `screenparser.failure` append, records the local
failure, and exits; it never changes model, device, capture backend, or sink.

## Current pinned artifacts

- repository: `docling-project/ScreenParser`;
- revision: `f029e565f1206577402e43206454522075be3f72`;
- file: `best.pt`;
- SHA-256: `dbcb4f583ccfdb8100a68e606525c247890a2de4c1a54b14741e0ee29ce0ab88`;
- license declared by the model card: Apache-2.0.

The checkpoint is a PyTorch pickle artifact and is loaded only in the isolated
build-time exporter after exact digest verification. It is not loaded by the
Windows runtime.

The fixed exporter uses PyTorch `2.13.0`, torchvision `0.28.0`, Ultralytics
`8.4.115`, and ONNX `1.22.0` to produce a verified build intermediate. The
accepted runtime artifact is converted with `onnxconverter-common==1.16.0` to
FP16 weights with float graph I/O, ONNX opset 20, and 1280x1280 input. Its
SHA-256 is
`8f22b0a224571076a2c9631649fbe2f54e0d07ae2682a9be03c665cf9396d055`
and it is 51,156,459 bytes. The production manifest and runtime reject FP32;
there is no precision fallback.

The acceptance corpus compared ten Palworld frames with six calibration and
four holdout frames. Across all frames FP16 matched 798 of 812 FP32 reference
detections; holdout recall was 98.36% and precision was 99.45% at equal class
and IoU at least 0.5. A bounded Windows DirectML one-shot test on a fixed
3840x2160 holdout frame matched all 57 detections and reduced median end-to-end
inference from 1,130.52 ms to 122.08 ms while Palworld remained running.

The formal Windows runtime pins `Microsoft.ML.OnnxRuntime.DirectML==1.24.4`
inside a self-contained .NET 8 `win-x64` executable. The installer verifies its
Windows GUI subsystem and companion `runtime-artifact.json`, stores runtime
versions by artifact hash, shares one runtime/model copy between Game
configs, and installs no Python, PyTorch, CUDA Toolkit, or .NET SDK.

## Browser capability acceptance

The 2026-08-07 interactive-session capability test used Edge on the ScreenParse
project page at 3840x2160. The runtime ran in Windows Session 1, changed from
`paused` to `active` when `msedge.exe` became foreground, and committed one
parsed event every configured two seconds. Observed DirectML inference was
typically 138-174 ms after warm-up; frame hashes, detection counts, classes,
and event sequences changed continuously with the live screen.

Foregrounding Notepad held the tick at 67 and produced no parsed events during
the pause window. Returning to Edge continued the same journal from sequence
80 through 85. A task restart changed both PID and producer session while the
durable journal continued from sequence 122 through 127. No
`screenparser.failure` event or local error log was observed. The accepted
installed runtime, model, operational log, and status footprint was
208,523,414 bytes; failed PyTorch deployment artifacts and acceptance-only
files were removed afterward. Edge was only a temporary test target; it is not
a shipped Game Rule, default target, or fallback.

## Deferred

- automatic discovery/start/stop for all Game Rules rather than one installed
  task per explicit Rule;
- durable image artifact storage for a later ScreenVLM reactor;
- log segment rotation and frame-artifact expiry;
- additional native-game tuning beyond the Palworld deployment target.
