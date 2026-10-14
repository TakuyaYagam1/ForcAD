"""Check isolation and storage semantics without starting containers."""

import sqlite3
import unittest

import jobs
import notes
import vault


class ServicesTest(unittest.TestCase):
    def test_notes_and_queue_isolation(self):
        for service, path, created in ((notes, "notes", 201), (jobs, "jobs", 202)):
            with self.subTest(service=path), sqlite3.connect(":memory:") as db:
                db.row_factory = sqlite3.Row
                service.initialize(db)
                service.initialize(db)
                status, saved = service.handle(
                    db, "one", "POST", path, {"body": "flag"}, {}
                )
                self.assertEqual(status, created)
                record = path + "/" + saved["id"]
                self.assertEqual(
                    service.handle(db, "two", "GET", record, {}, {})[0], 404
                )
                if service is jobs:
                    self.assertEqual(
                        service.handle(db, "one", "GET", record, {}, {})[0], 202
                    )
                    db.execute("UPDATE jobs SET ready=1")
                self.assertEqual(
                    service.handle(db, "one", "GET", record, {}, {}),
                    (200, {"body": "flag"}),
                )

    def test_vault_requires_owner_and_correct_team(self):
        with sqlite3.connect(":memory:") as db:
            db.row_factory = sqlite3.Row
            vault.initialize(db)
            vault.initialize(db)
            credentials = {"name": "owner", "password": "test-password"}
            self.assertEqual(
                vault.handle(db, "one", "POST", "register", credentials, {})[0], 201
            )
            bad = {**credentials, "password": "wrong"}
            self.assertEqual(vault.handle(db, "one", "POST", "login", bad, {})[0], 403)
            _, login = vault.handle(db, "one", "POST", "login", credentials, {})
            headers = {"Authorization": login["token"]}
            _, saved = vault.handle(
                db, "one", "POST", "records", {"body": "flag"}, headers
            )
            path = "records/" + saved["id"]
            self.assertEqual(vault.handle(db, "one", "GET", path, {}, {})[0], 403)
            self.assertEqual(vault.handle(db, "two", "GET", path, {}, headers)[0], 403)
            self.assertEqual(
                vault.handle(db, "one", "GET", path, {}, headers),
                (200, {"body": "flag"}),
            )


if __name__ == "__main__":
    unittest.main()
