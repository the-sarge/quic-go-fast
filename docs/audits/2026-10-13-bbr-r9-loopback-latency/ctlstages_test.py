#!/usr/bin/env python3
"""#744 extraction and stage-integration cases (README "Synthetic cases").

Recorder logs are generated from known segments and read back through
ctl.requests, including reordered halves, a lost original, duplicate delivery
and a stale clock calibration. Fake observation trees then exercise
ctlstages end to end: block choice under each mask, provenance against build
receipts, Stage 0 receipts, explicit gaps on empty or invalid evidence, and the
inventory's rerun admission.
"""
import json
import random
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import ctl  # noqa: E402
import ctlstages as S  # noqa: E402
import inventory  # noqa: E402

US = 1000
SEG = dict(s_pack=10, s_entry=1, s_queue=40, s_submit=1, t_req=15, r_backlog=6, r_wake=6, r_app=1, r_pack=3, r_entry=1,
           r_queue=3, r_submit=1, t_rep=12, s_backlog=6, s_wake=6)
CLOCK = dict(offset_ns=10**15, half_width_ns=30, flush_offset_ns=10**15 + 50, flush_half_width_ns=30)


class Log:
    """One endpoint's recorder log."""

    def __init__(self):
        self.events, self.pack, self.cum = [], 0, 0

    def add(self, k, t, a=0, b=0, c=0, d=0, e=0, f=0, g=0, r=0):
        self.events.append([ctl.K[k], int(t), int(a), int(b), int(c), int(d), int(e), int(f), int(g), int(r)])

    def send(self, t, lo, hi, seg, capacity, ahead=(30, 42000), retx=False, lost=False, entry=1400):
        """pack/enqueue/dequeue/submit/done of one frame [lo, hi); returns the submission-start time."""
        self.pack += 1
        p = self.pack
        self.add('pack', t, lo, hi, 1 if retx else 0, r=p)
        t += seg['pack_entry']
        self.add('enqueue', t, ahead[0], ahead[1], capacity, entry, p, self.cum, ahead[0], p)
        self.cum += entry
        t += seg['queue']
        self.add('dequeue', t, r=p)
        t += seg['submit']
        self.add('submit', t, r=p)
        self.add('done', t + 5 * US, r=p)
        return t

    def dump(self, path, role):
        Path(path, f'ctl-{role}.json').write_text(json.dumps(dict(
            role=role, fields=ctl.FIELDS, events=self.events, dropped=0, tags_exhausted=0, clock=CLOCK,
            synthetic={}, kinds=list(ctl.K))))


def logs(directory, n=120, seg_fn=None, capacity=32, edge=None, seed=1):
    """Write ctl-send.json and ctl-receive.json for n measured requests; returns the true per-request segments."""
    rng = random.Random(seed)
    s, r = Log(), Log()
    truth = []
    t = 10**12
    for k in range(1, n + 1):
        seg = {x: int(v * US * rng.uniform(0.8, 1.2)) for x, v in SEG.items()}
        if seg_fn:
            seg.update(seg_fn(k, rng))
        o, o2 = 200 + 32 * (k - 1), 1 + 32 * (k - 1)
        w = t
        s.add('write', w, o, k)
        pack_t = w + seg['s_pack']
        common = dict(pack_entry=seg['s_entry'], queue=seg['s_queue'], submit=seg['s_submit'])
        if edge == 'reordered' and k % 10 == 0:
            # Two halves in two entries; the second half arrives first, the first half 40 us later.
            sub1 = s.send(pack_t, o, o + 16, common, capacity)
            s.send(pack_t + 1, o + 16, o + 32, dict(common, queue=1), capacity)
            r.add('arrive', sub1 - 30 * US, o + 16, o + 32, sub1 - 29 * US)
            arr, hdl = sub1 + seg['t_req'], sub1 + seg['t_req'] + seg['r_backlog']
            r.add('arrive', arr, o, o + 16, hdl)
            sub = sub1
        elif edge == 'lost' and k % 10 == 0:
            s.send(pack_t, o, o + 32, common, capacity)  # never arrives
            sub = s.send(pack_t + 200 * US, o, o + 32, dict(common, queue=2 * US), capacity, retx=True)
            arr, hdl = sub + seg['t_req'], sub + seg['t_req'] + seg['r_backlog']
            r.add('arrive', arr, o, o + 32, hdl)
        else:
            sub = s.send(pack_t, o, o + 32, common, capacity)
            arr, hdl = sub + seg['t_req'], sub + seg['t_req'] + seg['r_backlog']
            r.add('arrive', arr, o, o + 32, hdl)
            if edge == 'duplicate' and k % 10 == 0:
                r.add('arrive', arr + 50 * US, o, o + 32, hdl + 50 * US)
        rd = hdl + seg['r_wake']
        r.add('read', rd, o + 32, 32)
        rw = rd + seg['r_app']
        r.add('write', rw, o2, k)
        sub2 = r.send(rw + seg['r_pack'], o2, o2 + 32, dict(pack_entry=seg['r_entry'], queue=seg['r_queue'], submit=seg['r_submit']),
                      capacity, ahead=(0, 0), entry=60)
        arr2 = sub2 + seg['t_rep']
        hdl2 = arr2 + seg['s_backlog']
        s.add('arrive', arr2, o2, o2 + 32, hdl2)
        rd2 = hdl2 + seg['s_wake']
        s.add('read', rd2, o2 + 32, 32)
        s.add('note', rd2 + 1, k, 1, 0, rd2 - w)
        truth.append(dict(id=k, total=rd2 - w, **{x: seg[x] for x in SEG}))
        t = rd2 + 1_000_000_000
    s.dump(directory, 'send')
    r.dump(directory, 'receive')
    return truth


