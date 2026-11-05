#!/usr/bin/env python3
"""Finite issue #1210 observer launcher. Execution attaches probes; importing does not."""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import pwd
import selectors
import signal
import stat
import subprocess
import sys
import time

EXPECTED_HOME = Path('/home/josh')
STAGE_PREFIX = 'wiremux-1460-diag-q1210-'
SCRIPT_SECONDS = 180
OUTER_SECONDS = 195
SCRIPT_SHA256 = '75c9fc3fa9d0931af72f2e29ee68ec64a3b9463cf6c20efc6cd8d4edde75e7a2'
ARTIFACTS = ('stdout.log', 'stderr.log', 'custody.json', 'ready.json', 'result.json')
NOTIFY = b'__BPFTRACE_NOTIFY_PROBES_ATTACHED'


def check_owned(path, uid, directory):
    info = path.lstat()
    wanted_type = stat.S_ISDIR if directory else stat.S_ISREG
    if not wanted_type(info.st_mode) or info.st_uid != uid or info.st_mode & 0o022:
        raise ValueError(f'not a private josh-owned {"directory" if directory else "file"}: {path}')


def validate_paths(output_arg, script_arg):
    uid = pwd.getpwnam('josh').pw_uid
    output, script = Path(output_arg), Path(script_arg)
    if not output.is_absolute() or not script.is_absolute():
        raise ValueError('absolute paths required')
    stage = output.parent
    if stage.parent != EXPECTED_HOME or not stage.name.startswith(STAGE_PREFIX) or len(stage.name) == len(STAGE_PREFIX):
        raise ValueError('output must be a direct subdirectory of the owned issue #1210 staging root')
    if output.resolve(strict=True) != output or script.resolve(strict=True) != script:
        raise ValueError('symlinks and noncanonical paths are not allowed')
    if not script.is_relative_to(stage):
        raise ValueError('script must be inside the same owned staging root')
    check_owned(stage, uid, True)
    check_owned(output, uid, True)
    for parent in script.parents:
        if parent == stage:
            break
        check_owned(parent, uid, True)
    check_owned(script, uid, False)
    source = script.read_bytes()
    if hashlib.sha256(source).hexdigest() != SCRIPT_SHA256:
        raise ValueError('script hash differs from the compiled candidate')
    fd = os.open(output, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        opened = os.fstat(fd)
        if (opened.st_dev, opened.st_ino) != (output.stat().st_dev, output.stat().st_ino):
            raise ValueError('output directory changed during validation')
        for name in (*ARTIFACTS, *(name + '.tmp' for name in ARTIFACTS), 'stop'):
            try:
                os.stat(name, dir_fd=fd, follow_symlinks=False)
            except FileNotFoundError:
                continue
            raise ValueError(f'output already contains reserved path: {name}')
        return output, script, source, fd
    except BaseException:
        os.close(fd)
        raise


def exclusive_file(fd, name):
    opened = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644, dir_fd=fd)
    os.fchmod(opened, 0o644)
    return os.fdopen(opened, 'wb', buffering=0)


def write_json(fd, name, value):
    temporary = name + '.tmp'
    with exclusive_file(fd, temporary) as stream:
        stream.write((json.dumps(value, indent=2) + '\n').encode())
        os.fsync(stream.fileno())
    os.link(temporary, name, src_dir_fd=fd, dst_dir_fd=fd, follow_symlinks=False)
    os.unlink(temporary, dir_fd=fd)


def death_signal(signum, expected_parent):
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(1, signum, 0, 0, 0) != 0:  # PR_SET_PDEATHSIG, process-local only.
        raise OSError(ctypes.get_errno(), 'PR_SET_PDEATHSIG')
    if os.getppid() != expected_parent:
        os.kill(os.getpid(), signum)


