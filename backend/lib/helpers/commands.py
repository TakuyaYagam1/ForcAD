import os
import selectors
import shlex
import signal
import subprocess
import sys
import time
from logging import Logger
from typing import Any, AnyStr

from lib import models
from lib.models import Action, TaskStatus

DEFAULT_CHECKER_MEMORY_LIMIT_MB = 1024
DEFAULT_CHECKER_OUTPUT_LIMIT_BYTES = 1024 * 1024
MAX_CHECKER_OUTPUT_LIMIT_BYTES = 16 * 1024 * 1024
CHECKER_KILL_DRAIN_SECONDS = 0.25
CHECKER_REAP_TIMEOUT_SECONDS = 0.5


def _checker_memory_limit_bytes(env: dict[str, str]) -> int | None:
    raw_limit = env.get(
        'FORCAD_CHECKER_MEMORY_LIMIT_MB',
        str(DEFAULT_CHECKER_MEMORY_LIMIT_MB),
    )
    try:
        limit_mb = int(raw_limit)
    except (TypeError, ValueError):
        limit_mb = DEFAULT_CHECKER_MEMORY_LIMIT_MB

    if limit_mb < 0:
        limit_mb = DEFAULT_CHECKER_MEMORY_LIMIT_MB
    if limit_mb == 0:
        return None
    return limit_mb * 1024 * 1024


def _checker_output_limit_bytes(env: dict[str, str]) -> int:
    raw_limit = env.get(
        'FORCAD_CHECKER_OUTPUT_LIMIT_BYTES',
        str(DEFAULT_CHECKER_OUTPUT_LIMIT_BYTES),
    )
    try:
        limit = int(raw_limit)
    except (TypeError, ValueError):
        limit = DEFAULT_CHECKER_OUTPUT_LIMIT_BYTES

    if not 1024 <= limit <= MAX_CHECKER_OUTPUT_LIMIT_BYTES:
        limit = DEFAULT_CHECKER_OUTPUT_LIMIT_BYTES
    return limit


def _memory_limited_command(
        command: list[str],
        memory_limit_bytes: int | None,
) -> list[str]:
    if sys.platform != 'linux' or memory_limit_bytes is None:
        return command

    launcher = (
        'import os, resource, sys; '
        'limit = int(sys.argv[1]); '
        'soft, hard = resource.getrlimit(resource.RLIMIT_AS); '
        'hard = min(hard, limit) if hard != resource.RLIM_INFINITY else limit; '
        'soft = min(soft, hard) if soft != resource.RLIM_INFINITY else hard; '
        'resource.setrlimit(resource.RLIMIT_AS, (soft, hard)); '
        'command = sys.argv[2:]; '
        'os.execvpe(command[0], command, os.environ)'
    )
    return [sys.executable, '-c', launcher, str(memory_limit_bytes), *command]


def _signal_process_tree(
        proc: subprocess.Popen,
        signum: int,
        process_group: bool,
) -> None:
    try:
        if process_group and os.name == 'posix':
            os.killpg(proc.pid, signum)
        else:
            proc.send_signal(signum)
    except ProcessLookupError:
        pass


