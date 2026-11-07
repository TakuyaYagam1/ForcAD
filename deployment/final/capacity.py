#!/usr/bin/env python3
"""Calculate final budgets from the rendered Compose configuration."""

import argparse
import json
import os
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ROLES = ("api", "receiver", "worker", "ticker")
ONESHOT = {"initializer", "config-loader", "postgres-data", "checker-db-access"}


def budget(config):
    services = config["services"]
    clients = {}
    servers = {}
    replicas = {}
    for role in ROLES:
        service = services[role]
        replicas[role] = int(service.get("deploy", {}).get("replicas", 1))
        clients[role] = replicas[role] * int(
            service["environment"][f"FORCAD_{role.upper()}_DATABASE_MAX_CONNS"]
        )
        servers[role] = int(
            services["pgbouncer"]["environment"][
                f"PGBOUNCER_{role.upper()}_CONNECTIONS"
            ]
        )
    reservations = {}
    hard_limits = {}
    errors = []
    for name, service in services.items():
        if name in ONESHOT:
            continue
        count = int(service.get("deploy", {}).get("replicas", 1))
        limit = int(service.get("mem_limit", 0))
        reservation = int(service.get("mem_reservation", 0))
        reservations[name] = round(count * reservation / 2**30, 3)
        hard_limits[name] = round(count * limit / 2**30, 3) if limit else None
    pooler_env = services["pgbouncer"]["environment"]
    client_limits = {}
    for role, count in clients.items():
        required = count + 8
        client_limits[role] = int(
            pooler_env.get(f"PGBOUNCER_{role.upper()}_CLIENTS") or 0
        ) or max(256, required, servers[role])
        if required > client_limits[role]:
            errors.append(
                f"{role}: {required} clients including reserve exceed alias budget"
            )
    pooler_max = int(pooler_env.get("PGBOUNCER_MAX_CLIENTS", 1024))
    if sum(clients.values()) + 32 > pooler_max:
        errors.append("PgBouncer global client budget exceeded")
    postgres_limit = next(
        int(arg.split("=", 1)[1])
        for arg in services["postgres"]["command"]
        if arg.startswith("--max-connections=")
    )
    if sum(servers.values()) + 16 > postgres_limit:
        errors.append("PostgreSQL has fewer than 16 spare connections")
    if sum(reservations.values()) > 56:
        errors.append("Soft memory reservations exceed the 56 GiB planning budget")
    ceiling = (
        round(sum(hard_limits.values()), 3)
        if all(limit is not None for limit in hard_limits.values())
        else None
    )
    slots = replicas["worker"] * int(
        services["worker"]["environment"]["WORKER_CONCURRENCY"]
    )
    if slots <= 0:
        raise ValueError("At least one checker action slot is required")
    actions = 28 * (3 + 40 + 3)
    return {
        "assumptions": {
            "teams": 28,
            "members": 196,
            "services": 3,
            "puts_per_team": 40,
            "gets_per_service": 1,
            "round_seconds": 60,
            "rounds": 300,
            "flag_lifetime": 5,
        },
        "replicas": replicas,
        "database_clients_by_role": clients,
        "database_clients_total": sum(clients.values()),
        "pooler_client_limits_by_role": client_limits,
        "pooler_global_client_limit": pooler_max,
        "pooler_servers_by_role": servers,
        "pooler_servers_total": sum(servers.values()),
        "postgres_connections": postgres_limit,
        "redis_client_ceiling": sum(replicas.values()) * 32,
        "checker_action_slots": slots,
        "actions_per_round_upper_bound": actions,
        "slot_work_seconds_by_mean_action_seconds": {
            str(seconds): round(actions * seconds / slots, 2)
            for seconds in (1, 2, 3, 5, 10, 20)
        },
        "max_mean_action_seconds_at_half_slot_utilization": round(
            slots * 30 / actions, 2
        ),
        "http_inflight_by_role": {
            role: replicas[role]
            * int(
                services[role]["environment"][
                    f"FORCAD_{role.upper()}_HTTP_MAX_INFLIGHT"
                ]
            )
            for role in ("api", "receiver")
        },
        "memory_reservation_gib_by_service": reservations,
        "memory_reservation_total_gib": round(sum(reservations.values()), 3),
        "hard_memory_gib_by_service": hard_limits,
        "hard_memory_total_gib": ceiling,
        "host_headroom_gib": round(64 - ceiling, 3) if ceiling is not None else None,
        "flags_created": 28 * 40 * 300,
        "flags_live": 28 * 40 * 5,
        "max_unique_steals": 28 * 40 * 300 * 27,
        "errors": errors,
        "note": "Soft reservations are not preallocated or guaranteed RAM. Null hard limits mean unlimited memory, not zero usage; host headroom requires measurement. Slot work is a lower bound, not measured round duration.",
    }


def compose_config():
    environment = dict(os.environ)
    for line in (ROOT / "deployment/final/limits.env.example").read_text().splitlines():
        if line and not line.startswith("#"):
            environment.pop(line.split("=", 1)[0], None)
    environment.update(
        GRAFANA_ADMIN_PASSWORD="capacity-check-only",
        GRAFANA_CHECKER_DB_PASSWORD="0" * 32,
    )
    result = subprocess.run(
        [
            "docker",
            "compose",
            "--env-file",
            str(ROOT / "deployment/final/limits.env.example"),
            "-f",
            str(ROOT / "deployment/docker/compose.yml"),
            "-f",
            str(ROOT / "deployment/docker/compose.monitoring.yml"),
            "--profile",
            "monitoring",
            "config",
            "--no-env-resolution",
            "--format",
            "json",
        ],
        env=environment,
        capture_output=True,
        text=True,
        check=True,
    )
    return json.loads(result.stdout)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--compose-json", type=Path, help="Use an already rendered Compose JSON file"
    )
    args = parser.parse_args()
    config = (
        json.loads(args.compose_json.read_text())
        if args.compose_json
        else compose_config()
    )
    report = budget(config)
    print(json.dumps(report, indent=2))
    return int(bool(report["errors"]))


if __name__ == "__main__":
    raise SystemExit(main())
