#!/usr/bin/env python3
"""#744 control-path recorder: per-request segments from both endpoints' logs.

Each recorder observation leaves ctl-send.json and ctl-receive.json beside
send.json and receive.json (ctl/ctlrec.go). Both endpoints stamp the same
CLOCK_MONOTONIC, so one request's events join across the two logs by stream
offsets: request id k is written by the sender at offset o (its write event),
and its reply by the receiver at offset o2 (its write event with the same id).

Segments, in microseconds, in the order a request and its reply traverse them
(SEGMENTS); their sum is the recorder's total, from the sender's write to the
sender's read of the reply:

  s_pack     sender: application write to packing (the frame carrying the request's last byte is popped)
  s_entry    sender: packing to queue entry
  s_queue    sender: send-queue wait (entry to the worker's dequeue)
  s_submit   sender: dequeue to submission (the write returned)
  t_req      residual: submission to the receiver's socket read (kernel loopback and anything unplaced)
  r_backlog  receiver: socket read to the frame handled by the connection loop
  r_wake     receiver: frame handled to the application read
  r_app      receiver: application read to the reply's write
  r_pack, r_entry, r_queue, r_submit   receiver: the reply's packing, queue entry, queue wait, submission
  t_rep      residual: the reply's submission to the sender's socket read
  s_backlog  sender: socket read to the reply's frame handled
  s_wake     sender: reply frame handled to the application read

Usability (registered, README "Recorder usability"): see `usable`.
"""
import json
from pathlib import Path

SEGMENTS = ['s_pack', 's_entry', 's_queue', 's_submit', 't_req', 'r_backlog', 'r_wake', 'r_app',
            'r_pack', 'r_entry', 'r_queue', 'r_submit', 't_rep', 's_backlog', 's_wake']
QUEUE_SEGMENTS = {'sender': ['s_queue'], 'receiver': ['r_queue']}
RESIDUAL = ['t_req', 't_rep']
K = dict(write=0, pack=1, enqueue=2, dequeue=3, submit=4, arrive=5, read=6, note=7)

# Registered usability thresholds (README "Recorder usability").
MIN_COMPLETE_SHARE = 0.95      # complete chains among measured requests
MIN_COMPLETE = 100             # complete measured requests per run
NEG_TOLERANCE_NS = 1_000       # a segment below -1 us is an ordering failure
TOTAL_TOLERANCE_NS = 20_000    # recorder total within 20 us of the fixture's latency ...
TOTAL_AGREE_SHARE = 0.99       # ... for at least 99% of complete measured requests


def load(directory, role):
    d = json.loads((Path(directory) / f'ctl-{role}.json').read_text())
    ev = {}
    for k, t, a, b, c, dd, e, r in d['events']:
        ev.setdefault(k, []).append(dict(t=t, a=a, b=b, c=c, d=dd, e=e, r=r))
    return d, ev


def _send_chain(ev, offset):
    """Packing, entry, dequeue and submission of the bytes ending at offset+32 at one endpoint, or None."""
    last = offset + 31
    packs = [p for p in ev.get(K['pack'], []) if p['a'] <= last < p['b']]
    if not packs:
        return None
    first = min(packs, key=lambda p: p['r'])
    retx = any(p['c'] == 1 for p in packs) or len(packs) > 1
    split = first['a'] > offset
    enq = [q for q in ev.get(K['enqueue'], []) if q['e'] <= first['r'] <= q['r']]
    if len(enq) != 1:
        return None
    q = enq[0]
    deq = [x for x in ev.get(K['dequeue'], []) if x['r'] == q['r']]
    sub = [x for x in ev.get(K['submit'], []) if x['r'] == q['r']]
    if len(deq) != 1 or len(sub) != 1:
        return None
    return dict(pack=first['t'], entry=q['t'], dequeue=deq[0]['t'], submit=sub[0]['t'], entries_ahead=q['a'],
                bytes_ahead=q['b'], capacity=q['c'], entry_bytes=q['d'], retransmitted=retx, split=split)


def _receive_chain(ev, offset):
    """Arrival (socket read), handling and application read of the bytes ending at offset+32, or None."""
    last = offset + 31
    arr = [x for x in ev.get(K['arrive'], []) if x['a'] <= last < x['b']]
    reads = [x for x in ev.get(K['read'], []) if x['a'] >= offset + 32]
    if not arr or not reads:
        return None
    a = min(arr, key=lambda x: x['c'])
    r = min(reads, key=lambda x: x['t'])
    return dict(arrive=a['t'], handled=a['c'], read=r['t'], duplicate_arrivals=len(arr) - 1)


