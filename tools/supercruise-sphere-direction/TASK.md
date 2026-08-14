# Offline full-resolution sphere-direction simulation

This developer tool validates a conventional-CV approach for the
`MOVE TO OBTAIN LINE OF SIGHT TO TARGET` obstruction case. The offline
simulation itself performs no capture, OCR, Action invocation, input,
deployment, or network access. Its reviewed geometry is also implemented by
the package-owned native library source in `src/lib.rs`; the production
`elite-dangerous/supercruise-sphere-direction` finite Action supplies that
library with one explicit 256×144 full-viewport reference sample.

The simulation follows three explicit stages:

1. **Maximum black/white separation.** Convert the complete native screenshot
   to luminance, apply a bounded 9×9 Gaussian blur, and choose the global Otsu
   threshold. Otsu maximizes between-class variance; the resulting binary image
   and black/white proportions are retained.
2. **Full-size sphere geometry.** Extract large external threshold contours and
   run deterministic robust circle fitting directly in the source coordinate
   space. The accepted fit reports centre, radius, angular coverage, inlier
   count, residual, candidate count, and confidence. No resized coordinate is
   used to calculate the geometry.
3. **Suggested escape direction.** Compare the projected sphere centre with the
   screen centre. If the screen centre remains inside the sphere or lacks the
   configured outside clearance, suggest the direction from the sphere centre
   toward and beyond the screen centre. This is the outward image-space
   direction: positive X is `YAW_RIGHT`, negative X is `YAW_LEFT`, positive Y
   is `PITCH_DOWN`, and negative Y is `PITCH_UP`.

The output includes the full-size binary image, full-size annotated source,
compact preview, and JSON report. The suggested direction is a simulation
result only. It does not prove the selected target is behind that body, predict
3D orbital motion, or authorize a flight control.

The production Action deliberately does not claim native-resolution geometry.
It exposes its reference sampling and algorithm provenance, while the owning
Streaming Action supplies the temporal body-exit, progress, control, and
failure-compensation Gates.

## Usage

```bash
python3 tools/supercruise-sphere-direction/offline_simulation.py \
  /absolute/path/to/screenshot.jpg \
  --output-dir /absolute/output-directory
```

Runtime dependencies are Python 3, NumPy, and OpenCV.

Build the pinned Windows native artifact with:

```bash
tools/supercruise-sphere-direction/build-windows-native.sh
```
