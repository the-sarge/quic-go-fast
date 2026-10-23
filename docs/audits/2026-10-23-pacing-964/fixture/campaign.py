"""Single registered #964 campaign. Refuses restart and records before spawn."""
import argparse
import hashlib
import itertools
import json
import os
from pathlib import Path
import platform
import random
import subprocess
import sys
import time
from datetime import datetime,timezone
ROOT=Path(__file__).resolve().parent.parent
FIX=ROOT/'fixture'
RAW=ROOT/'raw'

def save(path,value):
    path.write_text(json.dumps(value,indent=2)+'\n')
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def command(cmd):return subprocess.check_output(cmd,text=True).strip()
def schedule():
    cells=[dict(rtt=rtt,rate=rate,size=size,impairment=imp) for rtt,rate,size,imp in itertools.product([10,100],[10,100],[65536,1048576],['clean','loss','reorder'])]
    random.Random(964).shuffle(cells)
    rows=[]
    for n,arm in enumerate(['control','candidate','control','candidate']):
        rows.append(dict(stage='smoke',pair=f'smoke-{n}',arm=arm,seed=96400,cell=dict(rtt=10 if n<2 else 100,rate=100 if n<2 else 10,size=65536 if n<2 else 1048576,impairment='clean')))
    for rep in range(6):
        order=cells[rep:]+cells[:rep]
        for ci,cell in enumerate(order):
            pair=f'{rep}-{cells.index(cell):02}'
            arms=['control','control'] if rep==0 else (['control','candidate'] if rep%2 else ['candidate','control'])
            for arm in arms:rows.append(dict(stage='calibration' if rep==0 else 'comparison',pair=pair,arm=arm,seed=96400+rep,cell=cell))
    for n,row in enumerate(rows,1):row['id']=f'{n:03}'
    return rows

def register():
    if (ROOT/'registration.json').exists():raise RuntimeError('registration already exists')
    prep=json.loads((ROOT/'preparation.json').read_text())
    if (datetime.now(timezone.utc)-datetime.fromisoformat(prep['started_utc'])).total_seconds()>=7200:raise RuntimeError('preparation budget exhausted')
    paths=sorted(p for p in FIX.iterdir() if p.is_file() and p.suffix in ['.py','.src','.pem'])
    paths += [ROOT/'common.patch',ROOT/'candidate-only.patch',ROOT/'build-identities.json',ROOT/'issue-brief.md']
    doc=dict(registered_utc=datetime.now(timezone.utc).isoformat(),preparation=prep,environment={'platform':platform.platform(),'python':sys.version,'go':command(['go','version']),'cpu':command(['sysctl','-n','machdep.cpu.brand_string']),'cpu_count':os.cpu_count(),'loadavg':os.getloadavg(),'host_activity':command(['ps','-Ao','pcpu,comm','-r'])[:12000]},hashes={str(p.relative_to(ROOT)):digest(p) for p in paths},schedule=schedule(),reserved_unused_smoke=4,reserved_replacements=8,connection_ceiling=304,deadline_seconds=10,network_seconds=3600,impairment='SHA256 ASCII seed:direction:ordinal, first 8 bytes big endian mod 1000000; loss <1000, reorder <10000; ordinal starts at 1; loss before queue admission',queue='ceil(payload_bytes*1e9/rate_bytes_s) ns serialization; capacity max(65536,ceil(rate_bytes_s*RTT_ns/1e9)); includes in-service; FIFO service then RTT/2 propagation then selected RTT extra reorder delay',bursts='Successful endpoint UDP WriteTo records; burst ends at inter-write start gap >100 microseconds; also 1ms fixed-bin datagrams and bytes; no GSO or OOB',metrics='Per-attempt, per-direction p50/p95/max nearest-rank on delivered datagrams; <20 insufficient; transport declared-loss event count / packet_sent event count; STREAM retransmission bytes from repeated stream-offset intervals / all STREAM bytes sent; comparisons stop at verified EOF plus teardown observations, reported explicitly',replacements='No automatic replacements; only independently documented contamination, whole pair once, up to four pairs; no losing-result retries')
    save(ROOT/'registration.json',doc)
    (ROOT/'registration.sha256').write_text(digest(ROOT/'registration.json')+'  registration.json\n')
    print('Registered',len(doc['schedule']),'planned attempts, ceiling 304',flush=True)

def wait_ready(path,proc,until):
    while time.monotonic()<until:
        if path.exists():return json.loads(path.read_text())
        if proc.poll() is not None:raise RuntimeError('process exited before ready')
        time.sleep(.005)
    raise TimeoutError('ready deadline')

