#!/usr/bin/env python3
"""Synthetic cases for #777's registered rules (l6rules.py) and the runner's period assignment (run.py).

Run before any observation of this record existed: python3 l6rules_test.py
"""
import json
import os
import unittest
from pathlib import Path

import l6rules as r
import run

PERIODS = list(run.PERIODS)


def fake(comp, focal=50.0, comp_p95=150.0, focal_p95=150.0, cpu=10.0, rss=20.0):
    """A summary whose competitor delivers `comp` Mbit/s in every period unless comp is a dict."""
    comp = comp if isinstance(comp, dict) else {p: comp for p in PERIODS}
    side = lambda mbps, p95: dict(periods={p: dict(mbps=mbps[p] if isinstance(mbps, dict) else mbps) for p in PERIODS},
                                  control_periods=dict(method='exact', periods={p: dict(p95_ms=p95) for p in PERIODS}),
                                  control_p95_ms=p95, send=dict(cpu_seconds_per_gib=cpu, peak_rss_mib=rss),
                                  receive=dict(cpu_seconds_per_gib=cpu, peak_rss_mib=rss))
    return dict(competitor=side(comp, comp_p95), focal=side(focal, focal_p95))


def triples(cand_comp, reno_comp=50.0, aa_comp=50.0, **kw):
    """Every pair of both workloads alike, unless cand_comp is a list (one value per pair)."""
    out = {}
    for w in r.WORKLOADS:
        for pair in range(1, r.PAIRS + 1):
            c = cand_comp[pair - 1] if isinstance(cand_comp, list) else cand_comp
            out[(w, pair)] = dict(reno=fake(reno_comp), cand=fake(c, **kw), aa=fake(aa_comp))
    return out


class Classification(unittest.TestCase):
    def test_period_outcomes(self):
        cases = [
            ([1.0] * 5, 'acceptable'),
            ([0.5] * 5, 'adverse'),
            ([0.8, 0.8, 0.8, 0.6, 0.6], 'inconclusive'),
            ([0.8, 0.8, 0.8, 0.8, 0.6], 'acceptable'),
            ([0.6, 0.6, 0.6, 0.6, 0.9], 'adverse'),
            ([0.70] * 4 + [0.1], 'acceptable'),        # the threshold itself is acceptable
            ([0.6999] * 4 + [1.0], 'adverse'),
            ([None, 0.8, 0.8, 0.8, 0.6], 'inconclusive'),
            ([None, 0.8, 0.8, 0.8, 0.8], 'acceptable'),
            ([None, None, 0.5, 0.5, 0.5], 'inconclusive'),
            ([None] * 5, 'inconclusive'),
        ]
        for ratios, want in cases:
            self.assertEqual(r.classify_period(ratios), want, ratios)

    def test_needs_five_pairs(self):
        with self.assertRaises(AssertionError):
            r.classify_period([1.0] * 4)

    def test_overall(self):
        cells = {(w, p): 'acceptable' for w in r.WORKLOADS for p in r.CLASSIFIED}
        self.assertEqual(r.overall(cells), 'acceptable')
        self.assertEqual(r.overall({**cells, ('datagram', 'resumed'): 'adverse'}), 'adverse')
        self.assertEqual(r.overall({**cells, ('stream', 'short'): 'inconclusive'}), 'inconclusive')
        self.assertEqual(r.overall({**cells, ('stream', 'short'): 'inconclusive', ('stream', 'equal'): 'adverse'}), 'adverse')
        with self.assertRaises(AssertionError):
            r.overall({('stream', 'equal'): 'acceptable'})

    def test_denominator_floor(self):
        self.assertIsNone(r.pair_ratio(10.0, 4.99))
        self.assertAlmostEqual(r.pair_ratio(10.0, 5.0), 2.0)
        self.assertIsNone(r.pair_ratio(None, 50.0))