class Extraction(unittest.TestCase):
    def test_round_trip(self):
        with tempfile.TemporaryDirectory() as d:
            truth = logs(d)
            rows, facts = ctl.requests(d)
            self.assertEqual(facts['complete'], 120)
            for row, t in zip(rows, truth):
                self.assertEqual(row['total'], t['total'])
                for seg in ctl.SEGMENTS:
                    self.assertEqual(row[seg], t[seg], seg)
            ok, reasons, _, _ = ctl.usable(Path(d), 32, 100)
            self.assertTrue(ok, reasons)

    def test_reordered_halves_complete_at_the_last_half(self):
        with tempfile.TemporaryDirectory() as d:
            truth = logs(d, edge='reordered')
            rows, facts = ctl.requests(d)
            for row, t in zip(rows, truth):
                self.assertEqual(row['r_wake'], t['r_wake'])  # completion is the last half, not the earliest arrival
            self.assertEqual(facts['split'], 12)
            self.assertEqual(facts['ambiguous'], 0)

    def test_lost_original_is_ambiguous_and_its_recovery_is_residual(self):
        with tempfile.TemporaryDirectory() as d:
            truth = logs(d, edge='lost')
            rows, facts = ctl.requests(d)
            self.assertEqual(facts['ambiguous'], 12)
            self.assertEqual(facts['retransmitted'], 12)
            for row, t in zip(rows, truth):
                self.assertEqual(row['total'], t['total'])
                self.assertEqual(sum(row[s] for s in ctl.SEGMENTS), row['total'])
                if row['ambiguous']:
                    self.assertGreater(row['t_req'], 150 * US)

    def test_duplicate_delivery_uses_the_first_completion(self):
        with tempfile.TemporaryDirectory() as d:
            truth = logs(d, edge='duplicate')
            rows, _ = ctl.requests(d)
            for row, t in zip(rows, truth):
                self.assertEqual(row['r_backlog'], t['r_backlog'])

    def test_clock_limits(self):
        with tempfile.TemporaryDirectory() as d:
            logs(d)
            p = Path(d, 'ctl-send.json')
            j = json.loads(p.read_text())
            j['clock']['half_width_ns'] = 5000
            p.write_text(json.dumps(j))
            ok, reasons, _, _ = ctl.usable(Path(d), 32, 100)
            self.assertFalse(ok)
            self.assertTrue(any('clock' in x for x in reasons))

    def test_capacity_and_completeness(self):
        with tempfile.TemporaryDirectory() as d:
            logs(d, capacity=8)
            self.assertFalse(ctl.usable(Path(d), 32, 100)[0])
            self.assertTrue(ctl.usable(Path(d), 8, 100)[0])
        with tempfile.TemporaryDirectory() as d:
            logs(d, n=60)
            self.assertFalse(ctl.usable(Path(d), 32, 100)[0])
            self.assertTrue(ctl.usable(Path(d), 32, 50)[0])


# ---- fake observation trees ------------------------------------------------------------------------

