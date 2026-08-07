#!/usr/bin/env python3
"""Publish the PP-OCR ONNX DirectML runtime as a verified Windows bundle."""

from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


RUNTIME_ID = "ppocr-onnx-dml-v1"
RUNTIME_FILENAME = "PpOcr.DirectML.exe"
ONNX_RUNTIME_DIRECTML_VERSION = "1.24.4"


class PublishError(RuntimeError):
    pass


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def run(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dotnet", required=True)
    parser.add_argument("--output-dir", required=True)
    args = parser.parse_args(argv)
    dotnet = Path(args.dotnet)
    output = Path(args.output_dir)
    if not dotnet.is_absolute() or not dotnet.is_file():
        raise PublishError(f"--dotnet must be an existing absolute file: {dotnet}")
    if not output.is_absolute():
        raise PublishError("--output-dir must be absolute")
    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        raise PublishError(f"--output-dir must be empty: {output}")
    project = Path(__file__).resolve().parents[2] / "runtimes/ppocr-directml/PpOcr.DirectML/PpOcr.DirectML.csproj"
    if not project.is_file():
        raise PublishError(f"runtime project does not exist: {project}")

    with tempfile.TemporaryDirectory(prefix="ppocr-directml-publish-") as temporary:
        command = [
            str(dotnet),
            "publish",
            str(project),
            "--configuration",
            "Release",
            "--runtime",
            "win-x64",
            "--self-contained",
            "true",
            "-p:PublishSingleFile=true",
            "-p:IncludeNativeLibrariesForSelfExtract=true",
            "-p:DebugType=None",
            "-p:DebugSymbols=false",
            "--output",
            temporary,
        ]
        completed = subprocess.run(command, check=False)
        if completed.returncode != 0:
            raise PublishError(f"dotnet publish failed with exit code {completed.returncode}")
        source = Path(temporary) / RUNTIME_FILENAME
        if not source.is_file():
            raise PublishError(f"dotnet publish did not produce {RUNTIME_FILENAME}")
        executable = output / RUNTIME_FILENAME
        shutil.copyfile(source, executable)

    artifact = {
        "schemaVersion": 1,
        "runtimeId": RUNTIME_ID,
        "filename": RUNTIME_FILENAME,
        "sha256": sha256_file(executable),
        "bytes": executable.stat().st_size,
        "architecture": "win-x64",
        "subsystem": "console",
        "selfContained": True,
        "targetFramework": "net8.0-windows",
        "onnxRuntimeDirectML": ONNX_RUNTIME_DIRECTML_VERSION,
        "implementedPipelines": ["text-line-recognition", "text-region-detection-recognition"],
    }
    manifest = output / "runtime-artifact.json"
    manifest.write_text(json.dumps(artifact, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"executable": str(executable), "manifest": str(manifest), **artifact}))
    return 0


def main() -> None:
    try:
        raise SystemExit(run(sys.argv[1:]))
    except PublishError as error:
        print(f"[FATAL] {error}", file=sys.stderr)
        raise SystemExit(1) from error


if __name__ == "__main__":
    main()
