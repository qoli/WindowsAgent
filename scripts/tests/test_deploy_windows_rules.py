import base64
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
import zipfile


SCRIPT = Path(__file__).resolve().parents[1] / "deploy-windows-rules.py"
SPEC = importlib.util.spec_from_file_location("deploy_windows_rules", SCRIPT)
assert SPEC and SPEC.loader
deploy = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(deploy)


class DeployWindowsRulesTests(unittest.TestCase):
    def make_rules(self, root: Path) -> Path:
        rules = root / "Rules"
        rule = rules / "Game.exe"
        action = rule / "Actions" / "read"
        action.mkdir(parents=True)
        (rule / "rule.json").write_text('{"schemaVersion":6}\n', encoding="utf-8")
        (rule / "AGENTS.md").write_text("# Game\n", encoding="utf-8")
        (action / "manifest.json").write_text("{}\n", encoding="utf-8")
        return rules

    def test_inventory_is_deterministic_and_content_addressed(self):
        with tempfile.TemporaryDirectory() as temporary:
            rules = self.make_rules(Path(temporary))
            first, rule_ids, first_hash, excluded = deploy.build_inventory(rules)
            second, _, second_hash, _ = deploy.build_inventory(rules)
            self.assertEqual(rule_ids, ["Game.exe"])
            self.assertEqual(excluded, [])
            self.assertEqual(first, second)
            self.assertEqual(first_hash, second_hash)
            self.assertEqual(len(first_hash), 64)

            (rules / "Game.exe" / "AGENTS.md").write_text("# Changed\n", encoding="utf-8")
            _, _, changed_hash, _ = deploy.build_inventory(rules)
            self.assertNotEqual(first_hash, changed_hash)

    def test_inventory_explicitly_excludes_appledouble(self):
        with tempfile.TemporaryDirectory() as temporary:
            rules = self.make_rules(Path(temporary))
            (rules / "Game.exe" / "._TASK.md").write_bytes(b"metadata")
            files, _, _, excluded = deploy.build_inventory(rules)
            self.assertEqual(excluded, ["Game.exe/._TASK.md"])
            self.assertNotIn("Game.exe/._TASK.md", [entry["path"] for entry in files])

    def test_inventory_excludes_rules_development_contract(self):
        with tempfile.TemporaryDirectory() as temporary:
            rules = self.make_rules(Path(temporary))
            (rules / "AGENTS.md").write_text("# Development only\n", encoding="utf-8")
            files, _, _, _ = deploy.build_inventory(rules)
            self.assertNotIn("AGENTS.md", [entry["path"] for entry in files])

    def test_inventory_rejects_symlinks(self):
        with tempfile.TemporaryDirectory() as temporary:
            rules = self.make_rules(Path(temporary))
            link = rules / "Game.exe" / "Actions" / "read" / "linked.json"
            link.symlink_to(rules / "Game.exe" / "rule.json")
            with self.assertRaisesRegex(deploy.DeployError, "symlinks are forbidden"):
                deploy.build_inventory(rules)

    def test_archive_contains_only_declared_payload(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            rules = self.make_rules(root)
            files, rule_ids, digest, _ = deploy.build_inventory(rules)
            executor = root / deploy.REMOTE_EXECUTOR
            checker = root / deploy.CHECKER_NAME
            executor.write_text("Write-Output ok\n", encoding="utf-8")
            checker.write_bytes(b"MZchecker")
            manifest = {
                "schemaVersion": 1,
                "deploymentId": "a" * 32,
                "rules": rule_ids,
                "fileCount": len(files),
                "treeSha256": digest,
                "files": files,
            }
            archive = root / "rules.zip"
            deploy.create_archive(archive, root, executor, checker, manifest)

            with zipfile.ZipFile(archive) as bundle:
                names = bundle.namelist()
                self.assertIn("Rules/Game.exe/rule.json", names)
                self.assertFalse(any(name.startswith("__MACOSX/") for name in names))
                self.assertFalse(any("/._" in name for name in names))
                loaded = json.loads(bundle.read("manifest.json"))
                self.assertEqual(loaded["treeSha256"], digest)

    def test_powershell_command_is_utf16le_base64(self):
        script = "$value='safe'"
        encoded = deploy.powershell_encoded(script)
        self.assertEqual(base64.b64decode(encoded).decode("utf-16le"), script)

    def test_host_rejects_ssh_option_injection(self):
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            deploy.parse_args(["--host=-oProxyCommand=bad"])

    def test_remote_apply_does_not_read_unset_native_exit_code(self):
        script = deploy.remote_apply_script(
            "windowsagent-rules-deploy-test",
            prune_unknown=False,
            validate_only=False,
            timeout_seconds=60,
        )
        self.assertNotIn("LASTEXITCODE", script)
        self.assertIn("$ErrorActionPreference='Stop'", script)
        self.assertIn("-TimeoutSeconds 60", script)

    def test_ssh_transport_pins_windows_compatible_kex(self):
        self.assertEqual(
            deploy.SSH_TRANSPORT_OPTIONS,
            ["-o", "KexAlgorithms=ecdh-sha2-nistp256"],
        )


if __name__ == "__main__":
    unittest.main()
