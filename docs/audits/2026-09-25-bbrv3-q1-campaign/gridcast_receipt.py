#!/usr/bin/env python3
"""Reconcile only the fixed 60+60 GridCast/Reno overlap, never historical Q1 windows."""
import argparse
import json
import pathlib

SECOND = 1_000_000_000


def reconcile(sender, receiver, router, application):
    reasons=[]
    def require(condition, reason):
        if not condition:reasons.append(reason)
    s=sender['result'];r=receiver['result'];run=s['run']
    require(run==r['run'], 'competitor run identities differ')
    require(run.get('gridcast_gate') is True and run.get('controller')=='reno' and run.get('workload')=='stream' and run.get('warmup_ms')==run.get('measure_ms')==60000, 'not the fixed Reno 60+60 contract')
    require(s['controller']==r['controller']=='reno', 'actual competitor controller differs')
    require(not any(x.get('error') or x['result'].get('errors') for x in (sender,receiver)), 'competitor error')
    require(s['receiver']==r['receiver'], 'receiver receipt differs between peers')
    delivery=r['receiver'];bins=delivery['per_second_bytes']
    require(len(bins)==60 and all(type(x) is int and x>=0 for x in bins) and sum(bins)==delivery['useful_bytes'], 'missing delivery bins or inconsistent total')
    require(delivery['corrupt']==0 and delivery['duplicates']==0, 'invalid competitor payload accounting')
    control=s['control']
    require(not control.get('error') and control['missed_opportunities']==control['unresolved']==0 and control['skipped']>=0 and control['offered']+control['skipped']==len(control['opportunity_times_ns'])==60 and control['offered']==control['replies']==len(control['latency_ns']) and control['replies']>0, 'control-latency window cannot be reconciled')
    start=run['start_unix_ns'];measured=start+60*SECOND;end=measured+60*SECOND
    require(all(measured<=observed<end and measured<=scheduled<end for scheduled,observed in control['opportunity_times_ns']), 'control observation outside measured window')
    offsets=control['clock_offset_bounds_ns']
    require(bool(offsets), 'missing peer clock bounds')
    lo=max((x[0] for x in offsets),default=0);hi=min((x[1] for x in offsets),default=0)
    require(lo<=hi, 'inconsistent peer clock bounds')
    # Router is on the Linux sender clock. Cover both peers' possible windows;
    # never pretend their equal epoch numbers prove clock synchronization.
    earliest=min(start,start-hi);latest=max(end,end-lo)
    require(application['start_unix_ns']<=earliest and application['end_unix_ns']>=latest and application.get('exit')==0, 'GridCast lifetime does not cover warmup plus overlap')
    require(router['Config']['Scenario']=='gridcast-coexistence' and router['Config']['Phase']==0 and not router['Profiled'], 'wrong router profile or diagnostic build')
    require(not router.get('ClockError'), 'invalid sender-owned router clock')
    require(len(router['Observations'])==2, 'missing router direction')
    for observation in router['Observations']:
        require(not observation['Error'] and not any(observation[k] for k in ('SocketDrops','NonCanonical','SendErrors','MissingTimestamps')) and observation['Stats']['PropagationOverflow']==0 and observation['MaxIngressLagNS']<=5_000_000 and observation['MaxEgressLagNS']<=5_000_000, 'router adapter validity failure')
        samples=observation['Samples']
        require(bool(samples) and samples[0]['UnixNS']<=earliest and samples[-1]['UnixNS']>=latest, 'router observations do not bracket joint window')
        relevant=[(a,b) for a,b in zip(samples,samples[1:]) if a['UnixNS']<latest and b['UnixNS']>earliest]
        require(bool(relevant), 'no joint activity observations')
        for a,b in relevant:
            require(0<b['UnixNS']-a['UnixNS']<=2*SECOND, 'missing router sampling interval')
            require(b['FocalPackets']>a['FocalPackets'], 'missing focal activity during joint interval')
            # The competitor and router share the Linux clock; its engine waits
            # until start and stops at end. Do not demand pre-start traffic.
            if a['UnixNS']<end and b['UnixNS']>start:
                require(b['CompetitorPackets']>a['CompetitorPackets'], 'missing competitor activity during joint interval')
    return dict(status='inconclusive' if reasons else 'reconciled', reasons=sorted(set(reasons)), warmup_start_unix_ns=start, measured_start_unix_ns=measured, measured_end_unix_ns=end, peer_clock_offset_bounds_ns=[lo,hi], useful_bytes=delivery['useful_bytes'],per_second_bytes=bins,control_latency_ns=control['latency_ns'],control_skipped=control['skipped'],control_unresolved=control['unresolved'],activity_evidence='cumulative canonical UDP packet counts per router sampling interval; not steady GridCast useful bytes')


def main():
    p=argparse.ArgumentParser(description=__doc__)
    for name in ('sender','receiver','router','application'):p.add_argument('--'+name,type=pathlib.Path,required=True)
    args=p.parse_args()
    try:
        result=reconcile(*(json.loads(getattr(args,k).read_text()) for k in ('sender','receiver','router','application')))
    except (KeyError,TypeError,ValueError,IndexError,AttributeError,OSError) as error:
        result=dict(status='inconclusive',reasons=['missing or malformed receipt: '+str(error)])
    print(json.dumps(result,indent=2))
    return 0 if result['status']=='reconciled' else 1


if __name__=='__main__':raise SystemExit(main())
