"""Dispatch container roles to commands from the installed runtime."""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

SERVICE_MODULES = {
    'api': 'forcad.services.api.app:app',
    'admin': 'forcad.services.admin.app:app',
    'events': 'forcad.services.events.app:app',
    'http_receiver': 'forcad.services.http_receiver.app:app',
}
ROLES = (*SERVICE_MODULES, 'ticker', 'worker', 'flower', 'initializer')


def _executable(name: str) -> str:
    """Use the command beside this interpreter, without following venv links."""
    candidate = Path(sys.executable).absolute().parent / name
    if candidate.is_file() and os.access(candidate, os.X_OK):
        return str(candidate)
    raise SystemExit(
        f'Required runtime command is not installed beside {sys.executable}: {name}'
    )


def command_for(
        role: str,
        arguments: list[str],
        environment: dict[str, str] | None = None) -> list[str]:
    environment = os.environ if environment is None else environment
    if role in SERVICE_MODULES:
        port = environment.get('PORT') or '5000'
        log_level = environment.get('LOG_LEVEL') or 'INFO'
        threads = environment.get('FORCAD_HTTP_THREADS') or '100'
        return [
            _executable('gunicorn'),
            '--bind', f'0.0.0.0:{port}',
            '--log-level', log_level,
            '--worker-class', 'gthread',
            '--workers', '1',
            '--threads', threads,
            *arguments,
            SERVICE_MODULES[role],
        ]
    if role == 'ticker':
        return [sys.executable, '-m', 'forcad.services.ticker', *arguments]
    if role == 'worker':
        autoscale = environment.get('FORCAD_WORKER_AUTOSCALE') or '32,16'
        return [
            _executable('celery'), '-A', 'forcad.services.tasks.app:celery_app',
            'worker', '-E', '-l', 'info', '--autoscale', autoscale, *arguments,
        ]
    if role == 'flower':
        environment['FLOWER_PORT'] = environment.get('PORT') or '5000'
        admin = f'{environment.get("ADMIN_USERNAME", "")}:' \
            f'{environment.get("ADMIN_PASSWORD", "")}'
        broker_api = environment.get('BROKER_API_URL', '')
        return [
            _executable('celery'), '-A', 'forcad.services.tasks.app:celery_app',
            'flower', f'--basic_auth={admin}', '--url_prefix=flower',
            '--address=0.0.0.0', f'--broker_api={broker_api}', *arguments,
        ]
    if role == 'initializer':
        return [sys.executable, '-m', 'forcad.scripts.full_reset', *arguments]
    raise ValueError(f'Unknown ForcAD runtime role: {role}')


def runtime_environment(role: str) -> dict[str, str]:
    environment = os.environ.copy()
    if role == 'flower':
        environment['FLOWER_PORT'] = environment.get('PORT') or '5000'
    return environment


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog='python -m forcad.entrypoints',
        description='Run an installed ForcAD container role.',
    )
    parser.add_argument('role', choices=ROLES)
    parser.add_argument('arguments', nargs=argparse.REMAINDER)
    namespace = parser.parse_args(argv)
    environment = runtime_environment(namespace.role)
    command = command_for(namespace.role, namespace.arguments, environment)
    os.execve(command[0], command, environment)
    return 127


if __name__ == '__main__':
    raise SystemExit(main())