def _read_bounded_output(
        proc: subprocess.Popen,
        max_output_bytes: int,
        timeout: float,
        terminate_timeout: float,
        process_group: bool,
) -> tuple[bytes, bytes, bool, bool, bool]:
    """Drain pipes under an absolute deadline and stop the checker on overflow."""
    selector = selectors.DefaultSelector()
    output = {'stdout': bytearray(), 'stderr': bytearray()}
    output_limited = False
    timed_out = False
    killed = False
    termination_deadline = None
    cleanup_deadline = None

    try:
        for name in output:
            stream = getattr(proc, name)
            if stream is not None:
                os.set_blocking(stream.fileno(), False)
                selector.register(stream, selectors.EVENT_READ, name)

        deadline = time.monotonic() + max(timeout, 0)
        termination_requested = False
        force_kill_sent = False

        while True:
            now = time.monotonic()
            if not termination_requested and now >= deadline:
                timed_out = True
                termination_requested = True
                _signal_process_tree(proc, signal.SIGTERM, process_group)
                termination_deadline = now + max(terminate_timeout, 0)

            if output_limited and not termination_requested:
                termination_requested = True
                _signal_process_tree(proc, signal.SIGTERM, process_group)
                termination_deadline = now + max(terminate_timeout, 0)

            if termination_deadline is not None and now >= termination_deadline:
                if not force_kill_sent:
                    _signal_process_tree(proc, signal.SIGKILL, process_group)
                    force_kill_sent = True
                    killed = True
                    cleanup_deadline = now + CHECKER_KILL_DRAIN_SECONDS

            if force_kill_sent and now >= cleanup_deadline:
                break

            if proc.poll() is not None and not selector.get_map():
                if not termination_requested or force_kill_sent:
                    break

            wait_time = 0.1
            next_deadline = deadline
            if termination_requested:
                next_deadline = termination_deadline
            if force_kill_sent:
                next_deadline = cleanup_deadline
            wait_time = max(0, min(wait_time, next_deadline - time.monotonic()))

            if selector.get_map():
                ready_streams = selector.select(wait_time)
            else:
                time.sleep(wait_time)
                ready_streams = ()

            for key, _ in ready_streams:
                stream = key.fileobj
                try:
                    chunk = os.read(stream.fileno(), 65536)
                except BlockingIOError:
                    continue

                if not chunk:
                    selector.unregister(stream)
                    stream.close()
                    continue

                remaining = max_output_bytes - sum(map(len, output.values()))
                if remaining > 0:
                    output[key.data].extend(chunk[:remaining])
                if len(chunk) > max(remaining, 0):
                    output_limited = True

        if proc.poll() is None:
            _signal_process_tree(proc, signal.SIGKILL, process_group)
            killed = True
            try:
                proc.wait(timeout=CHECKER_REAP_TIMEOUT_SECONDS)
            except subprocess.TimeoutExpired:
                timed_out = True
        else:
            proc.wait()
    except BaseException:
        _signal_process_tree(proc, signal.SIGKILL, process_group)
        try:
            proc.wait(timeout=CHECKER_REAP_TIMEOUT_SECONDS)
        except subprocess.TimeoutExpired:
            pass
        raise
    finally:
        selector.close()
        for name in output:
            stream = getattr(proc, name)
            if stream is not None:
                stream.close()

    return (
        bytes(output['stdout']),
        bytes(output['stderr']),
        timed_out,
        killed,
        output_limited,
    )


def run_command_gracefully(
        command: list[str],
        input: AnyStr | None = None,
        capture_output: bool = False,
        timeout: float = 0,
        check: bool = False,
        terminate_timeout: float = 3,
        max_output_bytes: int | None = None,
        memory_limit_bytes: int | None = None,
        **kwargs: Any,
) -> tuple[subprocess.CompletedProcess, bool]:
    """
    Like subprocess.run, but with graceful shutdown.

    First sends SIGTERM, waits for "terminate_timeout" seconds and if
    the timeout occurs the second time, sends SIGKILL.

    :param command: command to run
    :param input: see corresponding "run" parameter
    :param capture_output: see corresponding "run" parameter
    :param timeout: "soft" timeout, after which the SIGTERM is sent
    :param check: see corresponding "run" parameter
    :param terminate_timeout: the "hard" timeout to wait after the SIGTERM
    :param max_output_bytes: maximum combined stdout and stderr retained
    :param memory_limit_bytes: Linux address-space limit for the child process
    :return: tuple of CompletedProcess instance and "killed" boolean
    """
    if max_output_bytes is not None:
        if max_output_bytes < 0:
            raise ValueError('max_output_bytes must be non-negative')
        if not capture_output:
            raise ValueError('Bounded output capture requires capture_output=True')
        if input is not None:
            raise ValueError('Bounded output capture does not support input')

    if input is not None:
        kwargs['stdin'] = subprocess.PIPE

    if capture_output:
        kwargs['stdout'] = subprocess.PIPE
        kwargs['stderr'] = subprocess.PIPE

    killed = False
    output_truncated = False
    limited_command = _memory_limited_command(command, memory_limit_bytes)
    if capture_output and max_output_bytes is not None:
        process_group = os.name == 'posix'
        if process_group:
            kwargs['start_new_session'] = True
        proc = subprocess.Popen(limited_command, **kwargs)
        try:
            stdout, stderr, timed_out, killed, output_truncated = (
                _read_bounded_output(
                    proc,
                    max_output_bytes,
                    timeout,
                    terminate_timeout,
                    process_group,
                )
            )
            retcode = proc.poll()
            if retcode is None:
                timed_out = True
                _signal_process_tree(proc, signal.SIGKILL, process_group)
                try:
                    proc.wait(timeout=CHECKER_REAP_TIMEOUT_SECONDS)
                except subprocess.TimeoutExpired:
                    pass
                retcode = proc.returncode
        finally:
            if proc.poll() is None:
                _signal_process_tree(proc, signal.SIGKILL, process_group)
                try:
                    proc.wait(timeout=CHECKER_REAP_TIMEOUT_SECONDS)
                except subprocess.TimeoutExpired:
                    pass
            for stream in (proc.stdout, proc.stderr):
                if stream is not None and not stream.closed:
                    stream.close()

        if timed_out:
            raise subprocess.TimeoutExpired(
                command,
                timeout,
                output=stdout,
                stderr=stderr,
            )
    else:
        with subprocess.Popen(limited_command, **kwargs) as proc:
            try:
                stdout, stderr = proc.communicate(input, timeout=timeout)
            except subprocess.TimeoutExpired as timeout_exc:
                proc.terminate()
                try:
                    stdout, stderr = proc.communicate(
                        input,
                        timeout=terminate_timeout,
                    )
                except subprocess.TimeoutExpired:
                    proc.kill()
                    killed = True
                    stdout, stderr = proc.communicate()
                except BaseException:
                    proc.kill()
                    raise

                raise subprocess.TimeoutExpired(
                    command,
                    timeout=timeout,
                    output=stdout,
                    stderr=stderr,
                ) from timeout_exc
            except BaseException:
                proc.kill()
                raise

            retcode = proc.poll()

    if check and retcode:
        raise subprocess.CalledProcessError(
            retcode,
            command,
            output=stdout,
            stderr=stderr,
        )

    res_proc: subprocess.CompletedProcess = subprocess.CompletedProcess(
        args=command,
        returncode=retcode,
        stdout=stdout,
        stderr=stderr,
    )
    res_proc.output_truncated = output_truncated
    return res_proc, killed


