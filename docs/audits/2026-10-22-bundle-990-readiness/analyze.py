#!/usr/bin/env python3
"""#990 uses #712 formulas with explicit option-C reference roles.

Called by stages.py on its selected and original observation views. Historical
comparisons use the extra old arm; they cannot change blocking flags.
"""
import json
from pathlib import Path
import analysis_load as A
from run import OBS
from campaign import readiness_observation, benefit_outcome, blocking_outcomes

HERE = Path(__file__).resolve().parent
A.rows = []
for receipt in sorted(OBS.glob('*/receipt.json')):
    A.rows.append(readiness_observation(receipt.parent))
groups, paired, flags = A.readiness('main')
aa = {(e['path'], e['workload']): e for e in flags if e['arm'] == 'reno-reno-aa'}
for e in flags:
    e['role'] = {'cand-bbrv3': 'candidate blocking', 'cand-reno': 'preservation blocking',
                 'old-reno': 'historical descriptive', 'reno-reno-aa': 'A/A descriptive'}[e['arm']]
    e['complete'] = e['blocks'] == 5
    if e['path'] == 'S6' and e['arm'] == 'cand-bbrv3':
        e['benefit_outcome'] = benefit_outcome(e['blocks'], e['useful_benefit']['pairs_above_reno'])
        e['useful_benefit']['aa'] = aa[(e['path'], e['workload'])]['ratios']['goodput']
    for k, f in e['flags'].items():
        f['aa'] = aa[(e['path'], e['workload'])]['ratios'][k]
_, historical_pairs, historical_flags = A.readiness('main', ref_arm='old-reno')
for e in historical_flags:
    e['role'] = 'historical descriptive'
    e['complete'] = e['blocks'] == 5
summary = dict(blocking_outcomes=blocking_outcomes(flags), observations=A.rows, groups=groups, paired=paired, readiness=flags,
               historical_paired=historical_pairs, historical_readiness=historical_flags,
               contaminated=[r['id'] for r in A.rows if r['contamination']['contaminated']],
               blocking_reference='8095c72244755f0c1e24f4158253bf09cfbe6e17',
               descriptive_reference='e4f322cbbfd4225a4b714e08ec19c958cccadcb0')
(HERE / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
print('observations', len(A.rows), 'blocking raised', [(e['path'], e['workload'], e['arm'], k) for e in flags if e['role'].endswith(' blocking') for k,f in e['flags'].items() if f['raised']])
print('S6 benefit', [(e['workload'], e['benefit_outcome']) for e in flags if 'benefit_outcome' in e])
