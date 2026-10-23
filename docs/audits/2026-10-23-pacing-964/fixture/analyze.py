"""Reproduce #964 summaries from the retained attempts directory."""
import argparse
from collections import defaultdict
import json
import math
from pathlib import Path
from statistics import median
ROOT=Path(__file__).resolve().parent.parent

def quantiles(values):
    v=sorted(values)
    if not v:return dict(n=0,p50=None,p95=None,max=None,insufficient=True)
    return dict(n=len(v),p50=v[math.ceil(.5*len(v))-1],p95=v[math.ceil(.95*len(v))-1],max=v[-1],insufficient=len(v)<20)
def ratio(a,b):return a/b if b else (1 if not a else None)
def retransmission(events):
    ranges=defaultdict(list);total=0;repeated=0
    for e in events:
        if e['Name']!='transport:packet_sent':continue
        for frame in e['Data'].get('Streams',[]):
            start=frame['Offset'];end=start+frame['Length'];total+=end-start
            old=ranges[frame['StreamID']]
            repeated+=sum(max(0,min(end,b)-max(start,a)) for a,b in old)
            merged=[]
            for a,b in sorted(old+[(start,end)]):
                if merged and a<=merged[-1][1]:merged[-1]=(merged[-1][0],max(merged[-1][1],b))
                else:merged.append((a,b))
            ranges[frame['StreamID']]=merged
    return dict(stream_bytes_sent=total,repeated_stream_bytes=repeated,repeated_fraction=ratio(repeated,total))
def bursts(writes):
    writes=[w for w in writes if not w.get('Error') and w['Size']>0]
    groups=[];sizes=[];bins=defaultdict(lambda:[0,0]);last=None
    for w in writes:
        if last is None or w['StartNS']-last>100000:groups.append(0);sizes.append(0)
        groups[-1]+=1;sizes[-1]+=w['Size'];last=w['StartNS']
        key=(last-writes[0]['StartNS'])//1000000;bins[key][0]+=1;bins[key][1]+=w['Size']
    return dict(datagrams=quantiles(groups),bytes=quantiles(sizes),nonempty_1ms_datagrams=quantiles([v[0] for v in bins.values()]),nonempty_1ms_bytes=quantiles([v[1] for v in bins.values()]),total_datagrams=len(writes))
def read_run(directory):
    meta=json.loads((directory/'attempt.json').read_text())
    out=dict(id=meta['id'],stage=meta['stage'],pair=meta['pair'],arm=meta['arm'],cell=meta['cell'],seed=meta['seed'],error=meta['error'])
    if out['error']:return out
    client=json.loads((directory/'client.json').read_text());server=json.loads((directory/'server.json').read_text());relay=json.loads((directory/'relay.json').read_text())
    out.update(request_ns=client['RequestEOFNS'],dial_ns=client['DialEOFNS'],handshake_ns=client['HandshakeNS'],goodput=client['Goodput'],verified=client['Verified'],hash=client['SHA256'],directions=[])
    for d,endpoint in enumerate([server,client]):
        rows=[r for r in relay['records'] if r['direction']==d]
        delivered=[r for r in rows if 'delivered_ns' in r]
        events=endpoint['Events'];sent=sum(e['Name']=='transport:packet_sent' for e in events);lost=sum(e['Name']=='recovery:packet_lost' for e in events)
        observations=[e['Data'] for e in events if e['Name']=='assessment964:pacing']
        drops=sum(r['status']=='injected_drop' for r in rows);overflow=sum(r['status']=='overflow' for r in rows)
        out['directions'].append(dict(ingress=len(rows),delivered=len(delivered),injected_drop=drops,injected_drop_rate=ratio(drops,len(rows)),overflow=overflow,overflow_rate=ratio(overflow,len(rows)),transport_sent=sent,transport_lost=lost,transport_loss_rate=ratio(lost,sent),retransmission=retransmission(events),wait_ns=quantiles([r['wait_ns'] for r in delivered]),latency_ns=quantiles([r['latency_ns'] for r in delivered]),serial_ns=quantiles([r['serial_ns'] for r in delivered]),prop_ns=quantiles([r['prop_ns'] for r in delivered]),reorder_ns=quantiles([r['reorder_ns'] for r in delivered]),late_ns=quantiles([r['late_ns'] for r in delivered]),occupancy_bytes=quantiles([r.get('occupancy_after',r['occupancy_before']) for r in rows]),emission_bursts=bursts(endpoint['Writes']),pacing_rate=quantiles([r['Rate'] for r in observations]),window=quantiles([r['Window'] for r in observations]),slow_start_samples=sum(r['SlowStart'] for r in observations)))
    return out

