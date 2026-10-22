"""Prospective outcome and completeness tests; no host measurements."""
import copy
import json
import tempfile
from unittest import mock
from pathlib import Path
import unittest

import memory


def blocks(port=1, prior=1, cand=1, aa=1):
    out = []
    ratios = dict(zip(memory.ARM_NAMES, [1, aa, port, prior, cand]))
    for b in range(1, 6):
        rows = [dict(arm=a, pair=b, seed=9000+b, send={m: (10+b)*ratio for m in memory.METRICS})
                for a, ratio in ratios.items()]
        out.append(dict(block=b, contaminated=False, observations=rows))
    return out


def setup_failure(directory):
    """Canonical pre-delivery failure from pinned fixture/main.go/session.go."""
    receipt = dict(exit_codes=[1, 1], relay_exit=0,
                   binary_sha256=memory.build.EXPECTED[('reno', 'fixture')],
                   relay_sha256=memory.build.EXPECTED[('relay', 'relay-linux')])
    shutdown = 'Application error 0x0 (remote): campaign complete'
    endpoint = dict(source=memory.build.REVISIONS['reno'], go_version='go1.27.0', platform='linux/amd64',
                    gomaxprocs=4, managed_direct=True, trace_enabled=False,
                    error='start deadline missed during setup', result=dict(controller='reno', sent_messages=0,
                    control=dict(offered=0, replies=0), receiver=dict(corrupt=0, duplicates=0, per_second_bytes=None, useful_bytes=0)))
    peer = copy.deepcopy(endpoint)
    peer['error'] = shutdown
    peer['result']['errors'] = [shutdown, shutdown]
    peer['result']['receiver']['per_second_bytes'] = [0] * 30
    relay = {direction: dict(SendErrors=0, TruncatedRead=0, Stats=dict(PropagationOverflow=0)) for direction in ['Forward', 'Reverse']}
    (directory/'relay.json').write_text(json.dumps(relay))
    for name, value in [('receipt', receipt), ('send', endpoint), ('receive', peer)]:
        (directory / f'{name}.json').write_text(json.dumps(value))


