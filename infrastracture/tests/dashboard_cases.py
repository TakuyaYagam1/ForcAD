"""PromQL fixtures for selecting traffic without one graph line per team pair."""

import json
import re


def interpolate(expression, source=None, target=None, **filters):
    """Apply the two escaping layers used by Grafana's Prometheus formatter."""
    expression = expression.replace("$__rate_interval", "1m")
    values = {"source": source, "target": target, **filters}
    for name, value in values.items():
        regex = ".*" if value is None else re.escape(value)
        escaped = regex if value is None else json.dumps(regex)[1:-1]
        expression = expression.replace("${" + name + "}", escaped)
        # Explicit :regex bypasses datasource escaping. Keep this case so a
        # regression to that format fails on real dotted IP addresses.
        expression = expression.replace("${" + name + ":regex}", regex)
    return expression


def build_infrastructure_cases(dashboard):
    panels = list(dashboard["panels"])
    for panel in panels:
        panels.extend(panel.get("panels", []))
    queries = {
        panel["id"]: panel["targets"][0]["expr"]
        for panel in panels
        if panel.get("targets")
    }
    series = []

    def add(metric, job, instance, value):
        series.append(
            {
                "series": f'{metric}{{job="{job}",instance="{instance}"}}',
                "values": f"{value}x8",
            }
        )

    for role in ("api", "receiver", "worker", "ticker"):
        job = "forcad_" + role
        replicas = (1, 2, 99) if role == "api" else (1,)
        for replica in replicas:
            instance = f"10.0.0.{replica}:9090"
            add("up", job, instance, int(replica != 99))
            used = 9 if role == "api" and replica == 1 else 1
            if replica == 99:
                used = 999
            for metric, value in {
                "forcad_database_connections_in_use": used,
                "forcad_database_connections": 10,
                "forcad_database_connection_limit": 10,
                "forcad_redis_connections": 2,
                "forcad_redis_connections_idle": 1,
                "forcad_redis_pool_pending_requests": 0,
            }.items():
                add(metric, job, instance, value)
    # Infrastructure targets must not create empty component rows.
    add("up", "prometheus", "localhost:9090", 1)
    add("up", "forcad_router", "router:9109", 1)

    def expr(panel, component=None, instance=None):
        return interpolate(queries[panel], component=component, instance=instance)

    def check(expression, value, labels="{}"):
        return {
            "expr": expression,
            "eval_time": "2m",
            "exp_samples": [{"labels": labels, "value": value}],
        }

    return {
        "rule_files": [],
        "evaluation_interval": "15s",
        "tests": [
            {
                "name": "component totals preserve hot replicas and exclude unrelated targets",
                "interval": "15s",
                "input_series": series,
                "promql_expr_test": [
                    check(expr(10), 1),
                    check(expr(13), 5),
                    check(expr(11), 90),
                    check(f"count({expr(7)})", 32),
                    check(f"count({expr(3)})", 4),
                    check(
                        expr(3, "forcad_api"), 90, '{component="api",job="forcad_api"}'
                    ),
                    check(
                        expr(3, "forcad_api", "10.0.0.2:9090"),
                        10,
                        '{component="api",job="forcad_api"}',
                    ),
                ],
            }
        ],
    }


