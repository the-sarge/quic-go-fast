#!/usr/bin/env python3
"""Launch one prepared finite Q2 Linux lease outside the app execution session."""
import argparse
import importlib.util
import os
from pathlib import Path
import subprocess
import sys

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('q2_linux', HERE / 'run-linux-v5.py')
cloud = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cloud)


def detached_process(arguments, directory):
    # Redirect every inherited terminal descriptor and create a separate session.
    # The launcher exits after writing the identity; the finite child is orphaned.
    with (directory / 'run.log').open('xb') as out, (directory / 'driver.stderr').open('xb') as err:
        return subprocess.Popen(arguments, cwd=directory, stdin=subprocess.DEVNULL,
                                stdout=out, stderr=err, start_new_session=True,
                                close_fds=True, env=dict(os.environ, PYTHONUNBUFFERED='1',
                                                        PYTHONDONTWRITEBYTECODE='1'))


def main(attempt, lease):
    os.umask(0o077)
    cloud.configure(attempt)
    cloud.preflight(lease)
    cloud.save(cloud.ROOT / 'detached-intent.json', dict(attempt=attempt, lease=lease,
               automatic_retry=False, separate_session=True))
    process = detached_process(['/usr/bin/caffeinate', '-i', sys.executable,
                                str(HERE / 'run-linux-v5.py'), '--attempt', attempt,
                                '--lease', lease, '--execute'], cloud.ROOT)
    cloud.save(cloud.ROOT / 'detached-pid.json', dict(pid=process.pid,
               session_id=os.getsid(process.pid), launcher_pid=os.getpid(),
               launcher_session_id=os.getsid(0), automatic_retry=False))
    print('Started finite detached process: ' + str(process.pid))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attempt', required=True)
    parser.add_argument('--lease', required=True, choices=['Q2-01', 'Q2-02', 'Q2-03'])
    args = parser.parse_args()
    main(args.attempt, args.lease)
