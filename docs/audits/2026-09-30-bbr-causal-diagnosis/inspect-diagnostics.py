#!/usr/bin/env python3
"""Independently recompute bounded sampled delivery observations."""
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
HERE=Path(__file__).resolve().parent
ART=ROOT/'.local/bbr-causal-diagnosis'
rows=[]
for directory in sorted(ART.glob('diagnostics-*')):
 if not directory.is_dir() or not (directory/'receipt.json').exists():continue
 for role in ['send','receive']:
  lines=[line.removeprefix('[BBR-CAUSAL] ') for line in (directory/(role+'.stderr')).read_text().splitlines() if line.startswith('[BBR-CAUSAL] ')]
  assert len(lines)==1,(directory,role,len(lines))
  diag=json.loads(lines[0]);r=diag.get('recovery')
  if r:
   samples=r.pop('samples')
   valid=limited=0
   for event in samples:
    acked=[p for p in (event['Acked'] or []) if p['AckEliciting'] and not p['PathProbe'] and p['PathGeneration']==event['Path'] and p['SampleGeneration']==event['Generation']]
    lost=[p for p in event['Lost'] or [] if p['PathGeneration']==event['Path'] and p['SampleGeneration']==event['Generation']]
    delivered=event['BeforeDelivered']+sum(p['Length'] for p in acked)
    loss=event['BeforeLost']+sum(p['Length'] for p in lost)
    sample=event['Sample'];assert sample['Delivered']==delivered and sample['Lost']==loss
    eligible=[p for p in acked if not p['MTUProbe']]
    if not eligible:
     assert sample['Ordinal']==0 and not sample['Valid'];continue
    anchor=max(eligible,key=lambda p:p['Ordinal']);snapshot=anchor['Delivery']
    interval=max(anchor['SendTime']-snapshot['SendOrigin'],event['Time']-snapshot['DeliveredTime'])
    assert sample['Ordinal']==anchor['Ordinal'] and sample['Interval']==interval
    assert sample['Limited']==snapshot['Limited']
    expected=(snapshot['Valid'] and interval>0 and interval>=event['MinimumRTT'] and event['Time']>anchor['SendTime'] and event['Time']>=event['BeforeTime'] and delivered>=snapshot['Delivered'] and delivered<2**64-1)
    assert sample['Valid']==expected
    rate=min(2**64-1,(delivered-snapshot['Delivered'])*10**9//interval) if expected else 0
    assert sample['BytesPerSecond']==rate
    valid+=bool(expected);limited+=bool(sample['Limited'])
   r['captured_samples']=len(samples);r['verified_valid_samples']=valid;r['verified_limited_samples']=limited
   if samples:r['captured_span_ns']=[min(s['Time'] for s in samples),max(s['Time'] for s in samples)]
  writes=diag['single_writes']+diag['batch_calls'];entries=diag['single_writes']+diag['batch_entries']
  diag['entries_per_submission']=entries/writes if writes else None
  diag['batch_entry_fraction']=diag['batch_entries']/entries if entries else None
  diag['batch_accepted_fraction']=diag['batch_accepted']/diag['batch_entries'] if diag['batch_entries'] else None
  rows.append(dict(id=directory.name,role=role,diagnostics=diag))
(HERE/'diagnostics.json').write_text(json.dumps(rows,indent=2)+'\n')
for row in rows:
 d=row['diagnostics'];r=d.get('recovery')
 print(row['id'],row['role'],'entries/submission',d['entries_per_submission'],'loops',d['loops'],'queue',d['queue_residence_histogram'],'delivery',None if r is None else {k:r[k] for k in ['feedback_events','valid_samples','lost_bytes','idle_calls','idle_one_rate','captured_samples']})
