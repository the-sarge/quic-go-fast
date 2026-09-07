#!/usr/bin/env python3
"""E1's fixed paired analysis; no adaptive exclusions or replacement pooling."""
import argparse
import json
import math
from pathlib import Path
import random
import re


def whole_pairs():
    rng=random.Random(20260907)
    return [[rng.randrange(10) for _ in range(10)] for _ in range(20000)]


def percentile(values, p):
    values=sorted(values)
    at=(len(values)-1)*p
    lo=math.floor(at)
    hi=math.ceil(at)
    return values[lo]+(values[hi]-values[lo])*(at-lo)


def summarize(root):
    manifest=json.loads((root/'manifest.json').read_text())
    if manifest['paired_samples'] != 10 or manifest['measurement_seconds'] != 60:
        raise ValueError('smoke data cannot qualify E1')
    resamples=whole_pairs()
    summary={'seed':20260907,'whole_pair_resamples':20000,'campaign':str(root),'cells':[]}
    for cell in manifest['cells']:
        name=cell['cell']
        pairs=[]
        for i in range(10):
            pair=[]
            for variant in ('base','candidate'):
                path=root/f'{name}-{i:02d}-{variant}'/'receipt.json'
                if not path.exists():
                    pair=[]; break
                receipt=json.loads(path.read_text())
                if receipt['status'] != 'valid':
                    pair=[]; break
                pair.append(receipt)
            if len(pair)==2: pairs.append(pair)
        result={'cell':name,'valid_pairs':len(pairs),'status':'incomplete','metrics':{}}
        if len(pairs)!=10:
            summary['cells'].append(result); continue
        for metric in ('goodput_bps','cpu_per_unit','allocated_bytes_per_unit','probe_p99_ns'):
            if any(p[v][metric] <= 0 for p in pairs for v in (0,1)):
                raise ValueError(f'{name}: undefined denominator or ratio in {metric}')
            logs=[math.log(candidate[metric]/base[metric]) for base,candidate in pairs]
            estimates=[math.exp(sum(logs[i] for i in sample)/10) for sample in resamples]
            bound=percentile(estimates,0.05 if metric=='goodput_bps' else 0.95)
            passed=bound>=0.95 if metric=='goodput_bps' else bound<=1.05
            result['metrics'][metric]={'geometric_mean_candidate_base_ratio':math.exp(sum(logs)/10),
                'one_sided_95_percent_bound':bound,'bound_direction':'lower' if metric=='goodput_bps' else 'upper',
                'pass':passed}
        differences=[candidate['probe_bad_fraction']-base['probe_bad_fraction'] for base,candidate in pairs]
        bound=percentile([sum(differences[i] for i in sample)/10 for sample in resamples],0.95)
        result['metrics']['probe_bad_fraction']={'mean_absolute_difference':sum(differences)/10,
            'one_sided_95_percent_upper_bound':bound,'pass':bound<=0.001}
        result['status']='pass' if all(m['pass'] for m in result['metrics'].values()) else 'noninferiority_not_established'
        summary['cells'].append(result)
    summary['status']='pass' if all(c['status']=='pass' for c in summary['cells']) else 'noninferiority_not_established'
    return summary


def summarize_benchmarks(root):
    manifest=json.loads((root/'manifest.json').read_text())
    if manifest['pairs'] != 10 or manifest['benchtime'] != '1s' or not manifest['benchmem']:
        raise ValueError('benchmark manifest does not match the accepted sample budget')
    expected={'BenchmarkHandshake-4'}
    samples={name:[] for name in expected}
    for pair in range(10):
        variants=[]
        for variant in ('base','candidate'):
            log=(root/f'{pair:02d}-{variant}.log').read_text()
            if not re.search(r'^PASS$',log,re.M):
                raise ValueError(f'{pair}/{variant}: benchmark did not pass')
            rows={}
            for line in log.splitlines():
                fields=line.split()
                if fields and fields[0] in expected:
                    values={fields[i+1]:float(fields[i]) for i in range(2,len(fields)-1,2)}
                    rows[fields[0]]={k:values[k] for k in ('ns/op','B/op','allocs/op')}
            if set(rows) != expected:
                raise ValueError(f'{pair}/{variant}: missing benchmark rows')
            variants.append(rows)
        for name in expected:
            samples[name].append([v[name] for v in variants])
    resamples=whole_pairs()
    results=[]
    for name,pairs in sorted(samples.items()):
        logs=[math.log(c['ns/op']/b['ns/op']) for b,c in pairs]
        upper=percentile([math.exp(sum(logs[i] for i in sample)/10) for sample in resamples],0.95)
        results.append(dict(benchmark=name,paired_samples=pairs,
                            geometric_mean_time_ratio=math.exp(sum(logs)/10),
                            one_sided_95_percent_upper_bound=upper,passed=upper<=1.05))
    return dict(seed=20260907,whole_pair_resamples=20000,benchmarks=results,
                status='pass' if all(r['passed'] for r in results) else 'noninferiority_not_established')


if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('campaign',type=Path)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--benchmarks',action='store_true')
    args=parser.parse_args()
    result=summarize_benchmarks(args.campaign) if args.benchmarks else summarize(args.campaign)
    args.output.write_text(json.dumps(result,indent=2)+'\n')
