import argparse
import ast
import contextlib
import io
import json
from pathlib import Path
import queue
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

import probe_clock


def sample(index, elapsed_ns, tick=10000, realtime_jump_ns=0):
    return {
        'index': index,
        'windows_qpc_before_ns': elapsed_ns,
        'windows_qpc_after_ns': elapsed_ns + 100,
        'windows_utc_before_ns': elapsed_ns + 10000,
        'windows_utc_after_ns': elapsed_ns + 10100,
        'linux_clocks': {
            'realtime_ns': elapsed_ns + realtime_jump_ns,
            'monotonic_ns': elapsed_ns,
            'boottime_ns': elapsed_ns,
            'raw_ns': elapsed_ns,
        },
        'timex': {
            'tick': tick,
            'freq_ppm': 0,
        },
        'linux_user_hz': 100,
        'roundtrip_ns': 100,
    }


class ClockSummaryTest(unittest.TestCase):
    def test_elapsed_ratio_is_bounded_by_roundtrip_uncertainty(self):
        result = probe_clock.summarize([sample(0, 0), sample(1, 1_000_000_000)])
        ratio = result['linux_to_windows_qpc_ratio']['raw_ns']
        self.assertEqual(ratio['midpoint'], 1)
        self.assertLess(ratio['lower'], 1)
        self.assertGreater(ratio['upper'], 1)
        self.assertEqual(result['nominal_tick_us'], 10000)
        self.assertEqual(result['windows_qpc_elapsed_bounds_seconds'], [0.9999999, 1.0000001])

    def test_records_tick_excursions_and_realtime_steps(self):
        result = probe_clock.summarize([
            sample(0, 0),
            sample(1, 1_000_000_000, tick=9468, realtime_jump_ns=1_800_000_000),
            sample(2, 2_000_000_000, realtime_jump_ns=1_800_000_000),
        ])
        self.assertEqual(result['tick_first'], 10000)
        self.assertEqual(result['tick_last'], 10000)
        self.assertEqual(result['tick_min'], 9468)
        self.assertEqual(result['tick_max'], 10000)
        self.assertEqual(result['realtime_steps_over_50ms'], [{
            'index': 1,
            'realtime_minus_monotonic_ns': 1_800_000_000,
        }])

    def test_rejects_insufficient_or_unbounded_interval(self):
        with self.assertRaisesRegex(RuntimeError, 'at least two'):
            probe_clock.summarize([sample(0, 0)])
        with self.assertRaisesRegex(RuntimeError, 'roundtrip uncertainty'):
            probe_clock.summarize([sample(0, 0), sample(1, 50)])

    def test_native_program_is_readonly_and_syntactically_valid(self):
        tree = ast.parse(probe_clock.LINUX_SOURCE)
        self.assertIn('tx = Timex()', probe_clock.LINUX_SOURCE)
        self.assertIn('assert tx.modes == 0', probe_clock.LINUX_SOURCE)
        for node in ast.walk(tree):
            if isinstance(node, ast.Assign):
                self.assertFalse(any(
                    isinstance(target, ast.Attribute) and target.attr == 'modes'
                    for target in node.targets
                ))

    def test_native_program_reads_clock_identity_once_before_sampling(self):
        tree = ast.parse(probe_clock.LINUX_SOURCE)
        for name in ('boot_id', 'time_namespace'):
            assignments = [node for node in tree.body if isinstance(node, ast.Assign) and
                           any(isinstance(target, ast.Name) and target.id == name for target in node.targets)]
            self.assertEqual(len(assignments), 1)
        self.assertIn("Path('/proc/sys/kernel/random/boot_id').read_text().strip()", probe_clock.LINUX_SOURCE)
        self.assertIn("os.readlink('/proc/self/ns/time')", probe_clock.LINUX_SOURCE)
        self.assertIn("'linux_boot_id': boot_id", probe_clock.LINUX_SOURCE)
        self.assertIn("'linux_time_namespace': time_namespace", probe_clock.LINUX_SOURCE)


