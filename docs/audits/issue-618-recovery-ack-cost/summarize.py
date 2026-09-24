"""Frozen issue-618 analysis, not a maintained tool or CI gate."""
import csv
import json
import math
import pathlib

root = pathlib.Path(__file__).parent
rows = [json.loads(line) for line in (root / 'measurements.jsonl').read_text().splitlines()]
summary = []
for n in (1024, 8192, 32768):
    for ranges in (1, 32):
        samples = [r for r in rows if r['kind'] == 'sample' and r['n'] == n and r['ranges'] == ranges]
        tail_path = root / f'tail-{n}-{ranges}.csv'
        durations = list(csv.DictReader(tail_path.open())) if tail_path.exists() else []
        def p99(values):
            values = sorted(values)
            return values[math.ceil(len(values)*.99)-1] if values else None
        ops = sum(r['ops'] for r in samples)
        summary.append(dict(n=n, ranges=ranges, samples=samples, ops=ops,
            mean_ns=sum(r['ack_ns'] for r in samples)/ops if ops else None,
            allocs_per_ack=sum(r['allocs'] for r in samples)/ops if ops else None,
            bytes_per_ack=sum(r['bytes'] for r in samples)/ops if ops else None,
            p99_ns=p99([int(r['elapsed_ns']) for r in durations]),
            tail_count=len(durations),
            sample_p99_ns=[p99([int(r['elapsed_ns']) for r in durations if int(r['sample']) == i]) for i in (1,2,3)]))
(root / 'summary.json').write_text(json.dumps(summary, indent=2)+'\n')
for r in summary:
    if r['ops']:
        means = ', '.join(f"{s['mean_ns']/1000:.2f}" for s in r['samples'])
        print(f"{r['n']} | {r['ranges']} | {means} | {r['mean_ns']/1000:.2f} | {r['p99_ns']/1000:.2f} | {r['allocs_per_ack']:.6f} | {r['bytes_per_ack']:.6f} | {r['tail_count']}")
