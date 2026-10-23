"""#871 calibration and closed-failure regressions transplanted to #990."""
import copy
import json
import tempfile
from pathlib import Path
import unittest
import guards as memory

def setup_failure(directory):
    """Canonical pre-delivery failure from pinned fixture/main.go/session.go."""
    receipt = dict(exit_codes=[1, 1], relay_exit=0,
                   binary_sha256=memory.build.REGISTERED[('reno', 'fixture')],
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

class GuardTests(unittest.TestCase):
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