class ProbeLifecycleTest(unittest.TestCase):
    def test_completion_is_checked_without_sleeping(self):
        completion = Mock()
        completion.is_file.return_value = True
        with patch.object(probe_clock.time, 'sleep') as sleep:
            self.assertEqual(probe_clock.wait_for_sample(10, completion), 'completion')
        sleep.assert_not_called()

    def test_wait_polls_completion_with_bounded_sleeps(self):
        completion = Mock()
        completion.is_file.side_effect = [False, True]
        with (
            patch.object(probe_clock.time, 'perf_counter_ns', return_value=0),
            patch.object(probe_clock.time, 'sleep') as sleep,
        ):
            self.assertEqual(probe_clock.wait_for_sample(5_000_000_000, completion), 'completion')
        sleep.assert_called_once_with(0.25)

    def test_snapshot_rejects_mismatched_response(self):
        process = Mock()
        responses = queue.Queue()
        responses.put(json.dumps(sample(7, 0)))
        with self.assertRaisesRegex(RuntimeError, 'does not match'):
            probe_clock.snapshot(process, responses, 1)

    def test_cleanup_escalates_only_after_bounded_waits(self):
        process = Mock()
        process.poll.return_value = None
        process.wait.side_effect = [
            subprocess.TimeoutExpired('probe', 3),
            subprocess.TimeoutExpired('probe', 3),
            0,
        ]
        probe_clock.stop_process(process)
        process.stdin.close.assert_called_once()
        process.terminate.assert_called_once()
        process.kill.assert_called_once()
        self.assertEqual(process.wait.call_count, 3)
        self.assertTrue(all(call.kwargs == {'timeout': 3} for call in process.wait.call_args_list))

    def test_completion_captures_final_sample_and_always_cleans_up(self):
        with tempfile.TemporaryDirectory() as directory:
            args = argparse.Namespace(
                distro='ForcAD-Load',
                duration=65,
                interval=5,
                out=Path(directory) / 'clock.jsonl',
                completion=Path(directory) / 'completion.json',
            )
            process = Mock()
            reader = Mock()
            reader.is_alive.return_value = False
            with (
                patch.object(probe_clock.subprocess, 'Popen', return_value=process),
                patch.object(probe_clock.threading, 'Thread', return_value=reader),
                patch.object(probe_clock, 'snapshot', side_effect=[
                    sample(-1, 0),
                    sample(0, 0),
                    sample(1, 65_000_000_000),
                ]),
                patch.object(probe_clock.time, 'perf_counter_ns', return_value=100),
                patch.object(probe_clock, 'wait_for_sample', return_value='completion'),
                patch.object(probe_clock, 'stop_process') as stop,
                contextlib.redirect_stdout(io.StringIO()),
            ):
                self.assertEqual(probe_clock.run_probe(args), 0)
            summary = json.loads(args.out.with_suffix('.summary.json').read_text())
            rows = [json.loads(line) for line in args.out.read_text().splitlines()]
            self.assertEqual(summary['stop_reason'], 'completion')
            self.assertEqual(summary['samples'], 2)
            self.assertEqual(rows[-1]['index'], 1)
            stop.assert_called_once_with(process)
            reader.join.assert_called_once_with(timeout=2)
            process.stdout.close.assert_called_once()

    def test_rpc_failure_preserves_error_artifact_and_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            args = argparse.Namespace(
                distro='ForcAD-Load',
                duration=65,
                interval=5,
                out=Path(directory) / 'clock.jsonl',
                completion=None,
            )
            process = Mock()
            reader = Mock()
            reader.is_alive.return_value = False
            with (
                patch.object(probe_clock.subprocess, 'Popen', return_value=process),
                patch.object(probe_clock.threading, 'Thread', return_value=reader),
                patch.object(probe_clock, 'snapshot', side_effect=RuntimeError('RPC failed')),
                patch.object(probe_clock, 'stop_process') as stop,
                contextlib.redirect_stdout(io.StringIO()),
            ):
                self.assertEqual(probe_clock.run_probe(args), 1)
            summary = json.loads(args.out.with_suffix('.summary.json').read_text())
            self.assertIn('RPC failed', summary['error'])
            self.assertIn('RPC failed', args.out.read_text())
            stop.assert_called_once_with(process)


if __name__ == '__main__':
    unittest.main()
