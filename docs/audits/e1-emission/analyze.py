#!/usr/bin/env python3
"""E1's fixed paired analysis; no adaptive exclusions or replacement pooling."""
import argparse
import json
import math
from pathlib import Path
import random


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
    rng=random.Random(20260907)
    resamples=[[rng.randrange(10) for _ in range(10)] for _ in range(20000)]
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


if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('campaign',type=Path)
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    args.output.write_text(json.dumps(summarize(args.campaign),indent=2)+'\n')
