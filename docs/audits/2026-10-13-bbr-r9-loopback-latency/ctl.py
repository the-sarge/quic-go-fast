#!/usr/bin/env python3
"""#744 control-path recorder: per-request segments from both endpoints' logs.

Each recorder observation leaves ctl-send.json and ctl-receive.json beside
send.json and receive.json (ctl/ctlrec.go). Both endpoints stamp the same
CLOCK_MONOTONIC, so one request's events join across the two logs by stream
offset: request id k is written by the sender at offset o (its write event),
and its reply by the receiver at offset o2 (its write event with the same id).

A message is complete at its reader when every one of its 32 bytes has been
handled (byte coverage, in handling order), so reordered halves cannot move
time into the reader's wake segment. The completing frame's range names the
transmission that delivered it. When that range was packed more than once (a
retransmission or duplicate) or matches no pack, the delivering transmission
is ambiguous: the request is kept, its sender-side segments come from the first
transmission carrying its last byte, and the remaining time to the completing
arrival is placed in the residual (`ambiguous` is set).

Segments, in microseconds, in the order a request and its reply traverse them
(SEGMENTS); their sum is the recorder's total, from the sender's write to the
sender's read of the reply:

  s_pack     sender: application write to packing (the delivering frame is popped)
  s_entry    sender: packing to queue entry
  s_queue    sender: send-queue wait (entry to the worker's dequeue)
  s_submit   sender: dequeue to the start of submission (before the socket write)
  t_req      residual: submission start to the receiver's socket read (the write itself, kernel loopback, anything unplaced)
  r_backlog  receiver: socket read to the completing frame handled by the connection loop
  r_wake     receiver: message complete to the application read
  r_app      receiver: application read to the reply's write
  r_pack, r_entry, r_queue, r_submit   receiver: the reply's packing, queue entry, queue wait, submission start
  t_rep      residual: the reply's submission start to the sender's socket read
  s_backlog  sender: socket read to the reply's completing frame handled
  s_wake     sender: reply complete to the application read

The socket write's own duration (submission start to the write's return) is
reported separately per endpoint (`*_write_ns`); it overlaps the residual.

Usability (registered, README "Recorder usability"): see `usable`.
"""
import json
from pathlib import Path

SEGMENTS = ['s_pack', 's_entry', 's_queue', 's_submit', 't_req', 'r_backlog', 'r_wake', 'r_app',
            'r_pack', 'r_entry', 'r_queue', 'r_submit', 't_rep', 's_backlog', 's_wake']
QUEUE_SEGMENTS = {'sender': ['s_queue'], 'receiver': ['r_queue']}
RESIDUAL = ['t_req', 't_rep']
K = dict(write=0, pack=1, enqueue=2, dequeue=3, submit=4, done=5, discard=6, arrive=7, read=8, note=9)
FIELDS = ['k', 't', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'r']

# Registered usability thresholds (README "Recorder usability").
MIN_COMPLETE_SHARE = 0.95      # complete chains among measured requests
MIN_COMPLETE = {'loopback-long': 100, 'loopback-syn': 50}  # complete measured requests per run, by path
NEG_TOLERANCE_NS = 1_000       # a segment below -1 us is an ordering failure
TOTAL_TOLERANCE_NS = 20_000    # recorder total within 20 us of the fixture's latency ...
TOTAL_AGREE_SHARE = 0.99       # ... for at least 99% of complete measured requests
CLOCK_HALF_WIDTH_NS = 1_000    # each endpoint's bracketed clock calibration, at start and at flush
CLOCK_DRIFT_NS = 2_000         # start and flush offsets agree within this


def load(directory, role):
    d = json.loads((Path(directory) / f'ctl-{role}.json').read_text())
    assert d['fields'] == FIELDS, d['fields']
    ev = {}
    for row in d['events']:
        e = dict(zip(FIELDS, row))
        ev.setdefault(e['k'], []).append(e)
    return d, ev


def _completion(ev, offset):
    """At a reader: the arrival that completed bytes [offset, offset+32) in handling order, or None."""
    lo, hi = offset, offset + 32
    arr = sorted((x for x in ev.get(K['arrive'], []) if x['a'] < hi and x['b'] > lo), key=lambda x: x['c'])
    covered = []
    for x in arr:
        covered.append((max(x['a'], lo), min(x['b'], hi)))
        covered.sort()
        reach = lo
        for a, b in covered:
            if a > reach:
                break
            reach = max(reach, b)
        if reach >= hi:
            return x, len(arr)
    return None, len(arr)