def stop_exists(fd):
    try:
        os.stat('stop', dir_fd=fd, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False


def supervise(proc, fd, stdout_log, requested, started, outer_seconds=OUTER_SECONDS):
    ready = end_seen = False
    reason = None
    stop_at = None
    sent_term = sent_kill = False
    pending = b''
    output_open = True
    selector = selectors.DefaultSelector()
    selector.register(proc.stdout, selectors.EVENT_READ)
    try:
        while proc.poll() is None or output_open:
            now = time.monotonic()
            if reason is None:
                if requested[0]:
                    reason = requested[0]
                elif stop_exists(fd):
                    reason = 'stop-file'
                elif now >= started + outer_seconds:
                    reason = 'outer-deadline'
            if proc.poll() is None and reason is not None:
                if now >= started + outer_seconds or (stop_at is not None and now - stop_at >= 5):
                    if not sent_kill:
                        proc.kill()
                        sent_kill = True
                elif stop_at is None:
                    proc.send_signal(signal.SIGINT)
                    stop_at = now
                elif now - stop_at >= 3 and not sent_term:
                    proc.terminate()
                    sent_term = True
            for key, _ in selector.select(timeout=0.1):
                block = os.read(key.fd, 65536)
                if not block:
                    selector.unregister(key.fileobj)
                    output_open = False
                    continue
                stdout_log.write(block)
                pending += block
                while b'\n' in pending:
                    line, pending = pending.split(b'\n', 1)
                    if line == NOTIFY and not ready and proc.poll() is None:
                        write_json(fd, 'ready.json', {'ready': True, 'helper_pid': os.getpid(), 'bpftrace_pid': proc.pid, 'monotonic_ns': time.monotonic_ns(), 'script_sha256': SCRIPT_SHA256})
                        ready = True
                    if line.startswith(b'END monotonic_ns='):
                        end_seen = True
                if len(pending) > 1048576:
                    raise RuntimeError('unexpected unbounded tracer output line')
            if proc.poll() is not None and not output_open:
                break
        return {'returncode': proc.wait(), 'ready_seen': ready, 'end_seen': end_seen, 'stop_reason': reason, 'sigterm_sent': sent_term, 'sigkill_sent': sent_kill}
    finally:
        selector.close()
        proc.stdout.close()


def main():
    original_parent = os.getppid()
    if len(sys.argv) != 3:
        raise SystemExit('usage: sudo -n python3 helper.py <owned-staging-subdir> <script>')
    if sys.platform != 'linux' or os.geteuid() != 0:
        raise SystemExit('requires Linux and sudo root execution')
    output, script, source, directory_fd = validate_paths(*sys.argv[1:])
    proc = None
    requested = [None]
    previous_handlers = {}
    result = {'script_sha256': SCRIPT_SHA256, 'helper_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    try:
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            previous_handlers[signum] = signal.signal(signum, lambda sig, frame: requested.__setitem__(0, f'parent-signal-{sig}'))
        death_signal(signal.SIGTERM, original_parent)
        if requested[0]:
            raise RuntimeError(requested[0])
        argv = ['/usr/bin/bpftrace', '-B', 'line', '-k', '-', '0', '0', str(SCRIPT_SECONDS)]
        environment = os.environ.copy()
        environment['__BPFTRACE_NOTIFY_PROBES_ATTACHED'] = '1'
        custody = {**result, 'argv': argv, 'script_path': str(script), 'output': str(output), 'script_seconds': SCRIPT_SECONDS, 'outer_seconds': OUTER_SECONDS, 'started_unix_ns': time.time_ns(), 'uid': os.geteuid()}
        write_json(directory_fd, 'custody.json', custody)
        helper_pid = os.getpid()
        started = time.monotonic()
        with exclusive_file(directory_fd, 'stdout.log') as stdout_log, exclusive_file(directory_fd, 'stderr.log') as stderr_log:
            proc = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=stderr_log, env=environment, start_new_session=True, preexec_fn=lambda: death_signal(signal.SIGKILL, helper_pid))
            proc.stdin.write(source)
            proc.stdin.close()
            result.update(supervise(proc, directory_fd, stdout_log, requested, started))
    except BaseException as error:
        result['exception'] = f'{type(error).__name__}: {error}'
    finally:
        if proc is not None and proc.poll() is None:
            try:
                proc.send_signal(signal.SIGINT)
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=2)
            except ProcessLookupError:
                proc.wait(timeout=2)
        if proc is not None:
            result['returncode'] = proc.returncode
        warnings = []
        warning_count = 0
        try:
            error_fd = os.open('stderr.log', os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory_fd)
            with os.fdopen(error_fd, 'rb') as errors:
                for line in errors:
                    if any(word in line.lower() for word in (b'warning', b'error', b'lost event', b'failed')):
                        warning_count += 1
                        if len(warnings) < 50:
                            warnings.append(line.decode(errors='replace').rstrip())
        except FileNotFoundError:
            pass
        result.update({'finished_unix_ns': time.time_ns(), 'warning_lines': warning_count, 'warning_examples': warnings})
        result['capture_complete'] = result.get('returncode') == 0 and result.get('ready_seen', False) and result.get('end_seen', False) and warning_count == 0 and 'exception' not in result
        try:
            write_json(directory_fd, 'result.json', result)
        finally:
            os.close(directory_fd)
            for signum, handler in previous_handlers.items():
                signal.signal(signum, handler)
    print(json.dumps(result), flush=True)
    return 0 if result['capture_complete'] else 2


if __name__ == '__main__':
    raise SystemExit(main())
