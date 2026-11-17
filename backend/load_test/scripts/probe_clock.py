#!/usr/bin/env python3
"""Compare WSL clock discipline with Windows QPC without changing clocks or services.

Run with Windows Python, for example:
  python backend/load_test/scripts/probe_clock.py --duration 65 --out clock.jsonl
For a long test, add --duration 21600 --completion path/to/completion.json.
The completion file is only observed; this tool never starts or stops a test.
"""

import argparse
import datetime as dt
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time


LINUX_SOURCE = r'''
import ctypes
import json
import os
from pathlib import Path
import sys
import time

class Timeval(ctypes.Structure):
    _fields_ = [
        ('tv_sec', ctypes.c_long),
        ('tv_usec', ctypes.c_long),
    ]

class Timex(ctypes.Structure):
    _fields_ = [
        ('modes', ctypes.c_uint),
        ('offset', ctypes.c_long),
        ('freq', ctypes.c_long),
        ('maxerror', ctypes.c_long),
        ('esterror', ctypes.c_long),
        ('status', ctypes.c_int),
        ('constant', ctypes.c_long),
        ('precision', ctypes.c_long),
        ('tolerance', ctypes.c_long),
        ('time', Timeval),
        ('tick', ctypes.c_long),
        ('ppsfreq', ctypes.c_long),
        ('jitter', ctypes.c_long),
        ('shift', ctypes.c_int),
        ('stabil', ctypes.c_long),
        ('jitcnt', ctypes.c_long),
        ('calcnt', ctypes.c_long),
        ('errcnt', ctypes.c_long),
        ('stbcnt', ctypes.c_long),
        ('tai', ctypes.c_int),
        ('reserved', ctypes.c_int * 11),
    ]

if ctypes.sizeof(ctypes.c_long) != 8 or ctypes.sizeof(Timex) != 208:
    raise RuntimeError('this probe supports the native Linux x86_64 timex ABI only')
if Timex.tick.offset != 88 or Timex.time.offset != 72:
    raise RuntimeError('unexpected timex ABI offsets')
libc = ctypes.CDLL(None, use_errno=True)
libc.adjtimex.argtypes = [ctypes.POINTER(Timex)]
libc.adjtimex.restype = ctypes.c_int
boot_id = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
time_namespace = os.readlink('/proc/self/ns/time')

for request in sys.stdin:
    if request.strip() == 'stop':
        break
    try:
        index = int(request)
        raw_before = time.clock_gettime_ns(time.CLOCK_MONOTONIC_RAW)
        clocks = {
            'realtime_ns': time.time_ns(),
            'monotonic_ns': time.monotonic_ns(),
            'boottime_ns': time.clock_gettime_ns(time.CLOCK_BOOTTIME),
            'raw_ns': time.clock_gettime_ns(time.CLOCK_MONOTONIC_RAW),
        }
        # New zero-initialized struct for every call. modes=0 is READ ONLY.
        tx = Timex()
        assert tx.modes == 0
        state = libc.adjtimex(ctypes.byref(tx))
        if state < 0:
            raise OSError(ctypes.get_errno(), 'read-only adjtimex failed')
        if tx.modes != 0:
            raise RuntimeError('read-only adjtimex unexpectedly changed its modes field')
        snapshot = {
            name: getattr(tx, name)
            for name, _ in Timex._fields_
            if name not in ('time', 'reserved')
        }
        snapshot['time'] = {'tv_sec': tx.time.tv_sec, 'tv_usec': tx.time.tv_usec}
        snapshot['reserved'] = list(tx.reserved)
        snapshot['state'] = state
        snapshot['freq_ppm'] = tx.freq / 65536
        snapshot['offset_unit'] = 'nanoseconds' if tx.status & 0x2000 else 'microseconds'
        row = {
            'index': index,
            'linux_boot_id': boot_id,
            'linux_time_namespace': time_namespace,
            'linux_clocks': clocks,
            'linux_raw_before_ns': raw_before,
            'linux_raw_after_ns': time.clock_gettime_ns(time.CLOCK_MONOTONIC_RAW),
            'linux_user_hz': os.sysconf('SC_CLK_TCK'),
            'timex_size': ctypes.sizeof(Timex),
            'timex': snapshot,
        }
    except Exception as exc:
        row = {'error': f'{type(exc).__name__}: {exc}'}
    print(json.dumps(row), flush=True)
'''


