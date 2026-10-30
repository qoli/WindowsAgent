import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest


CLIENT = Path(__file__).resolve().parents[1] / "windows-agent-pi-client.sh"


class PiArtifactClientTests(unittest.TestCase):
    def test_download_verifies_hash_and_keeps_raw_event_out_of_stdout(self):
        data = b'{"type":"tool.result","image":"synthetic-image"}'
        digest = hashlib.sha256(data).hexdigest()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source.json"
            source.write_bytes(data)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            ssh = binary_dir / "ssh"
            ssh.write_text(
                '#!/bin/bash\ncase "${*: -1}" in *delegated-api.token*) printf 0123456789abcdef0123456789abcdef;; esac\n',
                encoding="utf-8",
            )
            curl = binary_dir / "curl"
            curl.write_text(
                '#!/bin/bash\nwhile [[ $# -gt 0 ]]; do\n'
                'if [[ "$1" == --output ]]; then destination="$2"; shift 2; else shift; fi\n'
                'done\ncp ' + shlex.quote(str(source)) + ' "$destination"\n',
                encoding="utf-8",
            )
            ssh.chmod(0o700)
            curl.chmod(0o700)
            target = root / "saved event.json"
            completed = subprocess.run(
                ["bash", str(CLIENT), "--ssh-host", "fixture.example.test", "artifact",
                 "--task-id", "task_" + "0" * 32, "--artifact-id", digest, "--output", str(target)],
                env={**os.environ, "PATH": str(binary_dir) + os.pathsep + os.environ["PATH"]},
                capture_output=True, text=True, check=False,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            receipt = json.loads(completed.stdout)
            self.assertEqual(receipt["sha256"], digest)
            self.assertEqual(target.read_bytes(), data)
            self.assertNotIn("synthetic-image", completed.stdout)


if __name__ == "__main__":
    unittest.main()