def _transmission(ev, pack):
    """Entry, dequeue, submission start and write completion of the entry carrying one pack event, or None."""
    enq = [q for q in ev.get(K['enqueue'], []) if q['e'] <= pack['r'] <= q['r']]
    if len(enq) != 1:
        return None
    q = enq[0]
    one = lambda kind: [x for x in ev.get(K[kind], []) if x['r'] == q['r']]
    deq, sub, done = one('dequeue'), one('submit'), one('done')
    if len(deq) != 1 or not sub:
        return None
    return dict(pack=pack['t'], entry=q['t'], dequeue=deq[0]['t'], submit=min(x['t'] for x in sub),
                done=min(x['t'] for x in done) if done else None, entries_ahead=q['a'], bytes_ahead=q['b'],
                capacity=q['c'], entry_bytes=q['d'], cumulative_bytes=q['f'], channel_length=q['g'])


def _leg(writer, reader, offset):
    """One message from writer to reader: (sender-side chain, completing arrival, read time, facts) or None."""
    arrival, n_arr = _completion(reader, offset)
    reads = [x for x in reader.get(K['read'], []) if x['a'] >= offset + 32]
    if arrival is None or not reads:
        return None
    packs = writer.get(K['pack'], [])
    exact = [p for p in packs if p['a'] == arrival['a'] and p['b'] == arrival['b']]
    carrying_last = [p for p in packs if p['a'] <= offset + 31 < p['b']]
    retx = any(p['c'] == 1 for p in packs if p['a'] < offset + 32 and p['b'] > offset)
    ambiguous = len(exact) != 1
    pack = exact[0] if not ambiguous else (min(carrying_last, key=lambda p: p['r']) if carrying_last else None)
    if pack is None:
        return None
    tx = _transmission(writer, pack)
    if tx is None:
        return None
    facts = dict(ambiguous=ambiguous, retransmitted=retx, split=arrival['a'] > offset or arrival['b'] < offset + 32,
                 arrivals=n_arr)
    return tx, arrival, min(x['t'] for x in reads), facts


def clock_ok(meta):
    c = meta['clock']
    return (c['half_width_ns'] <= CLOCK_HALF_WIDTH_NS and c['flush_half_width_ns'] <= CLOCK_HALF_WIDTH_NS
            and abs(c['flush_offset_ns'] - c['offset_ns']) <= CLOCK_DRIFT_NS)


def requests(directory):
    """Per measured request: segments (ns), total (ns), fixture latency (ns), queue facts; incomplete requests
    with their fixture latency; and run facts."""
    meta_s, s = load(directory, 'send')
    meta_r, r = load(directory, 'receive')
    writes_s = {w['b']: w for w in s.get(K['write'], [])}
    writes_r = {w['b']: w for w in r.get(K['write'], [])}
    rows, incomplete = [], []
    notes = [n for n in s.get(K['note'], []) if n['b'] == 1]
    for n in notes:
        k = n['a']
        fixture = n['d'] - n['c']
        ws, wr = writes_s.get(k), writes_r.get(k)
        if ws is None or wr is None:
            incomplete.append(dict(id=k, fixture_latency_ns=fixture, missing=['write']))
            continue
        req, rep = _leg(s, r, ws['a']), _leg(r, s, wr['a'])
        if req is None or rep is None:
            incomplete.append(dict(id=k, fixture_latency_ns=fixture,
                                   missing=[name for name, v in (('request', req), ('reply', rep)) if v is None]))
            continue
        (sq, ra, rread, rf), (rq, sa, sread, sf) = req, rep
        stamps = [ws['t'], sq['pack'], sq['entry'], sq['dequeue'], sq['submit'], ra['t'], ra['c'], rread,
                  wr['t'], rq['pack'], rq['entry'], rq['dequeue'], rq['submit'], sa['t'], sa['c'], sread]
        seg = {name: stamps[i + 1] - stamps[i] for i, name in enumerate(SEGMENTS)}
        row = dict(id=k, fixture_latency_ns=fixture, **seg, total=stamps[-1] - stamps[0],
                   ordered=all(v >= -NEG_TOLERANCE_NS for v in seg.values()),
                   s_write_ns=sq['done'] - sq['submit'] if sq['done'] else None,
                   r_write_ns=rq['done'] - rq['submit'] if rq['done'] else None,
                   sender_queue={k2: sq[k2] for k2 in ('entries_ahead', 'bytes_ahead', 'channel_length', 'capacity', 'entry_bytes',
                                                       'cumulative_bytes')},
                   receiver_queue={k2: rq[k2] for k2 in ('entries_ahead', 'bytes_ahead', 'channel_length', 'capacity', 'entry_bytes',
                                                         'cumulative_bytes')},
                   enqueue_ns=dict(sender=sq['entry'], receiver=rq['entry']),
                   ambiguous=rf['ambiguous'] or sf['ambiguous'], retransmitted=rf['retransmitted'] or sf['retransmitted'],
                   split=rf['split'] or sf['split'])
        rows.append(row)
    caps = sorted({q['c'] for ev in (s, r) for q in ev.get(K['enqueue'], [])})
    facts = dict(measured=len(notes), complete=len(rows), incomplete=incomplete, capacities=caps,
                 dropped=meta_s['dropped'] + meta_r['dropped'], tags_exhausted=meta_s['tags_exhausted'] + meta_r['tags_exhausted'],
                 clock=dict(send=meta_s['clock'], receive=meta_r['clock']), clock_ok=clock_ok(meta_s) and clock_ok(meta_r),
                 synthetic=meta_s['synthetic'], ambiguous=sum(x['ambiguous'] for x in rows),
                 retransmitted=sum(x['retransmitted'] for x in rows), split=sum(x['split'] for x in rows),
                 unordered=sum(not x['ordered'] for x in rows),
                 ahead_vs_channel=sum(x[q]['entries_ahead'] != x[q]['channel_length'] for x in rows
                                      for q in ('sender_queue', 'receiver_queue')),
                 send_rate_bytes_per_s=send_rates(rows))
    return rows, facts


