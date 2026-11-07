#!/usr/bin/env python3
"""Independently check the saved CHRP load report and sampler artifacts."""

import argparse
from datetime import datetime, timezone
import json
import math
from pathlib import Path
import re


def rows(path):
    if not path.exists():
        return []
    return [json.loads(line) for line in path.read_text(encoding='utf-8').splitlines() if line.strip()]


def size_bytes(text):
    match = re.match(r'([\d.]+)([kMGT]?i?B|B)$', text.split(' / ', 1)[0])
    if not match:
        return 0
    unit = match.group(2)
    powers = {'B': 0, 'kB': 1, 'KiB': 1, 'MB': 2, 'MiB': 2,
              'GB': 3, 'GiB': 3, 'TB': 4, 'TiB': 4}
    return float(match.group(1)) * (1024 if 'i' in unit else 1000) ** powers[unit]


def checker_result_scope(report, complete, trailing_rounds, issues, limitations):
    """Compare the absolute round snapshot with absolute history, not its delta."""
    scope = {'mode': 'legacy_delta_unverified',
             'delta': report.get('checker_results'), 'trailing_results': None}
    keys = ('checker_results_before', 'checker_results_total')
    if not any(key in report for key in keys):
        limitations.append('legacy checker_results is a post-catalog delta with an unknown baseline; '
                           'its total cannot be cross-checked against the round snapshot')
        return scope
    scope['mode'] = 'explicit_baseline'
    before, total = (report.get(key) for key in keys)
    delta = scope['delta']
    scope.update(before=before, total=total)
    if any(type(value) is not int or value < 0 for value in (before, total, delta)):
        issues.append('checker result baseline, total and delta must all be nonnegative integers')
        return scope
    if total < before or delta != total - before:
        issues.append('checker result delta does not equal total minus baseline')
    trailing = total - 84 * len(complete)
    scope['trailing_results'] = trailing
    # The existing queries deliberately remain unchanged: the round snapshot is
    # collected before the final count, without a shared database transaction.
    scope['snapshot_order'] = 'round snapshot precedes final history count; not atomic'
    if trailing != 0 and (len(trailing_rounds) != 1 or
                          not 0 <= trailing <= trailing_rounds[0].get('jobs', 0)):
        issues.append('checker result total cannot be attributed to the trailing partial round')
    if trailing < 0:
        issues.append('checker result total is smaller than the complete round result count')
    return scope


def attack_clock_sample(result):
    keys = ('started_utc', 'drained_utc', 'monotonic_elapsed_to_drain_seconds')
    if not any(key in result for key in keys):
        return {'status': 'missing', 'reason': 'legacy report has no attack clock measurements'}
    try:
        started, drained = (datetime.fromisoformat(result[key].replace('Z', '+00:00')) for key in keys[:2])
        elapsed = result[keys[2]]
        if started.tzinfo is None or drained.tzinfo is None:
            raise ValueError('timestamps must include a timezone')
        if started.year == 1 or drained.year == 1:
            raise ValueError('timestamps must not be zero Go timestamps')
        if type(elapsed) not in (int, float) or not math.isfinite(elapsed) or elapsed <= 0:
            raise ValueError('monotonic elapsed must be finite and positive')
        wall = (drained - started).total_seconds()
        if wall <= 0:
            raise ValueError('UTC elapsed must be positive')
        discrepancy = abs(wall - elapsed) / elapsed
        if not math.isfinite(discrepancy):
            raise ValueError('clock discrepancy must be finite')
    except (KeyError, AttributeError, TypeError, ValueError, OverflowError) as error:
        return {'status': 'invalid', 'reason': str(error)}
    return {'status': 'skewed' if discrepancy > 0.001 else 'stable',
            'utc_elapsed_seconds': wall, 'monotonic_elapsed_seconds': elapsed,
            'absolute_relative_discrepancy': discrepancy}


