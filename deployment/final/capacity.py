#!/usr/bin/env python3
"""Calculate the pr-final budget from the rendered Compose configuration."""

import argparse
import json
import os
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PROFILE = "pr-final"
ROLES = ("api", "receiver", "worker", "ticker")
EXPECTED_REPLICAS = {"api": 2, "receiver": 1, "worker": 2, "ticker": 1}
ONESHOT = {"initializer", "config-loader", "postgres-data", "checker-db-access"}
VM_MEMORY_GIB = 16
CONTAINER_CEILING_GIB = 14
MIN_HOST_HEADROOM_GIB = 2
SERVICES = {
    "kincare": {"puts": 5, "gets": 3},
    "wahrwelt": {"puts": 10, "gets": 1},
    "sentinel": {"puts": 3, "gets": 3},
}
PLAYER_TEAMS = 6
NPC_TEAMS = 1
TEAMS = PLAYER_TEAMS + NPC_TEAMS
ROUNDS = 240
ROUND_SECONDS = 60
FLAG_LIFETIME = 5
CHECKS_PER_TEAM = len(SERVICES)
PUTS_PER_TEAM = sum(service["puts"] for service in SERVICES.values())
GETS_PER_TEAM = sum(service["gets"] for service in SERVICES.values())
ACTIONS_PER_TEAM = CHECKS_PER_TEAM + PUTS_PER_TEAM + GETS_PER_TEAM


def budget(config):
    services = config["services"]
    clients = {}
    servers = {}
    replicas = {}
    errors = []
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
    if replicas != EXPECTED_REPLICAS:
        errors.append(f"{PROFILE}: replicas differ from the 2/1/2/1 profile")

    reservations = {}
    hard_limits = {}
    for name, service in services.items():
        if name in ONESHOT:
            continue
        count = int(service.get("deploy", {}).get("replicas", 1))
        limit = int(service.get("mem_limit", 0))
        reservation = int(service.get("mem_reservation", 0))
        reservations[name] = round(count * reservation / 2**30, 3)
        hard_limits[name] = round(count * limit / 2**30, 3) if limit else None
        if not limit:
            errors.append(f"{name}: hard memory limit must be nonzero")
        elif reservation > limit:
            errors.append(f"{name}: memory reservation exceeds hard limit")

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
    if sum(clients.values()) + 8 * len(ROLES) > pooler_max:
        errors.append("PgBouncer global client budget exceeded")

    postgres_limit = next(
        int(arg.split("=", 1)[1])
        for arg in services["postgres"]["command"]
        if arg.startswith("--max-connections=")
    )
    if sum(servers.values()) + 16 > postgres_limit:
        errors.append("PostgreSQL has fewer than 16 spare connections")

    ceiling = (
        round(sum(hard_limits.values()), 3)
        if all(limit is not None for limit in hard_limits.values())
        else None
    )
    headroom = round(VM_MEMORY_GIB - ceiling, 3) if ceiling is not None else None
    if ceiling is not None and ceiling > CONTAINER_CEILING_GIB:
        errors.append("Container hard limits exceed the 14 GiB ceiling")
    if headroom is not None and headroom < MIN_HOST_HEADROOM_GIB:
        errors.append("Host memory headroom is below 2 GiB")

    worker_concurrency = int(
        services["worker"]["environment"]["WORKER_CONCURRENCY"]
    )
    if worker_concurrency != 8:
        errors.append(f"{PROFILE}: worker concurrency must be 8")
    checker_limit = int(
        services["worker"]["environment"]["FORCAD_CHECKER_MEMORY_LIMIT_MB"]
    )
    if checker_limit < 1024:
        errors.append("Checker process memory limit is below the 1024 MiB baseline")
    slots = replicas["worker"] * worker_concurrency
    if slots <= 0:
        raise ValueError("At least one checker action slot is required")

    actions = TEAMS * ACTIONS_PER_TEAM
    return {
        "profile": PROFILE,
        "assumptions": {
            "player_teams": PLAYER_TEAMS,
            "npc_teams": NPC_TEAMS,
            "teams": TEAMS,
            "services": SERVICES,
            "checks_per_team": CHECKS_PER_TEAM,
            "puts_per_team": PUTS_PER_TEAM,
            "gets_per_team": GETS_PER_TEAM,
            "actions_per_team": ACTIONS_PER_TEAM,
            "round_seconds": ROUND_SECONDS,
            "rounds": ROUNDS,
            "flag_lifetime": FLAG_LIFETIME,
            "vm_vcpus": 8,
            "vm_memory_gib": VM_MEMORY_GIB,
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
            slots * (ROUND_SECONDS / 2) / actions, 2
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
        "host_headroom_gib": headroom,
        "flags_created": TEAMS * PUTS_PER_TEAM * ROUNDS,
        "errors": errors,
        "note": (
            "Hard limits cap containers but do not reserve RAM. Slot work is a "
            "lower bound, not proof that a round finishes in 60 seconds. Each "
            "checker keeps its 1024 MiB process limit; eight concurrent checkers "
            "can still exhaust a 3 GiB worker container if their RSS is high."
        ),
    }


def compose_config():
    environment = dict(os.environ)
    limits_file = ROOT / "deployment/final/limits.env.example"
    for line in limits_file.read_text().splitlines():
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
            str(limits_file),
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
