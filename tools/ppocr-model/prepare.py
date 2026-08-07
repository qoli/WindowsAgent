#!/usr/bin/env python3
"""Prepare the official PP-OCRv6 small ONNX artifacts for WindowsAgent."""

from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import sys
import tarfile
import tempfile
import urllib.request
from pathlib import Path


MODEL_BASE = "https://paddle-model-ecology.bj.bcebos.com/paddlex/official_inference_model/paddle3.0.0"
ARCHIVES = {
    "detection": {
        "filename": "PP-OCRv6_small_det_onnx_infer.tar",
        "sha256": "d218f6fbf0f1c23d2161bd6ac7f5eaa6104fa89955c09290497e31008e2618e4",
        "directory": "PP-OCRv6_small_det_onnx_infer",
        "modelSha256": "d73e0058b7a8086bbd57f3d10b8bcd4ff95363f67e06e2762b5e814fe9c9410e",
        "configSha256": "193f435274bf9f0b5f71a929bbfbcf148282df7e633b34e7c373e8f44741b516",
    },
    "recognition": {
        "filename": "PP-OCRv6_small_rec_onnx_infer.tar",
        "sha256": "d267ab077a44a0eedb1ea8f8c542d263f211de8e9d7a029bf9fcfff7e5a88fb1",
        "directory": "PP-OCRv6_small_rec_onnx_infer",
        "modelSha256": "5435fd747c9e0efe15a96d0b378d5bd157e9492ed8fd80edf08f30d02fa24634",
        "configSha256": "ab078671bb49f06228eadccd34f1bb501e157f7a047095ffb943ba81512c77d1",
    },
}


class PrepareError(RuntimeError):
    pass


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def require_hash(path: Path, expected: str, label: str) -> None:
    actual = sha256_file(path)
    if actual != expected:
        raise PrepareError(f"{label} sha256 mismatch: expected={expected} actual={actual}")


def download(url: str, destination: Path) -> None:
    request = urllib.request.Request(url, headers={"User-Agent": "WindowsAgent-PP-OCR-artifact-preparer/1"})
    with urllib.request.urlopen(request, timeout=120) as response, destination.open("wb") as output:
        shutil.copyfileobj(response, output)


def extract_archive(archive: Path, destination: Path, expected_directory: str) -> None:
    with tarfile.open(archive, "r") as source:
        members = source.getmembers()
        expected = {
            expected_directory,
            f"{expected_directory}/inference.onnx",
            f"{expected_directory}/inference.yml",
        }
        actual = {member.name for member in members}
        if actual != expected:
            raise PrepareError(f"archive members do not match the pinned contract: {sorted(actual)}")
        for member in members:
            if member.issym() or member.islnk() or not (member.isdir() or member.isfile()):
                raise PrepareError(f"archive contains unsupported member: {member.name}")
        source.extractall(destination, members=members, filter="data")


def leading_spaces(value: str) -> int:
    return len(value) - len(value.lstrip(" "))


def parse_yaml_scalar(raw_value: str) -> str:
    value = raw_value.lstrip(" ")
    if len(value) >= 2 and value[0] == value[-1] == "'":
        return value[1:-1].replace("''", "'")
    if len(value) >= 2 and value[0] == value[-1] == '"':
        return bytes(value[1:-1], "utf-8").decode("unicode_escape")
    return value


def extract_characters(config: Path) -> list[str]:
    lines = config.read_text(encoding="utf-8").replace("\r\n", "\n").replace("\r", "\n").split("\n")
    postprocess = next((index for index, line in enumerate(lines) if line.strip() == "PostProcess:"), -1)
    if postprocess < 0:
        raise PrepareError("recognition config is missing PostProcess")
    postprocess_indent = leading_spaces(lines[postprocess])
    dictionary = -1
    for index in range(postprocess + 1, len(lines)):
        stripped = lines[index].strip()
        if not stripped or stripped.startswith("#"):
            continue
        if leading_spaces(lines[index]) <= postprocess_indent:
            break
        if stripped == "character_dict:":
            dictionary = index
            break
    if dictionary < 0:
        raise PrepareError("recognition config is missing PostProcess.character_dict")
    dictionary_indent = leading_spaces(lines[dictionary])
    characters: list[str] = []
    for line in lines[dictionary + 1 :]:
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        indent = leading_spaces(line)
        content = line[indent:]
        if not content.startswith("-"):
            if indent <= dictionary_indent:
                break
            continue
        characters.append(parse_yaml_scalar(content[1:]))
    if not characters:
        raise PrepareError("recognition character dictionary is empty")
    if characters[-1] != " ":
        characters.append(" ")
    if len(characters) != len(set(characters)):
        raise PrepareError("recognition character dictionary contains duplicates")
    return characters


