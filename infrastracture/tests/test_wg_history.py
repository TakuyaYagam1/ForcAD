"""A drilldown must identify the last observed endpoint without inventing activity."""

import sqlite3
import tempfile
import unittest
from contextlib import closing
from pathlib import Path

import helpers  # noqa: F401


class HistoryTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.db = Path(temp.name) / 'history.db'
        with closing(sqlite3.connect(self.db)) as db, db:
            db.execute('CREATE TABLE observations(id INTEGER PRIMARY KEY, observed_at INTEGER, source_ip TEXT, team_name TEXT, member TEXT, external_ip TEXT, venue TEXT, kind TEXT)')
            db.executemany('INSERT INTO observations VALUES (NULL,?,?,?,?,?,?,?)', [
                (100, '10.0.0.2', '<CUT>', 'member-01', '192.0.2.1', 'venue', 'baseline'),
                (199, '10.0.0.2', '<CUT>', 'member-01', '192.0.2.9', 'venue', 'change'),
                (201, '10.0.0.2', '<CUT>', 'member-01', '203.0.113.7', 'outside', 'change'),
                (204, '10.0.0.3', 'Blue', 'member-02', '203.0.113.99', 'unknown', 'first'),
                (209, '10.0.0.2', '<CUT>', 'member-01', '203.0.113.5', 'outside', 'change'),
            ])

    def test_peer_window_includes_only_one_prior_observation_and_escapes_team(self):
        from vpn.vpnlib.history import page

        html = page(self.db, 'peer=10.0.0.2&from=200000&to=205000')
        for expected in ('192.0.2.9', '203.0.113.7', '&lt;CUT&gt;'):
            self.assertIn(expected, html)
        for absent in ('192.0.2.1<', '203.0.113.99', '203.0.113.5', '<CUT>'):
            self.assertNotIn(absent, html)

    def test_unknown_filter_never_returns_all_peers(self):
        from vpn.vpnlib.history import page

        for query in ('peer=garbage', 'peer=', 'peer=10.0.0.2&peer=10.0.0.3', 'from=205000&to=200000'):
            with self.subTest(query=query), self.assertRaises(ValueError):
                page(self.db, query)

    def test_overview_excludes_raw_endpoints(self):
        from vpn.vpnlib.history import page

        html = page(self.db, 'from=100000&to=205000')
        self.assertIn('member-02', html)
        self.assertIn('10.0.0.2', html)
        self.assertNotIn('203.0.113.7', html)

    def test_summary_deduplicates_shared_nat_across_members_and_keeps_quiet_peers(self):
        from vpn.vpnlib.history import page

        with closing(sqlite3.connect(self.db)) as db, db:
            db.execute('INSERT INTO observations VALUES(NULL,202,?,?,?,?,?,?)',
                       ('10.0.0.4', '<CUT>', 'member-03', '203.0.113.7', 'outside', 'first'))
        result = page(self.db, 'from=200000&to=203000')
        self.assertIn('<td>&lt;CUT&gt;</td><td>2</td><td>1</td>', result)
        later = page(self.db, 'from=500000&to=501000')
        self.assertIn('member-03', later)
        self.assertNotIn('203.0.113.7', later)
