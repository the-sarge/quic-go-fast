#!/usr/bin/env python3
"""Check that the BBR sender's inputs match the path's ground truth (#792; verification aid).

For one traced L6 observation, compares what the focal BBRv3 sender's trace
recorded with what relay2 recorded for the same flow, per measured period:
  R1  lost packets the sender declared  vs  the relay's forward drops of that flow
  R2  bytes the sender saw delivered     vs  the relay's forward delivered bytes of that flow
  R3  the sender's max_bw                vs  the link's capacity in QUIC bytes
  R4  the sender's min_rtt               vs  the model's base round trip for that flow

The sender learns of a drop about one round trip later, and of a delivery about
half a round trip plus the ACK delay later, so its records are compared with a
fixed shift. Tolerances are fixed here, before any traced L6 observation:
  R1  |sender - relay| <= max(2% of relay, 15 packets) per period
  R2  sender / relay (both in QUIC bytes) within [0.98, 1.02] per period
  R3  max_bw <= 1.05 x capacity in every period
  R4  99 ms <= min_rtt <= 210 ms in every period (propagation 100 ms; queue up to about 100 ms)
A result outside a tolerance is a finding to explain, not a verdict by itself;
missing or unusable evidence makes a check incomplete, which also blocks
reconciliation (it never counts as passing).

Usage: reconcile.py OBSERVATION_DIR [--out RESULT.json]
"""
import argparse
import json
import pathlib
import sys

ROUND = 1
PERIODS = {'equal': (0, 100), 'short': (100, 200), 'back': (200, 240), 'absent': (240, 270), 'resumed': (270, 300)}
WARMUP_NS = 60_000_000_000
IP_UDP = 28                      # relay2 charges full IPv4 packets: UDP payload + 28
LINK_BYTES_PER_S = 100e6 / 8     # each direction, on full IPv4 bytes (L6)
LOSS_SHIFT_NS = 250_000_000      # drop -> declared loss
DELIVERY_SHIFT_NS = 75_000_000   # delivery at the receiver -> ACK at the sender
R1_SHARE, R1_PACKETS = 0.02, 15
R2_RANGE = (0.98, 1.02)
R3_LIMIT = 1.05
R4_RANGE_NS = (99_000_000, 210_000_000)
BOUNDARY_NS = 1_000_000_000        # a boundary record further than this from its boundary is missing


def rounds(trace):
    c = {n: i for i, n in enumerate(trace['columns'])}
    out = [dict(t=r[c['unix_ns']], lost=r[c['a1']], delivered=r[c['delivered']], bw=r[c['bandwidth']], min_rtt=r[c['min_rtt_ns']])
           for r in trace['rows'] if r[0] == ROUND]
    return sorted(out, key=lambda x: x['t'])


def at(series, t, key):
    """The value of a cumulative series at the last record at or before time t
    (rounds are about one round trip apart; the sender stops starting rounds when
    the measured window's data is delivered, so a later record may not exist).
    A record more than BOUNDARY_NS before t does not stand for t: missing."""
    value, when = None, None
    for x in series:
        if x['t'] > t:
            break
        value, when = x[key], x['t']
    return value if when is not None and t - when <= BOUNDARY_NS else None


def relay_at(samples, ms, col):
    """A cumulative relay column at the last sample at or before model time ms (100 ms apart);
    one more than BOUNDARY_NS before ms is missing."""
    value, when = None, None
    for s in samples:
        if s[0] <= ms:
            value, when = s[col], s[0]
        else:
            break
    return value if when is not None and (ms - when) * 1_000_000 <= BOUNDARY_NS else None


