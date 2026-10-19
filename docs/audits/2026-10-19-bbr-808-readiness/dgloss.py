#!/usr/bin/env python3
"""DATAGRAM loss in the readiness DATAGRAM cells (#808, descriptive; README, "D8").

Written before any observation of this record. For every counted DATAGRAM
observation selected under the registered rerun rule (stages.chosen), it
reads the fixture's own counts: messages sent, and distinct messages received
inside and outside the measured window. On S5 and S6 it also reads the relay's
forward drops (every forward packet it did not deliver: overflow and random
loss), so the loss the network path caused is separated from the rest, which
on loopback (no relay) is all receive-side or local. The
frozen fixture does not read DatagramReceiveDropsV1, so the receive-queue
discards themselves are not separated here; the side stage reports them.
Writes `datagram-loss.json` beside this file. Sets no flag.

Usage: dgloss.py
"""
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.dont_write_bytecode = True
sys.path.insert(0, str(HERE))
import stages  # noqa: E402  (imports art first, so the observation roots are this record's)

OUT = HERE / 'datagram-loss.json'


def loss(d):
    meta = json.loads((d / 'meta.json').read_text())
    send = json.loads((d / 'send.json').read_text())
    recv = json.loads((d / 'receive.json').read_text())
    sent = send['result']['sent_messages']
    rx = recv['result']['receiver']
    got = rx['unique_messages'] + rx['outside_window']
    row = dict(id=d.name, path=meta['path'], pair=meta['pair'], arm='-'.join([meta['variant'], json.loads((d / 'config.json').read_text())['controller']]
                                                                          + ([meta['tag']] if meta.get('tag') else [])),
               sent=sent, distinct_received=got, lost=sent - got, loss=(sent - got) / sent if sent else None)
    relay = d / 'relay.json'
    if relay.exists():
        st = json.loads(relay.read_text())['Forward']['Stats']
        dropped = st['Received'] - st['Delivered']  # every forward packet the relay did not deliver (overflow, random loss)
        row.update(relay_dropped=dropped, relay_overflow=st['Overflow'], lost_minus_relay_dropped=sent - got - dropped)
    return row


def main():
    rows = []
    for d in stages.chosen('main')[0]:
        if json.loads((d / 'config.json').read_text())['workload'] == 'datagram':
            rows.append(loss(d))
    cells = {}
    for r in rows:
        cells.setdefault(f"{r['path']}/{r['arm']}", []).append(r)
    summary = {k: dict(blocks=len(v), loss_median=statistics.median(x['loss'] for x in v), loss_max=max(x['loss'] for x in v),
                       lost_median=statistics.median(x['lost'] for x in v),
                       lost_minus_relay_dropped_median=(statistics.median(x['lost_minus_relay_dropped'] for x in v)
                                                        if 'relay_dropped' in v[0] else None))
               for k, v in sorted(cells.items())}
    OUT.write_text(json.dumps(dict(cells=summary, observations=sorted(rows, key=lambda r: r['id'])), indent=1) + '\n')
    for k, v in summary.items():
        print(f"{k:28} loss median {100 * v['loss_median']:.4f}% max {100 * v['loss_max']:.4f}%  lost {v['lost_median']}"
              + (f"  beyond relay drops {v['lost_minus_relay_dropped_median']}" if v['lost_minus_relay_dropped_median'] is not None else ''))


if __name__ == '__main__':
    main()