def send_rates(rows):
    """Each endpoint's send rate over the measured requests: cumulative enqueued bytes between its first and last
    control enqueue, per second (the endpoint's wire bytes; the bound rule converts goodput with it)."""
    out = {}
    for side, q in (('sender', 'sender_queue'), ('receiver', 'receiver_queue')):
        pts = sorted((x['enqueue_ns'][side], x[q]['cumulative_bytes']) for x in rows)
        out[side] = (pts[-1][1] - pts[0][1]) / ((pts[-1][0] - pts[0][0]) / 1e9) if len(pts) > 1 and pts[-1][0] > pts[0][0] else None
    return out


def usable(directory, expected_capacity, min_complete=100):
    """Registered recorder usability for one observation: (usable, reasons, rows, facts).

    min_complete: MIN_COMPLETE for the observation's path (100 on loopback-long, 50 on loopback-syn).
    """
    try:
        rows, facts = requests(directory)
    except (OSError, ValueError, KeyError, AssertionError) as e:
        return False, [f'recorder output unreadable: {e!r}'], [], {}
    reasons = []
    if facts['dropped']:
        reasons.append(f"{facts['dropped']} events dropped")
    if not facts['clock_ok']:
        reasons.append(f"clock calibration outside its limits: {facts['clock']}")
    if facts['capacities'] != [expected_capacity]:
        reasons.append(f"queue capacities {facts['capacities']} != [{expected_capacity}]")
    if facts['measured'] == 0 or facts['complete'] < MIN_COMPLETE_SHARE * facts['measured']:
        reasons.append(f"{facts['complete']} of {facts['measured']} measured requests complete")
    if facts['complete'] < min_complete:
        reasons.append(f"{facts['complete']} complete measured requests < {min_complete}")
    if facts['unordered']:
        reasons.append(f"{facts['unordered']} requests with a segment below -1 us")
    agree = sum(abs(x['total'] - x['fixture_latency_ns']) <= TOTAL_TOLERANCE_NS for x in rows)
    if rows and agree < TOTAL_AGREE_SHARE * len(rows):
        reasons.append(f'recorder total within 20 us of the fixture latency for {agree} of {len(rows)}')
    facts['total_agree'] = agree
    return not reasons, reasons, rows, facts


if __name__ == '__main__':
    import statistics
    import sys
    rows, facts = requests(sys.argv[1])
    print({k: v for k, v in facts.items() if k != 'incomplete'}, 'incomplete', facts['incomplete'][:5])
    for name in SEGMENTS + ['total', 'fixture_latency_ns']:
        v = sorted(x[name] / 1000 for x in rows)
        if v:
            print(f'{name:>18} p50 {statistics.median(v):9.1f}  p95 {v[int(len(v) * .95) - 1]:9.1f} us')
    for side in ['sender_queue', 'receiver_queue']:
        print(side, 'entries ahead p50', statistics.median(x[side]['entries_ahead'] for x in rows),
              'bytes ahead p50', statistics.median(x[side]['bytes_ahead'] for x in rows))
