#!/usr/bin/env python3
"""Build the exact stable WindowsAgent end-user Skill release artifact."""

from __future__ import annotations

import argparse
import pathlib
import stat
import zipfile


STABLE_SKILLS = (
    "tailscale-one-off-auth-key",
    "use-visual-log",
    "use-windows-pc",
)
FORBIDDEN_SKILLS = (
    "develop-windowsagent-rule",
    "maintain-windowsagent-runtime",
    "publish-windowsagent-release",
    "use-windows-starlark",
)
IGNORED_NAMES = {".DS_Store", "__pycache__"}


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--skills-dir", type=pathlib.Path, default=pathlib.Path(".agents/skills"))
    parser.add_argument("--output", type=pathlib.Path, required=True)
    return parser.parse_args()


def collect(skills_dir: pathlib.Path):
    entries = []
    for name in STABLE_SKILLS:
        root = skills_dir / name
        skill_file = root / "SKILL.md"
        if not skill_file.is_file():
            raise SystemExit(f"error: stable Skill is missing SKILL.md: {name}")
        frontmatter = skill_file.read_text(encoding="utf-8")
        if not frontmatter.startswith("---\n") or f"name: {name}\n" not in frontmatter:
            raise SystemExit(f"error: stable Skill has invalid frontmatter: {name}")
        for path in sorted(root.rglob("*")):
            if path.is_symlink():
                raise SystemExit(f"error: Skill bundle does not accept symlinks: {path}")
            if not path.is_file() or any(part in IGNORED_NAMES for part in path.parts):
                continue
            entries.append((path, pathlib.PurePosixPath(name) / path.relative_to(root)))
    return entries


def build(skills_dir: pathlib.Path, output: pathlib.Path):
    entries = collect(skills_dir.resolve())
    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + ".tmp")
    temporary.unlink(missing_ok=True)
    with zipfile.ZipFile(temporary, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for source, archive_path in entries:
            info = zipfile.ZipInfo(str(archive_path), date_time=(1980, 1, 1, 0, 0, 0))
            # Windows checkouts do not preserve POSIX executable bits. Emit a
            # portable Unix archive with canonical text regardless of the host.
            info.create_system = 3
            permissions = 0o755 if source.suffix in {".sh", ".py"} else 0o644
            info.external_attr = (stat.S_IFREG | permissions) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            contents = source.read_bytes()
            if source.suffix in {".md", ".sh", ".py", ".ps1", ".yaml", ".yml", ".json"}:
                contents = contents.replace(b"\r\n", b"\n")
            archive.writestr(info, contents)
    temporary.replace(output)

    with zipfile.ZipFile(output) as archive:
        names = archive.namelist()
        top_level = {pathlib.PurePosixPath(name).parts[0] for name in names}
        if top_level != set(STABLE_SKILLS):
            raise SystemExit(f"error: bundle top-level Skill set is invalid: {sorted(top_level)}")
        if any(name in top_level for name in FORBIDDEN_SKILLS):
            raise SystemExit("error: bundle contains a developer or experimental Skill")
        if len(names) != len(set(names)):
            raise SystemExit("error: bundle contains duplicate paths")


if __name__ == "__main__":
    arguments = parse_args()
    build(arguments.skills_dir, arguments.output)
