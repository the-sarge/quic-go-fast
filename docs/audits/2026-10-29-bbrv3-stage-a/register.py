#!/usr/bin/env python3
"""Freeze Stage A commands; emit configs at a supplied epoch. Never executes them."""
import argparse
import hashlib
import json
from pathlib import Path
import shlex
from build import digest

ROOT = Path(__file__).resolve().parent
WORK = '/var/tmp/q2-stage-a'


def observations():
    build = json.loads((ROOT/'build-receipt.json').read_text())
    if build['overlay_sha256'] != digest()[0]:
        raise SystemExit('overlay differs from built source')
    rows=[]
    for workload in ('stream','datagram'):
        for arm in ('R-Reno','C-Reno','C-BBR'):
            name='reference' if arm=='R-Reno' else 'candidate'
            identity=f'M-LAN-linux-amd64-{arm}-{workload}-r1-managed-s635599-p0'
            rows.append(dict(id=identity,stage='A',ordinal=len(rows)+1,group='M-LAN',scenario='S1',
                platform='linux/amd64',arm=arm,seed=635599,event_phase_seconds=0,repetition=1,
                mode='managed-ipv4-counted-callback',build=name,**build['builds'][name],
                config=dict(id=identity,controller='bbrv3' if arm=='C-BBR' else 'reno',workload=workload,
                  warmup_ms=30000,measure_ms=180000,payload_bytes=1200 if workload=='datagram' else 16384),
                future_start_seconds=25,tail_seconds=20,envelope_seconds=255,port=26000+8*len(rows)))
    return rows


def commands(row, epoch):
    cfg=dict(row['config'],start_unix_ns=epoch)
    commands=[]
    for role,host,ip in [('receive','bbr-b','10.63.2.10'),('send','bbr-a','10.63.1.10')]:
        prefix=f'{row["id"]}-{role}'
        argv=['env','GOMAXPROCS=4','timeout','--signal=TERM','--kill-after=2','250',
              f'./{row["build"]}-fixture','-role',role,'-managed=true','-config',prefix+'-config.json',
              '-local',f'{ip}:{row["port"]}','-cert','campaign.pem','-key','campaign-key.pem','-output',prefix+'.json']
        if role=='send':argv+=['-peer',f'10.63.2.10:{row["port"]}']
        commands.append(dict(host=host,role=role,cwd=WORK,argv=argv,
          shell=shlex.join(argv)+f' >{prefix}.stdout 2>{prefix}.stderr',
          config_file=prefix+'-config.json',config=cfg,
          ready_check=(f'grep -q "^ready " {prefix}.stderr' if role=='receive' else None)))
    return commands


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--start-unix-ns',type=int)
    parser.add_argument('--index',type=int,choices=range(6))
    args=parser.parse_args()
    rows=observations()
    if args.index is not None:
        if not args.start_unix_ns or args.start_unix_ns<=0:parser.error('positive --start-unix-ns required')
        print(json.dumps({'observation':rows[args.index],'commands':commands(rows[args.index],args.start_unix_ns)},indent=2))
        return
    if args.start_unix_ns is not None:parser.error('--index required')
    registration={'schema':'q2-stage-a-registration-v1','status':'registered-preparation; launch blocked',
       'experiment_dispatch_authorized':False,'qualification_cases':520,'readiness_map_total':73,
       'source_base':'ec01159a222055f3392ce0e30900c4473cb9261b','plan_commit':'b67cc105fbc5e133c44c0f4df51a60c290c1b422',
       'overlay_sha256':digest()[0],'observations':rows,'total_envelope_seconds':1530,
       'topology':{'status':'proposed; project unavailable','hosts':['bbr-a','bbr-b','bbr-gateway'],
          'endpoint_type':'n2-standard-8, threads_per_core=1, four visible cores, 32 GiB',
          'gateway_type':'n2-standard-8, gateway_cores=4, threads_per_core=1, independent VM',
          'gateway_interfaces':['ens4','ens5'],'gateway_ips':['10.63.1.2','10.63.2.2'],
          'endpoint_ips':['10.63.1.10','10.63.2.10'],'mtu':1460,'zone_candidate':'us-central1-a',
          'routing':'S1 native kernel forwarding; no model/router process; offloads disabled on gateway',
          'competitors':False,'lease_minutes':90,'project_id':None,'image':None},
       'runtime':{'go':'go1.27.3','GOMAXPROCS':4,'managed':True,'udp_initial':1400,'pmtu':False,
          'ack_mode':'ordinary-unset','ecn_mode':'existing default; no marked-ECN claim','gso':False},
       'controller_identity':{'bbrv3':'bbrv3-draft06-classic-ecn-v1','reno':'explicit selector; result must match on both endpoints'},
       'reuse':{'maximum_per_id':1,'inventory':'73-readiness-only','qualification_credit':0,
          'conditions':['same overlay and binaries','unchanged settings','valid attributable raw evidence','comparable host/calibration','explicit eligibility decision; cross-lease order limitation retained']},
       'deferred_stage_b_ids':[f'{group}-linux-amd64-{arm}-stream-r1-managed-s635599-p0'
          for group in ('M-DELAY','M-COEX') for arm in ('C-BBR','C-Reno')],
       'epoch_binding':'For each observation once: epoch=controller time +25s; translate with valid host clock bounds. Bind exact configs/commands and hashes before launch; never reuse attempt paths.',
       'command_templates':[{'id':r['id'],'commands':commands(r,'START_UNIX_NS')} for r in rows]}
    data=(json.dumps(registration,indent=2)+'\n').encode()
    (ROOT/'registration.json').write_bytes(data)
    (ROOT/'registration.sha256').write_text(hashlib.sha256(data).hexdigest()+'  registration.json\n')

if __name__=='__main__':main()
