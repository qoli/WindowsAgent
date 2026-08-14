#!/usr/bin/env python3
"""Validate, package, deploy, and live-verify the complete Rules tree."""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tempfile
import uuid
import zipfile


SCHEMA_VERSION = 1
REMOTE_ARCHIVE = "windowsagent-rules.zip"
REMOTE_EXECUTOR = "apply-windows-rules.ps1"
CHECKER_NAME = "windows-action-check.exe"
FORBIDDEN_NAMES = {".DS_Store", "Thumbs.db", "desktop.ini"}
RULES_DEVELOPMENT_FILES = {"AGENTS.md"}
SSH_HOST_PATTERN = re.compile(r"^(?!-)[A-Za-z0-9_.@-]+$")


class DeployError(RuntimeError):
    """A bounded Rule deployment failure."""


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def validate_relative_path(relative: PurePosixPath) -> None:
    if relative.is_absolute() or not relative.parts:
        raise DeployError(f"Rule payload path is not relative: {relative}")
    for part in relative.parts:
        try:
            part.encode("ascii")
        except UnicodeEncodeError as error:
            raise DeployError(f"Rule payload paths must be ASCII for cross-platform identity: {relative}") from error
        if part in ("", ".", "..") or ":" in part or "\\" in part:
            raise DeployError(f"Rule payload path is not Windows-safe: {relative}")
        if part.endswith(" ") or part.endswith("."):
            raise DeployError(f"Rule payload path is not Windows-canonical: {relative}")


def is_platform_metadata(relative: PurePosixPath) -> bool:
    return any(
        part.startswith("._") or part in FORBIDDEN_NAMES or part == "__pycache__"
        for part in relative.parts
    )


def build_inventory(
    rules_root: Path,
) -> tuple[list[dict[str, object]], list[str], str, list[str]]:
    if not rules_root.is_dir():
        raise DeployError(f"Rules directory does not exist: {rules_root}")

    rule_ids: list[str] = []
    root_excluded: list[str] = []
    for child in sorted(rules_root.iterdir(), key=lambda value: value.name.casefold()):
        if child.is_symlink():
            raise DeployError(f"symlinks are forbidden in Rules: {child}")
        if child.is_file() and is_platform_metadata(PurePosixPath(child.name)):
            root_excluded.append(child.name)
            continue
        if child.is_file() and child.name in RULES_DEVELOPMENT_FILES:
            continue
        if not child.is_dir() or not child.name.lower().endswith(".exe"):
            raise DeployError(f"Rules root may contain only executable Rule directories: {child.name}")
        if not (child / "rule.json").is_file() or not (child / "AGENTS.md").is_file():
            raise DeployError(f"Rule is missing rule.json or AGENTS.md: {child.name}")
        rule_ids.append(child.name)
    if not rule_ids:
        raise DeployError("Rules directory contains no executable Rule plugins")

    entries: list[dict[str, object]] = []
    excluded: list[str] = root_excluded
    casefold_paths: dict[str, str] = {}
    payload_paths = (
        path
        for rule_id in rule_ids
        for path in (rules_root / rule_id).rglob("*")
    )
    for path in sorted(payload_paths, key=lambda value: value.as_posix().casefold()):
        relative = PurePosixPath(path.relative_to(rules_root).as_posix())
        validate_relative_path(relative)
        if path.is_symlink():
            raise DeployError(f"symlinks are forbidden in Rules: {relative}")
        if is_platform_metadata(relative):
            if path.is_file():
                excluded.append(relative.as_posix())
            continue
        if path.is_dir():
            continue
        if not path.is_file():
            raise DeployError(f"unsupported filesystem entry in Rules: {relative}")
        folded = relative.as_posix().casefold()
        previous = casefold_paths.get(folded)
        if previous is not None:
            raise DeployError(f"case-insensitive Rule path collision: {previous} and {relative}")
        casefold_paths[folded] = relative.as_posix()
        entries.append(
            {
                "path": relative.as_posix(),
                "bytes": path.stat().st_size,
                "sha256": sha256_file(path),
            }
        )
    if not entries:
        raise DeployError("Rules directory contains no files")
    return entries, rule_ids, tree_hash(entries), sorted(excluded, key=str.casefold)


def tree_hash(entries: list[dict[str, object]]) -> str:
    digest = hashlib.sha256()
    for entry in sorted(entries, key=lambda item: str(item["path"]).casefold()):
        record = f'{entry["path"]}\0{entry["bytes"]}\0{entry["sha256"]}\n'
        digest.update(record.encode("utf-8"))
    return digest.hexdigest()