def normalized_stages(report, issues):
    """Keep failed/bootstrap reports auditable; malformed stages fail closed."""
    stages = report.get('stages')
    if stages is None or stages == []:
        issues.append('report has no stages')
        return []
    if not isinstance(stages, list):
        issues.append('report stages must be an array')
        return []
    valid = []
    for index, stage in enumerate(stages):
        try:
            if not isinstance(stage, dict):
                raise ValueError('stage must be an object')
            if not isinstance(stage.get('name'), str) or not stage['name'].strip():
                raise ValueError('stage name must be a nonempty string')
            for field in ('seconds', 'accepted', 'committed'):
                if field in stage:
                    value = stage[field]
                    if type(value) not in (int, float) or not math.isfinite(value) or value < 0:
                        raise ValueError(f'{field} must be a finite nonnegative number')
            if 'integrity_passed' in stage and type(stage['integrity_passed']) is not bool:
                raise ValueError('integrity_passed must be a boolean')
            stage = dict(stage)
            for traffic in ('reads', 'flags'):
                result = stage.get(traffic)
                if result is None:
                    stage[traffic] = {}
                    continue
                if not isinstance(result, dict):
                    raise ValueError(f'{traffic} result must be an object')
                statuses = result.get('statuses', {})
                if not isinstance(statuses, dict) or any(
                        not isinstance(status, str) or type(count) is not int or count < 0
                        for status, count in statuses.items()):
                    raise ValueError(f'{traffic} statuses must map strings to nonnegative integers')
                for field in ('requests', 'p99_ms', 'transport_errors_total', 'failures'):
                    if field in result:
                        value = result[field]
                        if type(value) not in (int, float) or not math.isfinite(value) or value < 0:
                            raise ValueError(f'{traffic} {field} must be a finite nonnegative number')
                for field in ('generator_valid', 'slo_passed'):
                    if field in result and type(result[field]) is not bool:
                        raise ValueError(f'{traffic} {field} must be a boolean')
            valid.append(stage)
        except (ValueError, OverflowError) as error:
            issues.append(f'stage[{index}] is malformed: {error}')
    return valid


def attack_clock_diagnostics(stages, required, issues, limitations):
    samples = []
    for stage in stages:
        for traffic in ('reads', 'flags'):
            sample = {'stage': stage['name'], 'traffic': traffic,
                      **attack_clock_sample(stage.get(traffic) or {})}
            samples.append(sample)
    unstable = [sample for sample in samples if sample['status'] != 'stable']
    if unstable or not samples:
        message = ('attack clocks do not validate physical throughput: ' +
                   ', '.join(f"{sample['stage']}/{sample['traffic']}={sample['status']}" for sample in unstable))
        if not samples:
            message += 'no attacks recorded'
        (issues if required else limitations).append(message)
    return {'maximum_allowed_relative_discrepancy': 0.001,
            'internally_consistent': bool(samples) and not unstable,
            'maximum_absolute_relative_discrepancy': max(
                (sample['absolute_relative_discrepancy'] for sample in samples
                 if 'absolute_relative_discrepancy' in sample), default=None),
            'attacks': samples}


def validate_paired_clock_samples(samples):
    fields = ('windows_qpc_before_ns', 'windows_qpc_after_ns',
              'windows_utc_before_ns', 'windows_utc_after_ns')
    clocks = ('monotonic_ns', 'raw_ns', 'realtime_ns')
    if len(samples) < 2:
        raise ValueError('at least two paired clock samples are required')
    previous = None
    for sample in samples:
        linux = sample.get('linux_clocks')
        if not isinstance(linux, dict):
            raise ValueError('linux_clocks must be an object')
        values = [sample.get(field) for field in fields] + [linux.get(clock) for clock in clocks]
        if any(type(value) is not int or value <= 0 for value in values):
            raise ValueError('paired clock nanoseconds must be positive integers')
        if sample[fields[0]] > sample[fields[1]] or sample[fields[2]] > sample[fields[3]]:
            raise ValueError('paired clock before/after ordering is invalid')
        if previous is not None:
            if any(linux[clock] <= previous['linux_clocks'][clock] for clock in clocks):
                raise ValueError('Linux clock samples must increase strictly')
            if (sample[fields[0]] <= previous[fields[1]] or
                    sample[fields[2]] <= previous[fields[3]]):
                raise ValueError('Windows paired clock intervals must increase without overlap')
        previous = sample
    first, last = samples[0], samples[-1]
    qpc_lower = last[fields[0]] - first[fields[1]]
    qpc_upper = last[fields[1]] - first[fields[0]]
    if qpc_lower <= 0:
        raise ValueError('paired QPC elapsed lower bound must be positive')
    ratios = {}
    for clock in clocks:
        elapsed = last['linux_clocks'][clock] - first['linux_clocks'][clock]
        ratios[clock] = {'lower': elapsed / qpc_upper, 'upper': elapsed / qpc_lower}
    ticks = [sample['timex']['tick'] for sample in samples
             if isinstance(sample.get('timex'), dict) and type(sample['timex'].get('tick')) is int]
    return {'samples': len(samples), 'qpc_elapsed_bounds_seconds': [qpc_lower / 1e9, qpc_upper / 1e9],
            'linux_to_qpc_ratio_bounds': ratios,
            'roundtrip_max_seconds': max(sample[fields[1]] - sample[fields[0]] for sample in samples) / 1e9,
            'tick_min': min(ticks, default=None), 'tick_max': max(ticks, default=None)}


