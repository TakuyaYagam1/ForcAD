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
        plain = json.dumps(value)[1:-1] if name == "profile" and value is not None else escaped
        expression = expression.replace("${" + name + "}", plain)
        # Regex characters need both RE2 escaping and PromQL string escaping;
        # real dotted IPs catch a broken interpolation path.
        expression = expression.replace("${" + name + ":regex}", escaped)
    return expression


def build_infrastructure_cases(dashboard):
    panels = list(dashboard["panels"])
    for panel in panels:
        panels.extend(panel.get("panels", []))
    queries = {
        panel["title"]: panel["targets"][0]["expr"]
        for panel in panels
        if panel.get("targets") and panel["targets"][0].get("expr")
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
                    check(expr("Unreachable replicas"), 1),
                    check(expr("Healthy replicas"), 5),
                    check(expr("Highest DB pool usage"), 90),
                    check(f'count({expr("Components - current pools")})', 32),
                    check(f'count({expr("Highest DB pool usage per component")})', 4),
                    check(
                        expr("Highest DB pool usage per component", "forcad_api"), 90, '{component="api",job="forcad_api"}'
                    ),
                    check(
                        expr("Highest DB pool usage per component", "forcad_api", "10.0.0.2:9090"),
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
        panel["title"]: panel["targets"][0]["expr"]
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

    overview = expression("Active directions - last minute")
    named_pair = "Team 01 [10.183.101.2] -> Team 02 [10.183.102.2]"
    tests = [
        scalar(expression("Game HTTP requests/s"), 398),
        scalar(expression("Game HTTP requests/s", "10.183.101.2"), 38),
        scalar(expression("Game HTTP requests/s", target="10.183.102.2"), 38),
        scalar(expression("Game HTTP requests/s", "10.183.101.2", "10.183.102.2"), 20),
        scalar(expression("Game HTTP requests/s", "10.183.102.2", "10.183.101.2"), 1),
        scalar(f"count({overview})", 380 * 6),
        scalar(f"count({expression('Active directions - last minute', '10.183.101.2')})", 19 * 6),
        check(expression("Active directions - last minute", "10.183.101.2", "10.183.101.2"), []),
        check(
            expression("Active directions - last minute", "10.183.101.2", "10.183.102.2"),
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
    for panel in ("Game HTTP requests/s", "Open game WebSockets", "New TCP flows/s", "Established TCP connections", "VPN -> vulnbox packets/s", "VPN -> vulnbox bandwidth"):
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
                    scalar(expression("Game HTTP requests/s"), 398),
                ],
            },
            {
                "name": "stale sensor produces gaps, not zero HTTP traffic",
                "interval": "15s",
                "input_series": stale,
                "promql_expr_test": [check(expression("Game HTTP requests/s"), [])],
            },
        ],
    }


def _panel_queries(dashboard):
    panels = list(dashboard["panels"])
    for panel in panels:
        panels.extend(panel.get("panels", []))
    return {
        panel["title"]: [target["expr"] for target in panel.get("targets", []) if target.get("expr")]
        for panel in panels
    }


