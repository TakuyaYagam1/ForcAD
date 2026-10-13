#!/usr/bin/env python3
"""Verify real Prometheus scrapes and provisioned Grafana dashboard queries."""
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def get_json(url, auth=None):
    request = urllib.request.Request(url)
    if auth:
        request.add_header("Authorization", "Basic " + base64.b64encode(auth.encode()).decode())
    with urllib.request.urlopen(request, timeout=5) as response:
        return json.load(response)


def wait_for_counter(query, expression, expected, timeout=70):
    # Replicas are scraped independently; a healthy target may still expose
    # the sample collected before the last request in the deployment check.
    deadline = time.monotonic() + timeout
    observed = None
    while time.monotonic() < deadline:
        result = query(expression)
        observed = float(result[0]["value"][1]) if result else None
        if observed == expected:
            return
        if observed is not None and observed > expected:
            break
        time.sleep(1)
    raise AssertionError(f"{expression}: expected {expected}, last observed {observed}")


def main():
    prometheus, grafana, repo, project = sys.argv[1:]
    # The API checks leave one deactivated CRUD fixture in addition to both teams.
    expected_teams = 3
    expected_rows = expected_teams + 1  # Unknown is a separate attribution row.

    def query(expression):
        response = get_json(prometheus + "/api/v1/query?" + urllib.parse.urlencode({"query": expression}))
        assert response["status"] == "success", "Prometheus query failed"
        return response["data"]["result"]

    deadline = time.monotonic() + 70
    while time.monotonic() < deadline:
        try:
            # Scrape both replicas of API/receiver, both workers and one ticker.
            targets = get_json(prometheus + "/api/v1/targets")["data"]["activeTargets"]
            application = [item for item in targets if item["scrapeUrl"].endswith(":9090/metrics")
                           and item["labels"].get("job") != "prometheus"]
            if len(application) >= 7 and all(item["health"] == "up" for item in application):
                if (query('sum(forcad_team_flags_total{verdict="accepted"})')
                        and query("forcad:team_info:latest")
                        and len(query('forcad:team_info:latest{team_id!="__unknown__"}')) == expected_teams
                        and query("sum(forcad_team_websocket_upgrades_total)")):
                    break
        except (urllib.error.URLError, TimeoutError):
            pass
        time.sleep(1)
    else:
        raise SystemExit("Monitoring did not discover and scrape all application replicas")

    teams = query('max by(team_id,team_name)(forcad_team_info{team_id!="__unknown__"})')
    assert len(teams) == expected_teams, "Configured teams missing or duplicated"
    wait_for_counter(query, 'sum(forcad_team_flags_total{verdict="accepted"})', 1)
    wait_for_counter(query, 'sum(forcad_team_flags_total{verdict="duplicate"})', 100)
    assert query('sum(forcad_team_http_requests_total{route="/flags/",status="200",team_id!="__unknown__"})'), "Verified flag token did not attribute HTTP request"
    assert not query("forcad_team_network_receive_bytes_total"), "Missing router metrics were fabricated"
    assert query("sum(forcad_team_websocket_upgrades_total)"), "WebSocket upgrade was not observed"

    dashboard_paths = list((Path(repo) / "deployment/monitoring").rglob("*.json"))
    dashboards = [json.loads(path.read_text()) for path in dashboard_paths if "panels" in json.loads(path.read_text())]
    assert dashboards, "No provisioned dashboard"
    auth = "admin:" + os.environ["GRAFANA_ADMIN_PASSWORD"]
    for dashboard in dashboards:
        loaded = get_json(grafana + "/api/dashboards/uid/" + dashboard["uid"], auth)
        assert loaded["dashboard"]["title"] == dashboard["title"], "Dashboard was not provisioned"
        for panel in dashboard["panels"]:
            for target in panel.get("targets", []):
                expression = target.get("expr", "")
                if expression:
                    expression = expression.replace("$__rate_interval", "1m").replace("$__interval", "15s")
                    expression = expression.replace("${team:regex}", ".*").replace("$team", ".*")
                    query(expression)
    table_query = dashboards[0]["panels"][0]["targets"][0]["expr"]
    table = query(table_query)
    columns = {"REST API RPS", "Flags/s", "HTTP errors/s", "p99 REST API (s)",
               "WS", "Outgoing Mbps", "Incoming Mbps", "Packets/s"}
    assert len(table) == 8 * expected_rows, "Expected eight columns per team and Unknown"
    assert {item["metric"]["column"] for item in table} == columns, "Table columns are missing"
    for item in table:
        if item["metric"]["column"] in {"Outgoing Mbps", "Incoming Mbps", "Packets/s"}:
                assert item["value"][1] == "NaN", "Missing router data must be N/A, not zero"

    def stop_test_role(role):
        assert project.startswith("forcad-deployment-"), "Refusing to stop a non-test project"
        containers = subprocess.check_output([
            "docker", "ps", "-q", "--filter", "label=com.docker.compose.project=" + project,
            "--filter", "label=com.docker.compose.service=" + role,
        ], text=True).split()
        assert len(containers) == 2, "Expected two test replicas"
        subprocess.run(["docker", "stop", "--time", "10", *containers], check=True, stdout=subprocess.DEVNULL)

    def expect_unavailable(column):
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            cells = [item for item in query(table_query) if item["metric"]["column"] == column]
            if len(cells) == expected_rows and all(item["value"][1] == "NaN" for item in cells):
                return
            time.sleep(1)
        raise AssertionError(column + " showed zero or stale data after loss of all sources")

    stop_test_role("receiver")
    expect_unavailable("Flags/s")
    stop_test_role("api")
    expect_unavailable("WS")
    expect_unavailable("REST API RPS")
    print("Monitoring checks passed: replicas, team/flag/WS metrics, Grafana queries, all table columns and missing-source handling.")


if __name__ == "__main__":
    main()
