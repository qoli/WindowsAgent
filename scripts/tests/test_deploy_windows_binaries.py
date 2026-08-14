import subprocess
from pathlib import Path
import unittest


REPO = Path(__file__).resolve().parents[2]
BASH_SCRIPT = REPO / "scripts" / "deploy-windows-agent.sh"
POWERSHELL_SCRIPT = REPO / "scripts" / "deploy-windows-binaries.ps1"


class DeployWindowsBinariesContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bash = BASH_SCRIPT.read_text(encoding="utf-8")
        cls.powershell = POWERSHELL_SCRIPT.read_text(encoding="utf-8")

    def test_bash_is_syntax_valid_and_rejects_invalid_timeout_before_deploy(self):
        syntax = subprocess.run(
            ["bash", "-n", str(BASH_SCRIPT)],
            cwd=REPO,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(syntax.returncode, 0, syntax.stderr)

        invalid = subprocess.run(
            ["bash", str(BASH_SCRIPT), "--timeout-seconds", "4"],
            cwd=REPO,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(invalid.returncode, 2)
        self.assertIn("integer from 5 through 300", invalid.stderr)

        invalid_host = subprocess.run(
            ["bash", str(BASH_SCRIPT), "--host", "-oProxyCommand=bad"],
            cwd=REPO,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(invalid_host.returncode, 2)
        self.assertIn("plain SSH host name", invalid_host.stderr)

    def test_validate_only_is_forwarded_and_success_controls_staging_cleanup(self):
        self.assertIn("--validate-only", self.bash)
        self.assertIn("-ValidateOnly:${validate_literal}", self.bash)
        self.assertEqual(self.bash.count("-ValidateOnly:${validate_literal}"), 1)
        self.assertIn('if [[ "$remote_status" -ne 0 ]]', self.bash)
        self.assertIn("staging retained at ${remote_stage}", self.bash)
        self.assertLess(
            self.bash.index('if [[ "$remote_status" -ne 0 ]]'),
            self.bash.index("Remove-Item -LiteralPath \\$stage -Recurse -Force"),
        )

    def test_preflight_precedes_any_process_or_binary_mutation(self):
        validate_branch = self.powershell.index("if ($ValidateOnly)")
        transaction = self.powershell.index('$transactionRoot = Join-Path $root')
        stop_watchdog = self.powershell.index("Stop-ScheduledTask -TaskName $watchdogTaskName")
        replace_binary = self.powershell.index(
            "Copy-Item -LiteralPath (Join-Path $payload $name) -Destination $destinations[$name] -Force"
        )
        self.assertLess(validate_branch, transaction)
        self.assertLess(validate_branch, stop_watchdog)
        self.assertLess(transaction, replace_binary)

    def test_transaction_has_verified_backup_failed_snapshot_and_verified_restore(self):
        for contract in (
            'Join-Path $transactionRoot "backup"',
            'Join-Path $transactionRoot "failed"',
            'throw "backup hash mismatch: $name"',
            'throw "installed binary escaped the bounded data directory: $destination"',
            'throw "installed binary must not be a reparse point: $destination"',
            'throw "rollback hash mismatch: $name"',
            'throw "previous runtime did not recover before rollback deadline"',
            '$receipt.failed_snapshot_errors += "snapshot ${name}:',
            '$receipt.rollback_verified = $true',
        ):
            self.assertIn(contract, self.powershell)

    def test_receipt_contains_identity_hash_tasks_probes_and_errors(self):
        for field in (
            "deployment_id = $DeploymentId",
            "payload_sha256 = $PayloadSha256",
            "last_task_result = [long]$info.LastTaskResult",
            "process_probes = @($processProbes)",
            "http_probes = @($httpProbes)",
            "last_error = $lastError",
            "task_actions_preserved = $false",
            "rollback_error = $null",
        ):
            self.assertIn(field, self.powershell)

    def test_executor_cannot_reconfigure_scheduled_tasks_or_watchdog_config(self):
        for forbidden in (
            "Register-ScheduledTask",
            "Set-ScheduledTask",
            "Unregister-ScheduledTask",
            "Set-Content -LiteralPath $configPath",
            "Copy-Item -LiteralPath $configPath",
        ):
            self.assertNotIn(forbidden, self.powershell)


if __name__ == "__main__":
    unittest.main()