def get_patched_environ(env_path: str) -> dict[str, str]:
    """
    Add path to the environment variable.

    :param env_path: path to be inserted to environment
    """
    env = os.environ.copy()
    env['PATH'] = f"{env_path}:{env['PATH']}"
    return env


def log_error(
        action: Action,
        team: models.Team,
        result: subprocess.CompletedProcess,
        logger: Logger):
    logger.warning(
        '%s for team %s failed with code %s.\nstdout: %s\nstderr: %s',
        action,
        team.id,
        result.returncode,
        _short_output(result.stdout),
        _short_output(result.stderr),
    )


def _short_output(output, limit: int = 1024) -> str:
    if output is None:
        return ''
    if isinstance(output, bytes):
        text = output[:limit].decode(errors='replace')
    else:
        text = str(output)[:limit]
    if len(output) > limit:
        text += ' [output truncated]'
    return text


def run_generic_command(
        command: list,
        action: Action,
        task: models.Task,
        team: models.Team,
        logger: Logger,
) -> models.CheckerVerdict:
    """Runs generic checker command, calls "run_command_gracefully"
        and handles exceptions

    :param command: command to run
    :param action: type of command (for logging)
    :param task: `models.Task` instance for env & timeout
    :param team: `models.Team` instance (for logging)
    :param logger: logger instance
    :return: models.CheckerVerdict instance
    """
    env = get_patched_environ(env_path=task.env_path)
    memory_limit_bytes = _checker_memory_limit_bytes(env)
    output_limit_bytes = _checker_output_limit_bytes(env)

    try:
        result, killed = run_command_gracefully(
            command,
            capture_output=True,
            timeout=task.checker_timeout,
            env=env,
            max_output_bytes=output_limit_bytes,
            memory_limit_bytes=memory_limit_bytes,
        )

        if result.output_truncated:
            logger.warning(
                'Checker output limit exceeded for team %s task %s',
                team.id,
                task.id,
            )
            status = TaskStatus.CHECK_FAILED
            public_message = 'Check failed'
            private_message = 'Checker output limit exceeded'
        else:
            if killed:
                logger.warning(
                    'Process was forcefully killed during %s for team %s task %s',
                    action,
                    team.id,
                    task.id,
                )

            try:
                status = TaskStatus(result.returncode)
                if action == Action.PUT and status == TaskStatus.UP:
                    # Successful PUT output is protocol data used by GET.
                    # The pipe reader already enforces the combined byte cap.
                    public_message = result.stdout.decode(errors='replace').strip()
                    private_message = result.stderr.decode(errors='replace').strip()
                else:
                    public_message = _short_output(result.stdout).strip()
                    private_message = _short_output(result.stderr).strip()
                if status == TaskStatus.CHECK_FAILED:
                    log_error(action, team, result, logger)
            except ValueError as e:
                status = TaskStatus.CHECK_FAILED
                public_message = 'Check failed'
                private_message = (
                    f'Check failed with ValueError: {e!s}\n'
                    f'Return code: {result.returncode}\n'
                    f'Stdout: {_short_output(result.stdout)}\n'
                    f'Stderr: {_short_output(result.stderr)}'
                )
                log_error(action, team, result, logger)

    except subprocess.TimeoutExpired:
        status = TaskStatus.DOWN
        private_message = f'{action} timeout (killed by ForcAD)'
        public_message = 'Checker timed out'

    command_str = ' '.join(shlex.quote(x) for x in command)
    verdict = models.CheckerVerdict(
        public_message=public_message,
        private_message=private_message,
        command=command_str,
        action=action,
        status=status,
    )

    return verdict