def attempt(row,global_deadline,ledger):
    started=time.monotonic();deadline=min(started+10,global_deadline)
    directory=RAW/'attempts'/row['id'];directory.mkdir(parents=True)
    record=dict(row,started_utc=datetime.now(timezone.utc).isoformat(),host_load=os.getloadavg())
    with ledger.open('a') as f:
        f.write(json.dumps(dict(event='started',**record))+'\n');f.flush();os.fsync(f.fileno())
    procs=[];logs=[];commands=[];error=None
    def launch(cmd,name,env=None):
        commands.append(cmd)
        log=(directory/(name+'.log')).open('w');logs.append(log)
        proc=subprocess.Popen(cmd,stdout=log,stderr=subprocess.STDOUT,env=env);procs.append(proc);return proc
    cell=row['cell'];binary=str(ROOT/'build'/(row['arm']+'-endpoint'))
    common=['--size',str(cell['size']),'--cert',str(FIX/'cert.pem'),'--key',str(FIX/'key.pem')]
    env=dict(os.environ,GOMAXPROCS='4')
    try:
        server=launch([binary,'--mode','server','--ready',str(directory/'server.ready'),'--output',str(directory/'server.json')]+common,'server',env)
        sr=wait_ready(directory/'server.ready',server,min(deadline,time.monotonic()+1))
        relay=launch([sys.executable,str(FIX/'relay.py'),'--server',str(sr['port']),'--rate',str(cell['rate']),'--rtt',str(cell['rtt']),'--impairment',cell['impairment'],'--seed',str(row['seed']),'--ready',str(directory/'relay.ready'),'--output',str(directory/'relay.json')],'relay')
        rr=wait_ready(directory/'relay.ready',relay,min(deadline,time.monotonic()+1))
        client=launch([binary,'--mode','client','--remote',f'127.0.0.1:{rr["port"]}','--output',str(directory/'client.json')]+common,'client',env)
        client.wait(timeout=max(.001,deadline-time.monotonic()-.5))
        try:server.wait(timeout=min(.3,max(.001,deadline-time.monotonic()-.2)))
        except subprocess.TimeoutExpired:
            server.terminate();server.wait(timeout=max(.001,deadline-time.monotonic()-.1))
        relay.terminate();relay.wait(timeout=max(.001,deadline-time.monotonic()))
        for name in ['client','server','relay']:
            result=json.loads((directory/(name+'.json')).read_text())
            if result.get('Error') or result.get('error') or result.get('Overflow'):error=f'{name}: '+str(result.get('Error') or result.get('error') or 'trace overflow')
        result=json.loads((directory/'client.json').read_text())
        if not result['Verified'] or result['Bytes']!=cell['size']:error='payload failure'
        for side in ['server','client']:
            ep=json.loads((directory/(side+'.json')).read_text())
            names=[e['Name'] for e in ep['Events']]
            if not ep['Writes'] or 'assessment964:pacing' not in names or 'transport:packet_sent' not in names:raise RuntimeError('missing required instrumentation')
            if ep['TLSResumed']:raise RuntimeError('unexpected TLS resumption')
    except Exception as exc:error=repr(exc)
    finally:
        for p in reversed(procs):
            if p.poll() is None:
                p.terminate()
                try:p.wait(timeout=.15)
                except subprocess.TimeoutExpired:p.kill();p.wait(timeout=.15)
        for log in logs:log.close()
        record.update(error=error,elapsed_seconds=time.monotonic()-started,commands=commands,exit_codes=[p.returncode for p in procs])
        save(directory/'attempt.json',record)
        with ledger.open('a') as f:f.write(json.dumps(dict(event='finished',**record))+'\n');f.flush();os.fsync(f.fileno())
    return record

def run(smoke_only=False):
    reg=json.loads((ROOT/'registration.json').read_text())
    if digest(ROOT/'registration.json')!=(ROOT/'registration.sha256').read_text().split()[0]:raise RuntimeError('registration hash')
    for name,expected in reg['hashes'].items():
        if digest(ROOT/name)!=expected:raise RuntimeError('identity changed: '+name)
    for arm,info in json.loads((ROOT/'build-identities.json').read_text()).items():
        if digest(ROOT/'build'/(arm+'-endpoint'))!=info['binary_sha256']:raise RuntimeError('binary changed')
    ledger=RAW/'ledger.jsonl'
    if ledger.exists():raise RuntimeError('campaign cannot restart')
    started=time.monotonic();deadline=started+3600
    save(ROOT/'network-start.json',dict(started_utc=datetime.now(timezone.utc).isoformat(),monotonic=started))
    results=[];reason='complete';consecutive=0;pair=[]
    try:
        for row in reg['schedule']:
            if len(results)>=304 or time.monotonic()>=deadline:reason='budget';break
            if smoke_only and row['stage']!='smoke':reason='smoke-only';break
            res=attempt(row,deadline,ledger);results.append(res)
            print(row['id'],row['stage'],row['arm'],row['cell'],res['error'] or 'OK',flush=True)
            if row['stage']=='smoke' and res['error']:reason='preflight failure';break
            if res['error'] and any(s in res['error'].lower() for s in ['payload','panic','deadlock','overflow','cap','unexpected','accounting']):reason='safety stop';break
            if row['stage']=='comparison':
                pair.append(res)
                if len(pair)==2:
                    c=next(x for x in pair if x['arm']=='control');a=next(x for x in pair if x['arm']=='candidate')
                    consecutive=consecutive+1 if a['error'] and not c['error'] else 0
                    pair=[]
                    if consecutive>=2:reason='two consecutive candidate failures';break
    finally:
        save(ROOT/'network-end.json',dict(finished_utc=datetime.now(timezone.utc).isoformat(),elapsed_seconds=time.monotonic()-started,attempts=len(results),reason=reason,unrun=[r['id'] for r in reg['schedule'][len(results):]],unused_smoke=4,unused_replacements=8))
    return 0

if __name__=='__main__':
    ap=argparse.ArgumentParser();ap.add_argument('operation',choices=['register','run']);ap.add_argument('--smoke-only',action='store_true');args=ap.parse_args()
    if args.operation=='register':register()
    else:raise SystemExit(run(args.smoke_only))
