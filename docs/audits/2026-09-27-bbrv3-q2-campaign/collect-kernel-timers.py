#!/usr/bin/env python3
"""Finite diagnostic timer/workqueue tracing in one fresh owned trace instance."""
import argparse
import json
import os
from pathlib import Path
import re
import select
import signal
import subprocess
import sys
import time

EVENTS = ('timer/timer_expire_entry', 'timer/timer_expire_exit',
          'workqueue/workqueue_execute_start', 'workqueue/workqueue_execute_end')
CAP = 128 * 1024 * 1024


def clock_samples():
    result = []
    for _ in range(8):
        low = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
        wall = time.time_ns()
        high = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
        result.append(dict(monotonic_lower_ns=low, wall_ns=wall,
                           monotonic_upper_ns=high))
    return result


def losses(stats):
    return {name: int(value) for name, value in re.findall(
        r'^\s*(overrun|commit overrun|dropped events):\s*(\d+)', stats, re.M)}


def interrupted(signum, frame):
    raise TimeoutError('finite trace interrupted: ' + str(signum))


def main(attempt, stage, probe):
    if not re.fullmatch(r'q2-linux-[0-9]{8}-router-diagnostic[0-9]+', attempt):
        raise ValueError('fresh diagnostic identity required')
    if stage != Path('/tmp') / attempt or os.geteuid() != 0:
        raise ValueError('expected owned stage and root on fresh diagnostic guest')
    tracing = Path('/sys/kernel/tracing')
    mounted = False
    instance = None
    fd = None
    result = dict(diagnostic_only=True, events=EVENTS, output_cap_bytes=CAP,
                  before_clock_samples=clock_samples(), probe=probe,
                  completed=False, cleanup_errors=[])
    emitted = 0
    try:
        if not (tracing / 'instances').is_dir():
            subprocess.run(['mount', '-t', 'tracefs', 'tracefs', str(tracing)],
                           check=True, timeout=5)
            mounted = True
        path = tracing / 'instances' / (attempt + ('-probe' if probe else '-trace'))
        path.mkdir()  # Never adopt an existing instance.
        instance = path
        (path / 'tracing_on').write_text('0')
        (path / 'current_tracer').write_text('nop')
        if 'mono' not in (path / 'trace_clock').read_text().replace('[', '').replace(']', '').split():
            raise ValueError('monotonic trace clock unavailable')
        (path / 'trace_clock').write_text('mono')
        (path / 'buffer_size_kb').write_text('8192')  # 32MiB on four cores.
        total = int((path / 'buffer_total_size_kb').read_text().strip())
        if total > 128 * 1024:
            raise ValueError('trace ring exceeds memory envelope')
        result['buffer_total_size_kb'] = total
        result['formats'] = {e: (path / 'events' / e / 'format').read_text() for e in EVENTS}
        for event in EVENTS:
            (path / 'events' / event / 'enable').write_text('1')
        (path / 'trace').write_text('')
        fd = os.open(path / 'trace_pipe', os.O_RDONLY | os.O_NONBLOCK)
        (path / 'tracing_on').write_text('1')
        (path / 'trace_marker').write_text('Q2_DIAGNOSTIC_BEGIN ' + attempt)
        if not probe:
            with (stage / 'trace-ready.json').open('x') as ready:
                json.dump(dict(pid=os.getpid(), instance=str(path)), ready)
        started = time.monotonic()
        limit = 2 if probe else 270
        while time.monotonic() - started < limit:
            if not probe and (stage / 'trace-stop').exists():
                result['completed'] = True
                break
            readable, _, _ = select.select([fd], [], [], .1)
            if not readable:
                continue
            try:
                chunk = os.read(fd, 65536)
            except BlockingIOError:
                continue
            if emitted + len(chunk) > CAP:
                raise ValueError('trace output cap exhausted')
            emitted += len(chunk)
            if not probe:
                sys.stdout.buffer.write(chunk)
                sys.stdout.buffer.flush()
            elif b'Q2_DIAGNOSTIC_BEGIN' in chunk:
                result['marker_observed'] = True
        if probe:
            result['completed'] = result.get('marker_observed') is True
        if not result['completed']:
            raise TimeoutError('trace deadline without stop or probe marker')
        (path / 'tracing_on').write_text('0')
        # Drain the final finite buffer; it cannot receive more events now.
        while True:
            try:
                chunk = os.read(fd, 65536)
            except BlockingIOError:
                break
            if not chunk:
                break
            if emitted + len(chunk) > CAP:
                raise ValueError('trace output cap exhausted while draining')
            emitted += len(chunk)
            if not probe:
                sys.stdout.buffer.write(chunk)
                sys.stdout.buffer.flush()
    except BaseException as exc:
        result['error'] = repr(exc)
    finally:
        signal.alarm(0)
        if instance is not None:
            try:
                (instance / 'tracing_on').write_text('0')
                result['cpu_stats'] = {p.parent.name: p.read_text()
                    for p in sorted((instance / 'per_cpu').glob('cpu*/stats'))}
                result['loss_counters'] = {cpu: losses(text)
                    for cpu, text in result['cpu_stats'].items()}
                if len(result['loss_counters']) != 4 or any(
                        not counters or any(counters.values())
                        for counters in result['loss_counters'].values()):
                    result['error'] = 'trace loss or incomplete CPU loss counters'
            except Exception as exc:
                result['cleanup_errors'].append('trace stop/stats: ' + repr(exc))
            if fd is not None:
                os.close(fd)
            try:
                instance.rmdir()
            except Exception as exc:
                result['cleanup_errors'].append('owned trace removal: ' + repr(exc))
        if mounted:
            try:
                subprocess.run(['umount', str(tracing)], check=True, timeout=5)
            except Exception as exc:
                result['cleanup_errors'].append('owned tracefs unmount: ' + repr(exc))
        result.update(after_clock_samples=clock_samples(), emitted_trace_bytes=emitted)
        print(('' if probe else '\n# Q2_TRACE_METADATA ') + json.dumps(result), flush=True)
    return 0 if result['completed'] and not result.get('error') and not result['cleanup_errors'] else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attempt', required=True)
    parser.add_argument('--stage', type=Path, required=True)
    parser.add_argument('--probe', action='store_true')
    args = parser.parse_args()
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGALRM):
        signal.signal(sig, interrupted)
    signal.alarm(15 if args.probe else 280)
    raise SystemExit(main(args.attempt, args.stage, args.probe))
