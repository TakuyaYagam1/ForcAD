import json
import os
import subprocess
from pathlib import Path

import click

from cli import constants, utils


STORAGES = ('postgres', 'redis', 'rabbitmq')
CHECK_DATA = '''set -eu
if [ -L /cleanup/data ]; then
    echo 'Refusing to delete a symlinked PostgreSQL directory' >&2
    exit 1
fi
if [ -e /cleanup/data ] && [ ! -d /cleanup/data ]; then
    echo 'PostgreSQL data path is not a directory' >&2
    exit 1
fi
if [ -d /cleanup/data ] && [ -n "$(ls -A /cleanup/data)" ]; then
    if [ ! -f /cleanup/data/PG_VERSION ] || [ -L /cleanup/data/PG_VERSION ]; then
        echo 'Refusing to delete data without a regular PG_VERSION file' >&2
        exit 1
    fi
fi
'''
DELETE_DATA = CHECK_DATA + '''rm -rf -- /cleanup/data
test ! -e /cleanup/data && test ! -L /cleanup/data
'''


def execute(command, *, capture=False, timeout=120):
    try:
        result = subprocess.run(
            [str(part) for part in command],
            cwd=constants.BASE_DIR,
            env={**os.environ, 'FORCAD_VERSION': constants.VERSION},
            stdout=subprocess.PIPE if capture else None,
            text=True,
            check=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired as error:
        raise click.ClickException(
            'Cleanup timed out; inspect the containers before retrying.'
        ) from error
    except OSError as error:
        raise click.ClickException(f'Could not run Docker: {error.strerror}') from error
    except subprocess.CalledProcessError as error:
        raise click.ClickException(
            'Cleanup failed. See the error above; no successful reset was reported.'
        ) from error
    return result.stdout.strip() if capture else None


def compose_command(fast):
    command = ['docker', 'compose', '-f', constants.BASE_COMPOSE_FILE]
    if fast:
        command += ['-f', constants.FAST_COMPOSE_FILE]
    elif os.getenv('TEST'):
        command += ['-f', constants.TESTS_COMPOSE_FILE]
    return command


def data_directory():
    root = constants.BASE_DIR.resolve()
    for relative in ('docker_volumes', 'docker_volumes/postgres',
                     'docker_volumes/postgres/data'):
        path = root / relative
        if path.is_symlink():
            raise click.ClickException(f'Refusing a symlinked storage path: {path}')
        if path.exists() and not path.is_dir():
            raise click.ClickException(f'Storage path is not a directory: {path}')
    return root / 'docker_volumes/postgres/data'


def local_docker():
    host = os.getenv('DOCKER_HOST') if not os.getenv('DOCKER_CONTEXT') else None
    if not host:
        host = execute([
            'docker', 'context', 'inspect', '--format',
            '{{.Endpoints.docker.Host}}',
        ], capture=True)
    if not host.startswith('unix://'):
        raise click.ClickException(
            'Full cleanup requires a local Docker daemon using a Unix socket.'
        )


def cleanup_container(image, directory, *, delete):
    mount = f'type=bind,src={directory.parent},dst=/cleanup,bind-recursive=disabled'
    if not delete:
        mount += ',readonly'
    return [
        'docker', 'run', '--rm', '--pull=never', '--network=none',
        '--user=0:0', '--read-only', '--cap-drop=ALL',
        '--cap-add=DAC_OVERRIDE', '--cap-add=FOWNER',
        '--security-opt=no-new-privileges', '--mount', mount,
        '--entrypoint=/bin/sh', image, '-c', DELETE_DATA if delete else CHECK_DATA,
    ]


def prepare_full_reset(config):
    if not all(service in config['services'] for service in STORAGES):
        raise click.ClickException(
            'Full cleanup only supports the built-in PostgreSQL, Redis and RabbitMQ. '
            'Use reset for a game reset with external storages.'
        )
    local_docker()
    directory = data_directory()
    postgres = config['services']['postgres']
    mounts = [volume for volume in postgres.get('volumes', [])
              if volume.get('target', '').rstrip('/') == '/var/lib/postgresql/data']
    bind_mount = len(mounts) == 1 and mounts[0].get('type') == 'bind'
    if not bind_mount or Path(mounts[0]['source']).absolute() != directory:
        raise click.ClickException(
            'Full cleanup requires the standard '
            'docker_volumes/postgres/data bind mount.'
        )
    if not directory.exists():
        return None
    image = execute([
        'docker', 'image', 'inspect', postgres['image'], '--format', '{{.Id}}',
    ], capture=True)
    execute(cleanup_container(image, directory, delete=False))
    return image, directory


def check_project(config):
    containers = execute([
        'docker', 'ps', '-aq', '--filter',
        f'label=com.docker.compose.project={config["name"]}',
    ], capture=True)
    if not containers:
        return
    labels = execute([
        'docker', 'inspect', '--format', '{{json .Config.Labels}}',
        *containers.split(),
    ], capture=True)
    for line in labels.splitlines():
        directory = json.loads(line).get('com.docker.compose.project.working_dir')
        if not directory or Path(directory).resolve() != constants.BASE_DIR.resolve():
            raise click.ClickException(
                'Compose project belongs to another directory; refusing cleanup.'
            )


def reset_game(*, full=False, fast=False):
    base_compose = constants.BASE_DIR / constants.BASE_COMPOSE_FILE
    if not base_compose.exists():
        directory = data_directory()
        working_dir = constants.BASE_DIR.resolve()
        containers = execute([
            'docker', 'ps', '-aq', '--filter',
            f'label=com.docker.compose.project.working_dir={working_dir}',
        ], capture=True)
        if directory.exists() or containers:
            raise click.ClickException(
                'Deployment configuration is missing. Run setup before cleanup.'
            )
        utils.print_bold('No initialized deployment to reset')
        return

    command = compose_command(fast)
    config = json.loads(execute(command + ['config', '--format', 'json'], capture=True))
    check_project(config)
    cleanup = prepare_full_reset(config) if full else None
    if full:
        utils.print_bold('Removing game containers and local database data')
    else:
        utils.print_bold('Stopping game services before resetting the database')
        execute(command + ['stop', '--timeout', '30'])
        storages = [service for service in STORAGES if service in config['services']]
        if storages:
            execute(command + ['up', '-d', '--no-deps', *storages])
        try:
            execute(command + [
                'run', '--rm', '--no-deps', '--entrypoint', 'python3',
                'initializer', '/app/scripts/reset_db.py',
            ], timeout=300)
        except click.ClickException as error:
            raise click.ClickException(
                'Database reset failed; game services remain stopped. '
                'Check storage access, or use reset --full to discard the local '
                'database, including its old credentials.'
            ) from error

    execute(command + ['down', '--volumes', '--remove-orphans', '--timeout', '30'])
    if cleanup:
        image, directory = cleanup
        execute(cleanup_container(image, directory, delete=True))
        if directory.exists() or directory.is_symlink():
            raise click.ClickException('PostgreSQL data directory still exists.')


def generated_config_files():
    files = [
        constants.ADMIN_ENV_PATH, constants.POSTGRES_ENV_PATH,
        constants.RABBITMQ_ENV_PATH, constants.REDIS_ENV_PATH,
        constants.BASE_DIR / constants.BASE_COMPOSE_FILE,
    ]
    for path in files:
        if path.is_symlink() or (path.exists() and not path.is_file()):
            raise click.ClickException(f'Not a regular generated config file: {path}')
    return files


def remove_generated_config():
    for path in generated_config_files():
        try:
            path.unlink(missing_ok=True)
        except OSError as error:
            raise click.ClickException(
                f'Could not remove {path}: {error.strerror}'
            ) from error
