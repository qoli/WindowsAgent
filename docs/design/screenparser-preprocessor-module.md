# ScreenParser Preprocessor Module

## Status

**Partially landed.**

The strict manifest, finite DirectML runtime, request/response contracts,
publisher, installer migration, Palworld Rule, and contract tests are
implemented. The ScreenVLM reactor that decides when to request preprocessing
and authors semantic events remains deferred.

## Boundary

`screenparser/ui-elements` is an on-demand `preprocessor`, not an observer loop
or event source. A trusted caller supplies one RGB24 frame artifact with its
absolute path, dimensions, capture time, target executable, and SHA-256. The
runtime validates that the artifact is below the caller-declared frame root,
runs exactly one inference, atomically creates one response, and exits.

The module does not capture the desktop, poll foreground state, keep a cursor,
write the event stream, call a VLM, infer semantic Game state, or request an
action. Repeated or timed use belongs to the later VLM reactor or its host.

## Invocation

```powershell
.\ScreenParser.DirectML.exe `
  --config C:\Rules\Palworld-Win64-Shipping.exe\Modules\screenparser\manifest.json `
  --model C:\Models\screenparser-v2-f029e565-opset20-fp16-1280.onnx `
  --request C:\Frames\request.json `
  --frame-root C:\Frames `
  --response C:\Frames\response.json
```

The request is strict JSON:

```json
{
  "schemaVersion": 1,
  "requestId": "vlm-frame-0001",
  "targetExecutable": "Palworld-Win64-Shipping.exe",
  "frame": {
    "artifactId": "capture-0001",
    "capturedAt": "2026-08-07T03:00:00.000000Z",
    "rgbPath": "C:\\Frames\\capture-0001.rgb",
    "sha256": "<64 lowercase hex characters>",
    "width": 3840,
    "height": 2160
  }
}
```

The response binds detections to the request ID, exact frame evidence, runtime,
provider, and model provenance. The response path must not already exist.

## Failure and precision policy

Unknown fields or arguments, target mismatch, path escape, missing frame,
incorrect RGB byte length or digest, changed model bytes, unsupported device,
and an existing response path are terminal. The runtime never captures a
replacement frame, changes device, switches models, retries another input, or
appends a partial result.

Production accepts only the pinned FP16 ONNX artifact
`screenparser-v2-f029e565-onnx-fp16-opset20-1280`, SHA-256
`8f22b0a224571076a2c9631649fbe2f54e0d07ae2682a9be03c665cf9396d055`.
FP32, CPU substitution, and precision fallback are rejected.

## Installation migration

The installer copies content-addressed runtime and model artifacts and syncs the
Rule. It creates no Scheduled Task and starts no process. When present, it
removes only the exact owned legacy ScreenParser loop and scene-reducer tasks;
name collisions with different task descriptions fail without removal. Existing
event journals and historical artifacts are preserved.

## Deferred

- VLM-owned frame selection and invocation policy;
- a durable image-artifact producer and expiry contract;
- semantic event authoring after VLM interpretation;
- bounded process reuse if measured startup cost later requires it.