def requests(directory):
    """Per measured request: segments (ns), total (ns), fixture latency (ns), queue facts; plus run facts."""
    meta_s, s = load(directory, 'send')
    meta_r, r = load(directory, 'receive')
    writes_s = {w['b']: w for w in s.get(K['write'], [])}
    writes_r = {w['b']: w for w in r.get(K['write'], [])}
    rows, incomplete = [], []
    notes = [n for n in s.get(K['note'], []) if n['b'] == 1]
    for n in notes:
        k = n['a']
        row = dict(id=k, fixture_latency_ns=n['d'] - n['c'])
        ws, wr = writes_s.get(k), writes_r.get(k)
        if ws is None or wr is None:
            incomplete.append(dict(id=k, missing='write'))
            continue
        sq = _send_chain(s, ws['a'])
        rx = _receive_chain(r, ws['a'])
        rq = _send_chain(r, wr['a'])
        sx = _receive_chain(s, wr['a'])
        if None in (sq, rx, rq, sx):
            incomplete.append(dict(id=k, missing=[name for name, v in zip(['s_send', 'r_recv', 'r_send', 's_recv'], [sq, rx, rq, sx]) if v is None]))
            continue
        stamps = [ws['t'], sq['pack'], sq['entry'], sq['dequeue'], sq['submit'], rx['arrive'], rx['handled'], rx['read'],
                  wr['t'], rq['pack'], rq['entry'], rq['dequeue'], rq['submit'], sx['arrive'], sx['handled'], sx['read']]
        seg = {name: stamps[i + 1] - stamps[i] for i, name in enumerate(SEGMENTS)}
        row.update(seg)
        row['total'] = stamps[-1] - stamps[0]
        row['ordered'] = all(v >= -NEG_TOLERANCE_NS for v in seg.values())
        row['sender_queue'] = {k2: sq[k2] for k2 in ('entries_ahead', 'bytes_ahead', 'capacity', 'entry_bytes')}
        row['receiver_queue'] = {k2: rq[k2] for k2 in ('entries_ahead', 'bytes_ahead', 'capacity', 'entry_bytes')}
        row['retransmitted'] = sq['retransmitted'] or rq['retransmitted']
        row['split'] = sq['split'] or rq['split']
        rows.append(row)
    caps = sorted({q['c'] for ev in (s, r) for q in ev.get(K['enqueue'], [])})
    facts = dict(measured=len(notes), complete=len(rows), incomplete=incomplete, capacities=caps,
                 dropped=meta_s['dropped'] + meta_r['dropped'], synthetic=meta_s['synthetic'],
                 retransmitted=sum(x['retransmitted'] for x in rows), split=sum(x['split'] for x in rows),
                 unordered=sum(not x['ordered'] for x in rows))
    return rows, facts


def usable(directory, expected_capacity):
    """Registered recorder usability for one observation: (usable, reasons, rows, facts)."""
    try:
        rows, facts = requests(directory)
    except (OSError, ValueError, KeyError) as e:
        return False, [f'recorder output unreadable: {e!r}'], [], {}
    reasons = []
    if facts['dropped']:
        reasons.append(f"{facts['dropped']} events dropped")
    if facts['capacities'] != [expected_capacity]:
        reasons.append(f"queue capacities {facts['capacities']} != [{expected_capacity}]")
    if facts['measured'] == 0 or facts['complete'] < MIN_COMPLETE_SHARE * facts['measured']:
        reasons.append(f"{facts['complete']} of {facts['measured']} measured requests complete")
    if facts['complete'] < MIN_COMPLETE:
        reasons.append(f"{facts['complete']} complete measured requests < {MIN_COMPLETE}")
    if facts['unordered']:
        reasons.append(f"{facts['unordered']} requests with a segment below -1 us")
    agree = sum(abs(x['total'] - x['fixture_latency_ns']) <= TOTAL_TOLERANCE_NS for x in rows)
    if rows and agree < TOTAL_AGREE_SHARE * len(rows):
        reasons.append(f'recorder total within 20 us of the fixture latency for {agree} of {len(rows)}')
    facts['total_agree'] = agree
    return not reasons, reasons, [x for x in rows if x['ordered']], facts


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
