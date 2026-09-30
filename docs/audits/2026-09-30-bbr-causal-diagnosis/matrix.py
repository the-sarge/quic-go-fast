#!/usr/bin/env python3
"""Finite screening and balanced 2x2 comparison; no concurrent endpoints."""
from pathlib import Path
import sys
RUNNER=Path(__file__).with_name('run.py')
ns={'__file__':str(RUNNER)}
exec(RUNNER.read_text().split('parser = argparse.ArgumentParser()')[0],ns)
ART=ns['ART']; run=ns['run_case']; write=ns['write']
rows=[]
stage=sys.argv[1]
if stage=='screen':
 for workload,variants in [('stream',['allocation','recovery','continuation','both']),('datagram',['both','continuation','recovery','allocation'])]:
  for variant in variants:
   rows.append(run(ART/variant,'screen-'+variant,workload,1,'bbrv3',['-trace=false']))
elif stage=='ablation':
 for workload in ['stream','datagram']:
  for variant in ['recovery-ack','recovery-pto','recovery-feedback','ecn-reuse','combined']:
   rows.append(run(ART/variant,'ablation-'+variant,workload,1,'bbrv3',['-trace=false']))
elif stage=='ablation-repeat':
 variants=['recovery-ack','recovery-pto','recovery-feedback','ecn-reuse']
 for pair in [2,3]:
  order=variants[pair-2:]+variants[:pair-2]
  for workload in ['datagram','stream'] if pair==2 else ['stream','datagram']:
   for variant in order:
    rows.append(run(ART/variant,'ablation-'+variant,workload,pair,'bbrv3',['-trace=false']))
elif stage=='diagnostics':
 for workload in ['stream','datagram']:
  for variant,controller in [('diag-profile-baseline','reno'),('diag-profile-baseline','bbrv3'),('diag-profile-recovery','bbrv3')]:
   rows.append(run(ART/variant,'diagnostics-'+variant,workload,1,controller,['-trace=false']))
elif stage=='factorial':
 conditions=[('baseline','reno'),('baseline','bbrv3'),('allocation','bbrv3'),('recovery','bbrv3'),('both','bbrv3')]
 for pair in range(1,4):
  order=conditions[pair-1:]+conditions[:pair-1]
  if pair==2: order=list(reversed(order))
  for workload in ['stream','datagram'] if pair%2 else ['datagram','stream']:
   for variant,controller in order:
    rows.append(run(ART/variant,'factorial-'+variant,workload,pair,controller,['-trace=false']))
else:
 raise ValueError(stage)
write(ART/(stage+'-summary.json'),rows)
