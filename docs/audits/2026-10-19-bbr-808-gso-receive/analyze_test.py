#!/usr/bin/env python3
"""Tests for #808's side-stage analysis (analyze.py) on synthetic runs: the incorrect-stage rule, round selection
and the empty cases."""
import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parent))
import analyze as A  # noqa: E402

MODE = {'S-pre': 'stream', 'S-rev': 'stream', 'S-bbr': 'stream'}


def run(arm, rnd, contaminated=False, controller=None, mbps=1000.0):
    cc = controller or A.CONTROLLER[arm]
    send = dict(controller=cc, cpu_user_s=1.0, cpu_sys_s=0.0, sendmsg_segments=10, sendmsg_calls=1, min_rtt_us=10, srtt_us=20)
    recv = dict(controller=A.CONTROLLER[arm], bytes=2**30, mbps=mbps, cpu_user_s=1.0, cpu_sys_s=0.0)
    if MODE.get(arm) != 'stream':
        send['sent_datagrams'] = 1000
        recv.update(datagrams=1000, datagram_queue_drops=0, unprocessed_drops=0, socket_drops=0, drops_v1=0)
    return dict(arm=arm, round=rnd, send=send, recv=recv, recv_maxrss_kb=1000, foreign_s=0.0, contaminated=contaminated)


def write(rows):
    f = tempfile.NamedTemporaryFile('w', suffix='.jsonl', delete=False)
    f.write(''.join(json.dumps(r) + '\n' for r in rows))
    f.close()
    return f.name


def rounds(n, overrides=None):
    overrides = overrides or {}
    rows = []
    for r in range(1, n + 1):
        for arm in A.ARMS:
            kw = overrides.get((r, arm), {})
            rows.append(run(arm, r, mbps=980.0 if arm == 'S-rev' else 1000.0, **kw))
    return rows


def analyze(counted, rerun):
    out = io.StringIO()
    with redirect_stdout(out):
        A.main(['analyze.py', write(counted), write(rerun)])
    return json.loads(out.getvalue())


class Correctness(unittest.TestCase):
    def test_clean_control(self):
        s = analyze(rounds(2), [])
        self.assertEqual(s['status'], 'complete')
        self.assertAlmostEqual(s['readings']['K3_rev_over_pre_mbps'], 0.98)
        self.assertEqual(s['readings']['K3_reading'], 'persists')

    def test_mismatch_in_replaced_original(self):
        counted = rounds(2, {(1, 'D-bbr'): dict(contaminated=True, controller='reno')})
        s = analyze(counted, [r for r in rounds(1)])
        self.assertEqual((s['status'], s['readings']), ('incorrect', None))
        self.assertEqual(s['controller_mismatch'], ['counted round 1 D-bbr'])

    def test_mismatch_in_excluded_original(self):
        counted = rounds(2, {(2, 'S-pre'): dict(contaminated=True, controller='bbrv3')})
        rerun = [dict(r, round=2, contaminated=True) for r in rounds(1)]
        s = analyze(counted, rerun)
        self.assertEqual((s['status'], s['readings']), ('incorrect', None))

    def test_mismatch_in_discarded_rerun(self):
        counted = rounds(2, {(1, 'M-bbr'): dict(contaminated=True)})
        rerun = rounds(1, {(1, 'Dk-bbr'): dict(contaminated=True, controller='reno')})
        s = analyze(counted, rerun)
        self.assertEqual(s['status'], 'incorrect')
        self.assertEqual(s['controller_mismatch'], ['rerun round 1 Dk-bbr'])

    def test_mismatch_in_retained_round(self):
        s = analyze(rounds(2, {(2, 'S-rev'): dict(controller='bbrv3')}), [])
        self.assertEqual((s['status'], s['readings']), ('incorrect', None))


class Selection(unittest.TestCase):
    def test_all_rounds_excluded(self):
        counted = [dict(r, contaminated=True) for r in rounds(2)]
        s = analyze(counted, [dict(r, contaminated=True) for r in rounds(2)])
        self.assertEqual((s['status'], s['readings'], s['rounds_used']), ('incomplete', None, []))
        self.assertEqual(len(s['notes']), 2)

    def test_clean_rerun_replaces(self):
        counted = rounds(2, {(2, 'S-rev'): dict(contaminated=True)})
        s = analyze(counted, [dict(r, round=2) for r in rounds(1)])
        self.assertEqual(s['rounds_used'], [1, 2])
        self.assertEqual(s['notes'], ['round 2 replaced by its clean rerun'])

    def test_missing_rerun_file_is_an_error(self):
        with self.assertRaises(FileNotFoundError):
            A.main(['analyze.py', write(rounds(1)), '/nonexistent/rerun.jsonl'])


if __name__ == '__main__':
    unittest.main()
