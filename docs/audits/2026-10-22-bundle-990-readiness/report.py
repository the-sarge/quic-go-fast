#!/usr/bin/env python3
"""Generate #990 decision prose from raw-derived JSON; --check is exact.

Historical values retain their old denominator, explicitly beside the current
old-reference comparison. They are not like-for-like new-reference ratios.
"""
import json
import gzip
from pathlib import Path
import sys
import stages as S
import campaign as C

HERE = Path(__file__).resolve().parent
KEYS = ['goodput', 'send_cpu', 'receive_cpu', 'send_rss', 'receive_rss', 'control_p95']


def conditional_plan():
    s=S.readiness_summary()
    rss=sorted({(p,w) for p,w,_ in S.raised(s,'cand-reno','rss')})
    lat=sorted({w for p,w,_ in S.raised(s,'cand-reno','p95') if p=='loopback'})
    return dict(selected=[f'presrss-{p}-{w}' for p,w in rss[:2]]+[f'preslat-{w}' for w in lat[:1]],
                not_run_presrss=rss[2:],not_run_preslat=lat[1:],
                readiness_sha256=__import__('hashlib').sha256((HERE/'summary.json').read_bytes()).hexdigest())


def render(s,a,loss,history):
    lines=['# #990 Linux readiness result','',f"Candidate and blocking Reno reference: `{s['blocking_reference']}`.",
           f"Historical descriptive Reno reference: `{s['descriptive_reference']}`.",'',
           'BBRv3 remains experimental, opt-in and unqualified; Reno remains default. This result grants no advancement. The fixture has no batched-send callback, so neither #858’s batch cap nor gluing is measured. macOS and Windows are not qualified.','',
           'The first table uses the intended bundle Reno denominator. Every value is a median of five paired ratios; raised values remain raised despite A/A overlap or follow-up outcomes. A/A and old-Reno rows are descriptive.','',
           '| Path/workload | Arm/role | Metric | Median (min–max) | Crossing blocks | Raised | A/A min–max |',
           '| --- | --- | --- | --- | --- | --- | --- |']
    for e in s['readiness']:
        assert e['complete'] and e['blocks']==5
        for k in KEYS:
            r=e['ratios'][k];f=e['flags'].get(k);aa=f.get('aa') if f else None
            raised=str(f['raised']) if f else f"S6 benefit {e['useful_benefit']['pairs_above_reno']}/5"
            lines.append(f"| {e['path']}/{e['workload']} | {e['arm']} ({e['role']}) | {k} | {r['median']:.3f} ({r['minimum']:.3f}–{r['maximum']:.3f}) | {f['blocks_crossing'] if f else '—'} | {raised} | {f'{aa["minimum"]:.3f}–{aa["maximum"]:.3f}' if aa else '—'} |")
    lines+=['','## Absolute medians','','| Path/workload | Arm | Goodput Mbit/s | CPU s/GiB send/receive | RSS MiB send/receive | Control p95/p50 ms |','| --- | --- | --- | --- | --- | --- |']
    for g in s['groups']:
        lines.append(f"| {g['path']}/{g['workload']} | {g['arm']} | {g['goodput_mbps']['median']:.3f} | {g['send']['cpu_seconds_per_gib']['median']:.3f}/{g['receive']['cpu_seconds_per_gib']['median']:.3f} | {g['send']['peak_rss_mib']['median']:.3f}/{g['receive']['peak_rss_mib']['median']:.3f} | {g['control_p95_ms']['median']:.3f}/{g['control_p50_ms']['median']:.3f} |")
    lines+=['','## Per-block ratios','','Every six-value array is goodput, sender CPU, receiver CPU, sender RSS, receiver RSS and control p95, in that order.','', '| Path/workload/block | Arm | Versus bundle Reno |','| --- | --- | --- |']
    for p in s['paired']:
        for arm,vals in sorted(p['vs_reno'].items()):
            lines.append(f"| {p['path']}/{p['workload']}/{p['block']} | {arm} | "+', '.join(f'{vals[k]:.3f}' for k in KEYS)+' |')
    lines+=['','## Historical comparison (descriptive)','','Current values below divide by the same-block old frozen Reno. Historical r9, port and #808 values divide by their own old frozen Reno observations, on different days. These are descriptive comparisons, never replacements for the current blocking-reference table. This also reports #738’s carried S5 DATAGRAM sender CPU cost and #734’s CPU-per-useful-GiB proxy for wake cost; wake counts are not measured.','',
            '| Path/workload | Arm/metric | Current old-reference ratio | r9 | Port | #808 |','| --- | --- | --- | --- | --- | --- |']
    for e in s['historical_readiness']:
        if e['arm'] not in ('cand-bbrv3','cand-reno'):continue
        for k in KEYS:
            cells=[]
            for name in ['r9','port','prior808']:
                old=next((v for v in history[name]['summary.json']['readiness'] if (v['path'],v['workload'],v['arm'])==(e['path'],e['workload'],e['arm'])),None)
                value=old['ratios'][k]['median'] if old else None
                cells.append(f'{value:.3f}' if value is not None else '—')
            lines.append(f"| {e['path']}/{e['workload']} | {e['arm']}/{k} | {e['ratios'][k]['median']:.3f} | "+' | '.join(cells)+' |')
    lines+=['','## Timeline, preservation and loss','','These raw-derived structures retain the registered classifications and their supporting values. Follow-up outcomes never automatically clear a raised readiness cell.','', '```json',json.dumps({k:a[k] for k in ['s5timeline','presrss','preslat','cpu_split'] if k in a},indent=2),'```','', 'DATAGRAM loss is descriptive:','', '```json',json.dumps(loss['cells'],indent=2),'```','']
    return '\n'.join(lines)


def main():
    s=S.readiness_summary()
    if '--plan' in sys.argv:
        plan=conditional_plan();p=C.run.ART/'conditional-plan.json'
        if p.exists():assert json.loads(p.read_text())==plan,'conditional plan changed'
        else:p.write_text(json.dumps(plan,indent=2)+'\n')
        print(json.dumps(plan));return
    # Recompute from the retained observations; a cached summary alone cannot
    # establish any decisive number. Preserve mismatches for investigation.
    prior_bytes=(HERE/'summary.json').read_bytes()
    S.readiness()
    if S.readiness_summary()!=s:
        (C.run.ART/'summary-before-reload.json').write_bytes(prior_bytes)
        raise AssertionError('readiness cache differs from raw reload')
    C.selection('main');C.selection('s5timeline')
    plan=conditional_plan()
    assert json.loads((C.run.ART/'conditional-plan.json').read_text())==plan
    for stage in plan['selected']:
        phase,path,arms,n,base,runner,opts,workload=C.matrix.STAGES[stage]
        for b in range(1,n+1):C.selected(stage,workload,b)
    a=json.loads((HERE/'attribution.json').read_text())
    result=render(s,a,json.loads((HERE/'datagram-loss.json').read_text()),json.loads(gzip.decompress((HERE/'historical-context.json.gz').read_bytes())))
    output=HERE/'result.md'
    if '--check' in sys.argv:assert output.read_text()==result,'decisive prose differs from JSON'
    else:output.write_text(result)


if __name__=='__main__':main()