class Analysis(unittest.TestCase):
    def test_end_to_end_outcomes(self):
        self.assertEqual(r.analyze(triples(50.0))['classification'], 'acceptable')
        self.assertEqual(r.analyze(triples(20.0))['classification'], 'adverse')   # 0.40
        self.assertEqual(r.analyze(triples(35.0))['classification'], 'acceptable')  # exactly 0.70
        self.assertEqual(r.analyze(triples([40, 40, 40, 30, 30]))['classification'], 'inconclusive')

    def test_one_adverse_period_decides(self):
        comp = {p: 50.0 for p in PERIODS}
        comp['resumed'] = 10.0
        t = triples(50.0)
        for pair in range(1, r.PAIRS + 1):
            t[('datagram', pair)]['cand'] = fake(comp)
        out = r.analyze(t)
        self.assertEqual(out['cells']['datagram/resumed'], 'adverse')
        self.assertEqual(out['cells']['stream/resumed'], 'acceptable')
        self.assertEqual(out['classification'], 'adverse')

    def test_unclassified_periods_never_decide(self):
        comp = {p: 50.0 for p in PERIODS}
        comp['back'], comp['absent'] = 1.0, 0.0
        t = triples(50.0)
        for k in t:
            t[k]['cand'] = fake(comp)
        self.assertEqual(r.analyze(t)['classification'], 'acceptable')

    def test_aa_never_changes_the_outcome(self):
        for cand in [50.0, 20.0, [40, 40, 40, 30, 30]]:
            base = r.analyze(triples(cand))['classification']
            for aa in [0.0, 5.0, 500.0]:
                self.assertEqual(r.analyze(triples(cand, aa_comp=aa))['classification'], base)

    def test_missing_triples_make_a_period_inconclusive(self):
        t = triples(50.0)
        t[('stream', 1)] = t[('stream', 2)] = None
        out = r.analyze(t)
        self.assertEqual(out['cells']['stream/equal'], 'inconclusive')
        self.assertEqual(out['classification'], 'inconclusive')

    def test_review_flags_never_change_the_outcome(self):
        out = r.analyze(triples(50.0, comp_p95=400.0, focal_p95=900.0, cpu=99.0, rss=99.0))
        self.assertEqual(out['classification'], 'acceptable')
        self.assertTrue(out['competitor_control']['stream/equal']['raised'])
        self.assertTrue(out['focal_flags']['stream/control_p95']['raised'])
        self.assertTrue(out['focal_flags']['datagram/send_peak_rss']['raised'])
        self.assertFalse(r.analyze(triples(50.0))['focal_flags']['stream/receive_cpu_per_gib']['raised'])


