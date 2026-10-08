import logging
import os
import sys
import time
import unittest
from pathlib import Path
from types import SimpleNamespace
from subprocess import TimeoutExpired
from unittest.mock import Mock, patch

PROJECT_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_DIR / 'backend'))

from lib.helpers.commands import (
    DEFAULT_CHECKER_MEMORY_LIMIT_MB,
    DEFAULT_CHECKER_OUTPUT_LIMIT_BYTES,
    _checker_memory_limit_bytes,
    _checker_output_limit_bytes,
    run_command_gracefully,
    run_generic_command,
)
from lib.models import Action, TaskStatus


class CheckerLimitTests(unittest.TestCase):
    def test_successful_put_keeps_flag_data_longer_than_log_excerpt(self):
        task = SimpleNamespace(env_path='', checker_timeout=5, id=7)
        team = SimpleNamespace(id=3)
        verdict = run_generic_command(
            [sys.executable, '-c',
             "import sys; print('p' * 2048); print('s' * 2048, file=sys.stderr); sys.exit(101)"],
            Action.PUT, task, team, Mock(spec=logging.Logger),
        )
        self.assertEqual(verdict.status, TaskStatus.UP)
        self.assertEqual(verdict.public_message, 'p' * 2048)
        self.assertEqual(verdict.private_message, 's' * 2048)

    def test_default_limits_and_memory_opt_out(self):
        self.assertEqual(
            _checker_memory_limit_bytes({}),
            DEFAULT_CHECKER_MEMORY_LIMIT_MB * 1024 * 1024,
        )
        self.assertIsNone(
            _checker_memory_limit_bytes({'FORCAD_CHECKER_MEMORY_LIMIT_MB': '0'})
        )
        self.assertEqual(
            _checker_output_limit_bytes({}),
            DEFAULT_CHECKER_OUTPUT_LIMIT_BYTES,
        )

    def test_output_capture_stays_within_shared_limit(self):
        command = [
            sys.executable,
            '-c',
            "import sys; sys.stdout.write('a' * 4096); "
            "sys.stderr.write('b' * 4096)",
        ]
        result, killed = run_command_gracefully(
            command,
            capture_output=True,
            timeout=5,
            max_output_bytes=256,
            terminate_timeout=0.1,
        )

        self.assertTrue(result.output_truncated)
        self.assertTrue(killed)
        self.assertLessEqual(len(result.stdout) + len(result.stderr), 256)

    def test_output_limit_returns_check_failed_without_checker_output(self):
        logger = Mock(spec=logging.Logger)
        task = SimpleNamespace(env_path='', checker_timeout=5, id=7)
        team = SimpleNamespace(id=3)
        env = {
            'PATH': os.environ.get('PATH', ''),
            'FORCAD_CHECKER_MEMORY_LIMIT_MB': '0',
            'FORCAD_CHECKER_OUTPUT_LIMIT_BYTES': '1024',
        }

        with patch('lib.helpers.commands.get_patched_environ', return_value=env):
            verdict = run_generic_command(
                [
                    sys.executable,
                    '-c',
                    "import sys; sys.stdout.write('x' * 4096)",
                ],
                Action.CHECK,
                task,
                team,
                logger,
            )

        self.assertEqual(verdict.status, TaskStatus.CHECK_FAILED)
        self.assertEqual(verdict.public_message, 'Check failed')
        self.assertEqual(verdict.private_message, 'Checker output limit exceeded')
        self.assertNotIn('x' * 64, str(logger.warning.call_args_list))

    def test_timeout_kills_child_holding_checker_pipes(self):
        child = (
            'import signal, time; '
            'signal.signal(signal.SIGTERM, signal.SIG_IGN); '
            'time.sleep(5)'
        )
        parent = (
            'import subprocess, sys, time; '
            f'subprocess.Popen([sys.executable, "-c", {child!r}]); '
            'time.sleep(0.05)'
        )
        started = time.monotonic()
        with self.assertRaises(TimeoutExpired):
            run_command_gracefully(
                [sys.executable, '-c', parent],
                capture_output=True,
                timeout=0.2,
                terminate_timeout=0.1,
                max_output_bytes=1024,
            )
        self.assertLess(time.monotonic() - started, 2)

    @unittest.skipUnless(sys.platform == 'linux', 'RLIMIT_AS is Linux-only here')
    def test_memory_limit_is_applied_by_launcher(self):
        limit_bytes = 256 * 1024 * 1024
        command = [
            sys.executable,
            '-c',
            'import resource; print(resource.getrlimit(resource.RLIMIT_AS)[1])',
        ]
        result, _ = run_command_gracefully(
            command,
            capture_output=True,
            timeout=5,
            max_output_bytes=1024,
            memory_limit_bytes=limit_bytes,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertLessEqual(int(result.stdout.strip()), limit_bytes)

    def test_existing_graceful_timeout_is_preserved(self):
        command = [sys.executable, '-c', 'import time; time.sleep(2)']
        with self.assertRaises(TimeoutExpired):
            run_command_gracefully(
                command,
                capture_output=True,
                timeout=0.2,
                terminate_timeout=0.2,
                max_output_bytes=1024,
            )


if __name__ == '__main__':
    unittest.main()