def utc():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def read_responses(stream, responses):
    try:
        for line in iter(lambda: stream.readline(65537), ''):
            if len(line) > 65536 or not line.endswith('\n'):
                responses.put(RuntimeError('invalid oversized/incomplete WSL response'), timeout=1)
                return
            responses.put(line, timeout=1)
    except Exception as exc:
        try:
            responses.put(exc, timeout=1)
        except queue.Full:
            pass
    finally:
        try:
            responses.put(None, timeout=1)
        except queue.Full:
            pass


def snapshot(process, responses, index):
    qpc_before = time.perf_counter_ns()
    utc_before = time.time_ns()
    process.stdin.write(f'{index}\n')
    process.stdin.flush()
    response = responses.get(timeout=15 if index <= 0 else 5)
    utc_after = time.time_ns()
    qpc_after = time.perf_counter_ns()
    if response is None:
        raise RuntimeError(f'WSL clock probe exited unexpectedly: {process.poll()}')
    if isinstance(response, Exception):
        raise response
    row = json.loads(response)
    if row.get('error'):
        raise RuntimeError(row['error'])
    if row.get('index') != index:
        raise RuntimeError('WSL clock probe response does not match request')
    row.update(time=utc(), windows_qpc_before_ns=qpc_before, windows_qpc_after_ns=qpc_after,
               windows_utc_before_ns=utc_before, windows_utc_after_ns=utc_after,
               roundtrip_ns=qpc_after - qpc_before)
    return row


def elapsed_summary(first, last):
    lower = last['windows_qpc_before_ns'] - first['windows_qpc_after_ns']
    upper = last['windows_qpc_after_ns'] - first['windows_qpc_before_ns']
    midpoint = (lower + upper) / 2
    if lower <= 0:
        raise RuntimeError('clock sample interval is shorter than its roundtrip uncertainty')
    elapsed = {
        key: last['linux_clocks'][key] - first['linux_clocks'][key]
        for key in first['linux_clocks']
    }
    ratios = {
        key: {'midpoint': delta / midpoint, 'lower': delta / upper, 'upper': delta / lower}
        for key, delta in elapsed.items()
    }
    utc_elapsed = ((last['windows_utc_before_ns'] + last['windows_utc_after_ns']) -
                   (first['windows_utc_before_ns'] + first['windows_utc_after_ns'])) / 2
    return {
        'windows_qpc_elapsed_seconds': midpoint / 1e9,
        'windows_qpc_elapsed_bounds_seconds': [lower / 1e9, upper / 1e9],
        'windows_utc_elapsed_seconds': utc_elapsed / 1e9,
        'linux_elapsed_seconds': {key: value / 1e9 for key, value in elapsed.items()},
        'linux_to_windows_qpc_ratio': ratios,
        'monotonic_to_raw_ratio': elapsed['monotonic_ns'] / elapsed['raw_ns'],
        'tick_first': first['timex']['tick'],
        'tick_last': last['timex']['tick'],
        'nominal_tick_us': 1_000_000 / first['linux_user_hz'],
        'freq_ppm_first': first['timex']['freq_ppm'],
        'freq_ppm_last': last['timex']['freq_ppm'],
        'readonly': True,
    }


class ClockSummary:
    """Keep a bounded in-memory aggregate; raw samples are written to JSONL."""

    def __init__(self):
        self.first = None
        self.last = None
        self.samples = 0
        self.roundtrip_max_ns = 0
        self.tick_min = None
        self.tick_max = None
        self.realtime_steps = []

    def add(self, row):
        if self.first is None:
            self.first = row
        if self.last is not None:
            real_delta = row['linux_clocks']['realtime_ns'] - self.last['linux_clocks']['realtime_ns']
            mono_delta = row['linux_clocks']['monotonic_ns'] - self.last['linux_clocks']['monotonic_ns']
            discontinuity = real_delta - mono_delta
            if abs(discontinuity) > 50_000_000:
                self.realtime_steps.append({
                    'index': row['index'],
                    'realtime_minus_monotonic_ns': discontinuity,
                })
        self.last = row
        self.samples += 1
        self.roundtrip_max_ns = max(self.roundtrip_max_ns, row['roundtrip_ns'])
        tick = row['timex']['tick']
        self.tick_min = tick if self.tick_min is None else min(self.tick_min, tick)
        self.tick_max = tick if self.tick_max is None else max(self.tick_max, tick)

    def result(self):
        if self.samples < 2:
            raise RuntimeError('at least two clock samples are required to estimate elapsed ratios')
        result = elapsed_summary(self.first, self.last)
        result.update(
            samples=self.samples,
            roundtrip_max_ms=self.roundtrip_max_ns / 1e6,
            tick_min=self.tick_min,
            tick_max=self.tick_max,
            realtime_steps_over_50ms=self.realtime_steps,
        )
        return result