def build_ddos_cases(dashboard):
    queries = _panel_queries(dashboard)
    series = []

    def add(name, labels, values):
        labels = ",".join(f"{key}={json.dumps(value)}" for key, value in labels.items())
        series.append({"series": f"{name}{{{labels}}}", "values": values})

    sensor = {"job": "forcad_game_sensor", "instance": "sensor"}
    router = {"job": "forcad_router", "instance": "router"}
    add("forcad:game_sensor_ready", sensor, "1x9")
    add("forcad_network_source_scrape_success", router, "1x9")
    add("up", router, "1x9")
    add(
        "forcad:game_profile_info",
        {
            "source_ip": "10.183.200.2",
            "source_team_ip": "10.183.101.2",
            "source_team_name": "Team 01",
            "member": "member-1",
        },
        "1x9",
    )
    add(
        "forcad_game_peer_http_requests_total",
        sensor
        | {
            "source_ip": "10.183.200.2",
            "team_ip": "10.183.102.2",
            "service_port": "8080/tcp",
        },
        "0+1800x8",
    )
    add(
        "forcad_game_peer_http_requests_total",
        sensor
        | {
            "source_ip": "10.183.200.9",
            "team_ip": "10.183.102.2",
            "service_port": "8080/tcp",
        },
        "0+1650x8",
    )
    body_labels = sensor | {
        "source_ip": "10.183.200.2",
        "team_ip": "10.183.102.2",
        "service_port": "8080/tcp",
    }
    add("forcad_game_http_last_large_body_bytes", body_labels, "2097152x9")
    add("forcad_game_http_last_large_body_timestamp_seconds", body_labels, "90x9")
    add("forcad_game_http_large_bodies_total", body_labels, "0+1x8")
    network_labels = router | {
        "source_ip": "10.183.200.2",
        "team_ip": "10.183.102.2",
    }
    add("forcad_network_source_packets_total", network_labels, "0+300x8")
    add("forcad_network_source_bytes_total", network_labels, "0+60000x8")
    unmapped_network = router | {
        "source_ip": "10.183.200.9",
        "team_ip": "10.183.102.2",
    }
    add("forcad_network_source_packets_total", unmapped_network, "0+150x8")
    add("forcad_network_source_bytes_total", unmapped_network, "0+9000x8")

    def panel(title, index=0):
        return queries[title][index]

    def expression(
        title,
        source="10.183.101.2",
        target="10.183.102.2",
        start=0,
        end=120,
    ):
        expr = interpolate(panel(title), source, target)
        return (
            expr.replace("${__from:date:seconds}", str(start))
            .replace("${__to:date:seconds}", str(end))
            .replace("$__range", "2m")
            .replace("$__rate_interval", "1m")
        )

    def check(expr, samples):
        return {"expr": expr, "eval_time": "2m", "exp_samples": samples}

    def scalar(expr, value):
        return check(expr, [{"labels": "{}", "value": value}])

    large_count = expression("Large plaintext HTTP bodies in selected range")
    large_size = expression("Last large plaintext HTTP body size (>=1 MiB)")
    large_time = expression("Last large plaintext HTTP body observed at (UTC)")
    body_pulse = expression(
        "Recent >=1 MiB plaintext body size pulse: peer -> target/service"
    )
    rps = expression("Peer HTTP >100 RPS (actual one-minute request rate)")
    postfilter = expression("Postfilter peer -> destination window")
    postfilter_bytes = expression("Postfilter bytes in selected range")
    other_source = "10.183.101.99"
    rps_other_source = expression(
        "Peer HTTP >100 RPS (actual one-minute request rate)", source=other_source
    )
    large_count_other_source = expression(
        "Large plaintext HTTP bodies in selected range", source=other_source
    )
    large_size_other_source = expression(
        "Last large plaintext HTTP body size (>=1 MiB)", source=other_source
    )
    large_time_other_source = expression(
        "Last large plaintext HTTP body observed at (UTC)", source=other_source
    )
    body_pulse_other_source = expression(
        "Recent >=1 MiB plaintext body size pulse: peer -> target/service",
        source=other_source,
    )
    postfilter_other_source = expression(
        "Postfilter peer -> destination window", source=other_source
    )
    postfilter_bytes_other_source = expression(
        "Postfilter bytes in selected range", source=other_source
    )
    large_size_after_event = expression(
        "Last large plaintext HTTP body size (>=1 MiB)", start=91
    )
    return {
        "rule_files": [],
        "evaluation_interval": "15s",
        "fuzzy_compare": True,
        "tests": [
            {
                "name": "DDoS views join profile context to sparse event metrics",
                "interval": "15s",
                "input_series": series,
                "promql_expr_test": [
                    check(
                        rps,
                        [
                            {
                                "labels": '{member="member-1",service_port="8080/tcp",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 120,
                            },
                            {
                                "labels": '{member="Unmapped",service_port="8080/tcp",source_ip="10.183.200.9",source_team_name="Unmapped",team_ip="10.183.102.2"}',
                                "value": 110,
                            },
                        ],
                    ),
                    scalar(large_count, 8),
                    check(
                        large_size,
                        [
                            {
                                "labels": '{member="member-1",service_port="8080/tcp",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 2097152,
                            }
                        ],
                    ),
                    check(
                        large_time,
                        [
                            {
                                "labels": '{member="member-1",service_port="8080/tcp",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 90000,
                            }
                        ],
                    ),
                    check(
                        postfilter,
                        [
                            {
                                "labels": '{measure="Packets/s",member="member-1",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 20,
                            },
                            {
                                "labels": '{measure="Bytes/s",member="member-1",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 4000,
                            },
                            {
                                "labels": '{measure="Packets/s",member="Unmapped",source_ip="10.183.200.9",source_team_name="Unmapped",team_ip="10.183.102.2"}',
                                "value": 10,
                            },
                            {
                                "labels": '{measure="Bytes/s",member="Unmapped",source_ip="10.183.200.9",source_team_name="Unmapped",team_ip="10.183.102.2"}',
                                "value": 600,
                            },
                        ],
                    ),
                    scalar(postfilter_bytes, 480000),  # Team filter excludes the unmapped 72,000 bytes.
                    check(
                        body_pulse,
                        [
                            {
                                "labels": '{member="member-1",service_port="8080/tcp",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 2097152,
                            }
                        ],
                    ),
                ],
            },
            {
                "name": "DDoS queries stay unavailable when sensor readiness is missing",
                "interval": "15s",
                "input_series": [
                    item
                    for item in series
                    if not item["series"].startswith("forcad:game_sensor_ready{")
                ],
                "promql_expr_test": [
                    check(rps, []),
                    check(large_count, []),
                    check(large_size, []),
                    check(large_time, []),
                    check(body_pulse, []),
                    check(postfilter, []),
                ],
            },
            {
                "name": "single large body remains visible without counter samples and respects selected time",
                "interval": "15s",
                "input_series": [
                    item
                    for item in series
                    if not item["series"].startswith(
                        "forcad_game_http_large_bodies_total{"
                    )
                ],
                "promql_expr_test": [
                    check(
                        large_size,
                        [
                            {
                                "labels": '{member="member-1",service_port="8080/tcp",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 2097152,
                            }
                        ],
                    ),
                    check(
                        large_time,
                        [
                            {
                                "labels": '{member="member-1",service_port="8080/tcp",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 90000,
                            }
                        ],
                    ),
                    check(
                        body_pulse,
                        [
                            {
                                "labels": '{member="member-1",service_port="8080/tcp",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01",team_ip="10.183.102.2"}',
                                "value": 2097152,
                            }
                        ],
                    ),
                    check(large_size_after_event, []),
                ],
            },
            {
                "name": "source filter limits mapped rows while preserving unclassified peers",
                "interval": "15s",
                "input_series": series,
                "promql_expr_test": [
                    check(
                        rps_other_source,
                        [
                            {
                                "labels": '{member="Unmapped",service_port="8080/tcp",source_ip="10.183.200.9",source_team_name="Unmapped",team_ip="10.183.102.2"}',
                                "value": 110,
                            }
                        ],
                    ),
                    check(
                        postfilter_other_source,
                        [
                            {
                                "labels": '{measure="Packets/s",member="Unmapped",source_ip="10.183.200.9",source_team_name="Unmapped",team_ip="10.183.102.2"}',
                                "value": 10,
                            },
                            {
                                "labels": '{measure="Bytes/s",member="Unmapped",source_ip="10.183.200.9",source_team_name="Unmapped",team_ip="10.183.102.2"}',
                                "value": 600,
                            },
                        ],
                    ),
                    check(large_count_other_source, []),
                    check(large_size_other_source, []),
                    check(large_time_other_source, []),
                    check(body_pulse_other_source, []),
                    check(postfilter_bytes_other_source, []),
                ],
            },
        ],
    }


