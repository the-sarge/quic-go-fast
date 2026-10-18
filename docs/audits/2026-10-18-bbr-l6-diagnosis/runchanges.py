#!/usr/bin/env python3
"""The recorded changes from #777's run.py (71e88789) to this record's run.py.

`python3 runchanges.py write` regenerates run.py from #777's file; the test
(runchanges_test.py) checks that run.py is exactly #777's file with these
substitutions, so the runner is otherwise unchanged.
"""
import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent
L6_RUN = HERE.parent / '2026-10-17-bbr-l6-coexistence' / 'run.py'

DOCSTRING_777 = '"""Finite, local-only BBRv3 L6 coexistence runner for one qualification ticket (#777).\n'
DOCSTRING_792 = '''"""Finite, local-only BBRv3 L6 diagnosis runner for one measurement ticket (#792).

#777's run.py (71e88789) with the recorded changes in runchanges.py, and
nothing else: this record's artifact directory; one more arm, `trace` (the
`cand-trace` fixture, the port with #792's measurement-only overlay, selecting
BBRv3); and, for that arm's focal sender only, the overlay's two environment
variables (its output file in the observation directory, and an end time 2 s
after the measured window, so losses the sender declares up to then are kept).
#777's docstring follows.

'''

SUBSTITUTIONS = [
    (DOCSTRING_777, DOCSTRING_792 + DOCSTRING_777[3:]),
    ("ART = ROOT / '.local/bbr-l6-coexistence'\n", "ART = ROOT / '.local/bbr-l6-diagnosis'\n"),
    ("ARMS = {'reno': ('reno', 'reno'), 'cand': ('cand', 'bbrv3'), 'aa': ('reno', 'reno')}  # arm: (fixture variant, controller)\n",
     "ARMS = {'reno': ('reno', 'reno'), 'cand': ('cand', 'bbrv3'), 'aa': ('reno', 'reno'),\n"
     "        'trace': ('cand-trace', 'bbrv3')}  # arm: (fixture variant, controller)\n"
     "TRACE_TAIL_NS = 2_000_000_000  # the overlay records until 2 s after the measured window\n"
     "\n"
     "\n"
     "def trace_env(arm, role, directory, measure_end_ns):\n"
     "    \"\"\"The environment of one endpoint: the overlay's variables for the trace arm's focal sender only.\"\"\"\n"
     "    if arm != 'trace' or role != 'focal-send':\n"
     "        return None\n"
     "    import os\n"
     "    return {**os.environ, 'BBR_TRACE_OUTPUT': str(directory / 'focal-trace.json'),\n"
     "            'BBR_TRACE_UNTIL_UNIX_NS': str(measure_end_ns + TRACE_TAIL_NS)}\n"),
    ("                                    stdout=out, stderr=err)\n",
     "                                    stdout=out, stderr=err, env=trace_env(arm, role, directory, measure_end))\n"),
]


def generate():
    s = L6_RUN.read_text()
    for old, new in SUBSTITUTIONS:
        assert s.count(old) == 1, old
        s = s.replace(old, new)
    return s


if __name__ == '__main__':
    if sys.argv[1:] == ['write']:
        (HERE / 'run.py').write_text(generate())
        print('run.py written')
    else:
        raise SystemExit(__doc__)