def clock_calibration_diagnostics(run, stages, required, issues, limitations, paired_samples=None):
    path = run / 'clock-calibration.jsonl'
    result = {'status': 'missing', 'artifact': str(path), 'coverage_tolerance_seconds': 1.0,
              'maximum_allowed_relative_discrepancy': 0.001}
    try:
        samples = rows(path)
        if samples:
            result.update(validate_paired_clock_samples(samples))
            if paired_samples is not None:
                paired_samples.extend(samples)
            starts, drains = [], []
            for stage in stages:
                for traffic in ('reads', 'flags'):
                    attack = stage.get(traffic) or {}
                    if attack_clock_sample(attack)['status'] in ('missing', 'invalid'):
                        raise ValueError('attack timing is missing or invalid; calibration coverage is unknown')
                    starts.append(datetime.fromisoformat(attack['started_utc'].replace('Z', '+00:00')).timestamp())
                    drains.append(datetime.fromisoformat(attack['drained_utc'].replace('Z', '+00:00')).timestamp())
            if not starts:
                raise ValueError('no attack intervals are available for calibration coverage')
            start = samples[0]['linux_clocks']['realtime_ns'] / 1e9
            end = samples[-1]['linux_clocks']['realtime_ns'] / 1e9
            result.update(coverage_started_utc=datetime.fromtimestamp(start, timezone.utc).isoformat(),
                          coverage_finished_utc=datetime.fromtimestamp(end, timezone.utc).isoformat(),
                          coverage_sufficient=start <= min(starts) + 1 and end >= max(drains) - 1)
            bounds = result['linux_to_qpc_ratio_bounds']['monotonic_ns']
            result['monotonic_qualified'] = bounds['lower'] >= 0.999 and bounds['upper'] <= 1.001
            result['status'] = ('qualified' if result['monotonic_qualified'] and result['coverage_sufficient']
                                else 'unqualified')
    except (OSError, ValueError, TypeError, AttributeError, OverflowError) as error:
        result.update(status='invalid', reason=str(error))
    if result['status'] != 'qualified':
        message = ('external clock calibration is ' + result['status'] +
                   '; internal UTC/monotonic agreement alone cannot certify physical throughput')
        if result.get('reason'):
            message += ': ' + result['reason']
        (issues if required else limitations).append(message)
    return result


