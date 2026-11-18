import copy
import unittest

import capacity


class CapacityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Compose parsing only; no containers, daemon mutation or credentials.
        cls.config = capacity.compose_config()

    def test_pr_profile_fits_host_and_pooler(self):
        report = capacity.budget(self.config)
        self.assertEqual(report["profile"], "pr-final")
        self.assertEqual(report["errors"], [])
        self.assertEqual(
            report["assumptions"],
            {
                "player_teams": 6,
                "npc_teams": 1,
                "teams": 7,
                "services": {
                    "kincare": {"puts": 5, "gets": 3},
                    "wahrwelt": {"puts": 10, "gets": 1},
                    "sentinel": {"puts": 3, "gets": 3},
                },
                "checks_per_team": 3,
                "puts_per_team": 18,
                "gets_per_team": 7,
                "actions_per_team": 28,
                "round_seconds": 60,
                "rounds": 240,
                "flag_lifetime": 5,
                "vm_vcpus": 8,
                "vm_memory_gib": 16,
            },
        )
        self.assertEqual(
            report["replicas"], {"api": 2, "receiver": 1, "worker": 2, "ticker": 1}
        )
        self.assertEqual(report["checker_action_slots"], 16)
        self.assertEqual(report["actions_per_round_upper_bound"], 196)
        self.assertEqual(report["database_clients_total"], 52)
        self.assertEqual(
            report["database_clients_by_role"],
            {"api": 16, "receiver": 16, "worker": 16, "ticker": 4},
        )
        self.assertEqual(
            report["pooler_client_limits_by_role"],
            {"api": 32, "receiver": 32, "worker": 32, "ticker": 16},
        )
        self.assertEqual(report["pooler_servers_total"], 52)
        self.assertEqual(report["postgres_connections"], 80)
        self.assertEqual(report["hard_memory_total_gib"], 13.5)
        self.assertEqual(report["hard_memory_gib_by_service"]["worker"], 6.0)
        self.assertEqual(report["hard_memory_gib_by_service"]["prometheus"], 1.0)
        self.assertEqual(report["hard_memory_gib_by_service"]["grafana"], 0.5)
        self.assertEqual(report["host_headroom_gib"], 2.5)
        self.assertEqual(report["memory_reservation_total_gib"], 8.625)
        self.assertEqual(report["slot_work_seconds_by_mean_action_seconds"]["2"], 24.5)

    def test_replica_profile_mismatch_fails(self):
        config = copy.deepcopy(self.config)
        config["services"]["api"]["deploy"]["replicas"] = 3
        self.assertIn(
            "pr-final: replicas differ from the 2/1/2/1 profile",
            capacity.budget(config)["errors"],
        )

    def test_hard_memory_ceiling_cannot_exceed_fourteen_gib(self):
        config = copy.deepcopy(self.config)
        config["services"]["worker"]["mem_limit"] = 4 * 2**30
        self.assertIn(
            "Container hard limits exceed the 14 GiB ceiling",
            capacity.budget(config)["errors"],
        )

    def test_every_long_running_service_requires_a_hard_memory_limit(self):
        config = copy.deepcopy(self.config)
        config["services"]["prometheus"]["mem_limit"] = 0
        self.assertIn(
            "prometheus: hard memory limit must be nonzero",
            capacity.budget(config)["errors"],
        )

    def test_insufficient_alias_budget_fails(self):
        config = copy.deepcopy(self.config)
        config["services"]["pgbouncer"]["environment"]["PGBOUNCER_WORKER_CLIENTS"] = (
            "23"
        )
        self.assertIn(
            "worker: 24 clients including reserve exceed alias budget",
            capacity.budget(config)["errors"],
        )

    def test_postgres_reserve_is_enforced(self):
        config = copy.deepcopy(self.config)
        config["services"]["postgres"]["command"] = [
            "--max-connections=67"
            if value.startswith("--max-connections=")
            else value
            for value in config["services"]["postgres"]["command"]
        ]
        self.assertIn(
            "PostgreSQL has fewer than 16 spare connections",
            capacity.budget(config)["errors"],
        )

    def test_checker_process_limit_stays_at_one_gibibyte(self):
        self.assertEqual(
            self.config["services"]["worker"]["environment"][
                "FORCAD_CHECKER_MEMORY_LIMIT_MB"
            ],
            "1024",
        )

    def test_checker_process_limit_cannot_be_silently_lowered(self):
        config = copy.deepcopy(self.config)
        config["services"]["worker"]["environment"][
            "FORCAD_CHECKER_MEMORY_LIMIT_MB"
        ] = "512"
        self.assertIn(
            "Checker process memory limit is below the 1024 MiB baseline",
            capacity.budget(config)["errors"],
        )


if __name__ == "__main__":
    unittest.main()
