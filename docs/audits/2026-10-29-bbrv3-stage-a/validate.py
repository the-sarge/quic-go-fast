#!/usr/bin/env python3
"""One bounded local correctness pass. No benchmark or pilot execution."""
import json
import os
from pathlib import Path
import re
import subprocess
import time
from datetime import datetime, timezone
ROOT = Path(__file__).resolve().parent
NAMES = ['TestManagedPacketIOBatchDatagrams', 'TestManagedPacketIOConcurrentBatchClose',
 'TestGSOEqualSizeBatches','TestGSOEqualSizeKillSwitch','TestGSOEqualSizeSegmentLimit',
 'TestGSOEqualSizeStreamStartsFreshBatch','TestGSOEqualSizeControlPacketGoesAlone',
 'TestBBRBoundedEqualSizeCredit','TestConnectionTimerWakesForIdleMTUProbe',
 'TestMTUDiscovererTiming','TestMTUDiscovererCompletion','TestMTUDiscovererSettledOnLoss']

def main():
    source = ROOT/'build/sources/candidate'
    names = NAMES + re.findall(r'^func (Test\w+)\(', (source/'send_queue_batch_test.go').read_text(), re.M)
    pattern = '^('+'|'.join(names)+')$'
    env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOMAXPROCS='4')
    commands = [
        ('overlay-packages', ROOT, ['go','test','-count=1','./...']),
        ('overlay-vet', ROOT, ['go','vet','./...']),
        ('overlay-tidy', ROOT, ['go','mod','tidy','-diff']),
        ('candidate-named-regressions', source, ['go','test','-count=1','-run',pattern,'.']),
        ('candidate-inherited-packages', source, ['go','test','-count=1','./internal/congestion','./internal/ackhandler']),
        ('overlay-callback-close-race', ROOT, ['go','test','-race','-count=1','-run','^(TestStageAForwarding|TestStageAMetadataAndSeparation|TestNativeFixtureRoundTrip)$','./fixture']),
    ]
    receipt = {'schema':'stage-a-validation-v1','kind':'ordinary local correctness, not measurement or native readiness',
       'platform':subprocess.check_output(['go','env','GOHOSTOS','GOHOSTARCH'],text=True).strip(),
       'library_revision':'23c83b9896980511286ca8b81cd632f7ec56afed','commands':[],
       'budget_seconds':7200,'live_ledger_modified':False}
    start=time.monotonic()
    for label,cwd,argv in commands:
        now=datetime.now(timezone.utc).isoformat(); before=time.monotonic()
        result=subprocess.run(argv,cwd=cwd,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=max(1,7200-(before-start)))
        receipt['commands'].append({'label':label,'cwd':str(cwd),'argv':argv,'started_at':now,
          'finished_at':datetime.now(timezone.utc).isoformat(),'elapsed_seconds':time.monotonic()-before,
          'exit_code':result.returncode,'output':result.stdout})
        receipt['elapsed_seconds']=time.monotonic()-start
        (ROOT/'validation-receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
        print(label, result.returncode,flush=True)
        if result.returncode: raise SystemExit(result.stdout)

if __name__ == '__main__': main()
