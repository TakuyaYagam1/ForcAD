import contextlib
import io
import json
import unittest
from unittest.mock import patch

import client


class OutputTest(unittest.TestCase):
    def test_put_protocol(self):
        state = {"path": "notes/object", "token": "private-token"}
        for mode in ("0", "1"):
            with self.subTest(mode=mode):
                stdout, stderr = io.StringIO(), io.StringIO()
                with (
                    patch.dict("os.environ", {"LOAD_PFR": mode}),
                    patch.object(
                        client.sys,
                        "argv",
                        ["checker", "put", "192.0.2.1", "seed", "flag", "1"],
                    ),
                    patch.object(client, "put", return_value=state),
                    patch.object(client, "get", return_value="flag"),
                    contextlib.redirect_stdout(stdout),
                    contextlib.redirect_stderr(stderr),
                ):
                    self.assertEqual(client.main("notes"), 101)
                private = stderr.getvalue() if mode == "1" else stdout.getvalue()
                self.assertEqual(json.loads(private), state)
                if mode == "1":
                    self.assertEqual(stdout.getvalue().strip(), "notes/object")
                    self.assertNotIn("private-token", stdout.getvalue())


if __name__ == "__main__":
    unittest.main()
