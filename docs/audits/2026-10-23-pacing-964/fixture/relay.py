"""Finite #964 UDP payload serializer. Frozen after this assessment."""
import argparse
from collections import deque
import hashlib
import heapq
import json
import selectors
import signal
import socket
import time
from pathlib import Path

MAX_RECORDS=32768
MAX_PENDING_BYTES=64*1024*1024

def selected(seed,direction,ordinal,threshold):
    word=hashlib.sha256(f'{seed}:{direction}:{ordinal}'.encode('ascii')).digest()[:8]
    return int.from_bytes(word,'big') % 1000000 < threshold

class Direction:
    def __init__(self,mbps,rtt_ms,impairment,seed,direction):
        self.rate=mbps*1000000//8
        self.rtt=rtt_ms*1000000
        self.capacity=max(65536,(self.rate*self.rtt+999999999)//1000000000)
        self.impairment,self.seed,self.direction=impairment,seed,direction
        self.ordinal=0
        self.service=deque()
        self.queued_bytes=0
        self.finish=0
    def admit(self,now,payload):
        self.ordinal+=1
        while self.service and self.service[0][0]<=now:
            self.queued_bytes-=self.service.popleft()[1]
        row=dict(direction=self.direction,ordinal=self.ordinal,ingress_ns=now,size=len(payload),wait_ns=0,serial_ns=0,prop_ns=self.rtt//2,reorder_ns=0,occupancy_before=self.queued_bytes)
        if self.impairment=='loss' and selected(self.seed,self.direction,self.ordinal,1000):
            row['status']='injected_drop'
            return row
        if self.queued_bytes+len(payload)>self.capacity:
            row['status']='overflow'
            return row
        start=max(now,self.finish)
        serial=(len(payload)*1000000000+self.rate-1)//self.rate
        self.finish=start+serial
        self.service.append((self.finish,len(payload)))
        self.queued_bytes+=len(payload)
        reorder=self.rtt if self.impairment=='reorder' and selected(self.seed,self.direction,self.ordinal,10000) else 0
        row.update(status='admitted',wait_ns=start-now,serial_ns=serial,reorder_ns=reorder,due_ns=self.finish+self.rtt//2+reorder,occupancy_after=self.queued_bytes)
        return row

def run(args):
    server=('127.0.0.1',args.server)
    sockets=[socket.socket(socket.AF_INET,socket.SOCK_DGRAM) for _ in range(2)]
    sel=selectors.DefaultSelector()
    for d,s in enumerate(sockets):
        s.bind(('127.0.0.1',0));s.setblocking(False);sel.register(s,selectors.EVENT_READ,d)
    # Socket 0 receives requests (reverse); socket 1 receives responses (forward).
    dirs=[Direction(100,args.rtt,args.impairment,args.seed,1),Direction(args.rate,args.rtt,args.impairment,args.seed,0)]
    Path(args.ready).write_text(json.dumps({'port':sockets[0].getsockname()[1],'capacities':[dirs[1].capacity,dirs[0].capacity]}))
    rows=[];pending=[];pending_bytes=0;client=None;stop=False;error=None
    def halt(*_):
        nonlocal stop
        stop=True
    signal.signal(signal.SIGTERM,halt)
    signal.signal(signal.SIGINT,halt)
    deadline=time.monotonic()+10
    try:
        while not stop and time.monotonic()<deadline:
            now=time.monotonic_ns()
            timeout=min(.01,max(0,(pending[0][0]-now)/1e9)) if pending else .01
            for key,_ in sel.select(timeout):
                d=key.data
                # Bound each read drain so due deliveries cannot be starved.
                for _ in range(64):
                    try: data,addr=key.fileobj.recvfrom(65536)
                    except BlockingIOError: break
                    now=time.monotonic_ns()
                    if d==0:
                        if client is None: client=addr
                        if addr!=client: raise RuntimeError('unexpected client address')
                    elif addr!=server: raise RuntimeError('unexpected server address')
                    if len(rows)>=MAX_RECORDS: raise RuntimeError('record cap')
                    row=dirs[d].admit(now,data);rows.append(row)
                    if row['status']=='admitted':
                        pending_bytes+=len(data)
                        if pending_bytes>MAX_PENDING_BYTES: raise RuntimeError('pending-byte cap')
                        heapq.heappush(pending,(row['due_ns'],len(rows),d,data,row))
            now=time.monotonic_ns()
            while pending and pending[0][0]<=now:
                _,_,d,data,row=heapq.heappop(pending)
                target=server if d==0 else client
                if target is None: raise RuntimeError('reverse without client')
                sent=sockets[1-d].sendto(data,target)
                if sent!=len(data): raise RuntimeError('short UDP write')
                delivered=time.monotonic_ns()
                row.update(delivered_ns=delivered,late_ns=delivered-row['due_ns'],latency_ns=delivered-row['ingress_ns'])
                pending_bytes-=len(data)
                now=delivered
    except Exception as exc:
        error=repr(exc)
    finally:
        for s in sockets: s.close()
        sel.close()
        Path(args.output).write_text(json.dumps({'error':error,'records':rows,'pending_at_stop':len(pending),'capacities':[dirs[1].capacity,dirs[0].capacity]},separators=(',',':'))+'\n')
    return 1 if error else 0

if __name__=='__main__':
    ap=argparse.ArgumentParser()
    ap.add_argument('--server',type=int,required=True);ap.add_argument('--rate',type=int,choices=[10,100],required=True)
    ap.add_argument('--rtt',type=int,choices=[10,100],required=True);ap.add_argument('--seed',type=int,required=True)
    ap.add_argument('--impairment',choices=['clean','loss','reorder'],required=True)
    ap.add_argument('--ready',required=True);ap.add_argument('--output',required=True)
    raise SystemExit(run(ap.parse_args()))
