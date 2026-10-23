"""#990 receipt guards copied from reviewed #871 registration 56ad455f.

The four functions/classes below are byte-identical except import context.
Canonical setup failure recovery is limited to plain relay-path observations;
unknown, missing, identity and integrity evidence stays fatal.
"""
import json
import build

def validate_calibration(facts):
    calibrations = {c['relay']: c for c in facts['calibration']}
    for name in ['relay-v3', 'relay-linux']:
        c = calibrations[name]
        positive = name == 'relay-linux'
        assert c['exit'] == (0 if positive else 1) and c['report']['pass'] is positive and not c['report']['errors'], ('calibration error', name)
        for direction in ['forward', 'reverse']:
            report = c['report'][direction]
            assert report['sent'] == [200] * 4 and report['unparsed'] == 0
            expected = [[200 if j == (i if name == 'relay-linux' else 0) else 0 for j in range(4)] for i in range(4)]
            assert report['received'] == expected, ('calibration codepoints', name, direction)


class UnusableObservation(RuntimeError):
    """An operational exit established by retained runner evidence."""


def retained_operational_exit(directory, variant, controller):
    receipt_path = directory / 'receipt.json'
    if not receipt_path.exists():
        return False
    try:
        receipt = json.loads(receipt_path.read_text())
        if receipt['exit_codes'] == [0, 0] and receipt.get('relay_exit') in (None, 0):
            return False
        assert receipt['exit_codes'] == [1, 1] and receipt['relay_exit'] == 0, 'unrecognized process failure'
        expected = build.REGISTERED.get((variant, 'fixture'), build.EXPECTED.get((variant, 'fixture')))
        assert expected and receipt['binary_sha256'] == expected, 'failed observation binary identity'
        assert receipt['relay_sha256'] == build.EXPECTED[('relay', 'relay-linux')]
        # Canonical, closed recovery domain: a sender missing its start
        # deadline before sending. A nonzero exit alone proves no cause.
        peer_shutdown = 'Application error 0x0 (remote): campaign complete'
        endpoints = {}
        for role in ['send', 'receive']:
            path = directory / f'{role}.json'
            assert path.exists(), 'missing endpoint failure evidence'
            endpoint = json.loads(path.read_text())
            assert endpoint['source'] == build.REVISIONS[variant], 'failed observation source identity'
            assert endpoint['go_version'] == 'go1.27.0' and endpoint['platform'] == 'linux/amd64'
            assert endpoint['gomaxprocs'] == 4 and endpoint['managed_direct'] and endpoint['trace_enabled'] is False
            result = endpoint['result']
            assert result['controller'] == controller
            errors = result.get('errors')
            if role == 'send':
                assert not errors, 'sender session integrity or unknown failure'
            else:
                assert isinstance(errors, list) and all(e == peer_shutdown for e in errors), 'receiver session integrity or unknown failure'
            assert not result['control'].get('error'), 'control integrity or unknown failure'
            endpoints[role] = endpoint
        sender = endpoints['send']
        assert sender.get('error') == 'start deadline missed during setup', 'unrecognized operational cause'
        assert endpoints['receive'].get('error') == peer_shutdown, 'unrecognized peer failure'
        assert sender['result']['sent_messages'] == 0, 'setup failure after sending'
        for endpoint in endpoints.values():
            result = endpoint['result']
            control = result['control']
            assert control['offered'] == 0 and control['replies'] == 0, 'setup failure after control traffic'
            delivery = result['receiver']
            assert delivery['corrupt'] == 0 and delivery['duplicates'] == 0, 'receiver integrity'
            assert delivery['useful_bytes'] == 0, 'setup failure after delivery'
            counts = delivery['per_second_bytes']
            assert counts is None or (isinstance(counts, list) and all(n == 0 for n in counts)), 'receiver accounting'
        # The sender has no receiver report at this pre-delivery failure.
        # Its null counters and the receiver's allocated zero counters differ
        # canonically; both must independently establish zero delivery.
        relay_path = directory / 'relay.json'
        assert relay_path.exists(), 'missing relay failure evidence'
        relay = json.loads(relay_path.read_text())
        assert not relay.get('Error'), 'relay failure'
        for direction in ['Forward', 'Reverse']:
            assert relay[direction]['SendErrors'] == 0 and relay[direction]['TruncatedRead'] == 0, 'relay integrity'
            assert relay[direction]['Stats']['PropagationOverflow'] == 0, 'relay propagation integrity'
        return True
    except (KeyError, ValueError, TypeError) as exc:
        raise AssertionError('malformed failure evidence') from exc


def validate_contamination(rows):
    """Unknown sampling evidence cannot establish either clean or disturbed."""
    flags = [row['contamination']['contaminated'] for row in rows]
    assert all(type(flag) is bool for flag in flags), 'missing contamination evidence'
    return any(flags)
