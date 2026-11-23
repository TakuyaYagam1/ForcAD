"""Install just the monitoring assets in an existing managed ForcAD checkout."""

import fcntl
import hashlib
import json
import os
import re
import secrets
import subprocess
import sys
from pathlib import Path

from hostlib.common import dump_yaml, merge_targets, read_yaml, regular

DASHBOARDS = ('forcad', 'traffic', 'ddos', 'relay')
ASSETS = tuple(f'grafana/dashboards/{name}.json' for name in DASHBOARDS) + ('rules/game.yml',)


def credentials(original: bytes, *, create=True) -> bytes:
    additions = []
    for key in (b"GRAFANA_ADMIN_PASSWORD", b"GRAFANA_CHECKER_DB_PASSWORD"):
        values = re.findall(rb"(?m)^[ \t]*(?:export[ \t]+)?" + key + rb"[ \t]*=[ \t]*([^\r\n]*)", original)
        if not values and create:
            additions.append(key + b'="' + secrets.token_hex(32).encode() + b'"\n')
            continue
        if len(values) != 1 or values[0].strip() in (b"", b'""', b"''"):
            raise ValueError(f"Missing, empty or duplicate {key.decode()} in deployment/docker/.env")
        if key == b"GRAFANA_CHECKER_DB_PASSWORD" and not re.fullmatch(rb"([\"']?)[0-9a-f]{32,}\1", values[0].strip()):
            raise ValueError("GRAFANA_CHECKER_DB_PASSWORD must be a literal lowercase hex secret (32+ characters)")
    if not additions:
        return original
    return original + (b"\n" if original and not original.endswith(b"\n") else b"") + b"".join(additions)


def check_asset(project, relative, content, expected_hash):
    if hashlib.sha256(content).hexdigest() == expected_hash:
        return
    # A first install may adopt the unchanged file from the managed checkout.
    upstream = subprocess.run(
        ['git', '-c', f'safe.directory={project}', '-C', str(project), 'cat-file', 'blob', f'HEAD:{relative}'],
        check=False, capture_output=True, timeout=10,
    )
    if upstream.returncode or content != upstream.stdout:
        raise ValueError(f'Locally modified monitoring file; save it separately before updating: {relative}')


def stage_assets(bundle, project, repo, installed):
    known = {row['path']: row.get('sha256') for row in installed.get('files', [])}
    for name in ASSETS:
        relative = 'deployment/monitoring/' + name
        dest = f'{repo}/{relative}'
        packaged = regular(bundle.directory / 'monitoring' / name).read_bytes()
        current = regular(project / relative)
        if current.exists():
            content = current.read_bytes()
            if content != packaged:
                check_asset(project, relative, content, known.get(dest))
        bundle.put(dest, packaged, 0o644)
    for name in DASHBOARDS:
        relative = f'deployment/monitoring/grafana/dashboards/{name}.ru.json'
        dest = f'{repo}/{relative}'
        current = regular(project / relative)
        if current.exists():
            check_asset(project, relative, current.read_bytes(), known.get(dest))
        elif dest not in known:
            continue
        bundle.remove(dest)


def prepare(bundle, repo, repository_url):
    bundle.check_role()
    installed = json.loads(regular(bundle.manifest).read_text())
    if installed.get('format') != 2 or installed.get('config') != bundle.config:
        raise ValueError('Monitoring requires the matching installed ForcAD inventory')
    if installed['options'].get('repo') != repo or not installed.get('rollback'):
        raise ValueError('No matching managed ForcAD checkout')
    project = bundle.path(repo)
    marker = regular(project / '.forcad-fresh-install').read_bytes()
    if not marker.split(b'\n', 1)[0].startswith(repository_url.encode() + b' '):
        raise ValueError('Managed checkout belongs to a different repository')
    if regular(project / '.forcad-uninstalled').exists():
        raise ValueError('Cannot enable monitoring during uninstall')
    for name in ('backend/bin/forcad', 'deployment/docker/compose.monitoring.yml',
                 'deployment/monitoring/grafana/setup_checker_reader.sh',
                 'deployment/monitoring/grafana/provisioning/datasources/checker.yml'):
        if not regular(project / name).is_file():
            raise ValueError(f'Existing checkout needs monitoring support: missing {name}')
    state = regular(project / '.forcad-monitoring').read_bytes()
    expected = b'monitoring_sha256=' + hashlib.sha256(state).hexdigest().encode()
    if state not in (b'true\n', b'false\n') or marker.splitlines().count(expected) != 1:
        raise ValueError('Invalid monitoring ownership record')
    env_name = f'{repo}/deployment/docker/.env'
    original = regular(bundle.path(env_name)).read_bytes()
    bundle.options = {**installed['options'], 'game_metrics': True, 'network_metrics': True}
    bundle.put(env_name, credentials(original, create=state == b'false\n'))
    for name, port in (('routers.yml', 9109), ('game-sensors.yml', 9911)):
        path = f'{repo}/deployment/monitoring/file_sd/{name}'
        current = regular(bundle.path(path))
        groups = read_yaml(current) if current.exists() else []
        target = f"{bundle.config['checks']['metrics_address']}:{port}"
        before = len(groups) if isinstance(groups, list) else 0
        merged = merge_targets(groups, target)
        content = current.read_bytes() if current.exists() and len(merged) == before else dump_yaml(merged)
        bundle.put(path, content, 0o644)
    stage_assets(bundle, project, repo, installed)
    if state == b'false\n':
        # Update the checksummed teardown mode in the same backed-up transaction.
        enabled = b'true\n'
        replacement = b'monitoring_sha256=' + hashlib.sha256(enabled).hexdigest().encode()
        bundle.put(f'{repo}/.forcad-monitoring', enabled)
        bundle.put(f'{repo}/.forcad-fresh-install', marker.replace(expected, replacement, 1))


def activate(bundle, repo, repository):
    # Lock the directory, not the receipt inode: install_files atomically replaces it.
    regular(bundle.manifest)
    lock = os.open(bundle.manifest.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        prepare(bundle, repo, repository)
        binary = str(bundle.path(repo) / 'backend/bin/forcad')
        prefix = [binary, 'control', '--project-root', str(bundle.path(repo))]
        running = subprocess.check_output([*prefix, 'compose', 'ps', '--status', 'running', '--services'], text=True, timeout=60)
        if 'postgres' not in running.splitlines():
            raise ValueError('Start the existing deployment before enabling monitoring')
        bundle.install_files(extend=True)
        def monitor(*args):
            subprocess.run([*prefix, 'monitoring', *args], check=True, timeout=300)
        # Never run setup, recreate gameplay containers, rebuild images, or remove volumes.
        monitor('run', '--rm', '--no-deps', '-T', 'checker-db-access')
        monitor('up', '-d', '--no-deps', '--no-build', '--wait', 'prometheus', 'grafana')
        monitor('kill', '-s', 'HUP', 'prometheus')
        print('Grafana and Prometheus enabled; gameplay services were not restarted.')
    finally:
        os.close(lock)


def main():
    from host import Bundle

    if os.geteuid() != 0 or len(sys.argv) != 3:
        raise SystemExit('Run the bundled sudo bash install.sh --monitoring')
    os.umask(0o077)
    activate(Bundle(Path(__file__).resolve().parent), *sys.argv[1:])


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        raise SystemExit(f'Monitoring install failed: {error}') from None
