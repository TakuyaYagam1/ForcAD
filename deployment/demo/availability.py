"""Check extra CHECKs on the local demo without resetting its game or database."""

import argparse
import http.cookiejar
import json
import os
import subprocess
import threading
import time
import urllib.request

from check import COMPOSE, ROOT, execute, inventory


def sql(query):
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
        input=query,
        capture_output=True,
        text=True,
        check=True,
        timeout=15,
    )
    return json.loads(result.stdout)


def api_client():
    credentials = dict(
        line.split("=", 1)
        for line in (ROOT / "config/runtime/admin.env").read_text().splitlines()
        if line and not line.startswith("#")
    )
    client = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
    )

    def request(path, data=None):
        req = urllib.request.Request(
            "http://localhost:8080" + path,
            data=json.dumps(data).encode() if data is not None else None,
            headers={
                "Content-Type": "application/json",
                "Origin": "http://localhost:8080",
            },
        )
        with client.open(req, timeout=10) as response:
            body = response.read()
            return json.loads(body) if body else None

    request(
        "/api/admin/login/",
        {
            "username": credentials["ADMIN_USERNAME"],
            "password": credentials["ADMIN_PASSWORD"],
        },
    )
    return request


def state():
    return sql("""
        SELECT row_to_json(s) FROM (
            SELECT g.real_round AS round, g.extra_checks, g.round_time,
                g.game_running, s.generation, s.paused_at IS NOT NULL AS paused,
                s.reset_pending, s.practice_start IS NOT NULL AS practice
            FROM GameConfig g CROSS JOIN GameSession s
        ) s;
    """)


def samples(since):
    return sql(f"""
        SELECT coalesce(json_agg(sample ORDER BY round, team, action), '[]')
        FROM (
            SELECT r.id AS run_id, r.round, t.name AS team, j.action,
                j.result->>'status' AS status,
                (extract(epoch FROM j.not_before)-r.round_start)*1000 AS planned_ms,
                (extract(epoch FROM j.sent_at)-r.round_start)*1000 AS sent_ms,
                extract(epoch FROM (j.sent_at-j.not_before))*1000 AS queue_ms,
                extract(epoch FROM (j.finished_at-j.sent_at))*1000 AS execution_ms
            FROM DispatchRuns r JOIN CheckerJobs j ON j.run_id=r.id
            JOIN Teams t ON t.id=j.team_id
            WHERE r.is_round AND r.round_start >= {float(since)}
              AND NOT EXISTS (
                SELECT 1 FROM CheckerJobs unfinished
                WHERE unfinished.run_id=r.id AND unfinished.finished_at IS NULL
              )
        ) sample;
    """)


def wait_rounds(since, rounds, team_count, extra_checks):
    deadline = time.monotonic() + (rounds + 2) * 60
    last_count = -1
    while time.monotonic() < deadline:
        rows = samples(since)
        count = len({row["run_id"] for row in rows})
        if count != last_count:
            print(f"Completed experiment rounds: {count}/{rounds}", flush=True)
            last_count = count
        if count >= rounds:
            assert len(rows) == count * team_count * (extra_checks + 1), "Missing jobs"
            assert all(row["status"] not in (None, "110") for row in rows), (
                "Incomplete CHECK"
            )
            return rows
        time.sleep(3)
    raise RuntimeError("Timed out waiting for complete demo rounds")


def summarize(rows):
    pairs = {}
    for row in rows:
        pairs.setdefault((row["run_id"], row["team"]), []).append(row)
    by_team = {}
    for (_, team), jobs in pairs.items():
        counts = by_team.setdefault(
            team,
            {
                "rounds": 0,
                "main_failed": 0,
                "combined_failed": 0,
                "extra_only_failed": 0,
            },
        )
        main_failed = any(
            j["action"] == "classic" and j["status"] != "101" for j in jobs
        )
        combined_failed = any(j["status"] != "101" for j in jobs)
        counts["rounds"] += 1
        counts["main_failed"] += main_failed
        counts["combined_failed"] += combined_failed
        counts["extra_only_failed"] += combined_failed and not main_failed
    delayed = [row["queue_ms"] for row in rows if row["queue_ms"] is not None]
    return {"teams": by_team, "max_dispatch_delay_ms": max(delayed), "jobs": len(rows)}


def fault_loop(stop, events, errors):
    """A fixed wall-clock cycle; never reads rounds or checker job timing."""
    role = "demo-team3"
    table = "availability_test"
    created = False
    try:
        cycle_start = time.monotonic()
        while not stop.is_set():
            if stop.wait(max(0, cycle_start + 15 - time.monotonic())):
                break
            execute(
                role,
                "nft",
                "-f",
                "-",
                input=f"""
                table inet {table} {{
                    chain input {{
                        type filter hook input priority -20; policy accept;
                        iifname != "lo" tcp dport 8080 reject with tcp reset;
                    }}
                }}
            """,
            )
            created = True
            events.append({"at": time.time(), "available": False})
            if stop.wait(max(0, cycle_start + 37 - time.monotonic())):
                break
            execute(role, "nft", "delete", "table", "inet", table)
            created = False
            events.append({"at": time.time(), "available": True})
            cycle_start += 37
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        errors.append(str(exc))
        stop.set()
    finally:
        if created:
            try:
                execute(role, "nft", "delete", "table", "inet", table)
                events.append({"at": time.time(), "available": True})
            except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
                errors.append(f"Fault cleanup failed: {exc}")