def reconcile(trace, relay, measured_start_ns):
    """Each check in each period is ok, fail or incomplete (with a reason); the
    observation reconciles only when every check in every period is ok."""
    rs = rounds(trace)
    samples = relay['Samples']
    focal = relay['Forward']['Focal']
    wire = focal['DeliveredBytes'] / focal['Delivered'] if focal['Delivered'] else None
    quic_share = None if wire is None else (wire - IP_UDP) / wire
    capacity = None if quic_share is None else LINK_BYTES_PER_S * quic_share
    out = dict(quic_share=quic_share, capacity_quic_bytes_per_s=capacity, periods={}, findings=[])

    def result(name, rule, ok, reason=None, **values):
        status = 'incomplete' if ok is None else 'ok' if ok else 'fail'
        if status != 'ok':
            out['findings'].append(dict(rule=rule, period=name, status=status, reason=reason, **values))
        return dict(status=status, reason=reason, **values)

    for name, (a, b) in PERIODS.items():
        s0, s1 = measured_start_ns + a * 1_000_000_000, measured_start_ns + b * 1_000_000_000
        p = {}
        # R1: declared losses in the shifted window against relay drops in the period.
        window = [x for x in rs if s0 + LOSS_SHIFT_NS <= x['t'] < s1 + LOSS_SHIFT_NS]
        declared = sum(x['lost'] for x in window)
        d0, d1 = relay_at(samples, a * 1000 - 1, 5), relay_at(samples, b * 1000 - 1, 5)
        dropped = None if d0 is None or d1 is None else d1 - d0
        if not window:
            p['r1'] = result(name, 'R1', None, 'no sender rounds in the window', declared=declared, dropped=dropped)
        elif dropped is None:
            p['r1'] = result(name, 'R1', None, 'relay boundary sample missing', declared=declared, dropped=dropped)
        else:
            p['r1'] = result(name, 'R1', abs(declared - dropped) <= max(R1_SHARE * dropped, R1_PACKETS), 'declared losses differ from relay drops',
                             declared=declared, dropped=dropped)
        # R2: delivered bytes, the relay converted to QUIC bytes with the observation's mean packet size.
        b0, b1 = at(rs, s0 + DELIVERY_SHIFT_NS, 'delivered'), at(rs, s1 + DELIVERY_SHIFT_NS, 'delivered')
        r0, r1 = relay_at(samples, a * 1000 - 1, 7), relay_at(samples, b * 1000 - 1, 7)
        sender = None if b0 is None or b1 is None else b1 - b0
        path = None if r0 is None or r1 is None or quic_share is None else (r1 - r0) * quic_share
        if sender is None:
            p['r2'] = result(name, 'R2', None, 'sender boundary record missing', sender_bytes=sender, relay_quic_bytes=path)
        elif path is None:
            p['r2'] = result(name, 'R2', None, 'relay boundary sample or packet size missing', sender_bytes=sender, relay_quic_bytes=path)
        elif path <= 0:
            p['r2'] = result(name, 'R2', None, 'no relay delivery in the period', sender_bytes=sender, relay_quic_bytes=path)
        else:
            ratio = sender / path  # zero sender delivery against positive relay delivery is ratio 0, a failure
            p['r2'] = result(name, 'R2', R2_RANGE[0] <= ratio <= R2_RANGE[1], 'delivered bytes differ from the relay',
                             sender_bytes=sender, relay_quic_bytes=path, ratio=ratio)
        inside = [x for x in rs if s0 <= x['t'] < s1]
        # R3: the bandwidth estimate against the link.
        bws = sorted(x['bw'] for x in inside if x['bw'] > 0)
        if not bws or not capacity:
            p['r3'] = result(name, 'R3', None, 'no bandwidth estimate in the period' if not bws else 'capacity unknown')
        else:
            top = bws[-1] / capacity
            p['r3'] = result(name, 'R3', top <= R3_LIMIT, 'max_bw above the link', max_over_capacity=top,
                             median_over_capacity=bws[len(bws) // 2] / capacity,
                             max_over_own_delivery=None if not path or path <= 0 else bws[-1] / (path / (b - a)))
        # R4: the minimum RTT against the model's propagation and queue.
        rtts = [x['min_rtt'] for x in inside if x['min_rtt'] > 0]
        if not rtts:
            p['r4'] = result(name, 'R4', None, 'no minimum RTT in the period')
        else:
            p['r4'] = result(name, 'R4', R4_RANGE_NS[0] <= min(rtts) and max(rtts) <= R4_RANGE_NS[1], 'min_rtt outside the path range',
                             min_ms=min(rtts) / 1e6, max_ms=max(rtts) / 1e6)
        out['periods'][name] = p
    statuses = [c['status'] for p in out['periods'].values() for c in p.values()]
    out['status'] = 'ok' if all(x == 'ok' for x in statuses) else 'fail' if 'fail' in statuses else 'incomplete'
    out['ok'] = out['status'] == 'ok'
    return out


def observation(directory):
    directory = pathlib.Path(directory)
    cfg = json.loads((directory / 'focal-config.json').read_text())
    trace = json.loads((directory / 'focal-trace.json').read_text())
    relay = json.loads((directory / 'relay.json').read_text())
    return reconcile(trace, relay, cfg['start_unix_ns'] + WARMUP_NS)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('observation')
    ap.add_argument('--out')
    args = ap.parse_args()
    result = observation(args.observation)
    text = json.dumps(result, indent=1, sort_keys=True)
    if args.out:
        pathlib.Path(args.out).write_text(text + '\n')
    print(text if len(text) < 4000 else json.dumps(dict(ok=result['ok'], findings=result['findings']), indent=1))
    return 0


if __name__ == '__main__':
    sys.exit(main())