class Usability(unittest.TestCase):
    def summary(self, pause=(240.01, 270.0), lag=0.5, late=2.0, cpu=30.0, error=None):
        side = lambda: dict(control_error=error, send=dict(total_cpu_seconds=cpu), receive=dict(total_cpu_seconds=cpu))
        return dict(competitor=dict(side(), bulk_pause_observed_s=list(pause)), focal=side(),
                    relay=dict(max_arrival_lag_ms=lag, forward=dict(max_late_ms=late), reverse=dict(max_late_ms=late)))

    def test_health(self):
        ok = dict(exit_codes=[0, 0, 0, 0], relay_exit=0)
        self.assertEqual(r.health(self.summary(), ok, 370), [])
        self.assertTrue(r.health(self.summary(), dict(ok, exit_codes=[0, 1, 0, 0]), 370))
        self.assertTrue(r.health(None, ok, 370))
        self.assertTrue(r.health(self.summary(pause=(241.0, 270.0)), ok, 370))
        self.assertTrue(r.health(self.summary(pause=()), ok, 370))
        self.assertTrue(r.health(self.summary(lag=6.0), ok, 370))
        self.assertTrue(r.health(self.summary(late=5.1), ok, 370))
        self.assertTrue(r.health(self.summary(error='timeout'), ok, 370))
        # 600 CPU seconds over 370 s is 0.81 of the competitor's two cores.
        self.assertTrue(any('competitor' in x for x in r.health(self.summary(cpu=600.0), ok, 370)))

    def test_contamination(self):
        def rows(foreign_ticks):
            out, busy = [], 0
            for i, f in enumerate(foreign_ticks):
                busy += f
                out.append(dict(unix_ns=i * 1_000_000_000, cpus={str(c): dict(busy=busy if c == 2 else 0) for c in range(32)},
                                fixture_ticks={'relay': 0}))
            return out
        quiet = r.contamination(rows([0] * 20), [0, 30e9], [2, 3], [18, 19])
        self.assertFalse(quiet['contaminated'])
        busy = r.contamination(rows([0] * 10 + [60] + [0] * 9), [0, 30e9], [2, 3], [18, 19])
        self.assertTrue(busy['contaminated'])  # one 0.6-core second exceeds the 0.50 peak

    @staticmethod
    def report(errors=(), control_error='', top='', corrupt=0, duplicates=0):
        return dict(error=top, result=dict(errors=list(errors), control=dict(error=control_error),
                                           receiver=dict(corrupt=corrupt, duplicates=duplicates)))

    def test_correctness(self):
        clean = self.report()
        self.assertEqual(r.correctness({'focal-receive': clean, 'competitor-send': clean, 'focal-send': None}), [])
        self.assertEqual(len(r.correctness({'focal-receive': self.report(corrupt=1), 'competitor-receive': self.report(duplicates=2)})), 2)
        # Each fixture integrity error counts with zero bulk counters, wherever it is reported.
        for e in r.INTEGRITY_ERRORS:
            self.assertTrue(r.correctness({'focal-receive': self.report(errors=[e])}), e)
            self.assertTrue(r.correctness({'competitor-send': self.report(top=e)}), e)
        self.assertTrue(r.correctness({'focal-send': self.report(control_error='control sequence mismatch')}))
        # Ordinary operational failures are not correctness failures.
        for e in ['timeout: no recent network activity', 'context deadline exceeded', 'start deadline missed during setup',
                  'competitor bulk pause was missed']:
            self.assertEqual(r.correctness({'focal-receive': self.report(errors=[e], top=e)}), [], e)

    def test_protocol_errors_and_report_divergence(self):
        for e in ['PROTOCOL_VIOLATION (remote): bad frame', 'FLOW_CONTROL_ERROR (local): window exceeded']:
            self.assertTrue(r.correctness({'competitor-receive': self.report(top=e)}), e)
            self.assertTrue(r.correctness({'focal-send': self.report(errors=[e])}), e)
        for e in ['CRYPTO_ERROR 0x12a (local): tls: bad certificate', 'INTERNAL_ERROR (local): x', 'NO_ERROR (remote)']:
            self.assertEqual(r.correctness({'focal-send': self.report(top=e)}), [], e)
        send, receive = self.report(), self.report()
        send['result']['receiver'] = dict(corrupt=0, duplicates=0, useful_bytes=1)
        receive['result']['receiver'] = dict(corrupt=0, duplicates=0, useful_bytes=2)
        self.assertTrue(r.correctness({'focal-send': send, 'focal-receive': receive}))
        send['error'] = 'context deadline exceeded'  # an incomplete exchange is not a divergence
        self.assertEqual(r.correctness({'focal-send': send, 'focal-receive': receive}), [])

    def test_udp_errors(self):
        self.assertEqual(r.udp_reasons({'InErrors': 0, 'RcvbufErrors': 0, 'SndbufErrors': 0, 'MemErrors': 0}), [])
        reasons = r.udp_reasons({'InErrors': 3, 'RcvbufErrors': 3, 'SndbufErrors': 0, 'MemErrors': 0})
        self.assertEqual(len(reasons), 1)
        self.assertIn('host-wide', reasons[0])
        self.assertNotIn('focal', reasons[0])

    def test_unevaluable_contamination_is_unusable(self):
        usable, dirty, reasons = r.verdict([], dict(contaminated=None, reason='no samples in window'))
        self.assertFalse(usable)
        self.assertTrue(any('not evaluable' in x for x in reasons))
        self.assertEqual(r.verdict([], dict(contaminated=False))[:2], (True, False))
        self.assertEqual(r.verdict([], dict(contaminated=True))[:2], (True, True))
        self.assertEqual(r.verdict(['process exit'], dict(contaminated=False))[:2], (False, False))
        self.assertNotEqual(r.choose_triple([('main', True, None)])[1], 'clean')
        self.assertEqual(r.choose_triple([('main', True, None), ('rerun', True, False)]), ('rerun', 'clean'))

    def test_choose_triple(self):
        self.assertEqual(r.choose_triple([('a', True, False)]), ('a', 'clean'))
        self.assertEqual(r.choose_triple([('a', True, True), ('b', True, False)]), ('b', 'clean'))
        self.assertEqual(r.choose_triple([('a', False, False), ('b', True, True)]), ('a', 'unusable'))
        self.assertEqual(r.choose_triple([('a', True, True), ('b', False, False)]), ('a', 'contaminated'))
        self.assertEqual(r.choose_triple([('a', True, True), ('b', True, True), ('c', True, False)]), ('a', 'contaminated'))


