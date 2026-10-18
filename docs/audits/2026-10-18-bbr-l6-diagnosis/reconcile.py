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
A result outside a tolerance is a finding to explain, not a verdict by itself.

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


def rounds(trace):
    c = {n: i for i, n in enumerate(trace['columns'])}
    out = [dict(t=r[c['unix_ns']], lost=r[c['a1']], delivered=r[c['delivered']], bw=r[c['bandwidth']], min_rtt=r[c['min_rtt_ns']])
           for r in trace['rows'] if r[0] == ROUND]
    return sorted(out, key=lambda x: x['t'])


def at(series, t, key):
    """The value of a cumulative series at the first record at or after time t."""
    for x in series:
        if x['t'] >= t:
            return x[key]
    return None


def relay_at(samples, ms, col):
    """A cumulative relay column at the last sample at or before model time ms."""
    value = None
    for s in samples:
        if s[0] <= ms:
            value = s[col]
        else:
            break
    return value


def reconcile(trace, relay, measured_start_ns):
    rs = rounds(trace)
    samples = relay['Samples']
    focal = relay['Forward']['Focal']
    wire = focal['DeliveredBytes'] / focal['Delivered'] if focal['Delivered'] else None
    quic_share = None if wire is None else (wire - IP_UDP) / wire
    capacity = None if quic_share is None else LINK_BYTES_PER_S * quic_share
    out = dict(quic_share=quic_share, capacity_quic_bytes_per_s=capacity, periods={}, findings=[])
    for name, (a, b) in PERIODS.items():
        s0, s1 = measured_start_ns + a * 1_000_000_000, measured_start_ns + b * 1_000_000_000
        p = {}
        # R1: declared losses in the shifted window against relay drops in the period.
        declared = sum(x['lost'] for x in rs if s0 + LOSS_SHIFT_NS <= x['t'] < s1 + LOSS_SHIFT_NS)
        d0, d1 = relay_at(samples, a * 1000 - 1, 5), relay_at(samples, b * 1000 - 1, 5)
        dropped = None if d0 is None or d1 is None else d1 - d0
        p['r1'] = dict(declared=declared, dropped=dropped)
        if dropped is not None:
            ok = abs(declared - dropped) <= max(R1_SHARE * dropped, R1_PACKETS)
            p['r1']['ok'] = ok
            if not ok:
                out['findings'].append(dict(rule='R1', period=name, declared=declared, dropped=dropped))
        # R2: delivered bytes, the relay converted to QUIC bytes with the observation's mean packet size.
        b0, b1 = at(rs, s0 + DELIVERY_SHIFT_NS, 'delivered'), at(rs, s1 + DELIVERY_SHIFT_NS, 'delivered')
        r0, r1 = relay_at(samples, a * 1000 - 1, 7), relay_at(samples, b * 1000 - 1, 7)
        sender = None if b0 is None or b1 is None else b1 - b0
        path = None if r0 is None or r1 is None or quic_share is None else (r1 - r0) * quic_share
        ratio = None if not sender or not path else sender / path
        p['r2'] = dict(sender_bytes=sender, relay_quic_bytes=path, ratio=ratio)
        if ratio is not None:
            ok = R2_RANGE[0] <= ratio <= R2_RANGE[1]
            p['r2']['ok'] = ok
            if not ok:
                out['findings'].append(dict(rule='R2', period=name, ratio=ratio))
        inside = [x for x in rs if s0 <= x['t'] < s1]
        # R3: the bandwidth estimate against the link.
        bws = sorted(x['bw'] for x in inside if x['bw'] > 0)
        if bws and capacity:
            top = bws[-1] / capacity
            p['r3'] = dict(max_over_capacity=top, median_over_capacity=bws[len(bws) // 2] / capacity,
                           max_over_own_delivery=None if not path else bws[-1] / (path / (b - a)), ok=top <= R3_LIMIT)
            if top > R3_LIMIT:
                out['findings'].append(dict(rule='R3', period=name, max_over_capacity=top))
        # R4: the minimum RTT against the model's propagation and queue.
        rtts = [x['min_rtt'] for x in inside if x['min_rtt'] > 0]
        if rtts:
            ok = R4_RANGE_NS[0] <= min(rtts) and max(rtts) <= R4_RANGE_NS[1]
            p['r4'] = dict(min_ms=min(rtts) / 1e6, max_ms=max(rtts) / 1e6, ok=ok)
            if not ok:
                out['findings'].append(dict(rule='R4', period=name, min_ms=min(rtts) / 1e6, max_ms=max(rtts) / 1e6))
        out['periods'][name] = p
    out['ok'] = not out['findings'] and all(all(v.get('ok', True) for v in p.values()) for p in out['periods'].values())
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