def run(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--recognition-input-width", required=True, type=int)
    args = parser.parse_args(argv)
    output = Path(args.output_dir)
    if not output.is_absolute():
        raise PrepareError("--output-dir must be absolute")
    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        raise PrepareError(f"--output-dir must be empty: {output}")
    if args.recognition_input_width < 16 or args.recognition_input_width > 3200:
        raise PrepareError("--recognition-input-width must be between 16 and 3200")

    with tempfile.TemporaryDirectory(prefix="ppocrv6-small-") as temporary_value:
        temporary = Path(temporary_value)
        extracted: dict[str, Path] = {}
        for role, declaration in ARCHIVES.items():
            archive = temporary / declaration["filename"]
            download(f"{MODEL_BASE}/{declaration['filename']}", archive)
            require_hash(archive, declaration["sha256"], f"{role} archive")
            extract_archive(archive, temporary, declaration["directory"])
            model_root = temporary / declaration["directory"]
            require_hash(model_root / "inference.onnx", declaration["modelSha256"], f"{role} ONNX")
            require_hash(model_root / "inference.yml", declaration["configSha256"], f"{role} config")
            extracted[role] = model_root

        files = {
            "detectionModel": "ppocrv6-small-det.onnx",
            "detectionConfig": "ppocrv6-small-det.yml",
            "recognitionModel": "ppocrv6-small-rec.onnx",
            "recognitionConfig": "ppocrv6-small-rec.yml",
            "characters": "ppocrv6-small-characters.json",
        }
        shutil.copyfile(extracted["detection"] / "inference.onnx", output / files["detectionModel"])
        shutil.copyfile(extracted["detection"] / "inference.yml", output / files["detectionConfig"])
        shutil.copyfile(extracted["recognition"] / "inference.onnx", output / files["recognitionModel"])
        shutil.copyfile(extracted["recognition"] / "inference.yml", output / files["recognitionConfig"])
        characters = extract_characters(extracted["recognition"] / "inference.yml")
        (output / files["characters"]).write_text(
            json.dumps(characters, ensure_ascii=False, separators=(",", ":")) + "\n",
            encoding="utf-8",
        )

    try:
        import onnx
    except ImportError as error:
        raise PrepareError("onnx==1.19.1 is required to specialize the recognition input shape") from error
    source_model = output / files["recognitionModel"]
    specialized_name = f"ppocrv6-small-rec-w{args.recognition_input_width}.onnx"
    specialized_path = output / specialized_name
    model = onnx.load(source_model)
    dimensions = model.graph.input[0].type.tensor_type.shape.dim
    if len(dimensions) != 4:
        raise PrepareError("recognition ONNX input must have rank 4")
    for dimension, value in zip(dimensions, [1, 3, 48, args.recognition_input_width]):
        dimension.ClearField("dim_param")
        dimension.dim_value = value
    model = onnx.shape_inference.infer_shapes(model)
    onnx.checker.check_model(model, full_check=True)
    onnx.save(model, specialized_path)

    artifact = {
        "schemaVersion": 1,
        "family": "PP-OCRv6_small",
        "format": "onnx",
        "source": {
            "project": "PaddlePaddle/PaddleOCR",
            "documentation": "https://www.paddleocr.ai/latest/en/version3.x/inference_deployment/cross_platform/android_deployment.html",
            "baseUrl": MODEL_BASE,
            "archives": ARCHIVES,
        },
        "models": {
            "detection": {
                "filename": files["detectionModel"],
                "sha256": sha256_file(output / files["detectionModel"]),
                "opset": 14,
                "inputName": "x",
                "outputName": "fetch_name_0",
            },
            "recognition": {
                "filename": files["recognitionModel"],
                "sha256": sha256_file(output / files["recognitionModel"]),
                "opset": 11,
                "inputName": "x",
                "outputName": "fetch_name_0",
                "inputHeight": 48,
                "maxInputWidth": 3200,
                "classCount": len(characters) + 1,
                "specializations": {
                    f"w{args.recognition_input_width}": {
                        "filename": specialized_name,
                        "sha256": sha256_file(specialized_path),
                        "inputWidth": args.recognition_input_width,
                    }
                },
            },
        },
        "characters": {
            "filename": files["characters"],
            "sha256": sha256_file(output / files["characters"]),
            "count": len(characters),
            "blankIndex": 0,
        },
    }
    artifact_path = output / "models-artifact.json"
    artifact_path.write_text(json.dumps(artifact, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    runtime_config = {
        "schemaVersion": 1,
        "runtime": "ppocr-onnx-dml-v1",
        "pipeline": "text-line-recognition",
        "model": {
            "artifactId": f"ppocrv6-small-rec-onnx-official-w{args.recognition_input_width}",
            "format": "onnx",
            "filename": specialized_name,
            "sha256": sha256_file(specialized_path),
            "opset": artifact["models"]["recognition"]["opset"],
            "inputName": artifact["models"]["recognition"]["inputName"],
            "outputName": artifact["models"]["recognition"]["outputName"],
            "inputHeight": artifact["models"]["recognition"]["inputHeight"],
            "inputWidth": args.recognition_input_width,
            "classCount": artifact["models"]["recognition"]["classCount"],
        },
        "characters": artifact["characters"],
        "inference": {"device": "directml:0"},
    }
    config_path = output / "runtime-config.json"
    config_path.write_text(json.dumps(runtime_config, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(
        json.dumps(
            {
                "output": str(output),
                "manifest": str(artifact_path),
                "runtimeConfig": str(config_path),
                "characterCount": len(characters),
            }
        )
    )
    return 0


def main() -> None:
    try:
        raise SystemExit(run(sys.argv[1:]))
    except (PrepareError, OSError) as error:
        print(f"[FATAL] {error}", file=sys.stderr)
        raise SystemExit(1) from error


if __name__ == "__main__":
    main()
