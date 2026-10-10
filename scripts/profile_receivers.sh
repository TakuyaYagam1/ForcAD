#!/usr/bin/env bash
# Sample receiver workers from the Docker host without restarting them.
set -euo pipefail

profile_dir=$(mktemp -d "$PWD/receiver-profile.XXXXXX")
trap 'rm -rf -- "$profile_dir"' EXIT
python3 -m venv "$profile_dir"
"$profile_dir/bin/python" -m pip install \
  --quiet --no-cache-dir --only-binary=:all: py-spy
sudo -v

"$profile_dir/bin/python" - "$profile_dir/bin/py-spy" <<'PY'
import json
import subprocess
import sys
import time
from collections import Counter
from concurrent.futures import ThreadPoolExecutor


def command(args):
    return subprocess.check_output(args, text=True, stderr=subprocess.PIPE, timeout=5)


def worker_pids(output):
    parents = {}
    for line in output.splitlines():
        fields = line.split(None, 2)
        if len(fields) == 3 and fields[0].isdigit() and 'gunicorn' in fields[2]:
            parents[int(fields[0])] = int(fields[1])
    return sorted(pid for pid, parent in parents.items() if parent in parents)


def request_stacks(traces):
    for trace in traces:
        frames = trace['frames']
        if not any(
            frame['filename'].endswith('gunicorn/workers/gthread.py')
            and frame['name'].split('.')[-1] == 'handle_request'
            for frame in frames
        ):
            continue
        selected = frames[:6]
        selected += [f for f in frames[6:] if f['filename'].startswith('/app/')][:4]
        yield tuple(f"{f['filename']}:{f['line']} {f['name']}" for f in selected)


def sample(worker):
    name, pid = worker
    try:
        output = command([
            'sudo', '-n', sys.argv[1], 'dump', '--pid', str(pid),
            '--nonblocking', '--json',
        ])
        return worker, list(request_stacks(json.loads(output))), None
    except (OSError, subprocess.SubprocessError, ValueError, KeyError) as exc:
        detail = getattr(exc, 'stderr', '') or str(exc)
        return worker, [], detail.splitlines()[0][:200]


def main():
    containers = command([
        'docker', 'ps', '--filter', 'name=forcad-http-receiver-',
        '--format', '{{.Names}}',
    ]).splitlines()
    workers = []
    for name in sorted(containers):
        pids = worker_pids(command(['docker', 'top', name, '-eo', 'pid,ppid,args']))
        print(f'{name}: worker_pids={pids}', flush=True)
        workers.extend((name, pid) for pid in pids)
    if not workers:
        raise SystemExit('No receiver workers found')

    counts = {worker: Counter() for worker in workers}
    failures = {worker: Counter() for worker in workers}
    dumps = Counter()
    print('Sampling request stacks for 20 seconds...', flush=True)
    deadline = time.monotonic() + 20
    with ThreadPoolExecutor(max_workers=min(4, len(workers))) as executor:
        while time.monotonic() < deadline:
            for worker, stacks, error in executor.map(sample, workers):
                if error:
                    failures[worker][error] += 1
                else:
                    dumps[worker] += 1
                    counts[worker].update(stacks)
            time.sleep(min(1, max(0, deadline - time.monotonic())))

    for worker in workers:
        name, pid = worker
        print(f'\n{name} pid={pid} dumps={dumps[worker]} '
              f'request_thread_samples={sum(counts[worker].values())}')
        for error, count in failures[worker].most_common(2):
            print(f'  ERROR x{count}: {error}')
        for stack, count in counts[worker].most_common(3):
            print(f'  Observations: {count}')
            for frame in stack:
                print(f'    {frame}')
    print('\nCounts are thread observations, not request counts or CPU percentages.')
    if not any(dumps.values()):
        raise SystemExit(1)


if __name__ == '__main__':
    main()
PY