def observation(root, phase, path, workload, block, variant, controller, tag='', p95_ms=0.2, goodput=5000.0, recorder=False,
                capacity=32, binary='bin', source='src', exit_codes=(0, 0), contaminated=False, seed=0, n=120):
    name = f'{phase}-{path}-{workload}-p{block}-{variant}-{controller}' + (f'-{tag}' if tag else '')
    d = Path(root, name)
    d.mkdir(parents=True)
    rng = random.Random(seed)
    lat = [int(p95_ms * 1e6 * rng.uniform(0.3, 1.0)) for _ in range(n)]
    measure_ms = 120000 if path == 'loopback-long' else 20000
    cfg = dict(id=name, controller=controller, workload=workload, start_unix_ns=0, payload_bytes=16384, warmup_ms=5000, measure_ms=measure_ms)
    useful = int(goodput * 1e6 / 8 * measure_ms / 1000)
    receiver = dict(corrupt=0, duplicates=0, per_second_bytes=[useful], useful_bytes=useful)
    res = dict(cpu_seconds=10.0, peak_rss_bytes=2**24, total_alloc_bytes=2**30)
    tx = dict(source=source, result=dict(run=cfg, receiver=receiver, controller=controller,
                                         control=dict(latency_ns=lat, replies=n, skipped=0, unresolved=0, missed_opportunities=0)),
              resources=res, trace_enabled=False)
    rx = dict(source=source, result=dict(run=cfg, receiver=receiver, controller=controller), resources=res, trace_enabled=False)
    for f, v in (('config.json', cfg), ('send.json', tx), ('receive.json', rx),
                 ('meta.json', dict(phase=phase, variant=variant, pair=block, path=path, seed=None, tag=tag)),
                 ('receipt.json', dict(exit_codes=list(exit_codes), relay_exit=None, binary_sha256=binary, relay_sha256=None)),
                 ('host-cpu.json', []), ('host-facts.json', dict(measure_window_unix_ns=[0, 1]))):
        (d / f).write_text(json.dumps(v))
    if contaminated:
        (d / 'CONTAMINATED').write_text('')
    if recorder:
        logs(d, n=n, capacity=capacity, seed=seed)
    return d


