"""Exercise history-count scope through the saved-artifact auditor."""

import copy
from contextlib import redirect_stdout
from datetime import datetime
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from audit_report import audit, main, steady_rest_capacity, validate_paired_clock_samples


BOOT_ID = 'c1388562-761a-4378-a4bd-372b1df4ce54'
TIME_NAMESPACE = 'time:[4026531834]'


def paired_sample(monotonic_seconds, qpc_seconds=None):
    mono = 10**12 + int(monotonic_seconds * 1e9)
    qpc = 10**12 + int((monotonic_seconds if qpc_seconds is None else qpc_seconds) * 1e9)
    return {'linux_boot_id': BOOT_ID, 'linux_time_namespace': TIME_NAMESPACE,
            'linux_clocks': {'monotonic_ns': mono, 'raw_ns': mono, 'realtime_ns': mono},
            'windows_qpc_before_ns': qpc - 1_000_000, 'windows_qpc_after_ns': qpc + 1_000_000,
            'windows_utc_before_ns': qpc - 1_000_000, 'windows_utc_after_ns': qpc + 1_000_000}


def linux_anchors(start_seconds, drain_seconds):
    return {'monotonic_start_before_ns': 10**12 + int(start_seconds * 1e9),
            'monotonic_drain_after_ns': 10**12 + int(drain_seconds * 1e9),
            'boot_id': BOOT_ID, 'drain_boot_id': BOOT_ID,
            'time_namespace': TIME_NAMESPACE, 'drain_time_namespace': TIME_NAMESPACE}