def stage_qpc_bracket(reads, samples):
    """Enclose the whole stage in paired QPC reads without rate interpolation."""
    clock = reads.get('linux_clock')
    if not isinstance(clock, dict):
        raise ValueError('absolute Linux clock anchors are missing (legacy report is unverified)')
    if clock.get('error'):
        raise ValueError(f'absolute Linux clock read failed: {clock["error"]}')
    start, drain = (clock.get(field) for field in ('monotonic_start_before_ns', 'monotonic_drain_after_ns'))
    if any(type(value) is not int or value <= 0 for value in (start, drain)) or start >= drain:
        raise ValueError('absolute monotonic anchors must be positive increasing integers')
    boot_id = clock.get('boot_id')
    if (not isinstance(boot_id, str) or
            not re.fullmatch(r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}', boot_id) or
            clock.get('drain_boot_id') != boot_id):
        raise ValueError('attack Linux boot IDs are missing, malformed or differ')
    if any(sample.get('linux_boot_id') != boot_id for sample in samples):
        raise ValueError('paired clock samples do not share the attack Linux boot ID')
    namespace = clock.get('time_namespace')
    if (not isinstance(namespace, str) or not re.fullmatch(r'time:\[[0-9]+\]', namespace) or
            clock.get('drain_time_namespace') != namespace or
            any(sample.get('linux_time_namespace') != namespace for sample in samples)):
        raise ValueError('attack and paired samples have missing, malformed or different Linux time namespaces')
    before = [(index, sample) for index, sample in enumerate(samples)
              if sample['linux_clocks']['monotonic_ns'] <= start]
    after = [(index, sample) for index, sample in enumerate(samples)
             if sample['linux_clocks']['monotonic_ns'] >= drain]
    if not before or not after:
        raise ValueError('paired QPC samples do not bracket the absolute monotonic anchors')
    before_index, before_sample = before[-1]
    after_index, after_sample = after[0]
    upper_ns = after_sample['windows_qpc_after_ns'] - before_sample['windows_qpc_before_ns']
    if upper_ns <= 0:
        raise ValueError('stage QPC elapsed upper bound must be positive')
    return {'qpc_elapsed_upper_bound_ns': upper_ns,
            'qpc_elapsed_upper_bound_seconds': upper_ns / 1e9,
            'start_sample_position': before_index, 'drain_sample_position': after_index,
            'linux_boot_id': boot_id, 'linux_time_namespace': namespace}


def steady_rest_capacity(stages, calibration, minimum, issues, paired_samples=None):
    """Bound successful responses/s using external samples enclosing each stage."""
    result = {'minimum_successful_rps': minimum, 'stages': []}
    if minimum == 0:
        return result
    if type(minimum) not in (int, float) or not math.isfinite(minimum) or minimum < 0:
        raise ValueError('minimum successful REST RPS must be finite and nonnegative')
    if calibration.get('status') != 'qualified':
        issues.append('successful REST capacity requires qualified external clock calibration')
        return result
    try:
        if not isinstance(paired_samples, list):
            raise ValueError('paired QPC samples are missing')
        validate_paired_clock_samples(paired_samples)
    except (KeyError, AttributeError, TypeError, ValueError, OverflowError) as error:
        issues.append(f'successful REST capacity has invalid paired clock samples: {error}')
        return result
    for stage in stages:
        if not stage['name'].startswith('steady-'):
            continue
        reads = stage.get('reads') or {}
        if not isinstance(reads, dict):
            issues.append(f'{stage["name"]}: successful REST capacity has malformed reads')
            continue
        statuses = reads.get('statuses')
        successful = statuses.get('200') if isinstance(statuses, dict) else None
        elapsed = reads.get('monotonic_elapsed_to_drain_seconds')
        if (type(successful) is not int or successful < 0 or
                type(elapsed) not in (int, float) or not math.isfinite(elapsed) or elapsed <= 0):
            issues.append(f'{stage["name"]}: successful REST capacity has invalid count or drain duration')
            continue
        try:
            bracket = stage_qpc_bracket(reads, paired_samples)
            lower = successful * 1_000_000_000 / bracket['qpc_elapsed_upper_bound_ns']
            if not math.isfinite(lower):
                raise ValueError('successful REST RPS must be finite')
        except (KeyError, AttributeError, TypeError, ValueError, OverflowError) as error:
            issues.append(f'{stage["name"]}: successful REST capacity is unverified: {error}')
            continue
        result['stages'].append({'name': stage['name'], 'successful_requests': successful,
                                 'monotonic_seconds_through_drain': elapsed,
                                 'method': 'bracketed_qpc_no_interpolation', **bracket,
                                 'successful_rps_qpc_lower_bound': lower})
        if lower < minimum:
            issues.append(f'{stage["name"]}: successful REST RPS lower bound {lower:.3f} < {minimum:g}')
    return result


