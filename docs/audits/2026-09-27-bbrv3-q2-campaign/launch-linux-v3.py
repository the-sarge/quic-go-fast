#!/usr/bin/env python3
"""Launch one prepared finite Q2 Linux lease as an app-independent launchd job."""
import argparse
import importlib.util
import os
from pathlib import Path
import plistlib
import subprocess
import sys

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('q2_linux', HERE / 'run-linux-v3.py')
cloud = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cloud)


def job_definition(label, arguments, directory):
    return dict(Label=label, ProgramArguments=arguments, WorkingDirectory=str(directory),
                RunAtLoad=True, KeepAlive=False, ProcessType='Background',
                EnvironmentVariables=dict(PATH='/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin',
                                          PYTHONUNBUFFERED='1', PYTHONDONTWRITEBYTECODE='1'),
                StandardOutPath=str(directory / 'run.log'),
                StandardErrorPath=str(directory / 'driver.stderr'))


def main(attempt, lease):
    os.umask(0o077)
    cloud.configure(attempt)
    cloud.preflight(lease)
    label = 'com.the-sarge.bbr-q2.' + attempt
    domain = 'gui/' + str(os.getuid())
    path = cloud.ROOT / 'launchd.plist'
    job = job_definition(label, ['/usr/bin/caffeinate', '-i', sys.executable,
                                str(HERE / 'run-linux-v3.py'), '--attempt', attempt,
                                '--lease', lease, '--execute'], cloud.ROOT)
    with path.open('xb') as out:
        plistlib.dump(job, out)
    cloud.save(cloud.ROOT / 'launchd-intent.json', dict(label=label, domain=domain,
               keep_alive=False, automatic_retry=False, plist=str(path)))
    # A failed/ambiguous bootstrap never authorizes another dispatch.
    subprocess.run(['/bin/launchctl', 'bootstrap', domain, str(path)], check=True, timeout=15)
    result = subprocess.run(['/bin/launchctl', 'print', domain + '/' + label],
                            capture_output=True, text=True, timeout=10)
    with (cloud.ROOT / 'launchd-at-dispatch.txt').open('x') as out:
        out.write(result.stdout + result.stderr)
    print('Submitted finite launchd job: ' + domain + '/' + label)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attempt', required=True)
    parser.add_argument('--lease', required=True, choices=['Q2-01', 'Q2-02', 'Q2-03'])
    args = parser.parse_args()
    main(args.attempt, args.lease)