class SuccessfulRESTCapacityTests(unittest.TestCase):
    def setUp(self):
        self.calibration = {'status': 'qualified'}
        self.samples = [paired_sample(seconds) for seconds in (0, 2, 302, 304)]
        self.stages = [{'name': 'steady-01', 'reads': {
            'statuses': {'200': 3_006_000}, 'monotonic_elapsed_to_drain_seconds': 300,
            'linux_clock': linux_anchors(2, 302),
            # The capacity guard must not trust the nominal or pre-drain rate.
            'offered_rps': 10_600, 'achieved_rps': 10_600}}]

    def check_capacity(self, count=3_006_000, elapsed=300, calibration_status='qualified'):
        self.calibration['status'] = calibration_status
        self.stages[0]['reads'].update(statuses={'200': count}, monotonic_elapsed_to_drain_seconds=elapsed)
        issues = []
        result = steady_rest_capacity(self.stages, self.calibration, 10_000, issues, self.samples)
        return result, issues

    def test_calibrated_successes_above_target_pass(self):
        result, issues = self.check_capacity()
        self.assertFalse(issues)
        sample = result['stages'][0]
        self.assertAlmostEqual(sample['successful_rps_qpc_lower_bound'], 3_006_000 / 300.002)
        self.assertEqual(sample['qpc_elapsed_upper_bound_ns'], 300_002_000_000)
        self.assertEqual(sample['method'], 'bracketed_qpc_no_interpolation')

    def test_native_delivery_pass_does_not_prove_ten_thousand(self):
        _, issues = self.check_capacity(count=2_860_740)
        self.assertEqual(len(issues), 1)
        self.assertIn('successful REST RPS lower bound', issues[0])

    def test_clock_uncertainty_and_response_drain_count(self):
        # Exactly 10k starts/s is insufficient with bracketing uncertainty.
        self.assertTrue(self.check_capacity(count=3_000_000)[1])
        self.samples[2:] = [paired_sample(303), paired_sample(305)]
        self.stages[0]['reads']['linux_clock'] = linux_anchors(2, 303)
        self.assertTrue(self.check_capacity(elapsed=301)[1])

    def test_frequency_changes_cannot_turn_global_average_into_stage_capacity(self):
        self.samples = [paired_sample(0, 0), paired_sample(495, 500), paired_sample(1000, 1000)]
        self.calibration.update(validate_paired_clock_samples(self.samples))
        ratio = self.calibration['linux_to_qpc_ratio_bounds']['monotonic_ns']['lower']
        self.assertGreater(ratio, 0.999)
        self.stages[0]['reads']['linux_clock'] = linux_anchors(0, 495)
        result, issues = self.check_capacity(count=4_975_000, elapsed=495)
        self.assertGreater(4_975_000 / 495 * ratio, 10_000)  # Old global-ratio formula.
        self.assertLess(result['stages'][0]['successful_rps_qpc_lower_bound'], 9950)
        self.assertTrue(issues)

    def test_nearest_outer_samples_are_used_without_interpolation(self):
        self.stages[0]['reads']['linux_clock'] = linux_anchors(2.1, 301.9)
        result, issues = self.check_capacity()
        self.assertFalse(issues)
        self.assertEqual(result['stages'][0]['start_sample_position'], 1)
        self.assertEqual(result['stages'][0]['drain_sample_position'], 2)
        self.assertEqual(result['stages'][0]['qpc_elapsed_upper_bound_ns'], 300_002_000_000)

    def test_legacy_missing_and_failed_absolute_anchors_fail_closed(self):
        for clock in (None, [], {}, {'error': 'CLOCK_MONOTONIC unavailable'}):
            with self.subTest(clock=clock):
                self.stages[0]['reads']['linux_clock'] = clock
                result, issues = self.check_capacity()
                self.assertFalse(result['stages'])
                self.assertTrue(issues)

    def test_malformed_nonmonotonic_and_unbracketed_anchors_fail_closed(self):
        for field, value in (('monotonic_start_before_ns', None), ('monotonic_start_before_ns', True),
                             ('monotonic_start_before_ns', '1000'), ('monotonic_start_before_ns', 0),
                             ('monotonic_start_before_ns', 2_000_000_000_000),
                             ('monotonic_drain_after_ns', 1), ('monotonic_drain_after_ns', 999_000_000_000),
                             ('monotonic_drain_after_ns', 2_000_000_000_000),
                             ('boot_id', 'bad'), ('drain_boot_id', 'different'),
                             ('time_namespace', None), ('time_namespace', 'bad'),
                             ('drain_time_namespace', 'time:[123]')):
            with self.subTest(field=field, value=value):
                self.stages[0]['reads']['linux_clock'] = {**linux_anchors(2, 302), field: value}
                self.assertTrue(self.check_capacity()[1])
        self.stages[0]['reads']['linux_clock'] = linux_anchors(-1, 302)
        self.assertTrue(self.check_capacity()[1])

    def test_missing_cross_instance_nonmonotonic_and_malformed_samples_fail_closed(self):
        valid = copy.deepcopy(self.samples)
        mutations = [None, [], [None], [{}]]
        for field, value in (('linux_boot_id', None), ('linux_boot_id', 'other'),
                             ('linux_time_namespace', None), ('linux_time_namespace', 'time:[123]'),
                             ('linux_clocks', None), ('windows_qpc_before_ns', 'invalid')):
            samples = copy.deepcopy(valid)
            samples[1][field] = value
            mutations.append(samples)
        samples = copy.deepcopy(valid)
        samples[2]['linux_clocks']['monotonic_ns'] = samples[1]['linux_clocks']['monotonic_ns']
        mutations.append(samples)
        for samples in mutations:
            with self.subTest(samples=samples):
                self.samples = samples
                self.assertTrue(self.check_capacity()[1])

    def test_unqualified_external_clock_cannot_certify_capacity(self):
        for status in ('missing', 'invalid', 'unqualified'):
            with self.subTest(status=status):
                result, issues = self.check_capacity(calibration_status=status)
                self.assertFalse(result['stages'])
                self.assertTrue(issues)

    def test_invalid_counts_and_durations_fail_closed(self):
        for count in (None, True, -1, 3_006_000.0, 10**1000):
            with self.subTest(count=count):
                self.assertTrue(self.check_capacity(count=count)[1])
        for elapsed in (None, True, 0, -1, float('nan'), float('inf')):
            with self.subTest(elapsed=elapsed):
                self.assertTrue(self.check_capacity(elapsed=elapsed)[1])

    def test_each_steady_stage_must_reach_target(self):
        stages = []
        for name, count in [('warmup', 1), ('steady-01', 3_100_000), ('steady-02', 2_900_000)]:
            stage = copy.deepcopy(self.stages[0])
            stage['name'] = name
            stage['reads']['statuses'] = {'200': count}
            stages.append(stage)
        issues = []
        result = steady_rest_capacity(stages, self.calibration, 10_000, issues, self.samples)
        self.assertEqual(len(result['stages']), 2)
        self.assertEqual(len(issues), 1)
        self.assertTrue(issues[0].startswith('steady-02:'))

    def test_disabled_guard_preserves_legacy_audit(self):
        self.assertEqual(steady_rest_capacity([], {}, 0, []),
                         {'minimum_successful_rps': 0, 'stages': []})


class CheckerResultScopeTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.run = Path(self.directory.name)
        self.report_dir = self.run / 'combined' / 'test'
        self.report_dir.mkdir(parents=True)
        for name in ('resources.jsonl', 'database.jsonl'):
            (self.report_dir / name).write_text('{}\n', encoding='utf-8')
        (self.report_dir / 'pool.log').write_text('sample\n', encoding='utf-8')
        (self.run / 'exit-code.txt').write_text('0\n', encoding='utf-8')
        (self.run / 'console.log').write_text('completed\n', encoding='utf-8')
        host = {'memory': {'MemAvailable': 1024, 'SwapTotal': 0, 'SwapFree': 0}}
        (self.run / 'linux-host-resources.jsonl').write_text(json.dumps(host) + '\n', encoding='utf-8')
        traffic = {'generator_valid': True, 'slo_passed': True, 'statuses': {'200': 1}}
        self.report = {
            'finished': '2026-11-05T12:00:00Z', 'correctness_passed': True,
            'stages': [{'name': 'steady-01', 'reads': traffic, 'flags': traffic,
                        'accepted': 1, 'committed': 1, 'integrity_passed': True}],
            'checker_results_before': 84, 'checker_results_total': 1092, 'checker_results': 1008,
            'checker': {'expected_cells': 84, 'expected_puts': 1120, 'rounds': [
                {'round': number, 'complete': True, 'jobs': 84, 'cells': 84,
                 'finished_jobs': 84, 'up_jobs': 84, 'put_actions': 1120,
                 'stored_flags': 1120, 'mismatched_cells': 0, 'completion_seconds': 40}
                for number in range(1, 14)
            ]},
        }

    def check(self, report=None, require_stable_clocks=False, require_clock_calibration=False):
        (self.report_dir / 'report.json').write_text(json.dumps(report or self.report), encoding='utf-8')
        return audit(self.run, 12, require_stable_clocks=require_stable_clocks,
                     require_clock_calibration=require_clock_calibration)

    def set_clocks(self, elapsed=600):
        for result in (self.report['stages'][0]['reads'], self.report['stages'][0]['flags']):
            result.update(started_utc='2026-11-05T11:00:00Z',
                          drained_utc='2026-11-05T11:10:00Z',
                          monotonic_elapsed_to_drain_seconds=elapsed,
                          linux_clock=linux_anchors(2, 602))

    def add_burst(self):
        burst = {'name': 'flag-burst', 'accepted': 0, 'committed': 0, 'integrity_passed': True,
                 'reads': {'generator_valid': True, 'slo_passed': True, 'statuses': {'200': 300}},
                 'flags': {'generator_valid': True, 'slo_passed': False,
                           'statuses': {'200': 900, '429': 300}}}
        self.report['stages'].append(burst)
        return burst

    def test_bootstrap_connection_reset_writes_failed_audit_json(self):
        # Exact report shape from the 20261106-010940 direct-metrics run,
        # combined/20261105-221025-load-4251201234/report.json. Keep this fixture
        # self-contained because saved load artifacts are not required in CI.
        report = {
            'started': '2026-11-05T22:10:25.677863014Z',
            'finished': '2026-11-05T22:10:25.969456682Z',
            'profile': 'load', 'diagnostics': True, 'database_storage': 'disk',
            'container_logging': 'local', 'go_version': 'go1.27.3',
            'generator_cpus': 32, 'gomaxprocs': 32,
            'docker_host': {'cpus': 32, 'memory_bytes': 29422723072,
                            'storage_driver': 'overlayfs', 'logging_driver': 'json-file',
                            'security_options': ['name=seccomp,profile=builtin', 'name=cgroupns']},
            'stages': None,
            'websockets': {'connections': 0, 'messages': 0, 'failures': 0},
            'websocket_reconnect_interval_seconds': 20,
            'checker_results': 0, 'correctness_passed': False,
            'error': 'load.deployment.post - send request: Post "http://127.0.0.1:44622/api/admin/login/": '
                     'read tcp 127.0.0.1:48550->127.0.0.1:44622: read: connection reset by peer',
        }
        (self.report_dir / 'report.json').write_text(json.dumps(report), encoding='utf-8')
        (self.run / 'exit-code.txt').write_text('2\n', encoding='utf-8')
        output = self.run / 'audit.json'
        printed = io.StringIO()
        with patch('sys.argv', ['audit_report.py', str(self.run), '--min-rounds', '12',
                                '--require-stable-clocks', '--require-clock-calibration',
                                '--output', str(output)]), redirect_stdout(printed):
            self.assertEqual(main(), 2)
        result = json.loads(output.read_text(encoding='utf-8'))
        self.assertEqual(result, json.loads(printed.getvalue()))
        self.assertFalse(result['passed'])
        self.assertEqual(result['native_exit_code'], 2)
        self.assertIn('native exit code is 2', result['issues'])
        self.assertIn('report has no stages', result['issues'])
        self.assertIn('report error: ' + report['error'], result['issues'])
        self.assertEqual(result['metrics']['complete_rounds'], 0)
        self.assertFalse(result['clock_diagnostics']['internally_consistent'])

    def test_absent_null_and_empty_stages_fail_without_crashing(self):
        for kind in ('absent', 'null', 'empty'):
            with self.subTest(kind=kind):
                report = copy.deepcopy(self.report)
                if kind == 'absent':
                    del report['stages']
                else:
                    report['stages'] = None if kind == 'null' else []
                result = self.check(report)
                self.assertFalse(result['passed'])
                self.assertIn('report has no stages', result['issues'])

    def test_non_array_stages_fail_without_crashing(self):
        for stages in ({}, {'name': 'steady-01'}, 'steady-01', 1, False):
            with self.subTest(stages=stages):
                report = copy.deepcopy(self.report)
                report['stages'] = stages
                result = self.check(report)
                self.assertFalse(result['passed'])
                self.assertIn('report stages must be an array', result['issues'])

    def test_malformed_stage_shapes_fail_even_alongside_valid_stage(self):
        valid = self.report['stages'][0]
        malformed = [None, [], 'steady-02', {}, {'name': None}, {'name': 1}, {'name': ''}]
        for field, value in (('reads', []), ('flags', 'invalid'), ('seconds', '600'),
                             ('seconds', float('nan')), ('accepted', None),
                             ('integrity_passed', 'true')):
            malformed.append({**valid, field: value})
        for field, value in (('statuses', None), ('statuses', []), ('statuses', {'429': 'bad'}),
                             ('requests', None), ('p99_ms', 'slow'), ('p99_ms', float('inf')),
                             ('generator_valid', 'true'), ('slo_passed', 1)):
            malformed.append({**valid, 'reads': {**valid['reads'], field: value}})
        for stage in malformed:
            with self.subTest(stage=stage):
                report = copy.deepcopy(self.report)
                report['stages'].append(stage)
                result = self.check(report)
                self.assertFalse(result['passed'])
                self.assertTrue(any('stage[1] is malformed' in issue for issue in result['issues']))

    def test_null_traffic_results_still_fail_existing_missing_result_gate(self):
        for traffic in ('reads', 'flags'):
            with self.subTest(traffic=traffic):
                report = copy.deepcopy(self.report)
                report['stages'][0][traffic] = None
                result = self.check(report)
                self.assertFalse(result['passed'])
                self.assertIn(f'steady-01: {traffic} result is missing', result['issues'])

    def test_report_error_cannot_pass_with_otherwise_valid_success(self):
        self.report['error'] = 'bootstrap failure'
        result = self.check()
        self.assertFalse(result['passed'])
        self.assertIn('report error: bootstrap failure', result['issues'])

    def test_valid_burst_can_include_controlled_429_without_success_slo(self):
        self.add_burst()
        result = self.check()
        self.assertTrue(result['passed'], result['issues'])

    def test_invalid_burst_generator_fails_even_with_native_success(self):
        for traffic in ('reads', 'flags'):
            with self.subTest(traffic=traffic):
                self.report['stages'] = self.report['stages'][:1]
                burst = self.add_burst()
                burst[traffic].update(generator_valid=False, generator_delivery_ratio=923 / 1200,
                                      generator_skipped_slots=277)
                result = self.check()
                self.assertEqual(result['native_exit_code'], 0)
                self.assertFalse(result['passed'])
                self.assertIn(f'flag-burst: {traffic} generator failed', result['issues'])

    def test_burst_reads_must_still_meet_their_slo(self):
        self.add_burst()['reads']['slo_passed'] = False
        self.assertIn('flag-burst: reads SLO failed', self.check()['issues'])

    def test_stable_clocks_pass_the_strict_guard(self):
        self.set_clocks()
        result = self.check(require_stable_clocks=True)
        self.assertTrue(result['passed'], result['issues'])
        self.assertTrue(result['clock_diagnostics']['internally_consistent'])
        self.assertEqual(result['clock_diagnostics']['maximum_absolute_relative_discrepancy'], 0)

    def test_five_percent_clock_skew_is_not_physical_throughput_evidence(self):
        self.set_clocks(elapsed=600 / 1.05)
        result = self.check()
        self.assertTrue(result['passed'], result['issues'])
        self.assertFalse(result['clock_diagnostics']['internally_consistent'])
        self.assertAlmostEqual(result['clock_diagnostics']['maximum_absolute_relative_discrepancy'], 0.05)
        self.assertTrue(any('attack clocks' in text for text in result['limitations']))
        self.assertFalse(self.check(require_stable_clocks=True)['passed'])

    def test_legacy_clock_scope_is_a_limitation_or_a_strict_failure(self):
        result = self.check()
        self.assertTrue(result['passed'], result['issues'])
        self.assertTrue(all(sample['status'] == 'missing' for sample in result['clock_diagnostics']['attacks']))
        self.assertTrue(any('attack clocks' in text for text in result['limitations']))
        self.assertFalse(self.check(require_stable_clocks=True)['passed'])

    def test_clock_tolerance_is_one_tenth_percent(self):
        self.set_clocks(elapsed=600 / 1.0009)
        self.assertTrue(self.check(require_stable_clocks=True)['passed'])
        self.set_clocks(elapsed=600 / 1.0011)
        self.assertFalse(self.check(require_stable_clocks=True)['passed'])

    def write_calibration(self, monotonic_scale=1, realtime_scale=1, start_offset=-2, span=604):
        epoch = int(datetime.fromisoformat('2026-11-05T11:00:00+00:00').timestamp() * 1e9)
        samples = []
        for index, seconds in enumerate((0, span / 2, span)):
            qpc = 10**12 + int(seconds * 1e9)
            utc = epoch + int((start_offset + seconds) * 1e9)
            samples.append({
                'linux_boot_id': BOOT_ID,
                'linux_time_namespace': TIME_NAMESPACE,
                'linux_clocks': {'monotonic_ns': 10**12 + int(seconds * 1e9 * monotonic_scale),
                                 'raw_ns': 10**12 + int(seconds * 1e9),
                                 'realtime_ns': epoch + int((start_offset + seconds * realtime_scale) * 1e9)},
                'windows_qpc_before_ns': qpc - 1_000_000, 'windows_qpc_after_ns': qpc + 1_000_000,
                'windows_utc_before_ns': utc - 1_000_000, 'windows_utc_after_ns': utc + 1_000_000,
                'timex': {'tick': 10000 + index},
            })
        self.save_calibration(samples)
        return samples

    def save_calibration(self, samples):
        (self.run / 'clock-calibration.jsonl').write_text(
            ''.join(json.dumps(sample) + '\n' for sample in samples), encoding='utf-8')

    def test_independent_calibration_qualifies_stable_covered_attacks(self):
        self.set_clocks()
        self.write_calibration()
        result = self.check(require_clock_calibration=True)
        self.assertTrue(result['passed'], result['issues'])
        calibration = result['clock_calibration']
        self.assertEqual(calibration['status'], 'qualified')
        bounds = calibration['linux_to_qpc_ratio_bounds']['monotonic_ns']
        self.assertLess(bounds['lower'], 1)
        self.assertGreater(bounds['upper'], 1)
        self.assertEqual((calibration['tick_min'], calibration['tick_max']), (10000, 10002))

    def test_cli_enforces_successful_capacity_after_native_pass(self):
        self.set_clocks()
        self.write_calibration()
        reads = self.report['stages'][0]['reads']
        for count, expected in ((5_720_000, 2), (6_060_000, 0)):
            with self.subTest(count=count):
                reads['statuses'] = {'200': count}
                self.check()
                output = io.StringIO()
                with patch('sys.argv', ['audit_report.py', str(self.run), '--min-rounds', '12',
                                       '--min-successful-rest-rps', '10000']), redirect_stdout(output):
                    self.assertEqual(main(), expected)
                result = json.loads(output.getvalue())
                self.assertEqual(result['passed'], expected == 0)
                self.assertEqual(result['native_exit_code'], 0)
                self.assertEqual(result['successful_rest_capacity']['minimum_successful_rps'], 10000)
                self.assertEqual(result['successful_rest_capacity']['stages'][0]['method'],
                                 'bracketed_qpc_no_interpolation')

    def test_capacity_requires_absolute_anchors_even_when_all_legacy_clock_gates_pass(self):
        self.set_clocks()
        self.write_calibration()
        self.report['stages'][0]['reads']['statuses'] = {'200': 6_060_000}
        del self.report['stages'][0]['reads']['linux_clock']
        legacy = self.check(require_stable_clocks=True, require_clock_calibration=True)
        self.assertTrue(legacy['passed'], legacy['issues'])
        result = audit(self.run, 12, require_stable_clocks=True, require_clock_calibration=True,
                       min_successful_rest_rps=10_000)
        self.assertFalse(result['passed'])
        self.assertEqual(result['native_exit_code'], 0)
        self.assertTrue(any('legacy report is unverified' in issue for issue in result['issues']))

    def test_capacity_does_not_override_native_failure(self):
        self.set_clocks()
        self.write_calibration()
        self.report['stages'][0]['reads']['statuses'] = {'200': 6_060_000}
        self.check()
        (self.run / 'exit-code.txt').write_text('2\n', encoding='utf-8')
        result = audit(self.run, 12, require_stable_clocks=True, require_clock_calibration=True,
                       min_successful_rest_rps=10_000)
        self.assertFalse(result['passed'])
        self.assertIn('native exit code is 2', result['issues'])
        self.assertGreater(result['successful_rest_capacity']['stages'][0]['successful_rps_qpc_lower_bound'],
                           10_000)

    def test_cli_rejects_invalid_capacity_threshold(self):
        for minimum in ('-1', 'nan', 'inf'):
            with self.subTest(minimum=minimum):
                with patch('sys.argv', ['audit_report.py', str(self.run), '--min-rounds', '12',
                                       '--min-successful-rest-rps', minimum]):
                    with self.assertRaises(SystemExit) as error:
                        main()
                    self.assertEqual(error.exception.code, 2)

    def test_paired_reference_detects_both_linux_clocks_running_slow(self):
        self.set_clocks(elapsed=570)
        for result in (self.report['stages'][0]['reads'], self.report['stages'][0]['flags']):
            result['drained_utc'] = '2026-11-05T11:09:30Z'
        self.write_calibration(monotonic_scale=0.95, realtime_scale=0.95)
        result = self.check(require_clock_calibration=True)
        self.assertTrue(result['clock_diagnostics']['internally_consistent'])
        self.assertFalse(result['passed'])
        self.assertFalse(result['clock_calibration']['monotonic_qualified'])
        self.assertTrue(result['clock_calibration']['coverage_sufficient'])

    def test_missing_independent_reference_is_an_explicit_limitation(self):
        self.set_clocks()
        result = self.check(require_stable_clocks=True)
        self.assertTrue(result['passed'], result['issues'])
        self.assertTrue(any('internal UTC/monotonic agreement alone' in text for text in result['limitations']))
        self.assertFalse(self.check(require_clock_calibration=True)['passed'])

    def test_calibration_must_cover_every_attack(self):
        self.set_clocks()
        for start, span in ((2, 604), (-2, 600)):
            with self.subTest(start=start, span=span):
                self.write_calibration(start_offset=start, span=span)
                result = self.check(require_clock_calibration=True)
                self.assertFalse(result['passed'])
                self.assertFalse(result['clock_calibration']['coverage_sufficient'])

    def test_invalid_pairs_or_nonmonotonic_samples_are_rejected(self):
        self.set_clocks()
        for change in ('type', 'ordering', 'qpc_regression', 'linux_regression', 'utc_regression', 'too_few'):
            with self.subTest(change=change):
                samples = self.write_calibration()
                if change == 'type':
                    samples[0]['windows_qpc_before_ns'] = True
                elif change == 'ordering':
                    samples[0]['windows_qpc_after_ns'] = samples[0]['windows_qpc_before_ns'] - 1
                elif change == 'qpc_regression':
                    samples[1]['windows_qpc_before_ns'] = samples[0]['windows_qpc_before_ns']
                elif change == 'linux_regression':
                    samples[1]['linux_clocks']['raw_ns'] = samples[0]['linux_clocks']['raw_ns']
                elif change == 'utc_regression':
                    samples[1]['windows_utc_before_ns'] = samples[0]['windows_utc_before_ns']
                else:
                    samples = samples[:1]
                self.save_calibration(samples)
                result = self.check(require_clock_calibration=True)
                self.assertFalse(result['passed'])
                self.assertEqual(result['clock_calibration']['status'], 'invalid')

    def test_wide_roundtrip_bounds_do_not_prove_a_stable_rate(self):
        self.set_clocks()
        samples = self.write_calibration()
        samples[0]['windows_qpc_before_ns'] -= 1_000_000_000
        self.save_calibration(samples)
        result = self.check(require_clock_calibration=True)
        self.assertFalse(result['passed'])
        self.assertFalse(result['clock_calibration']['monotonic_qualified'])

    def test_partial_and_invalid_clock_fields_fail_the_strict_guard(self):
        for field, value in [('started_utc', 'invalid'), ('drained_utc', '2026-11-05T11:10:00'),
                             ('started_utc', '0001-01-01T00:00:00Z'),
                             ('drained_utc', '2026-11-05T10:59:00Z'),
                             ('monotonic_elapsed_to_drain_seconds', -1),
                             ('monotonic_elapsed_to_drain_seconds', 0),
                             ('monotonic_elapsed_to_drain_seconds', float('nan')),
                             ('monotonic_elapsed_to_drain_seconds', float('inf')),
                             ('monotonic_elapsed_to_drain_seconds', True)]:
            with self.subTest(field=field, value=value):
                self.set_clocks()
                self.report['stages'][0]['reads'][field] = value
                result = self.check(require_stable_clocks=True)
                self.assertFalse(result['passed'])
                self.assertEqual(result['clock_diagnostics']['attacks'][0]['status'], 'invalid')
        self.set_clocks()
        del self.report['stages'][0]['reads']['drained_utc']
        self.assertFalse(self.check(require_stable_clocks=True)['passed'])

    def test_explicit_initial_round_is_not_a_missing_result(self):
        result = self.check()
        self.assertTrue(result['passed'], result['issues'])
        self.assertEqual(result['metrics']['complete_rounds'], 13)
        self.assertEqual(result['metrics']['trailing_checker_results'], 0)
        self.assertEqual(result['checker_result_scope']['mode'], 'explicit_baseline')

    def test_inconsistent_delta_fails(self):
        self.report['checker_results'] -= 1
        self.assertIn('checker result delta does not equal total minus baseline', self.check()['issues'])

    def test_absolute_missing_result_fails_even_with_consistent_delta(self):
        self.report['checker_results_total'] -= 1
        self.report['checker_results'] -= 1
        result = self.check()
        self.assertFalse(result['passed'])
        self.assertIn('checker result total is smaller than the complete round result count', result['issues'])

    def test_missing_round_fails_independently_of_count_scope(self):
        del self.report['checker']['rounds'][5]
        self.assertIn('complete round numbers are not contiguous from 1', self.check()['issues'])

    def test_legacy_delta_has_an_explicit_unverified_scope(self):
        del self.report['checker_results_before']
        del self.report['checker_results_total']
        result = self.check()
        self.assertTrue(result['passed'], result['issues'])
        self.assertEqual(result['checker_result_scope']['mode'], 'legacy_delta_unverified')
        self.assertIsNone(result['metrics']['trailing_checker_results'])
        self.assertTrue(result['limitations'])
        self.report['checker']['rounds'][2]['finished_jobs'] -= 1
        self.assertFalse(self.check()['passed'])

    def test_partial_explicit_scope_and_invalid_numbers_fail(self):
        for field, value in [('checker_results_before', None), ('checker_results_total', True),
                             ('checker_results', -1)]:
            with self.subTest(field=field, value=value):
                report = copy.deepcopy(self.report)
                report[field] = value
                self.assertFalse(self.check(report)['passed'])
        del self.report['checker_results_before']
        self.assertFalse(self.check()['passed'])

    def test_total_can_include_results_from_the_trailing_round(self):
        self.report['checker']['rounds'].append({'round': 14, 'complete': False, 'jobs': 84})
        self.report['checker_results_total'] += 65
        self.report['checker_results'] += 65
        result = self.check()
        self.assertTrue(result['passed'], result['issues'])
        self.assertEqual(result['metrics']['trailing_checker_results'], 65)
        self.report['checker_results_total'] += 20
        self.report['checker_results'] += 20
        self.assertFalse(self.check()['passed'])


if __name__ == '__main__':
    unittest.main()
