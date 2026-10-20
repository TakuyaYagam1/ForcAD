"""Team additions must preserve existing identities and recover partial writes."""

import contextlib
import copy
import io
import tempfile
import unittest
from pathlib import Path

from sync_demo import sync_teams


class SyncDemoTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.output = Path(self.directory.name)
        self.configured = [
            {"name": f"Team {n}", "ip": f"10.183.{100 + n}.2", "token": f"{n:016x}"}
            for n in range(1, 6)
        ]
        self.existing = [
            {**t, "id": str(i), "active": True}
            for i, t in enumerate(self.configured[:2])
        ]
        self.mutations = 0
        self.fail_update = False

    def request(self, path, data=None, method="GET"):
        if method == "GET":
            return copy.deepcopy(self.existing)
        self.mutations += 1
        if method == "POST":
            team = {
                **data,
                "id": str(len(self.existing)),
                "token": f"{100 + len(self.existing):016x}",
            }
            self.existing.append(team)
            return copy.deepcopy(team)
        if self.fail_update:
            raise OSError("interrupted")
        index = next(i for i, t in enumerate(self.existing) if t["id"] == data["id"])
        self.existing[index] = copy.deepcopy(data)
        return data

    def sync(self):
        with contextlib.redirect_stdout(io.StringIO()):
            sync_teams(self.request, self.configured, self.output)

    def test_add_and_repeat_preserve_existing_teams(self):
        before = copy.deepcopy(self.existing)
        self.sync()
        self.assertEqual(before, self.existing[:2])
        self.assertEqual(
            [t["token"] for t in self.existing], [t["token"] for t in self.configured]
        )
        mutations = self.mutations
        self.sync()
        self.assertEqual(self.mutations, mutations)

    def test_partial_create_recovers_without_duplicates(self):
        self.fail_update = True
        with self.assertRaises(OSError):
            self.sync()
        self.fail_update = False
        self.sync()
        self.assertEqual(len(self.existing), 5)
        self.assertEqual(
            [t["token"] for t in self.existing], [t["token"] for t in self.configured]
        )
        self.assertFalse((self.output / "teams-pending.json").exists())

    def test_unknown_team_refuses_before_mutation(self):
        self.existing[0]["token"] = "ffffffffffffffff"
        with self.assertRaisesRegex(SystemExit, "unrecognized"):
            self.sync()
        self.assertEqual(self.mutations, 0)


if __name__ == "__main__":
    unittest.main()
