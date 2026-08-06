#!/usr/bin/env python3
"""Convert the pinned verified ScreenParser FP32 build intermediate to accepted FP16 ONNX."""

from __future__ import annotations

import argparse
import hashlib
import importlib.metadata
import json
import sys
from pathlib import Path
from typing import Sequence


SOURCE_ARTIFACT_ID = "screenparser-v2-f029e565-onnx-fp32-opset20-1280"
SOURCE_SHA256 = "cce50b5720daa4d3cd3ab552a87f245feeb9711dc8aa26da01368d265f488d08"
ARTIFACT_ID = "screenparser-v2-f029e565-onnx-fp16-opset20-1280"
ARTIFACT_FILENAME = "screenparser-v2-f029e565-opset20-fp16-1280.onnx"
ARTIFACT_SHA256 = "8f22b0a224571076a2c9631649fbe2f54e0d07ae2682a9be03c665cf9396d055"
ARTIFACT_BYTES = 51_156_459
CONVERTER_VERSION = "1.16.0"
INPUT_NAME = "images"
OUTPUT_NAME = "output0"
INPUT_SHAPE = [1, 3, 1280, 1280]
OUTPUT_SHAPE = [1, 59, 33600]


class ConversionError(RuntimeError):
    pass


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def absolute_file(value: str, name: str) -> Path:
    path = Path(value)
    if not path.is_absolute() or not path.is_file():
        raise ConversionError(f"{name} must be an existing absolute file: {path}")
    return path


def empty_output_directory(value: str) -> Path:
    path = Path(value)
    if not path.is_absolute():
        raise ConversionError("--output-dir must be absolute")
    path.mkdir(parents=True, exist_ok=True)
    if any(path.iterdir()):
        raise ConversionError(f"--output-dir must be empty: {path}")
    return path


def dimensions(value: object) -> list[int]:
    return [item.dim_value for item in value.type.tensor_type.shape.dim]


def run(argv: Sequence[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-model", required=True)
    parser.add_argument("--source-artifact", required=True)
    parser.add_argument("--output-dir", required=True)
    arguments = parser.parse_args(argv)
    source_model = absolute_file(arguments.source_model, "--source-model")
    source_artifact_path = absolute_file(arguments.source_artifact, "--source-artifact")
    output = empty_output_directory(arguments.output_dir)

    source_artifact = json.loads(source_artifact_path.read_text(encoding="utf-8"))
    required_source = {
        "artifactId": SOURCE_ARTIFACT_ID,
        "sha256": SOURCE_SHA256,
        "precision": "fp32",
        "opset": 20,
        "inputName": INPUT_NAME,
        "outputName": OUTPUT_NAME,
        "inputWidth": 1280,
        "inputHeight": 1280,
    }
    for field, expected in required_source.items():
        if source_artifact.get(field) != expected:
            raise ConversionError(f"source artifact {field} mismatch: expected={expected} actual={source_artifact.get(field)}")
    source_provenance = source_artifact.get("source")
    if not isinstance(source_provenance, dict):
        raise ConversionError("source artifact must contain explicit model source provenance")
    required_provenance = {
        "repository": "docling-project/ScreenParser",
        "revision": "f029e565f1206577402e43206454522075be3f72",
        "filename": "best.pt",
        "sha256": "dbcb4f583ccfdb8100a68e606525c247890a2de4c1a54b14741e0ee29ce0ab88",
        "license": "Apache-2.0",
    }
    if source_provenance != required_provenance:
        raise ConversionError("source artifact model provenance does not equal the pinned ScreenParser source")
    actual_source_hash = sha256_file(source_model)
    if actual_source_hash != SOURCE_SHA256:
        raise ConversionError(f"source model sha256 mismatch: expected={SOURCE_SHA256} actual={actual_source_hash}")

    actual_converter_version = importlib.metadata.version("onnxconverter-common")
    if actual_converter_version != CONVERTER_VERSION:
        raise ConversionError(
            f"onnxconverter-common version mismatch: expected={CONVERTER_VERSION} actual={actual_converter_version}"
        )

    import onnx
    import onnxruntime
    from onnxconverter_common.float16 import convert_float_to_float16

    model = onnx.load(source_model)
    converted = convert_float_to_float16(model, keep_io_types=True, disable_shape_infer=False)
    artifact_path = output / ARTIFACT_FILENAME
    onnx.save(converted, artifact_path)
    onnx.checker.check_model(onnx.load(artifact_path), full_check=True)
    session = onnxruntime.InferenceSession(str(artifact_path), providers=["CPUExecutionProvider"])
    if len(session.get_inputs()) != 1 or session.get_inputs()[0].name != INPUT_NAME or session.get_inputs()[0].shape != INPUT_SHAPE:
        raise ConversionError("FP16 artifact input contract mismatch")
    if len(session.get_outputs()) != 1 or session.get_outputs()[0].name != OUTPUT_NAME or session.get_outputs()[0].shape != OUTPUT_SHAPE:
        raise ConversionError("FP16 artifact output contract mismatch")
    if session.get_inputs()[0].type != "tensor(float)" or session.get_outputs()[0].type != "tensor(float)":
        raise ConversionError("FP16 artifact must retain float graph I/O")

    actual_hash = sha256_file(artifact_path)
    actual_bytes = artifact_path.stat().st_size
    if actual_hash != ARTIFACT_SHA256 or actual_bytes != ARTIFACT_BYTES:
        raise ConversionError(
            f"FP16 artifact identity mismatch: expected_sha256={ARTIFACT_SHA256} actual_sha256={actual_hash} "
            f"expected_bytes={ARTIFACT_BYTES} actual_bytes={actual_bytes}"
        )

    artifact = {
        "schemaVersion": 1,
        "artifactId": ARTIFACT_ID,
        "format": "onnx",
        "filename": ARTIFACT_FILENAME,
        "sha256": actual_hash,
        "bytes": actual_bytes,
        "precision": "fp16",
        "opset": 20,
        "inputName": INPUT_NAME,
        "outputName": OUTPUT_NAME,
        "inputWidth": 1280,
        "inputHeight": 1280,
        "outputShape": OUTPUT_SHAPE,
        "source": source_provenance,
        "build": {
            "sourceArtifactId": SOURCE_ARTIFACT_ID,
            "sourceArtifactSha256": SOURCE_SHA256,
            "onnx": onnx.__version__,
            "onnxconverterCommon": actual_converter_version,
            "onnxruntime": onnxruntime.__version__,
            "keepGraphIoFloat": True,
        },
    }
    manifest = output / "artifact.json"
    manifest.write_text(json.dumps(artifact, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"artifact": str(artifact_path), "manifest": str(manifest), "sha256": actual_hash, "bytes": actual_bytes}))
    return 0


def main() -> None:
    try:
        raise SystemExit(run(sys.argv[1:]))
    except (ConversionError, json.JSONDecodeError) as error:
        print(f"[FATAL] {error}", file=sys.stderr)
        raise SystemExit(1) from error


if __name__ == "__main__":
    main()
