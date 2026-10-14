"""Regression checks for asynchronously scraped flag counters."""

import unittest
from unittest.mock import Mock, patch

from check_monitoring import wait_for_counter


def sample(value):
    return [{"value": [0, str(value)]}]


class MonitoringTest(unittest.TestCase):
    @patch("check_monitoring.time.sleep")
    def test_waits_for_both_replica_samples(self, sleep):
        query = Mock(side_effect=[[], sample(0), sample(99), sample(100)])

        wait_for_counter(query, "duplicates", 100)

        self.assertEqual(query.call_count, 4)
        self.assertEqual(sleep.call_count, 3)

    @patch("check_monitoring.time.sleep")
    def test_rejects_overcount(self, sleep):
        with self.assertRaisesRegex(AssertionError, "expected 100, last observed 101"):
            wait_for_counter(Mock(return_value=sample(101)), "duplicates", 100)
        sleep.assert_not_called()

    @patch("check_monitoring.time.monotonic", side_effect=[0, 0, 1, 2])
    @patch("check_monitoring.time.sleep")
    def test_reports_stale_counter_at_deadline(self, sleep, monotonic):
        query = Mock(return_value=sample(99))
        with self.assertRaisesRegex(AssertionError, "expected 100, last observed 99"):
            wait_for_counter(query, "duplicates", 100, timeout=2)
        self.assertEqual(query.call_count, 2)


if __name__ == "__main__":
    unittest.main()
