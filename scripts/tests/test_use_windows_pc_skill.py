import json
import os
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import shlex
import subprocess
import sys
import tempfile
import threading
import unittest


ROOT = Path(__file__).resolve().parents[2]
SKILL = ROOT / ".agents" / "skills" / "use-windows-pc"
RESOLVER = SKILL / "scripts" / "resolve-pc.sh"
CLIENT = SKILL / "scripts" / "windowsagent_client.py"
TEMP_QUERY_RESPONSE = json.dumps({
    "state": "COMPLETED",
    "output": {"exitCode": 0, "stdout": {"text": "TEMP=D:\\Users\\Test User\\AppData\\Local\\Temp\r\n"}},
})


class PortableClientHandler(BaseHTTPRequestHandler):
    requests = []

    def log_message(self, _format, *_args):
        pass

    def json_response(self, status, payload, headers=None):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        for name, value in (headers or {}).items():
            self.send_header(name, value)
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.requests.append(("GET", self.path, None))
        if self.path == "/healthz":
            self.json_response(200, {"status": "ok", "version": "test"})
        elif self.path == "/v1/invocations/inv_test":
            self.json_response(200, {"invocationId": "inv_test", "state": "COMPLETED", "output": {"ok": True}})
        elif self.path == "/v1/captures/cap_test/content":
            content = b"synthetic-image"
            self.send_response(200)
            self.send_header("Content-Type", "image/png")
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)
        else:
            self.json_response(404, {"error": {"code": "NOT_FOUND"}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        payload = json.loads(self.rfile.read(length) or b"null")
        self.requests.append(("POST", self.path, payload))
        if self.path == "/v1/captures":
            self.json_response(201, {"id": "cap_test", "format": "png", "content_url": "/v1/captures/cap_test/content"})
        elif self.path in ("/v1/executions/invoke", "/v1/key-inputs/invoke"):
            self.json_response(
                202,
                {"invocationId": "inv_test", "state": "RUNNING", "stop": {"method": "POST", "url": "/v1/invocations/inv_test/stop"}},
                {"Location": "/v1/invocations/inv_test"},
            )
        else:
            self.json_response(404, {"error": {"code": "NOT_FOUND"}})


class PortableClientTests(unittest.TestCase):
    def setUp(self):
        PortableClientHandler.requests = []
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), PortableClientHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.origin = f"http://127.0.0.1:{self.server.server_port}"

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def run_client(self, *arguments):
        return subprocess.run(
            ["python3", str(CLIENT), "--url", self.origin, *arguments],
            check=False,
            capture_output=True,
            text=True,
        )

    def test_health_capture_and_structured_execution_are_repo_independent(self):
        health = self.run_client("health")
        self.assertEqual(health.returncode, 0, health.stderr)
        self.assertEqual(json.loads(health.stdout)["status"], "ok")
        with tempfile.TemporaryDirectory() as temporary:
            capture = self.run_client("capture", "--output-dir", temporary, "--profile", "native-png")
            self.assertEqual(capture.returncode, 0, capture.stderr)
            metadata = json.loads(capture.stdout)
            self.assertEqual(Path(metadata["image_path"]).read_bytes(), b"synthetic-image")
        execution = self.run_client("exec", "run", "--executable", r"C:\Windows\System32\whoami.exe")
        self.assertEqual(execution.returncode, 0, execution.stderr)
        request_payload = next(
            payload for method, path, payload in PortableClientHandler.requests
            if method == "POST" and path == "/v1/executions/invoke"
        )
        self.assertEqual(request_payload["operation"], "run")
        self.assertEqual(request_payload["executable"], r"C:\Windows\System32\whoami.exe")

    def test_key_press_preserves_fresh_foreground_identity(self):
        completed = self.run_client(
            "key", "press", "--key", "Key_Home", "--hold-ms", "40",
            "--expected-process-id", "42", "--expected-executable-name", "app.exe",
            "--expected-executable-path", r"C:\app.exe",
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        request_payload = next(
            payload for method, path, payload in PortableClientHandler.requests
            if method == "POST" and path == "/v1/key-inputs/invoke"
        )
        self.assertEqual(request_payload["expectedForeground"]["processId"], 42)


class UseWindowsPCContractTests(unittest.TestCase):
    def resolve(self, host: str):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "pc.env"
            config.write_text(f"WINDOWS_AGENT_HOST={host!r}\n", encoding="utf-8")
            environment = os.environ.copy()
            environment.update(
                WINDOWS_AGENT_PC_ENV=str(config),
                HOME=str(root / "home"),
                XDG_STATE_HOME=str(root / "state"),
            )
            script = f"""
source {str(RESOLVER)!r}
python3 - <<'PY'
import json, os
print(json.dumps({{
    "host": os.environ["WINDOWS_AGENT_HOST"],
    "http": os.environ["WINDOWS_AGENT_HTTP_ORIGIN"],
    "sftpHost": os.environ["WINDOWS_AGENT_SFTP_HOST"],
    "sftpPort": os.environ["WINDOWS_AGENT_SFTP_PORT"],
    "sftpUser": os.environ["WINDOWS_AGENT_SFTP_USER"],
    "knownHosts": os.environ["WINDOWS_AGENT_SFTP_KNOWN_HOSTS_FILE"],
}}))
PY
"""
            completed = subprocess.run(
                ["bash", "-c", script],
                check=False,
                capture_output=True,
                text=True,
                env=environment,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            return json.loads(completed.stdout), root

    def test_hostname_derives_all_normal_service_fields(self):
        resolved, root = self.resolve("pc.example.test")
        self.assertEqual(resolved["host"], "pc.example.test")
        self.assertEqual(resolved["http"], "http://pc.example.test:8787")
        self.assertEqual(resolved["sftpHost"], "pc.example.test")
        self.assertEqual(resolved["sftpPort"], "2022")
        self.assertEqual(resolved["sftpUser"], "windowsagent")
        self.assertEqual(Path(resolved["knownHosts"]), root / "state" / "windowsagent" / "known_hosts")

    def test_ipv6_host_is_bracketed_only_for_http(self):
        resolved, _ = self.resolve("fd00::1234")
        self.assertEqual(resolved["http"], "http://[fd00::1234]:8787")
        self.assertEqual(resolved["sftpHost"], "[fd00::1234]")

    def test_host_must_not_include_a_port(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / "pc.env"
            config.write_text("WINDOWS_AGENT_HOST=pc.example.test:8787\n", encoding="utf-8")
            completed = subprocess.run(
                ["bash", "-c", f"source {str(RESOLVER)!r}"],
                check=False,
                capture_output=True,
                text=True,
                env={**os.environ, "WINDOWS_AGENT_PC_ENV": str(config)},
            )
            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("must be one hostname or IP address", completed.stderr)

    def test_host_rejects_uri_and_userinfo_metacharacters(self):
        for host in ("name@other-host", "name?query", "name#fragment", "[127.0.0.1]"):
            with self.subTest(host=host), tempfile.TemporaryDirectory() as temporary:
                config = Path(temporary) / "pc.env"
                config.write_text(f"WINDOWS_AGENT_HOST={host!r}\n", encoding="utf-8")
                completed = subprocess.run(
                    ["bash", "-c", f"source {str(RESOLVER)!r}"],
                    check=False,
                    capture_output=True,
                    text=True,
                    env={**os.environ, "WINDOWS_AGENT_PC_ENV": str(config)},
                )
                self.assertNotEqual(completed.returncode, 0)
                self.assertIn("must be one hostname or IP address", completed.stderr)

    def test_resolver_preserves_bash_allexport_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / "pc.env"
            config.write_text("WINDOWS_AGENT_HOST=pc.example.test\n", encoding="utf-8")
            for initial, expected in (("set +a", "off"), ("set -a", "on")):
                with self.subTest(initial=initial):
                    completed = subprocess.run(
                        ["bash", "-c", f"{initial}; source {str(RESOLVER)!r}; case $- in *a*) echo on;; *) echo off;; esac"],
                        check=False,
                        capture_output=True,
                        text=True,
                        env={**os.environ, "WINDOWS_AGENT_PC_ENV": str(config)},
                    )
                    self.assertEqual(completed.returncode, 0, completed.stderr)
                    self.assertEqual(completed.stdout.strip(), expected)

    def test_resolver_propagates_pc_env_source_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / "pc.env"
            config.write_text("return 7\n", encoding="utf-8")
            completed = subprocess.run(
                ["bash", "-c", f"source {str(RESOLVER)!r}"],
                check=False,
                capture_output=True,
                text=True,
                env={**os.environ, "WINDOWS_AGENT_PC_ENV": str(config)},
            )
            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("could not load", completed.stderr)

    @unittest.skipUnless(subprocess.run(["sh", "-c", "command -v zsh"], capture_output=True).returncode == 0, "zsh unavailable")
    def test_resolver_can_be_sourced_from_zsh(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / "pc.env"
            config.write_text("WINDOWS_AGENT_HOST=pc.example.test\n", encoding="utf-8")
            completed = subprocess.run(
                ["zsh", "-c", f"set -u; source {str(RESOLVER)!r}; print -r -- $WINDOWS_AGENT_HTTP_ORIGIN"],
                check=False,
                capture_output=True,
                text=True,
                env={**os.environ, "WINDOWS_AGENT_PC_ENV": str(config)},
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertEqual(completed.stdout.strip(), "http://pc.example.test:8787")

    def test_missing_host_fails_without_legacy_fields(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / "pc.env"
            config.write_text("WINDOWS_AGENT_HTTP_ORIGIN=http://legacy.invalid:8787\n", encoding="utf-8")
            completed = subprocess.run(
                ["bash", "-c", f"source {str(RESOLVER)!r}"],
                check=False,
                capture_output=True,
                text=True,
                env={**os.environ, "WINDOWS_AGENT_PC_ENV": str(config)},
            )
            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("WINDOWS_AGENT_HOST is required", completed.stderr)

    def test_public_pc_contract_contains_no_legacy_required_fields(self):
        contract = (SKILL / "references" / "pc-env.md").read_text(encoding="utf-8")
        for name in (
            "WINDOWS_AGENT_HTTP_ORIGIN",
            "WINDOWS_AGENT_REPO",
            "WINDOWS_AGENT_CAPTURE_HELPER",
            "WINDOWS_AGENT_SFTP_HOST",
            "WINDOWS_AGENT_SFTP_PORT",
            "WINDOWS_AGENT_SFTP_USER",
            "WINDOWS_AGENT_SFTP_HOST_KEY_SHA256",
            "WINDOWS_AGENT_SFTP_KNOWN_HOSTS",
            "WINDOWS_AGENT_SFTP_STAGING_DIR",
            "WINDOWS_AGENT_WINDOWS_STAGING_DIR",
        ):
            self.assertNotIn(name, contract)

    def test_sftp_wrapper_owns_connection_constants_and_known_hosts(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "pc.env"
            config.write_text("WINDOWS_AGENT_HOST=pc.example.test\n", encoding="utf-8")
            binary_dir = root / "bin"
            binary_dir.mkdir()
            argument_log = root / "sftp-arguments"
            fake_sftp = binary_dir / "sftp"
            fake_sftp.write_text(
                "#!/usr/bin/env bash\nprintf '%s\\n' \"$@\" >\"$WINDOWS_AGENT_TEST_ARGUMENT_LOG\"\n",
                encoding="utf-8",
            )
            fake_sftp.chmod(0o755)
            environment = {
                **os.environ,
                "WINDOWS_AGENT_PC_ENV": str(config),
                "XDG_STATE_HOME": str(root / "state"),
                "WINDOWS_AGENT_TEST_ARGUMENT_LOG": str(argument_log),
                "PATH": f"{binary_dir}:{os.environ['PATH']}",
            }
            completed = subprocess.run(
                [str(SKILL / "scripts" / "sftp.sh")],
                check=False,
                capture_output=True,
                text=True,
                env=environment,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            arguments = argument_log.read_text(encoding="utf-8").splitlines()
            self.assertIn("ConnectTimeout=5", arguments)
            self.assertIn("StrictHostKeyChecking=accept-new", arguments)
            self.assertIn("2022", arguments)
            self.assertEqual(arguments[-1], "windowsagent@pc.example.test")
            known_hosts = root / "state" / "windowsagent" / "known_hosts"
            self.assertTrue(known_hosts.is_file())
            self.assertEqual(known_hosts.stat().st_mode & 0o777, 0o600)

    def test_capture_wrapper_uses_the_bundled_portable_client(self):
        wrapper = (SKILL / "scripts" / "capture.sh").read_text(encoding="utf-8")
        self.assertIn("windowsagent_client.py", wrapper)
        self.assertIn("--url \"$WINDOWS_AGENT_HTTP_ORIGIN\"", wrapper)
        self.assertNotIn("curl ", wrapper)

    def test_stable_skill_has_no_repository_or_experimental_dependency(self):
        contents = "\n".join(
            path.read_text(encoding="utf-8")
            for path in SKILL.rglob("*")
            if path.is_file() and path.suffix in (".md", ".sh", ".py", ".yaml")
        )
        for forbidden in (
            "WINDOWS_AGENT_HARNESS_ROOT",
            "go run ./cmd/",
            "../gameGuide/tools/pc_screenshot",
            "maintain-windowsagent-runtime",
            "develop-windowsagent-rule",
            "publish-windowsagent-release",
            "windows-starlark-invoke",
            "$arc-cdp-browser",
        ):
            self.assertNotIn(forbidden, contents)

    def test_portable_client_is_shipped_with_the_skill(self):
        client = SKILL / "scripts" / "windowsagent_client.py"
        self.assertTrue(client.is_file())
        self.assertIn("/v1/executions/invoke", client.read_text(encoding="utf-8"))
        self.assertIn("/v1/key-inputs/invoke", client.read_text(encoding="utf-8"))
        self.assertIn("/v1/captures", client.read_text(encoding="utf-8"))

    def test_capture_wrapper_rejects_target_and_recovery_overrides(self):
        for argument in ("--agent-url=http://other:8787", "--ssh-host=other", "--ssh-user=other", "--ssh-port=2222"):
            with self.subTest(argument=argument):
                completed = subprocess.run(
                    [str(SKILL / "scripts" / "capture.sh"), argument],
                    check=False,
                    capture_output=True,
                    text=True,
                )
                self.assertEqual(completed.returncode, 2)
                self.assertIn("owned by the single-host capture adapter", completed.stderr)

    def test_sftp_wrapper_rejects_connection_overrides(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / "pc.env"
            config.write_text("WINDOWS_AGENT_HOST=pc.example.test\n", encoding="utf-8")
            for arguments in (("-P", "22"), ("-o", "Hostname=other"), ("other-host",)):
                with self.subTest(arguments=arguments):
                    completed = subprocess.run(
                        [str(SKILL / "scripts" / "sftp.sh"), *arguments],
                        check=False,
                        capture_output=True,
                        text=True,
                        env={**os.environ, "WINDOWS_AGENT_PC_ENV": str(config)},
                    )
                    self.assertEqual(completed.returncode, 2)
                    self.assertIn("unsupported SFTP operation argument", completed.stderr)

    def test_legacy_pc_env_migrates_without_transport_fields(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / "pc.env"
            config.write_text(
                "\n".join(
                    (
                        "WINDOWS_AGENT_PC_NAME=Test-PC",
                        "WINDOWS_AGENT_HTTP_ORIGIN=http://test-pc.example:8787",
                        "WINDOWS_AGENT_REPO=/legacy/repo",
                        "WINDOWS_AGENT_CAPTURE_HELPER=/legacy/capture.py",
                        "WINDOWS_AGENT_SFTP_HOST=test-pc.example",
                        "WINDOWS_AGENT_SFTP_PORT=2022",
                        "WINDOWS_AGENT_SFTP_USER=windowsagent",
                        "WINDOWS_AGENT_SFTP_HOST_KEY_SHA256=SHA256:legacy",
                        "WINDOWS_AGENT_SFTP_KNOWN_HOSTS=/legacy/known_hosts",
                        "WINDOWS_AGENT_SFTP_STAGING_DIR=/c:/legacy",
                        r"WINDOWS_AGENT_WINDOWS_STAGING_DIR=C:\legacy",
                        "WINDOWS_AGENT_PI_SSH_HOST=user@test-pc.example",
                        "",
                    )
                ),
                encoding="utf-8",
            )
            config.chmod(0o600)
            completed = subprocess.run(
                [str(SKILL / "scripts" / "migrate-pc-env.sh")],
                check=False,
                capture_output=True,
                text=True,
                env={**os.environ, "WINDOWS_AGENT_PC_ENV": str(config)},
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            migrated = config.read_text(encoding="utf-8")
            self.assertIn("WINDOWS_AGENT_HOST=test-pc.example", migrated)
            self.assertIn("WINDOWS_AGENT_PC_NAME=Test-PC", migrated)
            self.assertIn("WINDOWS_AGENT_PI_SSH_HOST=user@test-pc.example", migrated)
            self.assertNotIn("WINDOWS_AGENT_HTTP_ORIGIN", migrated)
            self.assertNotIn("WINDOWS_AGENT_SFTP_HOST=", migrated)
            self.assertNotIn("WINDOWS_AGENT_REPO", migrated)
            self.assertEqual(config.stat().st_mode & 0o777, 0o600)

    def test_powershell_adapter_hides_staging_path_translation(self):
        helper = (SKILL / "scripts" / "ps1.sh").read_text(encoding="utf-8")
        self.assertNotIn(r"C:\Windows\Temp", helper)
        self.assertIn("--arg 'set TEMP'", helper)
        operations = (SKILL / "references" / "operations.md").read_text(encoding="utf-8")
        self.assertNotIn("WINDOWS_AGENT_SFTP_STAGING_DIR", operations)
        self.assertNotIn("WINDOWS_AGENT_WINDOWS_STAGING_DIR", operations)

    def test_powershell_adapter_stages_invokes_and_cleans_up(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "pc.env"
            config.write_text("WINDOWS_AGENT_HOST=pc.example.test\n", encoding="utf-8")
            local_script = root / "task.ps1"
            local_script.write_text('Write-Output "ok"\n', encoding="utf-8")
            binary_dir = root / "bin"
            binary_dir.mkdir()
            sftp_log = root / "sftp-batches"
            client_log = root / "client-arguments"
            fake_sftp = binary_dir / "sftp"
            fake_sftp.write_text(
                """#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-b" ]]; then
    printf '%s\\n' '---batch---' >>"$WINDOWS_AGENT_TEST_SFTP_LOG"
    cat "$2" >>"$WINDOWS_AGENT_TEST_SFTP_LOG"
    shift 2
    continue
  fi
  shift
done
""",
                encoding="utf-8",
            )
            fake_sftp.chmod(0o755)
            fake_python = binary_dir / "python3"
            fake_python.write_text(
                "#!/usr/bin/env bash\n"
                "if [[ \"${1:-}\" == *windowsagent_client.py ]]; then\n"
                "  printf '%s\\n' \"$@\" >\"$WINDOWS_AGENT_TEST_CLIENT_LOG\"\n"
                f"  printf '%s\\n' {shlex.quote(TEMP_QUERY_RESPONSE)}\n"
                "  exit 0\n"
                "fi\n"
                f"exec {shlex.quote(sys.executable)} \"$@\"\n",
                encoding="utf-8",
            )
            fake_python.chmod(0o755)
            environment = {
                **os.environ,
                "WINDOWS_AGENT_PC_ENV": str(config),
                "XDG_STATE_HOME": str(root / "state"),
                "WINDOWS_AGENT_TEST_SFTP_LOG": str(sftp_log),
                "WINDOWS_AGENT_TEST_CLIENT_LOG": str(client_log),
                "PATH": f"{binary_dir}:{os.environ['PATH']}",
            }
            completed = subprocess.run(
                [str(SKILL / "scripts" / "ps1.sh"), str(local_script), "--arg", "Inspect"],
                check=False,
                capture_output=True,
                text=True,
                env=environment,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertEqual(json.loads(completed.stdout)["state"], "COMPLETED")
            client_arguments = client_log.read_text(encoding="utf-8").splitlines()
            self.assertTrue(client_arguments[0].endswith("windowsagent_client.py"))
            self.assertIn("http://pc.example.test:8787", client_arguments)
            self.assertIn("exec", client_arguments)
            self.assertIn("ps1", client_arguments)
            self.assertIn("--script-path", client_arguments)
            self.assertTrue(any(value.startswith(r"D:\Users\Test User\AppData\Local\Temp\task-") for value in client_arguments))
            batches = sftp_log.read_text(encoding="utf-8")
            self.assertIn("put ", batches)
            self.assertIn("rm ", batches)
            self.assertIn("/D:/Users/Test User/AppData/Local/Temp/task-", batches)

    def test_powershell_adapter_rejects_owned_flag_overrides(self):
        with tempfile.TemporaryDirectory() as temporary:
            local_script = Path(temporary) / "task.ps1"
            local_script.write_text('Write-Output "ok"\n', encoding="utf-8")
            for argument in ("--url", "--url=http://other:8787", "--script-path", r"--script-path=C:\other.ps1"):
                with self.subTest(argument=argument):
                    completed = subprocess.run(
                        [str(SKILL / "scripts" / "ps1.sh"), str(local_script), argument],
                        check=False,
                        capture_output=True,
                        text=True,
                    )
                    self.assertEqual(completed.returncode, 2)
                    self.assertIn("owned by the single-host staging adapter", completed.stderr)

    def test_powershell_adapter_reports_cleanup_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "pc.env"
            config.write_text("WINDOWS_AGENT_HOST=pc.example.test\n", encoding="utf-8")
            local_script = root / "task.ps1"
            local_script.write_text('Write-Output "ok"\n', encoding="utf-8")
            binary_dir = root / "bin"
            binary_dir.mkdir()
            sftp_count = root / "sftp-count"
            fake_sftp = binary_dir / "sftp"
            fake_sftp.write_text(
                """#!/usr/bin/env bash
count=0
[[ ! -f "$WINDOWS_AGENT_TEST_SFTP_COUNT" ]] || count="$(cat "$WINDOWS_AGENT_TEST_SFTP_COUNT")"
count=$((count + 1))
printf '%s' "$count" >"$WINDOWS_AGENT_TEST_SFTP_COUNT"
[[ "$count" -eq 1 ]]
""",
                encoding="utf-8",
            )
            fake_sftp.chmod(0o755)
            fake_python = binary_dir / "python3"
            fake_python.write_text(
                "#!/usr/bin/env bash\n"
                "if [[ \"${1:-}\" == *windowsagent_client.py ]]; then\n"
                f"  printf '%s\\n' {shlex.quote(TEMP_QUERY_RESPONSE)}\n"
                "  exit 0\n"
                "fi\n"
                f"exec {shlex.quote(sys.executable)} \"$@\"\n",
                encoding="utf-8",
            )
            fake_python.chmod(0o755)
            completed = subprocess.run(
                [str(SKILL / "scripts" / "ps1.sh"), str(local_script)],
                check=False,
                capture_output=True,
                text=True,
                env={
                    **os.environ,
                    "WINDOWS_AGENT_PC_ENV": str(config),
                    "XDG_STATE_HOME": str(root / "state"),
                    "WINDOWS_AGENT_TEST_SFTP_COUNT": str(sftp_count),
                    "PATH": f"{binary_dir}:{os.environ['PATH']}",
                },
            )
            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("failed to remove staged PowerShell file", completed.stderr)


if __name__ == "__main__":
    unittest.main()
