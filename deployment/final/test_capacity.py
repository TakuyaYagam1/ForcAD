import copy
import unittest

import capacity


class CapacityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Compose parsing only; no containers, daemon mutation or credentials.
        cls.config = capacity.compose_config()

    def test_final_profile_fits_host_and_pooler(self):
        for name, service in self.config["services"].items():
            self.assertEqual(int(service.get("mem_limit", 0)), 0, name)
        report = capacity.budget(self.config)
        self.assertEqual(report["errors"], [])
        self.assertEqual(
            report["replicas"], {"api": 4, "receiver": 2, "worker": 4, "ticker": 1}
        )
        self.assertEqual(report["assumptions"]["teams"], 22)
        self.assertEqual(report["assumptions"]["puts_per_team"], 18)
        self.assertEqual(report["assumptions"]["gets_per_team"], 7)
        self.assertEqual(report["assumptions"]["rounds"], 240)
        self.assertEqual(report["checker_action_slots"], 32)
        self.assertEqual(report["actions_per_round_upper_bound"], 616)
        self.assertEqual(report["database_clients_total"], 120)
        self.assertEqual(report["pooler_servers_total"], 136)
        self.assertIsNone(report["hard_memory_total_gib"])
        self.assertIsNone(report["host_headroom_gib"])
        self.assertEqual(report["memory_reservation_total_gib"], 13.875)
        self.assertEqual(report["slot_work_seconds_by_mean_action_seconds"]["20"], 385)

    def test_worker_reservations_cannot_silently_exceed_planning_budget(self):
        config = copy.deepcopy(self.config)
        config["services"]["worker"]["mem_reservation"] = 8 * 2**30
        self.assertTrue(
            any("memory" in error for error in capacity.budget(config)["errors"])
        )

    def test_insufficient_alias_budget_fails(self):
        config = copy.deepcopy(self.config)
        config["services"]["worker"]["environment"][
            "FORCAD_WORKER_DATABASE_MAX_CONNS"
        ] = "16"
        config["services"]["pgbouncer"]["environment"]["PGBOUNCER_WORKER_CLIENTS"] = (
            "64"
        )
        self.assertTrue(
            any("alias" in error for error in capacity.budget(config)["errors"])
        )

    def test_unlimited_memory_is_not_counted_as_zero_usage(self):
        report = capacity.budget(self.config)
        self.assertIsNone(report["hard_memory_gib_by_service"]["prometheus"])
        self.assertEqual(report["errors"], [])


if __name__ == "__main__":
    unittest.main()
