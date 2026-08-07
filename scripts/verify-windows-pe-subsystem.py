#!/usr/bin/env python3
"""Verify the PE subsystem of a Windows executable without external packages."""

from __future__ import annotations

import argparse
import hashlib
import json
import struct
from pathlib import Path


SUBSYSTEMS = {
    "gui": 2,
    "console": 3,
}


def read_subsystem(executable: Path) -> int:
    data = executable.read_bytes()
    if len(data) < 0x40 or data[:2] != b"MZ":
        raise ValueError("file does not have a valid DOS header")
    pe_offset = struct.unpack_from("<I", data, 0x3C)[0]
    optional_offset = pe_offset + 24
    if pe_offset + 4 > len(data) or data[pe_offset : pe_offset + 4] != b"PE\0\0":
        raise ValueError("file does not have a valid PE signature")
    if optional_offset + 0x46 > len(data):
        raise ValueError("file has a truncated PE optional header")
    magic = struct.unpack_from("<H", data, optional_offset)[0]
    if magic not in (0x10B, 0x20B):
        raise ValueError(f"unsupported PE optional-header magic 0x{magic:04x}")
    return struct.unpack_from("<H", data, optional_offset + 0x44)[0]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("executable", type=Path)
    parser.add_argument("--expect", choices=sorted(SUBSYSTEMS), required=True)
    args = parser.parse_args()

    executable = args.executable.resolve(strict=True)
    actual = read_subsystem(executable)
    expected = SUBSYSTEMS[args.expect]
    if actual != expected:
        raise SystemExit(
            f"{executable} has PE subsystem {actual}; expected {expected} ({args.expect})"
        )

    print(
        json.dumps(
            {
                "executable": str(executable),
                "subsystem": actual,
                "subsystemName": args.expect,
                "sha256": hashlib.sha256(executable.read_bytes()).hexdigest(),
            },
            separators=(",", ":"),
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