def availability(rounds, faulty, settings):
    count = len(inventory())
    stop = threading.Event()
    events, errors = [], []
    thread = None
    if faulty:
        existing = json.loads(
            execute("demo-team3", "nft", "-j", "list", "tables").stdout
        )
        assert not any(
            item.get("table", {}).get("name") == "availability_test"
            for item in existing["nftables"]
        ), "Fault table already exists"
        thread = threading.Thread(target=fault_loop, args=(stop, events, errors))
        thread.start()
    try:
        rows = wait_rounds(time.time(), rounds, count, settings["extra_checks"])
    finally:
        stop.set()
        if thread is not None:
            thread.join(timeout=30)
            assert not thread.is_alive(), "Fault cleanup did not finish"
    assert not errors, errors
    summary = summarize(rows)
    if not faulty:
        assert all(
            team["combined_failed"] == 0 for team in summary["teams"].values()
        ), summary
    else:
        target = inventory()[2]["name"]
        assert summary["teams"][target]["combined_failed"] > 0, "Missed every outage"
        assert all(
            team["combined_failed"] == 0
            for name, team in summary["teams"].items()
            if name != target
        ), summary
        remaining = json.loads(
            execute("demo-team3", "nft", "-j", "list", "tables").stdout
        )
        assert not any(
            item.get("table", {}).get("name") == "availability_test"
            for item in remaining["nftables"]
        )
    return {"summary": summary, "samples": rows, "outages": events}


def pending_jobs():
    return sql("""
        SELECT coalesce(json_agg(s ORDER BY id), '[]') FROM (
            SELECT id, run_id, sent_at IS NOT NULL AS sent,
                extract(epoch FROM not_before) AS not_before,
                extract(epoch FROM expires_at) AS expires_at
            FROM CheckerJobs WHERE action='check_only' AND finished_at IS NULL
        ) s;
    """)


def pause_restart():
    request = api_client()
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        jobs = pending_jobs()
        if jobs and all(
            not job["sent"] and job["not_before"] > time.time() + 3 for job in jobs
        ):
            main_pending = sql("""
                SELECT count(*) FROM CheckerJobs
                WHERE action='classic' AND finished_at IS NULL;
            """)
            if main_pending == 0:
                break
        time.sleep(1)
    else:
        raise RuntimeError("No fresh round with pending CHECKs")
    resumed = False
    try:
        request("/api/admin/game/pause/", {"generation": state()["generation"]})
        before = pending_jobs()
        assert before and all(not job["sent"] for job in before)
        time.sleep(10)
        subprocess.run(
            COMPOSE + ["restart", "--timeout", "5", "ticker", "worker"],
            check=True,
            timeout=45,
        )
        time.sleep(10)
        assert state()["paused"]
        assert before == pending_jobs(), "Pending jobs changed during paused restart"
        request("/api/admin/game/resume/", {"generation": state()["generation"]})
        resumed = True
        after = pending_jobs()
        assert [job["id"] for job in before] == [job["id"] for job in after]
        shifts = [b["not_before"] - a["not_before"] for a, b in zip(before, after)]
        assert min(shifts) >= 20 and max(shifts) - min(shifts) < 0.001
        for a, b in zip(before, after):
            assert abs((b["expires_at"] - a["expires_at"]) - shifts[0]) < 0.001
        ids = {job["id"] for job in after}
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            if not ids.intersection(job["id"] for job in pending_jobs()):
                closed = sql("""
                    SELECT coalesce(json_agg(json_build_object('id', id, 'status', result->>'status')), '[]')
                    FROM CheckerJobs WHERE action='check_only' AND finished_at IS NOT NULL;
                """)
                statuses = [job["status"] for job in closed if job["id"] in ids]
                assert len(statuses) == len(ids) and all(
                    status == "101" for status in statuses
                ), statuses
                return {
                    "jobs_retained": len(ids),
                    "pause_seconds": shifts[0],
                    "all_up": True,
                }
            time.sleep(2)
        raise RuntimeError("Pending jobs did not finish after resume")
    finally:
        if not resumed and state()["paused"]:
            request("/api/admin/game/resume/", {"generation": state()["generation"]})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenario", choices=["healthy", "fault", "pause"])
    parser.add_argument("--rounds", type=int, default=5)
    args = parser.parse_args()
    if not 1 <= args.rounds <= 10:
        parser.error("Use 1..10 rounds")
    subprocess.run(
        COMPOSE + ["exec", "-T", "postgres", "pwd", "-P"],
        check=True,
        capture_output=True,
        timeout=10,
    )
    settings = state()
    if (
        not settings["practice"]
        or not settings["game_running"]
        or settings["paused"]
        or settings["extra_checks"] != 2
        or settings["round_time"] != 60
    ):
        parser.error(
            "Requires running local practice, 60-second rounds and extra_checks=2"
        )
    report = (
        pause_restart()
        if args.scenario == "pause"
        else availability(args.rounds, args.scenario == "fault", settings)
    )
    path = ROOT / f"config/runtime/demo/availability-{args.scenario}.json"
    path.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report.get("summary", report), indent=2))
    print(f"Saved {path.relative_to(ROOT)}")


if __name__ == "__main__":
    os.umask(0o077)
    main()