class Orchestration(unittest.TestCase):
    """matrix.py and driver.sh behavior, on temporary directories only."""

    def setUp(self):
        import tempfile
        import matrix
        self.matrix = matrix
        self.tmp = Path(tempfile.mkdtemp())
        self.saved = (run.ART, run.OBS)
        run.ART, run.OBS = self.tmp, self.tmp / 'observations'
        run.OBS.mkdir()

    def tearDown(self):
        import shutil
        run.ART, run.OBS = self.saved
        shutil.rmtree(self.tmp)

    def test_integrity_error_stops_even_without_a_summary(self):
        d = run.OBS / 'main-L6-stream-p1-cand'
        d.mkdir()
        (d / 'focal-receive.json').write_text(json.dumps(Usability.report(errors=['non-contiguous stream identity'])))
        with self.assertRaises(SystemExit):
            self.matrix.correctness(d)
        self.assertTrue((self.tmp / 'correctness-failure.json').exists())
        e = run.OBS / 'main-L6-stream-p1-reno'
        e.mkdir()
        (e / 'focal-receive.json').write_text(json.dumps(Usability.report(errors=['timeout: no recent network activity'])))
        self.matrix.correctness(e)  # an operational failure does not stop the stage

    def write_obs(self, name, receipt=True, pause=(1_000 + 240 * 10**9 + 60 * 10**9, 1_000 + 270 * 10**9 + 60 * 10**9), errors=()):
        d = run.OBS / name
        d.mkdir()
        (d / 'competitor-config.json').write_text(json.dumps(dict(start_unix_ns=1_000, warmup_ms=60000)))
        rep = Usability.report(errors=list(errors))
        rep['result']['bulk_pause_observed_ns'] = list(pause)
        rep['resources'] = dict(cpu_seconds=10.0, peak_rss_bytes=1 << 20)
        (d / 'competitor-send.json').write_text(json.dumps(rep))
        if receipt:
            (d / 'receipt.json').write_text(json.dumps(dict(start_unix_ns=0, end_unix_ns=370 * 10**9, exit_codes=[0, 1, 0, 0], relay_exit=0,
                                                         binary_sha256={'focal': 'x', 'competitor': 'y'}, relay_sha256='z')))
        return d

    def test_diagnostics_survive_without_summary_or_receipt(self):
        expected = self.matrix.registered_hashes()
        d = self.write_obs('main-L6-stream-p1-cand', receipt=False, errors=['competitor bulk pause was missed'])
        v = self.matrix.observation(d, 'cand', expected)
        self.assertFalse(v['usable'])
        raw = v['raw']['competitor-send']
        self.assertEqual(raw['errors'], ['competitor bulk pause was missed'])
        self.assertEqual([round(x) for x in raw['pause_observed_s']], [240, 270])
        self.assertEqual(v['raw']['focal-send'], dict(report='missing'))
        e = self.write_obs('main-L6-stream-p1-reno')
        v = self.matrix.observation(e, 'reno', expected)
        self.assertFalse(v['usable'])
        self.assertTrue(any('summary' in x for x in v['reasons']))
        self.assertTrue(any('fixture hash' in x for x in v['reasons']))
        self.assertTrue(any('not evaluable' in x or 'host facts missing' in x for x in v['reasons']))
        self.assertAlmostEqual(v['raw']['competitor-send']['cpu_load'], 10.0 / 370 / 2)

    def test_smoke_fails_on_unregistered_hashes_and_missing_samples(self):
        saved = run.run_case
        run.run_case = lambda phase, workload, pair, arm, seed, tag='': self.write_obs(f'smoke-L6-{workload}-p0-{arm}')
        try:
            with self.assertRaises(SystemExit):
                self.matrix.smoke()
        finally:
            run.run_case = saved
        report = json.loads((self.tmp / 'smoke.json').read_text())
        self.assertFalse(report['passed'])
        for v in report['observations'].values():
            self.assertTrue(any('hash' in x for x in v['reasons']))

    def test_completion_guard(self):
        import analyze
        self.assertFalse(analyze.completion()[0])  # empty tree
        decisions = [dict(pair=p, workload=w, usable=True, contaminated=False) for p in range(1, 6) for w in self.matrix.workloads(p)]
        (self.tmp / 'follow-decisions.json').write_text(json.dumps(decisions[:7]))
        self.assertFalse(analyze.completion()[0])  # partial
        decisions[3].update(usable=False)
        (self.tmp / 'follow-decisions.json').write_text(json.dumps(decisions))
        self.assertFalse(analyze.completion()[0])  # a needed rerun without a decision
        decisions[3]['rerun'] = 'cap reached'
        (self.tmp / 'follow-decisions.json').write_text(json.dumps(decisions))
        self.assertTrue(analyze.completion()[0])
        decisions[3]['rerun'] = dict(usable=False, contaminated=False, detail={})
        (self.tmp / 'follow-decisions.json').write_text(json.dumps(decisions))
        self.assertTrue(analyze.completion()[0])
        (self.tmp / 'correctness-failure.json').write_text('{}')
        self.assertFalse(analyze.completion()[0])
        analyze.OUT, saved = self.tmp / 'summary.json', analyze.OUT
        try:
            analyze.main()
            self.assertIsNone(json.loads((self.tmp / 'summary.json').read_text())['classification'])
        finally:
            analyze.OUT = saved

    def test_partial_rerun_is_an_attempt(self):
        import analyze
        (run.OBS / 'rerun-L6-datagram-p2-cand').mkdir()
        self.assertTrue(analyze.attempted('rerun', 2, 'datagram'))
        self.assertFalse(analyze.attempted('rerun', 2, 'stream'))

    def test_binary_check(self):
        import hashlib
        b = self.tmp / 'bin'
        b.mkdir()
        expected = {}
        for name in self.matrix.BINARIES:
            (b / name).write_bytes(name.encode())
            expected[name] = hashlib.sha256(name.encode()).hexdigest()
        self.assertEqual(self.matrix.check_binaries(b, expected), [])
        (b / 'relay2').write_bytes(b'other')
        (b / 'launch').unlink()
        problems = self.matrix.check_binaries(b, expected)
        self.assertEqual(len(problems), 2)
        self.assertEqual(set(self.matrix.registered_hashes()), set(self.matrix.BINARIES))

    def test_clean_start_preflight(self):
        import subprocess
        driver = Path(__file__).resolve().parent / 'driver.sh'
        check = lambda: subprocess.run(['/bin/sh', str(driver), '--preflight-only'], env={**os.environ, 'L6_ART': str(self.tmp)}).returncode
        (run.OBS / 'dev-L6-stream-p0-cand-dev1').mkdir()
        (self.tmp / 'prereq-dev1').mkdir()
        self.assertEqual(check(), 0)  # development artifacts are allowed
        for leftover in ['observations/main-L6-stream-p1-reno', 'observations/rerun-L6-stream-p1-reno', 'observations/smoke-L6-stream-p0-cand',
                         'prereq', 'smoke.json', 'follow-decisions.json', 'correctness-failure.json', 'binaries.json']:
            path = self.tmp / leftover
            path.mkdir() if '.' not in leftover else path.write_text('{}')
            self.assertEqual(check(), 1, leftover)
            path.rmdir() if path.is_dir() else path.unlink()
        self.assertEqual(check(), 0)