def summarize(rows):
    aggregate = ClockSummary()
    for row in rows:
        aggregate.add(row)
    return aggregate.result()


def wait_for_sample(deadline_ns, completion):
    """Use the host clock and check completion without busy waiting."""
    while True:
        if completion is not None and completion.is_file():
            return 'completion'
        remaining = (deadline_ns - time.perf_counter_ns()) / 1e9
        if remaining <= 0:
            return None
        time.sleep(min(remaining, 0.25))


def stop_process(process):
    if process.poll() is not None:
        return
    try:
        if process.stdin:
            process.stdin.close()
        process.wait(timeout=3)
    except (OSError, subprocess.TimeoutExpired):
        if process.poll() is not None:
            return
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--distro', default='ForcAD-Load')
    parser.add_argument('--duration', type=float, default=65)
    parser.add_argument('--interval', type=float, default=5)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--completion', type=Path,
                        help='stop and summarize when this completion file appears')
    args = parser.parse_args(argv)
    if os.name != 'nt':
        parser.error('run this controller with Windows Python so QPC is independent of WSL')
    if not 2 <= args.duration <= 8 * 3600 or not 0.2 <= args.interval <= 30:
        parser.error('duration must be 2..28800s and interval 0.2..30s')
    if args.out.exists() or args.out.with_suffix('.summary.json').exists():
        parser.error('output already exists; choose a new path')
    if args.completion is not None and args.completion.is_file():
        parser.error('completion file already exists; choose the current test run completion path')
    args.out.parent.mkdir(parents=True, exist_ok=True)
    return run_probe(args)


def run_probe(args):
    aggregate = ClockSummary()
    responses = queue.Queue(maxsize=2)
    process = subprocess.Popen(['wsl.exe', '-d', args.distro, '--', 'python3', '-u', '-c', LINUX_SOURCE],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                               text=True, bufsize=1,
                               creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))
    reader = threading.Thread(target=read_responses, args=(process.stdout, responses), daemon=True)
    reader.start()
    stop_reason = 'duration'
    error = None
    try:
        # Start-up latency belongs to process creation, not to clock-rate uncertainty.
        snapshot(process, responses, -1)
        with args.out.open('x', encoding='utf-8') as stream:
            first_qpc = None
            index = 0
            while True:
                row = snapshot(process, responses, index)
                aggregate.add(row)
                stream.write(json.dumps(row) + '\n')
                stream.flush()
                if first_qpc is None:
                    first_qpc = (row['windows_qpc_before_ns'] + row['windows_qpc_after_ns']) // 2
                now = time.perf_counter_ns()
                if now - first_qpc >= args.duration * 1e9:
                    break
                if args.completion is not None and args.completion.is_file():
                    stop_reason = 'completion'
                    break
                index += 1
                deadline = min(first_qpc + int(args.duration * 1e9),
                               now + int(args.interval * 1e9))
                reason = wait_for_sample(deadline, args.completion)
                if reason is not None:
                    # Capture a final sample at completion, then summarize the exact observed window.
                    stop_reason = reason
                    row = snapshot(process, responses, index)
                    aggregate.add(row)
                    stream.write(json.dumps(row) + '\n')
                    stream.flush()
                    break
    except KeyboardInterrupt:
        stop_reason = 'interrupted'
    except Exception as exc:
        error = f'{type(exc).__name__}: {exc}'
        stop_reason = 'error'
        with args.out.open('a', encoding='utf-8') as stream:
            stream.write(json.dumps({'time': utc(), 'error': error}) + '\n')
    finally:
        try:
            stop_process(process)
            reader.join(timeout=2)
            process.stdout.close()
            if reader.is_alive():
                raise RuntimeError('WSL clock probe reader did not stop')
        except Exception as exc:
            error = f'{type(exc).__name__}: {exc}'
            stop_reason = 'error'
    try:
        summary = aggregate.result()
    except RuntimeError as exc:
        summary = {'samples': aggregate.samples, 'readonly': True}
        if error is None:
            error = str(exc)
    summary['stop_reason'] = stop_reason
    if error is not None:
        summary['error'] = error
    args.out.with_suffix('.summary.json').write_text(json.dumps(summary, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(summary, indent=2), flush=True)
    if stop_reason == 'interrupted':
        return 130
    return int(error is not None)


if __name__ == '__main__':
    raise SystemExit(main())