def run(command: list[str], *, cwd: Path, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(
            command,
            cwd=cwd,
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
    except OSError as error:
        raise DeployError(f"cannot execute {command[0]}: {error}") from error


def require_command(name: str) -> None:
    if shutil.which(name) is None:
        raise DeployError(f"required command is missing: {name}")


def git_state(repo_root: Path) -> tuple[str, bool, str]:
    revision = run(["git", "rev-parse", "HEAD"], cwd=repo_root)
    if revision.returncode != 0:
        raise DeployError(f"resolve Git revision: {revision.stderr.strip()}")
    status = run(["git", "status", "--porcelain", "--untracked-files=all"], cwd=repo_root)
    if status.returncode != 0:
        raise DeployError(f"inspect Git worktree: {status.stderr.strip()}")
    return revision.stdout.strip(), bool(status.stdout.strip()), status.stdout.rstrip()


def run_local_checker(repo_root: Path) -> dict[str, object]:
    result = run(
        ["go", "run", "./cmd/windows-action-check", "--rules-dir", "Rules", "--json"],
        cwd=repo_root,
    )
    if result.returncode != 0:
        detail = result.stdout.strip() or result.stderr.strip()
        raise DeployError(f"local Rule check failed with exit {result.returncode}: {detail}")
    try:
        report = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise DeployError(f"local Rule checker returned invalid JSON: {error}") from error
    if report.get("valid") is not True:
        raise DeployError("local Rule checker did not report valid=true")
    return report


def build_windows_checker(repo_root: Path, output: Path) -> None:
    environment = os.environ.copy()
    environment.update({"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"})
    result = run(
        ["go", "build", "-trimpath", "-o", str(output), "./cmd/windows-action-check"],
        cwd=repo_root,
        env=environment,
    )
    if result.returncode != 0:
        raise DeployError(f"build Windows Rule checker: {result.stderr.strip()}")


def create_archive(
    archive: Path,
    repo_root: Path,
    executor: Path,
    checker: Path,
    manifest: dict[str, object],
) -> None:
    manifest_bytes = (json.dumps(manifest, indent=2, sort_keys=True) + "\n").encode("utf-8")
    entries = manifest["files"]
    assert isinstance(entries, list)
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as bundle:
        bundle.writestr("manifest.json", manifest_bytes)
        bundle.write(executor, REMOTE_EXECUTOR)
        bundle.write(checker, CHECKER_NAME)
        for entry in entries:
            assert isinstance(entry, dict)
            relative = str(entry["path"])
            bundle.write(repo_root / "Rules" / PurePosixPath(relative), f"Rules/{relative}")

    expected = {"manifest.json", REMOTE_EXECUTOR, CHECKER_NAME}
    expected.update(f'Rules/{entry["path"]}' for entry in entries if isinstance(entry, dict))
    with zipfile.ZipFile(archive) as bundle:
        actual = set(bundle.namelist())
        if actual != expected:
            raise DeployError("created Rule archive does not contain the exact expected file set")
        for info in bundle.infolist():
            if info.is_dir() or info.filename.startswith("__MACOSX/") or "/._" in info.filename:
                raise DeployError(f"created Rule archive contains platform metadata: {info.filename}")


def powershell_encoded(script: str) -> str:
    return base64.b64encode(script.encode("utf-16le")).decode("ascii")


def ssh_powershell(host: str, script: str, repo_root: Path) -> subprocess.CompletedProcess[str]:
    return run(
        [
            "ssh",
            "-o",
            "BatchMode=yes",
            "-o",
            "ConnectTimeout=10",
            host,
            "powershell.exe",
            "-NoProfile",
            "-NonInteractive",
            "-ExecutionPolicy",
            "Bypass",
            "-EncodedCommand",
            powershell_encoded(script),
        ],
        cwd=repo_root,
    )


def deploy(args: argparse.Namespace) -> dict[str, object]:
    script_path = Path(__file__).resolve()
    repo_root = script_path.parent.parent
    rules_root = repo_root / "Rules"
    executor = script_path.parent / REMOTE_EXECUTOR
    if not executor.is_file():
        raise DeployError(f"remote Rule executor is missing: {executor}")
    for command in ("git", "go", "ssh", "scp"):
        require_command(command)

    revision, dirty, dirty_detail = git_state(repo_root)
    if dirty and not args.allow_dirty:
        raise DeployError(
            "worktree is modified; commit/stash it or pass --allow-dirty to deploy the exact current Rules tree"
        )
    files, rule_ids, digest, excluded = build_inventory(rules_root)
    local_check = run_local_checker(repo_root)
    deployment_id = uuid.uuid4().hex
    generated_at = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")

    with tempfile.TemporaryDirectory(prefix="windowsagent-rules-") as temporary:
        temp_root = Path(temporary)
        checker = temp_root / CHECKER_NAME
        archive = temp_root / REMOTE_ARCHIVE
        build_windows_checker(repo_root, checker)
        manifest: dict[str, object] = {
            "schemaVersion": SCHEMA_VERSION,
            "deploymentId": deployment_id,
            "generatedAt": generated_at,
            "gitRevision": revision,
            "gitDirty": dirty,
            "rules": rule_ids,
            "fileCount": len(files),
            "treeSha256": digest,
            "files": files,
            "excludedPlatformFiles": excluded,
            "artifacts": {
                CHECKER_NAME: {"bytes": checker.stat().st_size, "sha256": sha256_file(checker)},
                REMOTE_EXECUTOR: {"bytes": executor.stat().st_size, "sha256": sha256_file(executor)},
            },
            "localCheck": local_check,
        }
        create_archive(archive, repo_root, executor, checker, manifest)

        remote_name = f"windowsagent-rules-deploy-{deployment_id}"
        create_script = (
            "$ErrorActionPreference='Stop';"
            f"$stage=Join-Path $env:USERPROFILE '{remote_name}';"
            "if(Test-Path -LiteralPath $stage){throw 'remote staging directory already exists'};"
            "New-Item -ItemType Directory -Path $stage -ErrorAction Stop|Out-Null;"
            "$stage"
        )
        created = ssh_powershell(args.host, create_script, repo_root)
        if created.returncode != 0:
            raise DeployError(f"create remote Rule staging directory: {created.stderr.strip() or created.stdout.strip()}")

        remote_archive = f"{remote_name}/{REMOTE_ARCHIVE}"
        uploaded = run(
            ["scp", "-q", "-o", "ConnectTimeout=10", str(archive), f"{args.host}:{remote_archive}"],
            cwd=repo_root,
        )
        if uploaded.returncode != 0:
            raise DeployError(f"upload Rule archive: {uploaded.stderr.strip() or uploaded.stdout.strip()}")

        prune = "$true" if args.prune_unknown else "$false"
        validate_only = "$true" if args.validate_only else "$false"
        apply_script = (
            "$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue';"
            f"$stage=Join-Path $env:USERPROFILE '{remote_name}';"
            f"Expand-Archive -LiteralPath (Join-Path $stage '{REMOTE_ARCHIVE}') -DestinationPath $stage -Force;"
            f"& (Join-Path $stage '{REMOTE_EXECUTOR}') -PayloadRoot $stage -PruneUnknown:{prune} "
            f"-ValidateOnly:{validate_only} "
            f"-TimeoutSeconds {args.timeout_seconds};"
            "if($LASTEXITCODE -ne 0){exit $LASTEXITCODE}"
        )
        applied = ssh_powershell(args.host, apply_script, repo_root)
        if applied.returncode != 0:
            detail = applied.stderr.strip() or applied.stdout.strip()
            raise DeployError(
                f"remote Rule deployment failed; staging retained at {remote_name}: {detail}"
            )
        try:
            receipt = json.loads(applied.stdout.strip())
        except json.JSONDecodeError as error:
            raise DeployError(
                f"remote Rule deployment returned invalid receipt; staging retained at {remote_name}: {error}"
            ) from error
        if receipt.get("deployment_id") != deployment_id or receipt.get("tree_sha256") != digest:
            raise DeployError(f"remote Rule deployment receipt identity mismatch; staging retained at {remote_name}")

        if not args.keep_remote_stage:
            cleanup_script = (
                "$ErrorActionPreference='Stop';"
                f"$stage=Join-Path $env:USERPROFILE '{remote_name}';"
                "if(Test-Path -LiteralPath $stage){Remove-Item -LiteralPath $stage -Recurse -Force}"
            )
            cleaned = ssh_powershell(args.host, cleanup_script, repo_root)
            if cleaned.returncode != 0:
                receipt["cleanup_warning"] = (
                    f"remote staging cleanup failed at {remote_name}: "
                    f"{cleaned.stderr.strip() or cleaned.stdout.strip()}"
                )

    local_receipt_dir = repo_root / ".build" / "rule-deployments"
    local_receipt_dir.mkdir(parents=True, exist_ok=True)
    local_receipt = local_receipt_dir / f"{deployment_id}.json"
    local_receipt.write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    receipt["local_receipt"] = str(local_receipt)
    receipt["dirty_detail"] = dirty_detail
    return receipt


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate, upload, transactionally publish, hash-check, and live-verify the complete Rules tree."
    )
    parser.add_argument("--host", default=os.environ.get("WINDOWS_AGENT_SSH_HOST"))
    parser.add_argument("--allow-dirty", action="store_true")
    parser.add_argument(
        "--prune-unknown",
        action="store_true",
        help="allow replacement to remove installed Rule directories absent from the source tree",
    )
    parser.add_argument("--keep-remote-stage", action="store_true")
    parser.add_argument(
        "--validate-only",
        action="store_true",
        help="run local and staged Windows validation without publishing Rules",
    )
    parser.add_argument("--timeout-seconds", type=int, default=45)
    args = parser.parse_args(argv)
    if not args.host:
        parser.error("--host or WINDOWS_AGENT_SSH_HOST is required")
    if SSH_HOST_PATTERN.fullmatch(args.host) is None:
        parser.error("--host must be one SSH alias, hostname, or user@host without whitespace or options")
    if args.timeout_seconds < 5 or args.timeout_seconds > 300:
        parser.error("--timeout-seconds must be between 5 and 300")
    return args


def main(argv: list[str]) -> int:
    try:
        receipt = deploy(parse_args(argv))
    except DeployError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    print(json.dumps(receipt, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