def audit(run, min_rounds, require_owned_state=False, require_pgbouncer=False,
          require_windows_host=False, require_stable_clocks=False, require_clock_calibration=False,
          min_successful_rest_rps=0):
    candidates = list((run / 'combined').glob('*/report.json'))
    if len(candidates) != 1:
        raise ValueError(f'expected one report.json, found {len(candidates)}')
    report_path = candidates[0]
    report = json.loads(report_path.read_text(encoding='utf-8'))
    issues = []
    limitations = []
    for name in ('resources.jsonl', 'pool.log', 'database.jsonl'):
        if not (report_path.parent / name).is_file() or (report_path.parent / name).stat().st_size == 0:
            issues.append(f'{name} diagnostic artifact is missing')
    exit_path = run / 'exit-code.txt'
    exit_code = int(exit_path.read_text().strip()) if exit_path.exists() else None
    if exit_code != 0:
        issues.append(f'native exit code is {exit_code}')
    completion_path = run / 'completion.json'
    if completion_path.exists():
        completion = json.loads(completion_path.read_text(encoding='utf-8'))
        if completion.get('owned_containers_after'):
            issues.append('isolated Compose project left owned containers behind')
    elif min_rounds >= 300:
        issues.append('runner completion record is missing')
    if not (run / 'console.log').is_file() or (run / 'console.log').stat().st_size == 0:
        issues.append('full console log is missing')
    try:
        finished = datetime.fromisoformat(report.get('finished', '').replace('Z', '+00:00'))
        if finished.year == 1 or finished.tzinfo is None:
            raise ValueError('zero or timezone-free completion timestamp')
    except (AttributeError, TypeError, ValueError):
        issues.append('report has no valid completion timestamp')
    if not report.get('correctness_passed'):
        issues.append('report correctness_passed is false')
    if report.get('error'):
        issues.append(f'report error: {report["error"]}')

    stages = normalized_stages(report, issues)
    clock_diagnostics = attack_clock_diagnostics(stages, require_stable_clocks or require_clock_calibration,
                                               issues, limitations)
    paired_samples = []
    calibration = clock_calibration_diagnostics(run, stages, require_clock_calibration, issues, limitations,
                                               paired_samples)
    capacity = steady_rest_capacity(stages, calibration, min_successful_rest_rps, issues, paired_samples)
    steady = [stage for stage in stages if stage.get('name', '').startswith('steady-')]
    if not steady:
        issues.append('steady stage is missing')
    if min_rounds >= 300 and sum(stage.get('seconds', 0) for stage in steady) < 18000:
        issues.append('steady stage is shorter than five hours')
    for stage in stages:
        name = stage['name']
        if not stage.get('integrity_passed') or stage.get('accepted') != stage.get('committed'):
            issues.append(f'{name}: accepted/committed integrity failed')
        for traffic in ('reads', 'flags'):
            result = stage.get(traffic)
            if not result:
                issues.append(f'{name}: {traffic} result is missing')
                continue
            statuses = result.get('statuses', {})
            if result.get('transport_errors_total', 0) or result.get('failures', 0):
                issues.append(f'{name}: {traffic} has transport or validation failures')
            if not result.get('generator_valid'):
                issues.append(f'{name}: {traffic} generator failed')
            if name == 'flag-burst':
                allowed = {'200', '429'} if traffic == 'flags' else {'200'}
                if set(statuses) - allowed:
                    issues.append(f'{name}: {traffic} unexpected HTTP status {statuses}')
                if traffic == 'flags' and int(statuses.get('429', 0)) == 0:
                    issues.append('flag-burst did not reach the limiter')
                if traffic == 'reads' and not result.get('slo_passed'):
                    issues.append(f'{name}: reads SLO failed')
            else:
                if not result.get('slo_passed'):
                    issues.append(f'{name}: {traffic} SLO failed')
                if set(statuses) != {'200'}:
                    issues.append(f'{name}: {traffic} HTTP status {statuses}')

    checker = report.get('checker') or {}
    for name in ('bad_jobs', 'bad_actions', 'action_timeouts', 'expired_jobs', 'overdue_jobs', 'past_round_open_jobs'):
        if checker.get(name, 0):
            issues.append(f'checker {name}={checker[name]}')
    if checker.get('expected_cells') != 84 or checker.get('expected_puts') != 1120:
        issues.append('checker geometry differs from 84 cells and 1120 PUTs')
    rounds = checker.get('rounds', [])
    complete = [item for item in rounds if item.get('complete')]
    if len(complete) < min_rounds:
        issues.append(f'only {len(complete)} complete rounds; need {min_rounds}')
    numbers = [item['round'] for item in complete]
    if numbers != list(range(1, len(complete) + 1)):
        issues.append('complete round numbers are not contiguous from 1')
    if any(not item.get('complete') for item in rounds[:len(complete)]):
        issues.append('an incomplete round precedes a complete one')
    trailing_rounds = rounds[len(complete):]
    result_scope = checker_result_scope(report, complete, trailing_rounds, issues, limitations)
    if len(trailing_rounds) > 1:
        issues.append('more than one trailing checker round is incomplete')
    for item in complete:
        if (item.get('jobs'), item.get('cells'), item.get('finished_jobs'), item.get('up_jobs')) != (84, 84, 84, 84):
            issues.append(f'round {item["round"]}: incomplete checker results')
        if (item.get('put_actions'), item.get('stored_flags'), item.get('mismatched_cells')) != (1120, 1120, 0):
            issues.append(f'round {item["round"]}: PUT or flag mismatch')
        if item.get('completion_seconds', float('inf')) >= 60:
            issues.append(f'round {item["round"]}: completion exceeds 60 seconds')

    sockets = report.get('websockets') or {}
    if sockets.get('failures', 0):
        issues.append(f'websocket failures={sockets["failures"]}')
    container_samples = rows(run / 'owned-container-state.jsonl')
    if require_owned_state and not container_samples:
        issues.append('owned-container restart/OOM samples are missing')
    if container_samples:
        if any(sample.get('error') for sample in container_samples):
            issues.append('container sampler reported an error')
        if any(container.get('restart_count', 0) or container.get('oom_killed')
               for sample in container_samples for container in sample.get('containers', [])):
            issues.append('owned container restarted or was OOM-killed')
    pgbouncer_samples = rows(run / 'pgbouncer-pools.jsonl')
    if require_pgbouncer and not pgbouncer_samples:
        issues.append('PgBouncer pool samples are missing')
    if any(sample.get('error') and not sample.get('container_removed') and not sample.get('container_stopped')
           for sample in pgbouncer_samples):
        issues.append('PgBouncer pool sampler reported an error')
    pool_wait_peak = {}
    pool_servers_peak = {}
    pool_maxwait_peak = {}
    for sample in pgbouncer_samples:
        for pool in sample.get('pools', []):
            name = pool['database']
            pool_wait_peak[name] = max(pool_wait_peak.get(name, 0), pool['cl_waiting'])
            pool_servers_peak[name] = max(pool_servers_peak.get(name, 0), pool['sv_active'])
            wait = pool['maxwait'] + pool['maxwait_us'] / 1_000_000
            pool_maxwait_peak[name] = max(pool_maxwait_peak.get(name, 0), wait)
    host_samples = rows(run / 'linux-host-resources.jsonl')
    valid_host = [sample for sample in host_samples if 'memory' in sample]
    if not valid_host or any(sample.get('sampler_error') for sample in host_samples):
        issues.append('host resource sampler missing or errored')
    windows_samples = rows(run / 'windows-host-resources.jsonl')
    if require_windows_host and not windows_samples:
        issues.append('Windows host resource samples are missing')
    if any(sample.get('error') for sample in windows_samples):
        issues.append('Windows host resource sampler reported an error')
    valid_windows = [sample for sample in windows_samples if 'available_physical' in sample]
    role_cpu_peak = {}
    role_ram_peak = {}
    resource_samples = rows(report_path.parent / 'resources.jsonl')
    if not resource_samples:
        issues.append('container resource samples are missing')
    for sample in resource_samples:
        cpu = {}
        ram = {}
        for container in sample.get('containers', []):
            match = re.search(r'-(api|receiver|worker|postgres|pgbouncer|nats|rabbitmq|nginx)-\d+$', container.get('Name', ''))
            if not match:
                continue
            role = match.group(1)
            cpu[role] = cpu.get(role, 0) + float(container['CPUPerc'].rstrip('%'))
            ram[role] = ram.get(role, 0) + size_bytes(container['MemUsage'])
        for role, value in cpu.items():
            role_cpu_peak[role] = max(role_cpu_peak.get(role, 0), value)
        for role, value in ram.items():
            role_ram_peak[role] = max(role_ram_peak.get(role, 0), value)

    metric = {
        'complete_rounds': len(complete),
        'trailing_checker_results': result_scope['trailing_results'],
        'worst_round_seconds': max((item['completion_seconds'] for item in complete), default=None),
        'worst_publish_to_action_wait_seconds': max((item.get('max_publish_to_action_wait_seconds', 0) for item in complete), default=None),
        'steady_rest_requests': sum(stage.get('reads', {}).get('requests', 0) for stage in steady),
        'steady_rest_p99_ms_max': max((stage.get('reads', {}).get('p99_ms', 0) for stage in steady), default=None),
        'steady_flag_requests': sum(stage.get('flags', {}).get('requests', 0) for stage in steady),
        'steady_flag_p99_ms_max': max((stage.get('flags', {}).get('p99_ms', 0) for stage in steady), default=None),
        'host_cpu_peak_percent': max((sample.get('cpu_busy_percent') or 0 for sample in valid_host), default=None),
        'host_ram_available_min_gib': min((sample['memory']['MemAvailable'] / 2**30 for sample in valid_host), default=None),
        'host_swap_used_peak_gib': max(((sample['memory']['SwapTotal'] - sample['memory']['SwapFree']) / 2**30 for sample in valid_host), default=None),
        'windows_host_sample_start': windows_samples[0]['time'] if windows_samples else None,
        'windows_host_cpu_peak_percent': max((sample.get('cpu_busy_percent') or 0 for sample in valid_windows), default=None),
        'windows_host_ram_available_min_gib': min((sample['available_physical'] / 2**30 for sample in valid_windows), default=None),
        'windows_host_pagefile_available_min_gib': min((sample['available_pagefile'] / 2**30 for sample in valid_windows), default=None),
        'role_cpu_peak_cores': {role: round(value / 100, 2) for role, value in role_cpu_peak.items()},
        'role_ram_peak_gib': {role: round(value / 2**30, 3) for role, value in role_ram_peak.items()},
        'container_restart_peak': max((container.get('restart_count', 0) for sample in container_samples for container in sample.get('containers', [])), default=None),
        'container_oom_count': len({container.get('id') for sample in container_samples
                                    for container in sample.get('containers', []) if container.get('oom_killed')}),
        'pgbouncer_sample_start': pgbouncer_samples[0]['time'] if pgbouncer_samples else None,
        'pgbouncer_sample_end': pgbouncer_samples[-1]['time'] if pgbouncer_samples else None,
        'pgbouncer_client_wait_peak': pool_wait_peak,
        'pgbouncer_server_active_peak': pool_servers_peak,
        'pgbouncer_maxwait_peak_seconds': {name: round(value, 3) for name, value in pool_maxwait_peak.items()},
    }
    return {'passed': not issues, 'issues': issues, 'limitations': limitations,
            'clock_diagnostics': clock_diagnostics,
            'clock_calibration': calibration,
            'successful_rest_capacity': capacity,
            'checker_result_scope': result_scope, 'native_exit_code': exit_code,
            'report': str(report_path), 'resources': str(report_path.parent / 'resources.jsonl'),
            'host_resources': str(run / 'linux-host-resources.jsonl'), 'console_log': str(run / 'console.log'),
            'metrics': metric}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('run', type=Path)
    parser.add_argument('--min-rounds', type=int, required=True)
    parser.add_argument('--require-owned-state', action='store_true')
    parser.add_argument('--require-pgbouncer', action='store_true')
    parser.add_argument('--require-windows-host', action='store_true')
    parser.add_argument('--require-stable-clocks', action='store_true')
    parser.add_argument('--require-clock-calibration', action='store_true')
    parser.add_argument('--min-successful-rest-rps', type=float, default=0,
                        help='require each steady stage to reach this successful RPS through drain, '
                             'using the external QPC calibration lower bound')
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if not math.isfinite(args.min_successful_rest_rps) or args.min_successful_rest_rps < 0:
        parser.error('--min-successful-rest-rps must be finite and nonnegative')
    result = audit(args.run, args.min_rounds, args.require_owned_state, args.require_pgbouncer,
                   args.require_windows_host, args.require_stable_clocks, args.require_clock_calibration,
                   args.min_successful_rest_rps)
    data = json.dumps(result, indent=2)
    if args.output:
        args.output.write_text(data + '\n', encoding='utf-8')
    print(data)
    return 0 if result['passed'] else 2


if __name__ == '__main__':
    raise SystemExit(main())
