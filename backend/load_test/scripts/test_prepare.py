import os
import unittest
from unittest.mock import patch

import prepare


class PrepareTest(unittest.TestCase):
    def test_final_pfr_shape(self):
        with patch.dict(
            os.environ,
            {
                "LOAD_PFR": "1",
                "LOAD_TEAM_COUNT": "28",
                "LOAD_PUTS": "14,13,13",
                "LOAD_ROUND_TIME": "60",
                "LOAD_CHECKER_TIMEOUT": "20",
            },
        ):
            document = prepare.document()
        self.assertEqual(document.count("checker_type: pfr"), 3)
        self.assertEqual(document.count("    ip: 192.0.2."), 28)
        self.assertIn("puts: 14", document)
        self.assertEqual(document.count("puts: 13"), 2)

    def test_invalid_pfr_mode_is_rejected(self):
        with (
            patch.dict(os.environ, {"LOAD_PFR": "invalid"}),
            self.assertRaisesRegex(ValueError, "LOAD_PFR"),
        ):
            prepare.document()


if __name__ == "__main__":
    unittest.main()
