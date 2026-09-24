"""Frozen analysis of the predeclared 18 comparison pairs."""
import csv
import json
import math
import pathlib

root = pathlib.Path(__file__).parent
summary = []
for n in (1024,8192,32768):
    for ranges in (1,32):
        implementations = {}
        for implementation in ('baseline','candidate'):
            samples = [json.loads((root/f'pair-{n}-{ranges}-{i}-{implementation}.json').read_text()) for i in (1,2,3)]
            ops = sum(s['ops'] for s in samples)
            implementations[implementation] = dict(samples=samples,mean_ns=sum(s['ack_ns'] for s in samples)/ops,allocs_per_ack=sum(s['allocs'] for s in samples)/ops,bytes_per_ack=sum(s['bytes'] for s in samples)/ops)
        tail=[]
        for i in (1,2,3):
            tail.extend(int(r['elapsed_ns']) for r in csv.DictReader((root/f'candidate-tail-{n}-{ranges}-{i}.csv').open()))
        tail.sort()
        b,c=implementations['baseline'],implementations['candidate']
        r=dict(n=n,ranges=ranges,implementations=implementations,candidate_p99_ns=tail[math.ceil(len(tail)*.99)-1],tail_count=len(tail),mean_reduction_percent=100*(1-c['mean_ns']/b['mean_ns']),pair_reduction_percent=[100*(1-cc['mean_ns']/bb['mean_ns']) for bb,cc in zip(b['samples'],c['samples'])])
        summary.append(r)
        bm=', '.join(f"{s['mean_ns']/1000:.2f}" for s in b['samples'])
        cm=', '.join(f"{s['mean_ns']/1000:.2f}" for s in c['samples'])
        print(f"{n} | {ranges} | {bm} | {cm} | {r['mean_reduction_percent']:.1f}% | {r['candidate_p99_ns']/1000:.2f}")
(root/'comparison-summary.json').write_text(json.dumps(summary,indent=2)+'\n')
