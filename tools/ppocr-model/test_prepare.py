import importlib.util
import tempfile
import unittest
from pathlib import Path


SPEC = importlib.util.spec_from_file_location("ppocr_prepare", Path(__file__).with_name("prepare.py"))
prepare = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(prepare)


class PrepareTests(unittest.TestCase):
    def test_extracts_quoted_characters_and_appends_space(self):
        with tempfile.TemporaryDirectory() as value:
            path = Path(value) / "inference.yml"
            path.write_text(
                "Global:\n  model_name: sample\nPostProcess:\n  name: CTCLabelDecode\n"
                "  character_dict:\n  - '!'\n  - ''''\n  - A\n",
                encoding="utf-8",
            )
            self.assertEqual(prepare.extract_characters(path), ["!", "'", "A", " "])

    def test_rejects_duplicate_characters(self):
        with tempfile.TemporaryDirectory() as value:
            path = Path(value) / "inference.yml"
            path.write_text("PostProcess:\n  character_dict:\n  - A\n  - A\n", encoding="utf-8")
            with self.assertRaisesRegex(prepare.PrepareError, "duplicates"):
                prepare.extract_characters(path)


if __name__ == "__main__":
    unittest.main()
