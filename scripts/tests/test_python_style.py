import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location(
    "check_python_style", Path(__file__).resolve().parents[1] / "check_python_style.py"
)
STYLE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(STYLE)


class PythonStyleTest(unittest.TestCase):
    def test_rejects_inline_suites_and_statement_separators(self):
        examples = [
            "def answer(): return 42\n",
            "async def answer(): return 42\n",
            "if ready: run()\n",
            "if ready:\n    run()\nelse: stop()\n",
            "try:\n    run()\nexcept Exception: stop()\n",
            "match value:\n    case 1: run()\n",
            "first(); second()\n",
            "first();\n",
        ]
        for source in examples:
            with self.subTest(source=source):
                self.assertTrue(STYLE.check_source(source))

    def test_allows_annotations_multiline_headers_strings_and_comments(self):
        source = '''
def answer(
    values: dict[str, int],
) -> int:
    text = "if ready: run(); stop()"
    # Statements in documentation: first(); second()
    mapping = {"key": values["key"]}
    if (
        mapping["key"]
    ):
        return len(text)
    else:
        return 0
'''
        self.assertEqual(STYLE.check_source(source), [])

    def test_reports_invalid_python(self):
        self.assertTrue(STYLE.check_source("def broken(\n"))


if __name__ == "__main__":
    unittest.main()
