#!/usr/bin/env python3
"""Explicit, disposable E1 capture. Never imported by normal repository gates."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import signal
import subprocess
import time

ROOT = Path('/home/josh/.cache/qgf-e1')
CELLS = [
    dict(cell='P1', mode='datagram', rate=1_000_000_000, processors=2, streams=1, gso=True),
    dict(cell='P2', mode='datagram', rate=4_000_000_000, processors=2, streams=1, gso=True),
    dict(cell='P3', mode='datagram', rate=4_000_000_000, processors=4, streams=1, gso=True),
    dict(cell='P4', mode='datagram', rate=4_000_000_000, processors=2, streams=1, gso=False),
    dict(cell='P5', mode='stream', rate=0, processors=2, streams=1, gso=True),
    dict(cell='P6', mode='stream', rate=0, processors=2, streams=16, gso=True),
]


def stop(process):
    if process and process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()


def run(cell, variant, out, measure, qlog=False):
    out.mkdir(parents=True, exist_ok=False)
    n = cell['processors']
    client_cpus = list(range(8, 8+n))
    server_cpus = list(range(12, 12+n))
    siblings = [c+16 for c in client_cpus+server_cpus]
    cpus = client_cpus+server_cpus+siblings
    env = dict(os.environ, GOMAXPROCS=str(n), QUIC_GO_DISABLE_GSO='false' if cell['gso'] else 'true')
    binary = str(ROOT / (variant+'-endpoint'))
    common = ['-qlog'] if qlog else []
    server_cmd = ['taskset', '-c', ','.join(map(str,server_cpus)), binary, '-role', 'server']+common
    receipt = dict(cell, variant=variant, measurement_seconds=measure, qlog=qlog,
                   client_cpus=client_cpus, server_cpus=server_cpus, smt_siblings=siblings,
                   go=subprocess.check_output(['go','version'],text=True).strip(),
                   binary_sha256=hashlib.sha256(Path(binary).read_bytes()).hexdigest())
    processes = []
    try:
        with (out/'server.stderr').open('w') as server_err, (out/'cpu.json').open('w') as activity:
            monitor = subprocess.Popen(['mpstat','-P',','.join(map(str,cpus)),'-o','JSON','1',str(math.ceil(measure+4))],stdout=activity, start_new_session=True)
            processes.append(monitor)
            server = subprocess.Popen(server_cmd,stdout=subprocess.PIPE,stderr=server_err,text=True,env=env,start_new_session=True)
            processes.append(server)
            line = server.stdout.readline()
            if not line:
                raise RuntimeError('server did not become ready')
            address = json.loads(line)['ready']
            client_cmd = ['taskset','-c',','.join(map(str,client_cpus)),binary,'-role','client','-addr',address,
                          '-mode',cell['mode'],'-streams',str(cell['streams']),'-rate',str(cell['rate']),'-measure',f'{measure}s']+common
            receipt.update(server_command=server_cmd,client_command=client_cmd)
            (out/'invocation.json').write_text(json.dumps(receipt,indent=2)+'\n')
            with (out/'client.stdout').open('w') as stdout, (out/'client.stderr').open('w') as stderr:
                client = subprocess.Popen(client_cmd,stdout=stdout,stderr=stderr,env=env,start_new_session=True)
                processes.append(client)
                code = client.wait(timeout=measure+20)
            tail, _ = server.communicate(timeout=10)
            (out/'server.stdout').write_text(line+tail)
            monitor.wait(timeout=10)
            if code or server.returncode or monitor.returncode:
                raise RuntimeError(f'endpoint/monitor exit: client={code},server={server.returncode},monitor={monitor.returncode}')
        data = json.loads((out/'client.stdout').read_text())
        c,s = data['Client'],data['Server']
        if c['GSO'] != cell['gso'] or s['GSO'] != cell['gso']:
            raise RuntimeError('GSO capability differs from manifest')
        if c['ProbeFailures'] or c.get('Error') or s.get('Error') or s['Invalid'] or s['Duplicate']:
            raise RuntimeError('probe or payload correctness failed')
        if c['Admitted'] <= 0 or s['Delivered'] <= 0 or s['Delivered'] > c['Admitted']:
            raise RuntimeError('invalid delivery ledger')
        if cell['mode']=='stream' and s['Delivered'] != c['Admitted']:
            raise RuntimeError('reliable delivery ledger did not close')
        if cell['mode']=='datagram' and c['Offered'] != c['Admitted']+c['IngressDrop']:
            raise RuntimeError('offered/admitted/drop ledger did not close')
        attempts = len(c['ProbeNS'])+c['ProbeMissed']+c['ProbeFailures']
        if attempts != round(measure*100):
            raise RuntimeError(f'incomplete probe ledger: {attempts}')
        activity = json.loads((out/'cpu.json').read_text())['sysstat']['hosts'][0]['statistics']
        rows = [r for sample in activity for r in sample['cpu-load'] if r['cpu'] != 'all']
        avg_guest = max(sum(r['guest'] for r in rows if int(r['cpu'])==cpu)/len(activity) for cpu in cpus)
        avg_sibling_busy = max(sum(100-r['idle'] for r in rows if int(r['cpu'])==cpu)/len(activity) for cpu in siblings)
        receipt.update(max_average_guest_percent=avg_guest,max_average_smt_sibling_busy_percent=avg_sibling_busy)
        if avg_guest > 0.1 or avg_sibling_busy > 1:
            raise RuntimeError('host contention exceeded the frozen qualification threshold')
        unit_bytes = 1071 if cell['mode']=='datagram' else 1
        receipt.update(goodput_bps=s['Delivered']*unit_bytes*8/measure,
                       cpu_per_unit=(c['CPUSeconds']+s['CPUSeconds'])/s['Delivered'],
                       allocated_bytes_per_unit=(c['AllocatedBytes']+s['AllocatedBytes'])/s['Delivered'],
                       probe_p99_ns=c['ProbeP99NS'],probe_p50_ns=c['ProbeP50NS'],
                       probe_bad_fraction=(c['ProbeMissed']+c['ProbeFailures'])/attempts,
                       offered=c['Offered'],admitted=c['Admitted'],delivered=s['Delivered'],
                       ingress_drop=c['IngressDrop'],post_admission_drop=c['Admitted']-s['Delivered'],
                       status='valid')
    except Exception as exc:
        receipt.update(status='failed',error=str(exc))
        raise
    finally:
        for process in reversed(processes):
            stop(process)
        (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
    return receipt


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--smoke',action='store_true')
    parser.add_argument('--qlog',action='store_true')
    parser.add_argument('--cells',default='P1,P2,P3,P4,P5,P6')
    args=parser.parse_args()
    if args.output.exists():
        raise SystemExit('refusing to overwrite a capture')
    args.output.mkdir(parents=True)
    cells=[c for c in CELLS if c['cell'] in args.cells.split(',')]
    frozen=dict(cells=cells,paired_samples=1 if args.smoke else 10,measurement_seconds=3 if args.smoke else 60,
                warmup_seconds=2,drain_seconds=1,processor_budget='per endpoint',probe_hz=100,probe_deadline_seconds=1,
                quic_version=1,qlog=args.qlog,datagram_bytes=1071,probe_record_bytes=64,stream_record_bytes=65536,
                initial_packet_size=1200,path_mtu_discovery=False,stream_receive_window=8<<20,connection_receive_window=64<<20,
                transport='loopback UDP, two processes; generator cost included in client CPU',
                cpu_and_allocation_interval='measurement start through drain end',
                datagram_admission_queue=32,pacer='elapsed-time offered ledger with scheduler yields',
                benchmark_flags='native go build; no compiler or GODEBUG overrides; GOMAXPROCS and GSO as listed',
                max_average_guest_percent=0.1,max_average_smt_sibling_busy_percent=1,
                endpoint_sha256=hashlib.sha256((ROOT/'endpoint.go').read_bytes()).hexdigest(),
                collector_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                base='e90617366674535bcefa8f90a2e92b153be1342b',candidate='edea78eabf72a0ac2dacd0d55dd68a8f2cabdc3a')
    (args.output/'manifest.json').write_text(json.dumps(frozen,indent=2)+'\n')
    for cell in cells:
        for pair in range(frozen['paired_samples']):
            order=['base','candidate'] if pair%2==0 else ['candidate','base']
            for variant in order:
                label=f'{cell["cell"]}-{pair:02d}-{variant}'
                print(f'{time.strftime("%Y-%m-%dT%H:%M:%S%z")} start {label}',flush=True)
                receipt=run(cell,variant,args.output/label,frozen['measurement_seconds'],args.qlog)
                print(json.dumps({k:receipt[k] for k in ('cell','variant','status','goodput_bps','probe_p99_ns','probe_bad_fraction')}),flush=True)


if __name__=='__main__':
    main()
