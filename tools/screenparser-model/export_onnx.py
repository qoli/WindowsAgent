#!/usr/bin/env python3
"""Export and verify the pinned ScreenParser v2 checkpoint as a DirectML artifact."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
from pathlib import Path
from typing import Any, Sequence


SOURCE_REPOSITORY = "docling-project/ScreenParser"
SOURCE_REVISION = "f029e565f1206577402e43206454522075be3f72"
SOURCE_FILENAME = "best.pt"
SOURCE_SHA256 = "dbcb4f583ccfdb8100a68e606525c247890a2de4c1a54b14741e0ee29ce0ab88"
SOURCE_LICENSE = "Apache-2.0"
ARTIFACT_ID = "screenparser-v2-f029e565-onnx-fp32-opset20-1280"
ARTIFACT_FILENAME = "screenparser-v2-f029e565-opset20-fp32-1280.onnx"
INPUT_NAME = "images"
OUTPUT_NAME = "output0"
INPUT_SIZE = 1280
OPSET = 20


class ExportError(RuntimeError):
    pass


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def require_absolute_file(raw: str, name: str) -> Path:
    path = Path(raw)
    if not path.is_absolute() or not path.is_file():
        raise ExportError(f"{name} must be an existing absolute file: {path}")
    return path


def require_output_directory(raw: str) -> Path:
    path = Path(raw)
    if not path.is_absolute():
        raise ExportError("--output-dir must be absolute")
    path.mkdir(parents=True, exist_ok=True)
    if any(path.iterdir()):
        raise ExportError(f"--output-dir must be empty: {path}")
    return path


def verify_equivalence(pytorch_model: Any, onnx_path: Path, images: Sequence[Path]) -> list[dict[str, Any]]:
    import cv2
    import numpy
    import onnxruntime
    import torch

    session = onnxruntime.InferenceSession(str(onnx_path), providers=["CPUExecutionProvider"])
    evidence: list[dict[str, Any]] = []
    for image in images:
        bgr = cv2.imread(str(image), cv2.IMREAD_COLOR)
        if bgr is None:
            raise ExportError(f"OpenCV could not decode validation image: {image}")
        height, width = bgr.shape[:2]
        scale = min(INPUT_SIZE / width, INPUT_SIZE / height)
        resized = (round(width * scale), round(height * scale))
        pad_x = (INPUT_SIZE - resized[0]) / 2
        pad_y = (INPUT_SIZE - resized[1]) / 2
        bgr = cv2.resize(bgr, resized, interpolation=cv2.INTER_LINEAR)
        bgr = cv2.copyMakeBorder(
            bgr,
            round(pad_y - 0.1),
            round(pad_y + 0.1),
            round(pad_x - 0.1),
            round(pad_x + 0.1),
            cv2.BORDER_CONSTANT,
            value=(114, 114, 114),
        )
        tensor = numpy.ascontiguousarray(bgr[:, :, ::-1].transpose(2, 0, 1)[None]).astype(numpy.float32) / 255
        with torch.no_grad():
            expected = pytorch_model.model(torch.from_numpy(tensor))
            if isinstance(expected, (tuple, list)):
                expected = expected[0]
            expected = expected.detach().cpu().numpy()
        actual = session.run([OUTPUT_NAME], {INPUT_NAME: tensor})[0]
        if expected.shape != actual.shape:
            raise ExportError(f"raw output shape mismatch for {image}: pytorch={expected.shape} onnx={actual.shape}")
        delta = numpy.abs(expected - actual)
        bbox_delta = delta[:, :4, :]
        score_delta = delta[:, 4:, :]
        bbox_max = float(bbox_delta.max())
        bbox_p99 = float(numpy.quantile(bbox_delta, 0.99))
        score_max = float(score_delta.max())
        score_p99 = float(numpy.quantile(score_delta, 0.99))
        if bbox_max > 0.25 or bbox_p99 > 0.005 or score_max > 0.0001 or score_p99 > 0.00001:
            raise ExportError(
                f"raw output numeric drift exceeds contract for {image}: bbox_max={bbox_max} bbox_p99={bbox_p99} score_max={score_max} score_p99={score_p99}"
            )
        evidence.append(
            {
                "image": image.name,
                "imageSha256": sha256_file(image),
                "outputShape": list(expected.shape),
                "bboxMaximumAbsoluteDelta": round(bbox_max, 9),
                "bboxP99AbsoluteDelta": round(bbox_p99, 9),
                "scoreMaximumAbsoluteDelta": round(score_max, 9),
                "scoreP99AbsoluteDelta": round(score_p99, 9),
            }
        )
    return evidence


def run(argv: Sequence[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-model", required=True)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--validation-image", action="append", required=True)
    args = parser.parse_args(argv)

    source = require_absolute_file(args.source_model, "--source-model")
    if sha256_file(source) != SOURCE_SHA256:
        raise ExportError(f"source model sha256 mismatch: expected={SOURCE_SHA256} actual={sha256_file(source)}")
    images = [require_absolute_file(value, "--validation-image") for value in args.validation_image]
    output_dir = require_output_directory(args.output_dir)
    staged_source = output_dir / SOURCE_FILENAME
    shutil.copyfile(source, staged_source)
    settings_directory = output_dir / "ultralytics-settings"
    settings_directory.mkdir()
    os.environ["YOLO_CONFIG_DIR"] = str(settings_directory)
    os.environ["YOLO_OFFLINE"] = "true"

    import onnx
    import torch
    import torchvision
    import ultralytics
    from ultralytics import YOLO

    pytorch_model = YOLO(str(staged_source))
    exported = Path(
        pytorch_model.export(
            format="onnx",
            imgsz=INPUT_SIZE,
            opset=OPSET,
            simplify=False,
            dynamic=False,
            nms=False,
            batch=1,
            half=False,
            device="cpu",
        )
    )
    if not exported.is_file():
        raise ExportError(f"Ultralytics did not create the declared ONNX file: {exported}")
    artifact_path = output_dir / ARTIFACT_FILENAME
    exported.replace(artifact_path)
    staged_source.unlink()

    model = onnx.load(str(artifact_path))
    onnx.checker.check_model(model, full_check=True)
    imported_opsets = {entry.domain: entry.version for entry in model.opset_import}
    if imported_opsets.get("") != OPSET:
        raise ExportError(f"ONNX default opset mismatch: expected={OPSET} actual={imported_opsets.get('')}")
    if len(model.graph.input) != 1 or model.graph.input[0].name != INPUT_NAME:
        raise ExportError("ONNX graph does not contain the canonical images input")
    if len(model.graph.output) != 1 or model.graph.output[0].name != OUTPUT_NAME:
        raise ExportError("ONNX graph does not contain the canonical output0 output")
    input_dimensions = [item.dim_value for item in model.graph.input[0].type.tensor_type.shape.dim]
    expected_input = [1, 3, INPUT_SIZE, INPUT_SIZE]
    if input_dimensions != expected_input:
        raise ExportError(f"ONNX input shape mismatch: expected={expected_input} actual={input_dimensions}")
    labels = [pytorch_model.names[index] for index in range(len(pytorch_model.names))]
    output_dimensions = [item.dim_value for item in model.graph.output[0].type.tensor_type.shape.dim]
    if len(output_dimensions) != 3 or output_dimensions[0] != 1 or output_dimensions[1] != len(labels) + 4:
        raise ExportError(f"ONNX output shape is incompatible with {len(labels)} ScreenParser classes: {output_dimensions}")

    validation = verify_equivalence(pytorch_model, artifact_path, images)
    artifact = {
        "schemaVersion": 1,
        "artifactId": ARTIFACT_ID,
        "format": "onnx",
        "filename": ARTIFACT_FILENAME,
        "sha256": sha256_file(artifact_path),
        "bytes": artifact_path.stat().st_size,
        "precision": "fp32",
        "opset": OPSET,
        "inputName": INPUT_NAME,
        "outputName": OUTPUT_NAME,
        "inputWidth": INPUT_SIZE,
        "inputHeight": INPUT_SIZE,
        "outputShape": output_dimensions,
        "labels": labels,
        "source": {
            "repository": SOURCE_REPOSITORY,
            "revision": SOURCE_REVISION,
            "filename": SOURCE_FILENAME,
            "sha256": SOURCE_SHA256,
            "license": SOURCE_LICENSE,
        },
        "build": {
            "torch": torch.__version__,
            "torchvision": torchvision.__version__,
            "ultralytics": ultralytics.__version__,
            "onnx": onnx.__version__,
            "validation": validation,
        },
    }
    manifest_path = output_dir / "artifact.json"
    manifest_path.write_text(json.dumps(artifact, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"artifact": str(artifact_path), "manifest": str(manifest_path), "sha256": artifact["sha256"], "bytes": artifact["bytes"]}))
    return 0


def main() -> None:
    try:
        raise SystemExit(run(os.sys.argv[1:]))
    except ExportError as error:
        print(f"[FATAL] {error}", file=os.sys.stderr)
        raise SystemExit(1) from error


if __name__ == "__main__":
    main()
