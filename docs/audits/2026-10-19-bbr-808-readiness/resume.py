#!/usr/bin/env python3
"""Complete a stage after a failed observation, without replacing any attempt.

Written before any observation of this record (registration review finding
CLAUDEFABLE-F-001). #739's `run.run_case` and `stage_run.run_case_x` raise
after writing a failed observation's receipt, and refuse to pass a retained
failed attempt when the stage is invoked again, so one failed run would stop
the unattended order. This helper runs the stage's registered blocks through
matrix.py's unchanged `block` and STAGES table, with a runner that returns a
completed observation as it is (run_case's own behaviour), leaves a failed or
incomplete attempt in place, and runs every observation that does not exist
yet. The failed block is then rerun once, with the same seed, by follow.py's
unchanged rule. More than MAX_FAILED failed attempts in one stage stop it, so a
systematic failure ends the order for the operator's session instead of
running through the window.

`mark` handles an attempt that ended before its receipt was written (a
timeout, or an endpoint that never became ready): stages.py and follow.py find
attempts by their receipt.json, so such an attempt would be invisible to the
rerun rule. `mark` writes a marker receipt (exit_codes null, `incomplete`) into
every attempt directory that has meta.json and config.json but no receipt, and
changes nothing else. stages.py then reads the attempt as unusable, so its
block is rerun once and the attempt is left out of every analysis view.

Usage: resume.py <stage>   (under taskset -c 1-3, as matrix.py)
       resume.py mark
"""
import json
import time
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import matrix as M  # noqa: E402  (imports art first, so run.ART/OBS are this record's)
import run  # noqa: E402

MAX_FAILED = 3


def mark():
    marked = []
    for root in [run.ART / 'observations', run.ART / 'observations-rerun']:
        for meta in sorted(root.glob('*/meta.json')):
            d = meta.parent
            if (d / 'config.json').exists() and not (d / 'receipt.json').exists():
                run.write(d / 'receipt.json', dict(exit_codes=None, incomplete=True, marked_by='resume.py mark', unix_ns=time.time_ns()))
                marked.append(str(d.relative_to(run.ART)))
    print('marked incomplete attempts:', marked, flush=True)
    log = run.ART / 'resume-marks.json'
    prior = json.loads(log.read_text()) if log.exists() else []
    run.write(log, prior + marked)
    return marked


def main(stage):
    phase, path, arms, n, base, runner, opts, workload = M.STAGES[stage]
    runner = runner or run.run_case
    failed = []

    def tolerant(*args, **kw):
        try:
            return runner(*args, **kw)
        except (AssertionError, FileNotFoundError, KeyError, ValueError) as e:
            failed.append(repr(e))
            print('kept failed attempt:', repr(e), flush=True)
            if len(failed) > MAX_FAILED:
                raise SystemExit(f'{stage}: more than {MAX_FAILED} failed attempts; stopping for the operator')
            return None

    run.ART.mkdir(parents=True, exist_ok=True)
    run.ensure_credentials()
    rows = []
    for b in range(1, n + 1):
        rows += M.block(phase, path, arms, b, seed=base + b if base else None, runner=tolerant, only=workload, **opts)
    run.write(run.ART / f'{stage}-resume-summary.json',
              dict(failed=failed, rows=[dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows if r]))


if __name__ == '__main__':
    mark() if sys.argv[1] == 'mark' else main(sys.argv[1])