def build_relay_cases(dashboard):
    queries = _panel_queries(dashboard)
    series = []

    def add(name, labels, values):
        labels = ",".join(f"{key}={json.dumps(value)}" for key, value in labels.items())
        series.append({"series": f"{name}{{{labels}}}", "values": values})

    sensor = {"job": "forcad_game_sensor", "instance": "sensor"}
    add("forcad:game_sensor_ready", sensor, "1x9")
    profiles = (
        ("10.183.200.2", "10.183.101.2", "Team 01", "alpha"),
        ("10.183.200.3", "10.183.102.2", "Team 02", "beta"),
        ("10.183.200.4", "10.183.103.2", "Team 03", "gamma"),
    )
    for source_ip, source_team_ip, source_team_name, member in profiles:
        add(
            "forcad:game_profile_info",
            {
                "source_ip": source_ip,
                "source_team_ip": source_team_ip,
                "source_team_name": source_team_name,
                "member": member,
            },
            "1x9",
        )

    target = {"team_ip": "10.183.110.2", "service_port": "8080/tcp"}
    add("forcad:game_target_info", {"team_ip": target["team_ip"], "target_team_name": "Team 10"}, "1x9")
    outcomes = {
        "10.183.200.2": {
            "tcp_established": "0+15x8",
            "tcp_no_reply": "0+3x8",
            "tcp_reset": "0+2x8",
            "http_403": "0+1.5x8",
            "http_429": "0+0.5x8",
            "http_other": "0+15x8",
        },
        "10.183.200.3": {
            "tcp_established": "0+7.5x8",
            "tcp_refused": "0+1.5x8",
            "tcp_reset": "0+1x8",
            "http_403": "0+0.5x8",
            "http_429": "0+0.25x8",
            "http_other": "0+7.5x8",
        },
        # This peer has no unique profile metadata. It must remain visible in
        # the unmapped table and count as an other observed peer.
        "10.183.200.9": {
            "tcp_no_reply": "0+3x8",
        },
    }
    for source_ip, series_by_outcome in outcomes.items():
        for outcome, values in series_by_outcome.items():
            add(
                "forcad_game_observations_total",
                sensor | target | {"source_ip": source_ip, "outcome": outcome},
                values,
            )

    # Other traffic on a different service or target must not be mixed into
    # this selected profile's comparison.
    add(
        "forcad_game_observations_total",
        sensor
        | target
        | {
            "service_port": "9090/tcp",
            "source_ip": "10.183.200.3",
            "outcome": "http_403",
        },
        "0+1000x8",
    )
    add(
        "forcad_game_observations_total",
        sensor
        | {
            "team_ip": "10.183.111.2",
            "service_port": "8080/tcp",
            "source_ip": "10.183.200.3",
            "outcome": "http_403",
        },
        "0+1000x8",
    )

    def expression(title):
        expr = queries[title][0]
        return (
            interpolate(
                expr,
                source="10.183.101.2",
                target="10.183.110.2",
                profile="10.183.200.2",
            )
            .replace("$__range", "2m")
            .replace("$__rate_interval", "1m")
        )

    def check(expr, samples):
        return {"expr": expr, "eval_time": "2m", "exp_samples": samples}

    def sample(measure, outcome, value, comparison=None):
        labels = {
            "member": "alpha",
            "measure": measure,
            "service_port": target["service_port"],
            "source_ip": "10.183.200.2",
            "source_team_ip": "10.183.101.2",
            "source_team_name": "Team 01",
            "team_ip": "10.183.110.2",
        }
        if outcome is not None:
            labels["outcome"] = outcome
        if comparison:
            labels.update(comparison=comparison, target_team_name="Team 10")
        return {
            "labels": "{"
            + ",".join(
                f'{key}={json.dumps(value)}' for key, value in sorted(labels.items())
            )
            + "}",
            "value": value,
        }

    def unmapped_sample(outcome, value):
        labels = {
            "member": "Unmapped",
            "measure": "Observed outcome count",
            "outcome": outcome,
            "service_port": target["service_port"],
            "source_ip": "10.183.200.9",
            "source_team_name": "Unmapped",
            "team_ip": "10.183.110.2",
        }
        return {
            "labels": "{"
            + ",".join(
                f'{key}={json.dumps(value)}' for key, value in sorted(labels.items())
            )
            + "}",
            "value": value,
        }

    http = expression("HTTP outcomes: selected profile and other peers")
    tcp = expression("TCP outcomes: selected profile and other peers")
    unmapped = expression("Unmapped observations (no profile attribution)")
    selected_profile = expression("Selected source profile")
    http_share = expression("Cross-profile HTTP 403/429 outcome share triage (>=10 responses)")
    tcp_share = expression("Cross-profile TCP no-reply/refused share triage (>=10 attempts)")
    denied = expression("Historical router-denied packets")
    enough_http = "Comparable: other responses >=10"
    enough_tcp = "Comparable: other attempts >=10"
    return {
        "rule_files": [],
        "evaluation_interval": "15s",
        "fuzzy_compare": True,
        "tests": [
            {
                "name": "Relay compares only same target and service with explicit denominators",
                "interval": "15s",
                "input_series": series,
                "promql_expr_test": [
                    check(http_share, [
                        sample("Observed peer share", "http_403", 12 / 136, enough_http),
                        sample("Observed peer share", "http_429", 4 / 136, enough_http),
                        sample("Other peers share", "http_403", 4 / 66, enough_http),
                        sample("Other peers share", "http_429", 2 / 66, enough_http),
                    ]),
                    check(tcp_share, [
                        sample("Observed peer share", "tcp_no_reply", 24 / 144, enough_tcp),
                        sample("Other peers share", "tcp_no_reply", 24 / 96, enough_tcp),
                    ]),
                    check(
                        http,
                        [
                            sample("Selected outcome count", "http_403", 12),
                            sample("Selected outcome count", "http_429", 4),
                            sample("Selected outcome count", "http_other", 120),
                            sample("Selected HTTP response denominator", None, 136),
                            sample("Other peers outcome count", "http_403", 4),
                            sample("Other peers outcome count", "http_429", 2),
                            sample("Other peers outcome count", "http_other", 60),
                            sample("Other peers HTTP response denominator", None, 66),
                        ],
                    ),
                    check(
                        tcp,
                        [
                            sample("Selected TCP outcome count", "tcp_established", 120),
                            sample("Selected TCP outcome count", "tcp_no_reply", 24),
                            sample("Selected TCP attempt denominator", None, 144),
                            sample("Other peers TCP outcome count", "tcp_established", 60),
                            sample("Other peers TCP outcome count", "tcp_refused", 12),
                            sample("Other peers TCP outcome count", "tcp_no_reply", 24),
                            sample("Other peers TCP attempt denominator", None, 96),
                            sample("Selected reset-after-handshake count", "tcp_reset", 16),
                            sample(
                                "Selected established denominator for resets",
                                None,
                                120,
                            ),
                            sample(
                                "Other peers reset-after-handshake count",
                                "tcp_reset",
                                8,
                            ),
                            sample(
                                "Other peers established denominator for resets",
                                None,
                                60,
                            ),
                        ],
                    ),
                    check(unmapped, [unmapped_sample("tcp_no_reply", 24)]),
                    check(
                        selected_profile,
                        [
                            {
                                "labels": '{member="alpha",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01"}',
                                "value": 1,
                            }
                        ],
                    ),
                ],
            },
            {
                "name": "Selective failures without other peers are not corroborated",
                "interval": "15s",
                "input_series": [s for s in series if "forcad_game_observations_total" not in s["series"]
                                 or 'source_ip="10.183.200.2"' in s["series"]],
                "promql_expr_test": [check(http_share, [
                    sample("Observed peer share", "http_403", 12 / 136, "No comparison: other responses <10"),
                    sample("Observed peer share", "http_429", 4 / 136, "No comparison: other responses <10"),
                ])],
            },
            {
                "name": "VLAN drops survive absent Zeek and are not attributed to a member",
                "interval": "15s",
                "input_series": [
                    {"series": 'up{job="forcad_router",instance="router"}', "values": "1x9"},
                    {"series": 'forcad_network_relay_scrape_success{job="forcad_router",instance="router"}', "values": "1x9"},
                    {"series": 'forcad_network_relay_attempt_packets_total{job="forcad_router",instance="router",source_team_ip="10.183.101.2",team_ip="10.183.110.2"}', "values": "0+3x8"},
                ],
                "promql_expr_test": [check(denied, [{"labels": "{}", "value": 24}])],
            },
            {
                "name": "Profile metadata exists before any sparse outcome event",
                "interval": "15s",
                "input_series": [
                    item
                    for item in series
                    if item["series"].startswith(
                        ("forcad:game_sensor_ready{", "forcad:game_profile_info{")
                    )
                ],
                "promql_expr_test": [
                    check(
                        selected_profile,
                        [
                            {
                                "labels": '{member="alpha",source_ip="10.183.200.2",source_team_ip="10.183.101.2",source_team_name="Team 01"}',
                                "value": 1,
                            }
                        ],
                    ),
                    check(http, []),
                    check(tcp, []),
                    check(unmapped, []),
                ],
            },
            {
                "name": "Relay outcome views are unavailable when sensor readiness is missing",
                "interval": "15s",
                "input_series": [
                    item
                    for item in series
                    if not item["series"].startswith("forcad:game_sensor_ready{")
                ],
                "promql_expr_test": [
                    check(http, []),
                    check(tcp, []),
                    check(unmapped, []),
                    check(selected_profile, []),
                ],
            },
        ],
    }
