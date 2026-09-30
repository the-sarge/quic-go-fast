#!/usr/bin/env python3
"""Validate finite observations and report conditional intervention effects."""
import hashlib
import json
import math
from pathlib import Path
import statistics
ROOT=Path(__file__).resolve().parents[3]
HERE=Path(__file__).resolve().parent
ART=ROOT/'.local/bbr-causal-diagnosis'
rows=[]
for receipt_path in sorted(ART.glob('*/receipt.json')):
 d=receipt_path.parent
 receipt=json.loads(receipt_path.read_text())
 assert receipt['exit_codes']==[0,0],d
 tx=json.loads((d/'send.json').read_text());rx=json.loads((d/'receive.json').read_text())
 cfg=tx['result']['run'];delivery=rx['result']['receiver']
 assert delivery==tx['result']['receiver']
 assert delivery['corrupt']==delivery['duplicates']==0
 assert sum(delivery['per_second_bytes'])==delivery['useful_bytes']
 assert not tx.get('error') and not rx.get('error')
 for rec in [tx,rx]:
  assert not rec['result'].get('errors')
  assert rec['result']['controller']==cfg['controller']
  assert rec['gomaxprocs']==4 and rec['managed_direct']
  assert rec['go_version']=='go1.27.0' and rec['platform']=='darwin/arm64'
 assert cfg['warmup_ms']==5000 and cfg['measure_ms']==20000
 gib=delivery['useful_bytes']/2**30
 control=tx['result']['control'];lat=sorted(control['latency_ns'])
 row=dict(id=cfg['id'],controller=cfg['controller'],workload=cfg['workload'],source=tx['source'],trace_enabled=tx['trace_enabled'],goodput_mbps=delivery['useful_bytes']*.4/1e6,useful_bytes=delivery['useful_bytes'],control_p95_ms=lat[math.ceil(len(lat)*.95)-1]/1e6,control_replies=control['replies'],control_skipped=control['skipped'],control_unresolved=control['unresolved'],control_missed=control['missed_opportunities'],binary_sha256=receipt['binary_sha256'])
 for role,rec in [('send',tx),('receive',rx)]:
  resource=rec['resources']
  row[role]=dict(total_cpu_seconds=resource['cpu_seconds'],cpu_seconds_per_gib=resource['cpu_seconds']/gib,peak_rss_mib=resource['peak_rss_bytes']/2**20,allocated_gib=resource['total_alloc_bytes']/2**30,allocated_gib_per_gib=resource['total_alloc_bytes']/delivery['useful_bytes'])
 row['combined_cpu_seconds_per_gib']=row['send']['cpu_seconds_per_gib']+row['receive']['cpu_seconds_per_gib']
 rows.append(row)

def range_stats(values):
 return dict(median=statistics.median(values),minimum=min(values),maximum=max(values),n=len(values))
groups=[]
for workload in ['stream','datagram']:
 for variant,controller in [('baseline','reno'),('baseline','bbrv3'),('allocation','bbrv3'),('recovery','bbrv3'),('both','bbrv3')]:
  selected=[r for r in rows if r['workload']==workload and r['controller']==controller and r['id'].startswith('factorial-'+variant+'-')]
  if not selected:continue
  group=dict(workload=workload,variant=variant,controller=controller,goodput_mbps=range_stats([r['goodput_mbps'] for r in selected]),control_p95_ms=range_stats([r['control_p95_ms'] for r in selected]))
  for role in ['send','receive']:
   group[role]={key:range_stats([r[role][key] for r in selected]) for key in selected[0][role]}
  groups.append(group)
ablations=[]
for workload in ['stream','datagram']:
 for variant in ['recovery-ack','recovery-pto','recovery-feedback','ecn-reuse']:
  selected=[row for row in rows if row['id'].startswith('ablation-'+variant+'-'+workload+'-')]
  if selected:
   ablations.append(dict(workload=workload,variant=variant,goodput_mbps=range_stats([row['goodput_mbps'] for row in selected]),sender_cpu_seconds_per_gib=range_stats([row['send']['cpu_seconds_per_gib'] for row in selected]),sender_allocated_gib_per_gib=range_stats([row['send']['allocated_gib_per_gib'] for row in selected])))
pairs=[]
for workload in ['stream','datagram']:
 for pair in [1,2,3]:
  block={}
  for variant,controller in [('reno','reno'),('baseline','bbrv3'),('allocation','bbrv3'),('recovery','bbrv3'),('both','bbrv3')]:
   prefix='factorial-'+('baseline' if variant=='reno' else variant)+'-'+workload+'-p'+str(pair)+'-'+controller
   matches=[row for row in rows if row['id']==prefix]
   if matches:block[variant]=matches[0]
  if len(block)!=5:continue
  ref=block['reno']; ratios={}
  for variant in ['baseline','allocation','recovery','both']:
   row=block[variant]
   ratios[variant]=dict(goodput=row['goodput_mbps']/ref['goodput_mbps'],sender_cpu=row['send']['cpu_seconds_per_gib']/ref['send']['cpu_seconds_per_gib'],receiver_cpu=row['receive']['cpu_seconds_per_gib']/ref['receive']['cpu_seconds_per_gib'],combined_cpu=row['combined_cpu_seconds_per_gib']/ref['combined_cpu_seconds_per_gib'],sender_rss=row['send']['peak_rss_mib']/ref['send']['peak_rss_mib'],receiver_rss=row['receive']['peak_rss_mib']/ref['receive']['peak_rss_mib'],control_p95=row['control_p95_ms']/ref['control_p95_ms'])
  effect={}
  for output in ['goodput_mbps','combined_cpu_seconds_per_gib']:
   base=block['baseline'][output];a=block['allocation'][output];r=block['recovery'][output];ar=block['both'][output]
   effect[output]=dict(allocation_with_original_recovery=a-base,allocation_with_recovery_intervention=ar-r,recovery_with_original_allocation=r-base,recovery_with_allocation_intervention=ar-a,interaction=ar-r-a+base)
  pairs.append(dict(workload=workload,pair=pair,ratios=ratios,conditional_effects=effect))
(HERE/'summary.json').write_text(json.dumps(dict(observations=rows,factorial=groups,paired_factorial=pairs,ablations=ablations),indent=2)+'\n')
for g in groups:
 print(g['workload'],g['variant'],g['controller'],'goodput',g['goodput_mbps'],'cpu',g['send']['cpu_seconds_per_gib'],'rss',g['send']['peak_rss_mib'])
print('validated observations',len(rows))