class Stages(unittest.TestCase):
    VARIANTS = ['reno', 'cand', 'cand8', 'cand-ctl', 'cand8-ctl', 'new'] + [f'syn-{c}-ctl' for c in S.SYN_CASES]

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        t = Path(self.tmp.name)
        self.saved = (S.ROOTS, S.OUT, S.BUILDS, S.contaminated)
        S.ROOTS = [t / 'observations', t / 'observations-rerun']
        S.OUT, S.BUILDS = t / 'results.json', t / 'builds'
        S.BUILDS.mkdir()
        S.contaminated = lambda d: (Path(d) / 'CONTAMINATED').exists()
        for v in self.VARIANTS:
            (S.BUILDS / f'{v}-build.json').write_text(json.dumps(dict(builds=dict(fixture=dict(
                sha256=f'sha-{v}', command=['go', 'build', '-ldflags', f'-X main.sourceRevision=label-{v}'])))))

    def tearDown(self):
        S.ROOTS, S.OUT, S.BUILDS, S.contaminated = self.saved
        self.tmp.cleanup()

    def s1_block(self, b, root=0, recorder_ok=True, swap=False, **kw):
        arms = [('reno', 'reno', ''), ('cand', 'bbrv3', ''), ('cand', 'bbrv3', 'aa'), ('cand8', 'bbrv3', ''),
                ('cand-ctl', 'bbrv3', ''), ('cand8-ctl', 'bbrv3', '')]
        p95 = {'reno': 0.14, 'cand': 0.26, 'cand8': 0.16, 'cand-ctl': 0.26, 'cand8-ctl': 0.16}
        for i, (v, c, tag) in enumerate(arms):
            rec = v.endswith('-ctl')
            binv = {'cand': 'cand8', 'cand8': 'cand'}.get(v, v) if swap else v
            observation(S.ROOTS[root], 's1', 'loopback-long', 'stream', b, v, c, tag, p95_ms=p95[v], recorder=rec and recorder_ok,
                        capacity=8 if v == 'cand8-ctl' else 32, binary=f'sha-{binv}', source=f'label-{binv}', seed=b * 10 + i, **kw)

    def test_recorder_failure_keeps_plain_evidence(self):
        for b in range(1, 7):
            self.s1_block(b, recorder_ok=False)
        res = S.s1()
        self.assertEqual(res['clean_blocks'], dict(plain=6, recorder=0))
        self.assertNotEqual(res['depth']['ending'], 'evidence gap')
        self.assertIn(res['mechanism']['ending'], ('evidence gap', 'instrumentation gap'))
        self.assertEqual(res['perturbation']['state'], 'insufficient')
        self.assertTrue(json.loads(S.OUT.read_text())['stage1'])

    def test_empty_stage_writes_explicit_gaps(self):
        res = S.s1()
        self.assertEqual(res['depth']['ending'], 'evidence gap')
        self.assertEqual(res['baseline']['ending'], 'evidence gap')
        self.assertIn(res['mechanism']['ending'], ('evidence gap', 'instrumentation gap'))
        syn = S.syn()
        self.assertFalse(syn['passes'])
        self.assertTrue(all(c['ending'] == 'evidence gap' for c in syn['cases'].values()))
        s2 = S.s2()
        self.assertEqual(s2['outcome'], 'not attempted')

    def test_swapped_binaries_are_unusable(self):
        for b in range(1, 7):
            self.s1_block(b, swap=True)
        res = S.s1()
        self.assertEqual(res['clean_blocks']['plain'], 0)
        ev = res['blocks'][0]['evidence']['original']['unusable']
        self.assertTrue(any('executable' in r for r in ev['cand-bbrv3']))

    def test_stage0_prerequisite_binds_to_the_fingerprint(self):
        pass_ = dict(state='pass')
        self.assertEqual(S.stage0_prerequisite(pass_)['state'], 'instrumentation gap')  # missing receipts
        fp = S.fingerprint()
        S.save('stage0_data_level', dict(passes=True, fingerprint=fp))
        S.save('stage0_synthetic_runtime', dict(passes=True, fingerprint=fp))
        self.assertEqual(S.stage0_prerequisite(pass_)['state'], 'pass')
        self.assertEqual(S.stage0_prerequisite(dict(state='fail'))['state'], 'instrumentation gap')
        self.assertEqual(S.stage0_prerequisite(dict(state='insufficient'))['state'], 'evidence gap')
        S.save('stage0_synthetic_runtime', dict(passes=True, fingerprint='stale'))
        p = S.stage0_prerequisite(pass_)
        self.assertEqual(p['state'], 'instrumentation gap')
        self.assertTrue(any('stale' in r for r in p['reasons']))
        S.save('stage0_synthetic_runtime', dict(passes=False, fingerprint=fp))
        self.assertEqual(S.stage0_prerequisite(pass_)['state'], 'instrumentation gap')

    def test_rerun_choice_and_admission(self):
        for b in range(1, 7):
            self.s1_block(b, contaminated=(b == 2))
        rec = S.chosen('s1')
        self.assertEqual([r['status'] for r in rec], ['clean', 'contaminated', 'clean', 'clean', 'clean', 'clean'])
        ok, why = inventory.admit_rerun('s1', 'stream', 2, rec, S.ROOTS, 6)
        self.assertTrue(ok, why)
        self.assertFalse(inventory.admit_rerun('s1', 'stream', 1, rec, S.ROOTS, 6)[0])  # clean
        self.assertFalse(inventory.admit_rerun('s2-long', 'stream', 2, S.chosen('s2-long'), S.ROOTS, 4)[0])  # no such block there
        self.assertFalse(inventory.admit_rerun('s9', 'stream', 2, rec, S.ROOTS, 6)[0])
        self.s1_block(2, root=1)  # the clean same-seed rerun replaces the block
        rec = S.chosen('s1')
        self.assertEqual(rec[1]['status'], 'clean')
        self.assertEqual(rec[1]['masks']['plain']['counted'], 'rerun')
        self.assertFalse(inventory.admit_rerun('s1', 'stream', 2, rec, S.ROOTS, 6)[0])  # already rerun

    def test_caps(self):
        rec = [dict(workload='stream', block=1, status='contaminated', attempts=[('original',)])]
        roots = S.ROOTS
        for i in range(175):
            (roots[0] / f's1-x-{i}').mkdir(parents=True)
        self.assertFalse(inventory.admit_rerun('s1', 'stream', 1, rec, roots, 6)[0])  # 175 + 6 > 180
        self.assertTrue(inventory.admit_rerun('syn', 'stream', 1, rec, roots, 8)[0])  # synthetic reruns are not capped
        for i in range(20):
            (roots[1] / f's2-y-{i}').mkdir(parents=True)
        ok, why = inventory.admit_rerun('s2-long', 'stream', 1, rec, [roots[0].parent / 'none', roots[1]], 4)
        self.assertTrue(ok, why)  # 20 + 4 = 24 reruns
        ok, why = inventory.admit_rerun('s2-loopback', 'stream', 1, rec, [roots[0].parent / 'none', roots[1]], 6)
        self.assertFalse(ok)
        self.assertIn('rerun cap', why)


if __name__ == '__main__':
    unittest.main()
