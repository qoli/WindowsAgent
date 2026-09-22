import importlib.util
from pathlib import Path
import re
import shutil
import tempfile
import unittest
import zipfile


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "build-user-skills-bundle.py"
SPEC = importlib.util.spec_from_file_location("build_user_skills_bundle", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class UserSkillsBundleTests(unittest.TestCase):
    def test_tailscale_skill_uses_a_portable_browser_contract(self):
        contents = (ROOT / ".agents" / "skills" / "tailscale-one-off-auth-key" / "SKILL.md").read_text(encoding="utf-8")
        self.assertNotIn("$arc-cdp-browser", contents)
        self.assertIn("interactive browser capability", contents)
        self.assertIn("Single-use", contents)
        self.assertIn("Ephemeral", contents)
        self.assertIn("local clipboard", contents)
        self.assertIn("Generating the key and enrolling the device are separate actions", contents)

    def test_bundle_contains_only_complete_stable_skills(self):
        with tempfile.TemporaryDirectory() as temporary:
            archive_path = Path(temporary) / "windowsagent-user-skills.zip"
            MODULE.build(ROOT / ".agents" / "skills", archive_path)
            with zipfile.ZipFile(archive_path) as archive:
                names = archive.namelist()
                top_level = {Path(name).parts[0] for name in names}
                self.assertEqual(top_level, set(MODULE.STABLE_SKILLS))
                for skill in MODULE.STABLE_SKILLS:
                    self.assertIn(f"{skill}/SKILL.md", names)
                self.assertIn("use-windows-pc/scripts/windowsagent_client.py", names)
                self.assertIn("use-windows-pc/references/setup.md", names)
                self.assertIn("use-visual-log/scripts/read-config.ps1", names)
                for forbidden in MODULE.FORBIDDEN_SKILLS:
                    self.assertFalse(any(name.startswith(forbidden + "/") for name in names))

    def test_bundled_markdown_links_resolve_inside_the_bundle(self):
        with tempfile.TemporaryDirectory() as temporary:
            archive_path = Path(temporary) / "windowsagent-user-skills.zip"
            MODULE.build(ROOT / ".agents" / "skills", archive_path)
            extract_root = Path(temporary) / "skills"
            with zipfile.ZipFile(archive_path) as archive:
                archive.extractall(extract_root)
            for markdown in extract_root.rglob("*.md"):
                contents = markdown.read_text(encoding="utf-8")
                for target in re.findall(r"\[[^]]+\]\(([^)]+)\)", contents):
                    if "://" in target or target.startswith("#"):
                        continue
                    resolved = (markdown.parent / target.split("#", 1)[0]).resolve()
                    self.assertTrue(resolved.exists(), f"unresolved bundle link {target} in {markdown}")

    def test_build_is_deterministic(self):
        with tempfile.TemporaryDirectory() as temporary:
            first = Path(temporary) / "first.zip"
            second = Path(temporary) / "second.zip"
            MODULE.build(ROOT / ".agents" / "skills", first)
            MODULE.build(ROOT / ".agents" / "skills", second)
            self.assertEqual(first.read_bytes(), second.read_bytes())

    def test_windows_checkout_preserves_portable_script_contract(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            sources = root / "skills"
            shutil.copytree(ROOT / ".agents" / "skills", sources)
            for skill in MODULE.STABLE_SKILLS:
                for path in (sources / skill).rglob("*"):
                    if path.is_file() and path.suffix in {".md", ".sh", ".py", ".ps1", ".yaml", ".yml", ".json"}:
                        path.write_bytes(path.read_bytes().replace(b"\r\n", b"\n").replace(b"\n", b"\r\n"))
                        path.chmod(0o644)
            windows_zip = root / "windows.zip"
            native_zip = root / "native.zip"
            MODULE.build(sources, windows_zip)
            MODULE.build(ROOT / ".agents" / "skills", native_zip)
            self.assertEqual(windows_zip.read_bytes(), native_zip.read_bytes())
            with zipfile.ZipFile(windows_zip) as archive:
                script = archive.getinfo("use-windows-pc/scripts/ps1.sh")
                self.assertEqual(script.create_system, 3)
                self.assertEqual((script.external_attr >> 16) & 0o777, 0o755)
                self.assertNotIn(b"\r\n", archive.read(script))


if __name__ == "__main__":
    unittest.main()