class ControlPeriods(unittest.TestCase):
    START = 1_000_000_000_000

    def control(self, offers, latencies, skipped=0, unresolved=0):
        opps = [[self.START + int(s * 1e9), self.START + int(s * 1e9) + 1000] for s in offers]
        return dict(opportunity_times_ns=opps, latency_ns=latencies, skipped=skipped, unresolved=unresolved,
                    offered=len(offers) - skipped, replies=len(latencies))

    def test_exact_assignment(self):
        offers = list(range(300))
        lat = [int(100e6) if s < 100 else int(200e6) for s in offers]
        out = run.control_periods(self.control(offers, lat), self.START)
        self.assertEqual(out['method'], 'exact')
        self.assertEqual(out['periods']['equal']['replies'], 100)
        self.assertAlmostEqual(out['periods']['equal']['p95_ms'], 100.0)
        self.assertAlmostEqual(out['periods']['short']['p95_ms'], 200.0)
        self.assertEqual(out['periods']['resumed']['replies'], 30)

    def test_a_skip_makes_periods_unusable(self):
        # The request offered at t = 10 s takes 1.5 s, so the t = 11 s opportunity is skipped.
        offers = list(range(300))
        lat = [int(1.5e9) if s == 10 else int(100e6) for s in offers if s != 11]
        out = run.control_periods(self.control(offers, lat, skipped=1), self.START)
        self.assertEqual(out['method'], 'unusable')
        self.assertIsNone(out['periods']['equal']['p95_ms'])

    def test_warmup_carryover_cannot_yield_a_wrong_p95(self):
        # The review's case: a request pending from warm-up skips t = 0; t = 1..298 are answered, with
        # 200 ms at the true offers t = 95..99; t = 299 is unresolved. Counts match a shifted assignment.
        lat = [int(200e6) if 95 <= t <= 99 else int(100e6) for t in range(1, 299)]
        control = self.control(list(range(300)), lat, skipped=1, unresolved=1)
        control['offered'] = 299
        out = run.control_periods(control, self.START)
        self.assertEqual(out['method'], 'unusable')
        for p in out['periods'].values():
            self.assertIsNone(p['p95_ms'])


class RelayPeriods(unittest.TestCase):
    def test_drops_and_queue_per_period(self):
        samples = []
        for ms in range(-1000, 300000, 100):
            drops_c = 0 if ms < 100000 else (5 if ms < 200000 else 7)
            samples.append([ms, 1000, 3000 if 100000 <= ms < 200000 else 500, 0, 0, 1, drops_c, 0, 0, 2_000_000])
        out = run.relay_periods(samples)
        self.assertEqual(out['equal']['competitor_drops'], 0)
        self.assertEqual(out['short']['competitor_drops'], 5)
        self.assertEqual(out['back']['competitor_drops'], 2)
        self.assertAlmostEqual(out['short']['competitor_queue_mean_bytes'], 3000)
        self.assertAlmostEqual(out['equal']['max_late_ms'], 2.0)


if __name__ == '__main__':
    unittest.main()