def analyze(root=ROOT):
    reg=json.loads((root/'registration.json').read_text());end=json.loads((root/'network-end.json').read_text())
    runs=[read_run(p) for p in sorted((root/'raw/attempts').iterdir())]
    cells=[];problems=[];qualifying=[]
    for cell in sorted({json.dumps(r['cell'],sort_keys=True) for r in reg['schedule'] if r['stage']=='comparison'}):
        config=json.loads(cell);selected=[r for r in runs if r['cell']==config and r['stage']!='smoke']
        calibration=[r for r in selected if r['stage']=='calibration'];comparison=[r for r in selected if r['stage']=='comparison']
        problems_cell=[];flags=[];stats={};pairs=[]
        if len(calibration)!=2 or len(comparison)!=10 or any(r['error'] for r in selected):problems_cell.append('missing or failed runs')
        else:
            a,b=calibration
            for metric in ['request_ns','dial_ns']:
                value=b[metric]/a[metric];stats['calibration_'+metric+'_ratio']=value
                if not .95<=value<=1.05:problems_cell.append('noisy calibration '+metric)
            for d in range(2):
                for metric in ['wait_ns','latency_ns']:
                    av,bv=a['directions'][d][metric],b['directions'][d][metric]
                    if av['insufficient'] or bv['insufficient']:problems_cell.append(f'insufficient calibration d{d} {metric}')
                    elif abs(bv['p95']-av['p95'])>max(2000000,.1*av['p95']):problems_cell.append(f'noisy calibration d{d} {metric}')
            grouped=defaultdict(dict)
            for r in comparison:grouped[r['pair']][r['arm']]=r
            for pair in sorted(grouped):
                control,candidate=grouped[pair]['control'],grouped[pair]['candidate']
                pairs.append(dict(pair=pair,control_id=control['id'],candidate_id=candidate['id'],request_ratio=candidate['request_ns']/control['request_ns'],dial_ratio=candidate['dial_ns']/control['dial_ns'],request_difference_ns=candidate['request_ns']-control['request_ns'],dial_difference_ns=candidate['dial_ns']-control['dial_ns']))
            for metric in ['request','dial']:
                values=[p[metric+'_ratio'] for p in pairs];stats[metric+'_ratio']=dict(median=median(values),min=min(values),max=max(values),all=values)
                if median(values)>1.05:flags.append(metric+' completion regression')
            stats['directions']=[]
            for d in range(2):
                ds={}
                for metric in ['wait_ns','latency_ns']:
                    cs=[grouped[p]['control']['directions'][d][metric] for p in sorted(grouped)];as_=[grouped[p]['candidate']['directions'][d][metric] for p in sorted(grouped)]
                    if any(v['insufficient'] for v in cs+as_):problems_cell.append(f'insufficient comparison d{d} {metric}');continue
                    cv=[v['p95'] for v in cs];av=[v['p95'] for v in as_];delta=median([a-c for a,c in zip(av,cv)])
                    ds[metric]=dict(control_median_p95=median(cv),candidate_median_p95=median(av),median_paired_increase=delta,allowed=max(2000000,.1*median(cv)))
                    if delta>ds[metric]['allowed']:flags.append(f'd{d} {metric} regression')
                for metric in ['overflow_rate','transport_loss_rate']:
                    cv=[grouped[p]['control']['directions'][d][metric] for p in sorted(grouped)];av=[grouped[p]['candidate']['directions'][d][metric] for p in sorted(grouped)];delta=median([a-c for a,c in zip(av,cv)])
                    ds[metric]=dict(control_median=median(cv),candidate_median=median(av),median_paired_increase=delta,allowed=.001)
                    if delta>.001:flags.append(f'd{d} {metric} regression')
                stats['directions'].append(ds)
            if config['rtt']==100 and config['impairment']=='clean' and stats['request_ratio']['median']<=.9 and sum(p['request_ratio']<1 for p in pairs)>=4:qualifying.append(config)
        cells.append(dict(cell=config,problems=problems_cell,regression_flags=flags,stats=stats,pairs=pairs))
        problems.extend(problems_cell)
    complete=end['reason']=='complete' and len(runs)==len(reg['schedule'])
    regressions=[dict(cell=c['cell'],flags=c['regression_flags'],usable=not c['problems']) for c in cells if c['regression_flags']]
    benefit=len(qualifying)>=2 and {c['size'] for c in qualifying}=={65536,1048576}
    decision='adopt' if complete and not problems and not regressions and benefit else 'defer'
    # Repeated in-cell screening regressions are visible evidence; noisy cells do not establish reproducibility.
    if any(r['usable'] for r in regressions):decision='reject'
    report=dict(recommendation=decision,complete=complete,attempts=len(runs),network=end,problems=sorted(set(problems)),qualifying_clean_100ms=qualifying,benefit_gate=benefit,regressions=regressions,cells=sorted(cells,key=lambda x:json.dumps(x['cell'],sort_keys=True)),runs=runs)
    (root/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({k:report[k] for k in ['recommendation','complete','attempts','problems','qualifying_clean_100ms','regressions']},indent=2))
    return report
if __name__=='__main__':
    ap=argparse.ArgumentParser();ap.add_argument('--root',type=Path,default=ROOT);args=ap.parse_args();analyze(args.root)
