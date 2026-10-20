"""Read completed demo rounds without changing services or game state."""

import argparse
import json
import os
import subprocess
from pathlib import Path

import tomllib
from check import COMPOSE, inventory


def collect(rounds, team_count, extra_checks):
    jobs_per_round = team_count * (1 + extra_checks)
    sql = f"""
    WITH complete AS (
        SELECT r.id, r.round, r.round_start
        FROM DispatchRuns r JOIN CheckerJobs j ON j.run_id = r.id
        WHERE r.is_round
        GROUP BY r.id
        HAVING count(*) = {jobs_per_round}
            AND count(DISTINCT j.team_id) = {team_count}
            AND count(j.finished_at) = {jobs_per_round}
            AND count(j.sent_at) = {jobs_per_round}
        ORDER BY r.round_start DESC LIMIT {rounds}
    )
    SELECT coalesce(json_agg(sample ORDER BY round_start, sent_ms), '[]')
    FROM (
        SELECT r.round, r.round_start, t.name AS team, j.action,
            (extract(epoch FROM j.sent_at) - r.round_start) * 1000 AS sent_ms,
            extract(epoch FROM (j.sent_at - j.not_before)) * 1000 AS dispatch_delay_ms,
            (extract(epoch FROM j.finished_at) - r.round_start) * 1000 AS finished_ms
        FROM complete r JOIN CheckerJobs j ON j.run_id = r.id
        JOIN Teams t ON t.id = j.team_id
    ) sample;
    """
    result = subprocess.run(
        COMPOSE
        + [
            "exec",
            "-T",
            "postgres",
            "sh",
            "-c",
            'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -X -At -v ON_ERROR_STOP=1',
        ],
        input=sql,
        capture_output=True,
        text=True,
        check=True,
        timeout=15,
    )
    samples = json.loads(result.stdout)
    if len(samples) != rounds * jobs_per_round:
        raise SystemExit(
            f"Need {rounds} completed rounds with {team_count} teams; found {len(samples) // jobs_per_round}"
        )
    return {
        "teams": team_count,
        "extra_checks": extra_checks,
        "rounds": rounds,
        "jobs": len(samples),
        "sent_ms": {
            "min": min(s["sent_ms"] for s in samples),
            "max": max(s["sent_ms"] for s in samples),
        },
        "finished_ms": {
            "min": min(s["finished_ms"] for s in samples),
            "max": max(s["finished_ms"] for s in samples),
        },
        "samples": samples,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rounds", type=int, default=5)
    args = parser.parse_args()
    if not 1 <= args.rounds <= 20:
        parser.error("Use 1..20 rounds")
    settings = tomllib.loads((Path(__file__).with_name("settings.toml")).read_text())
    report = collect(
        args.rounds, len(inventory()), settings["game"].get("extra_checks", 0)
    )
    output = Path(__file__).resolve().parents[2] / "config/runtime/demo/timing.json"
    output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({k: v for k, v in report.items() if k != "samples"}, indent=2))
    print(f"Saved {output.relative_to(output.parents[3])}")


if __name__ == "__main__":
    os.umask(0o077)
    main()