class MemoryTests(unittest.TestCase):
    def test_placement_intervals_and_nonmonotonic_outcomes(self):
        self.assertIn('historical appearance in abfa5456..d94b3ec2', memory.summarize_blocks(blocks(prior=1.2, cand=1.2))['placement'])
        self.assertIn('appearance after d94b3ec2 in the bundle interval', memory.summarize_blocks(blocks(cand=1.2))['placement'])
        self.assertIn('rise present at or before the port', memory.summarize_blocks(blocks(port=1.2, prior=1.2, cand=1.2))['placement'])
        p = memory.summarize_blocks(blocks(prior=1.2))['placement']
        self.assertIn('not reproduced on this candidate', p)
        self.assertIn('nonmonotonic outcomes; no causal attribution', p)

    def test_aa_range_does_not_erase_raise(self):
        r = memory.summarize_blocks(blocks(cand=1.2, aa=1.3))
        self.assertTrue(r['arms']['cand-reno']['raised'])
        self.assertAlmostEqual(r['high_memory_threshold_mib'], 14.3)
        self.assertEqual(r['arms']['reno-reno']['high_memory'], 1)
        self.assertEqual(r['arms']['cand-reno']['high_memory'], 4)

    def test_no_partial_contaminated_or_duplicate_blocks(self):
        cases = [blocks()[:-1], blocks(), blocks(), blocks()]
        cases[1][0]['contaminated'] = True
        cases[2][0]['observations'].pop()
        cases[3][1] = copy.deepcopy(cases[3][0])
        for case in cases:
            with self.assertRaises(AssertionError):
                memory.summarize_blocks(case)


    def test_calibration_expected_controls_and_rejections(self):
        facts = {'calibration': []}
        for name in ['relay-v3', 'relay-linux']:
            positive = name == 'relay-linux'
            direction = dict(sent=[200]*4, unparsed=0,
                             received=[[200 if j == (i if positive else 0) else 0 for j in range(4)] for i in range(4)])
            facts['calibration'].append(dict(relay=name, exit=0 if positive else 1,
                report=dict(errors=False, **{'pass': positive}, forward=direction, reverse=copy.deepcopy(direction))))
        memory.validate_calibration(facts)
        for arm in range(2):
            for defect in ['exit', 'pass', 'errors', 'count', 'matrix', 'unparsed']:
                bad = copy.deepcopy(facts)
                c = bad['calibration'][arm]
                if defect == 'exit': c['exit'] = 1-c['exit']
                elif defect == 'pass': c['report']['pass'] = not c['report']['pass']
                elif defect == 'errors': c['report']['errors'] = True
                elif defect == 'count': c['report']['forward']['sent'][0] = 199
                elif defect == 'matrix': c['report']['reverse']['received'][3][2] += 1
                else: c['report']['reverse']['unparsed'] = 1
                with self.subTest(arm=arm, defect=defect), self.assertRaises(AssertionError):
                    memory.validate_calibration(bad)

    def test_operational_exit_requires_disposition_and_only_one_rerun(self):
        gate = dict(window_end='2099-01-01T00:00:00+00:00')
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(memory, 'preflight', return_value=gate), \
                mock.patch.object(memory.run, 'ART', Path(tmp)), mock.patch.object(memory.run, 'ensure_credentials'):
            def failed_case(*args, **kwargs):
                d = memory.run.OBS / 'main-S5-datagram-p1-reno-reno'
                d.mkdir(parents=True)
                setup_failure(d)
                raise AssertionError('main-S5-datagram-p1-reno-reno')
            def failed_block(*args, **kwargs):
                return kwargs['runner']('main', 'reno', 'datagram', 1, 'reno', path='S5', seed=9001, tag='')
            with mock.patch.object(memory.run, 'run_case', side_effect=failed_case), \
                    mock.patch.object(memory.matrix, 'block', side_effect=failed_block):
                with self.assertRaises(memory.UnusableObservation): memory.run_block(1, False)
            folder = Path(tmp)/'memory-block-1'
            original = json.loads((folder/'original.json').read_text())
            self.assertFalse(original['usable']); self.assertFalse(original['fatal'])
            with self.assertRaises(FileNotFoundError): memory.run_block(1, True)
            (folder/'failure-disposition.json').write_text(json.dumps(dict(decision_url='https://example.test/decision', explanation='resolved process failure')))
            rows = [dict(id=a, arm=a, contamination=dict(contaminated=False)) for a in memory.ARM_NAMES]
            with mock.patch.object(memory.matrix, 'block', return_value=rows) as block, \
                    mock.patch.object(memory.memory_load, 'load', side_effect=rows), mock.patch.object(memory, 'validate_identity'):
                memory.run_block(1, True)
                self.assertEqual(block.call_args.kwargs['seed'], 9001)
            self.assertTrue(json.loads((folder/'rerun.json').read_text())['usable'])
            with self.assertRaises(AssertionError): memory.run_block(1, True)
            self.assertTrue((Path(tmp)/'observations/main-S5-datagram-p1-reno-reno/receipt.json').exists())
            clean = Path(tmp)/'memory-block-2'; clean.mkdir()
            (clean/'original.json').write_text(json.dumps(dict(usable=True, contaminated=False)))
            with self.assertRaises(AssertionError): memory.run_block(2, True)

    def test_known_setup_failure_and_unknown_or_integrity_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            setup_failure(d)
            self.assertTrue(memory.retained_operational_exit(d, 'reno', 'reno'))
            for defect in ['binary', 'source', 'receiver', 'session', 'control', 'unknown_sender',
                           'unknown_peer', 'missing_peer', 'sent', 'control_traffic', 'delivery', 'relay_exit', 'missing_reports']:
                setup_failure(d)
                receipt = json.loads((d/'receipt.json').read_text())
                endpoint = json.loads((d/'send.json').read_text())
                peer = json.loads((d/'receive.json').read_text())
                if defect == 'binary': receipt['binary_sha256'] = 'wrong'
                elif defect == 'source': endpoint['source'] = 'wrong'
                elif defect == 'receiver': endpoint['result']['receiver']['corrupt'] = 1
                elif defect == 'session': endpoint['result']['errors'] = ['control sequence mismatch']
                elif defect == 'control': peer['result']['control']['error'] = 'control sequence mismatch'
                elif defect == 'unknown_sender': endpoint['error'] = 'unrecognized failure'
                elif defect == 'unknown_peer': peer['result']['errors'].append('run config mismatch')
                elif defect == 'sent': endpoint['result']['sent_messages'] = 1
                elif defect == 'control_traffic': endpoint['result']['control']['offered'] = 1
                elif defect == 'delivery': peer['result']['receiver']['useful_bytes'] = 1
                elif defect == 'relay_exit': receipt['relay_exit'] = 1
                for name, value in [('receipt', receipt), ('send', endpoint), ('receive', peer)]:
                    (d/f'{name}.json').write_text(json.dumps(value))
                if defect in ['missing_peer', 'missing_reports']: (d/'receive.json').unlink()
                if defect == 'missing_reports': (d/'send.json').unlink()
                with self.subTest(defect=defect), self.assertRaises(AssertionError):
                    memory.retained_operational_exit(d, 'reno', 'reno')

    def test_session_integrity_failure_retains_fatal_block(self):
        gate = dict(window_end='2099-01-01T00:00:00+00:00')
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(memory, 'preflight', return_value=gate), \
                mock.patch.object(memory.run, 'ART', Path(tmp)), mock.patch.object(memory.run, 'ensure_credentials'):
            def failed_case(*args, **kwargs):
                d = memory.run.OBS / 'main-S5-datagram-p1-reno-reno'
                d.mkdir(parents=True)
                setup_failure(d)
                endpoint = json.loads((d/'send.json').read_text())
                endpoint['result']['errors'] = ['control sequence mismatch']
                (d/'send.json').write_text(json.dumps(endpoint))
                raise AssertionError('legacy nonzero exit')
            def block(*args, **kwargs):
                return kwargs['runner']('main', 'reno', 'datagram', 1, 'reno', path='S5', seed=9001, tag='')
            with mock.patch.object(memory.run, 'run_case', side_effect=failed_case), \
                    mock.patch.object(memory.matrix, 'block', side_effect=block):
                with self.assertRaises(AssertionError): memory.run_block(1, False)
            original = Path(tmp)/'memory-block-1/original.json'
            self.assertTrue(json.loads(original.read_text())['fatal'])
            (original.parent/'failure-disposition.json').write_text(json.dumps(dict(decision_url='https://example.test/decision', explanation='cannot override integrity')))
            with self.assertRaises(AssertionError): memory.run_block(1, True)
            self.assertTrue((Path(tmp)/'observations/main-S5-datagram-p1-reno-reno/send.json').exists())

    def test_integrity_and_booking_stops_keep_evidence_and_forbid_rerun(self):
        gate = dict(window_end='2099-01-01T00:00:00+00:00')
        for reason in ['integrity', 'booking']:
            with self.subTest(reason=reason), tempfile.TemporaryDirectory() as tmp, \
                    mock.patch.object(memory.run, 'ART', Path(tmp)), mock.patch.object(memory.run, 'ensure_credentials'):
                def block(*args, **kwargs):
                    d = Path(tmp)/'observations/partial-p1-receipt'
                    d.mkdir(parents=True); (d/'retained.txt').write_text('partial evidence')
                    if reason == 'integrity': raise AssertionError('receiver integrity')
                    return kwargs['runner']('main', 'reno', 'datagram', 1, 'reno', path='S5', seed=9001)
                with mock.patch.object(memory, 'preflight', side_effect=[gate, dict(window_end='2000-01-01T00:00:00+00:00')]), \
                        mock.patch.object(memory.matrix, 'block', side_effect=block):
                    with self.assertRaises(AssertionError): memory.run_block(1, False)
                folder = Path(tmp)/'memory-block-1'
                self.assertTrue(json.loads((folder/'original.json').read_text())['fatal'])
                (folder/'failure-disposition.json').write_text(json.dumps(dict(decision_url='https://example.test/decision', explanation='cannot override integrity or booking')))
                with mock.patch.object(memory, 'preflight', return_value=gate):
                    with self.assertRaises(AssertionError): memory.run_block(1, True)
                self.assertTrue((Path(tmp)/'observations/partial-p1-receipt/retained.txt').exists())

    def test_historical_formulas_are_verbatim(self):
        here = Path(__file__).parent
        source = (here / 'analyze.py').read_text().split('\nrows = [load(p.parent)')[0].rstrip() + '\n'
        self.assertEqual(source, (here / 'memory_load.py').read_text())


if __name__ == '__main__':
    unittest.main()