def build_cases(dashboard):
    panels = list(dashboard["panels"])
    for panel in panels:
        panels.extend(panel.get("panels", []))
    queries = {
        panel["id"]: panel["targets"][0]["expr"]
        for panel in panels
        if panel.get("targets")
    }
    series = []

    def add(name, labels, values="1x8"):
        labels = ",".join(f"{key}={json.dumps(value)}" for key, value in labels.items())
        series.append({"series": f"{name}{{{labels}}}", "values": values})

    sensor = {"job": "forcad_game_sensor", "instance": "router"}
    router = {"job": "forcad_router", "instance": "nftables"}
    add("up", sensor)
    add("up", router)
    add("forcad:game_sensor_ready", sensor)
    add("forcad_network_source_scrape_success", router)
    for source in range(1, 21):
        source_ip = f"10.183.{100 + source}.2"
        peer = f"10.183.200.{source * 8 - 6}"
        add(
            "forcad:game_source_info",
            {"source_team_ip": source_ip, "source_team_name": f"Team {source:02}"},
        )
        add(
            "forcad:game_target_info",
            {"team_ip": source_ip, "target_team_name": f"Team {source:02}"},
        )
        add(
            "forcad_game_peer_info",
            sensor | {"source_ip": peer, "source_team_ip": source_ip},
        )
        add("forcad:game_peer_info", {"source_ip": peer, "source_team_ip": source_ip})
        for target in range(1, 21):
            target_ip = f"10.183.{100 + target}.2"
            labels = sensor | {"source_team_ip": source_ip, "team_ip": target_ip}
            active = int(source != target)
            # One hot pair; another carries only non-HTTP network traffic.
            rate = 20 if (source, target) == (1, 2) else active
            if (source, target) == (19, 20):
                rate = 0
            add("forcad_game_http_requests_total", labels, f"0+{15 * rate}x8")
            add("forcad_game_websocket_connections", labels, f"{active}x8")
            add("forcad_game_tcp_connections", labels, f"{active}x8")
            add("forcad_game_tcp_flows_total", labels, f"0+{15 * active}x8")
            network = router | {"source_ip": peer, "team_ip": target_ip}
            add("forcad_network_source_packets_total", network, f"0+{600 * active}x8")
            add("forcad_network_source_bytes_total", network, f"0+{60000 * active}x8")

    def expression(panel, source=None, target=None):
        return interpolate(queries[panel], source, target)

    def check(expr, samples):
        return {"expr": expr, "eval_time": "2m", "exp_samples": samples}

    def scalar(expr, value):
        return check(expr, [{"labels": "{}", "value": value}])

    overview = expression(11)
    named_pair = "Team 01 [10.183.101.2] -> Team 02 [10.183.102.2]"
    tests = [
        scalar(expression(5), 398),
        scalar(expression(5, "10.183.101.2"), 38),
        scalar(expression(5, target="10.183.102.2"), 38),
        scalar(expression(5, "10.183.101.2", "10.183.102.2"), 20),
        scalar(expression(5, "10.183.102.2", "10.183.101.2"), 1),
        scalar(f"count({overview})", 380 * 6),
        scalar(f"count({expression(11, '10.183.101.2')})", 19 * 6),
        check(expression(11, "10.183.101.2", "10.183.101.2"), []),
        check(
            expression(11, "10.183.101.2", "10.183.102.2"),
            [
                {
                    "labels": "{"
                    + f"column={json.dumps(column)},direction={json.dumps(named_pair)}"
                    + "}",
                    "value": value,
                }
                for column, value in (
                    ("HTTP RPS", 20),
                    ("WS", 1),
                    ("TCP", 1),
                    ("New TCP/s", 1),
                    ("Packets/s", 40),
                    ("Bandwidth", 32000),
                )
            ],
        ),
    ]
    for panel in (5, 6, 7, 8, 12, 13):
        tests.append(scalar(f"count({expression(panel)})", 1))
    unnamed = [
        row
        for row in series
        if not row["series"].startswith(
            ("forcad:game_source_info{", "forcad:game_target_info{")
        )
    ]
    stale = [
        row
        for row in series
        if not row["series"].startswith("forcad:game_sensor_ready{")
    ]
    return {
        "rule_files": [],
        "evaluation_interval": "15s",
        "fuzzy_compare": True,
        "tests": [
            {
                "name": "20 teams, 380 active directions, one hot pair",
                "interval": "15s",
                "input_series": series,
                "promql_expr_test": tests,
            },
            {
                "name": "missing team names preserve traffic and table rows",
                "interval": "15s",
                "input_series": unnamed,
                "promql_expr_test": [
                    scalar(f"count({overview})", 380 * 6),
                    scalar(expression(5), 398),
                ],
            },
            {
                "name": "stale sensor produces gaps, not zero HTTP traffic",
                "interval": "15s",
                "input_series": stale,
                "promql_expr_test": [check(expression(5), [])],
            },
        ],
    }
