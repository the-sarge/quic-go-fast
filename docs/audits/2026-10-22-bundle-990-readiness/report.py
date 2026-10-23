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
                not_run_presrss=[list(pair) for pair in rss[2:]],not_run_preslat=lat[1:],
                readiness_sha256=__import__('hashlib').sha256((HERE/'summary.json').read_bytes()).hexdigest())


def render(s,a,loss,history,inventory,plan):
    lines=['# #990 Linux readiness result','',f"Candidate and blocking Reno reference: `{s['blocking_reference']}`.",
           f"Historical descriptive Reno reference: `{s['descriptive_reference']}`.",'',
           'BBRv3 remains experimental, opt-in and unqualified; Reno remains default. This result grants no advancement. The fixture has no batched-send callback, so neither #858’s batch cap nor gluing is measured. macOS and Windows are not qualified.','',
           'The independently built candidate/reference Reno plain and diagnostic binaries are identical. Their preservation ratios do not establish preservation against old frozen Reno; that comparison remains descriptive.', '',
           'The first table uses the intended bundle Reno denominator. Complete cells use medians of five paired ratios. Incomplete cells show the available clean complete blocks and remain evidence gaps; raised values remain raised despite A/A overlap or follow-up outcomes. A/A and old-Reno rows are descriptive.','',
           '| Path/workload | Arm/role (blocks) | Metric | Median (min–max) | Crossing blocks | Outcome | A/A min–max |',
           '| --- | --- | --- | --- | --- | --- | --- |']
    for e in s['readiness']:
        for k in KEYS:
            r=e['ratios'][k];f=e['flags'].get(k);aa=f.get('aa') if f else e.get('useful_benefit',{}).get('aa')
            raised=str(f['raised']) if f else f"S6 benefit {e.get('benefit_outcome', 'descriptive')} ({e['useful_benefit']['pairs_above_reno']}/5)"
            if not e['complete']: raised='incomplete evidence gap; observed '+raised
            lines.append(f"| {e['path']}/{e['workload']} | {e['arm']} ({e['role']}; {e['blocks']}/5) | {k} | {r['median']:.3f} ({r['minimum']:.3f}–{r['maximum']:.3f}) | {f['blocks_crossing'] if f else '—'} | {raised} | {f'{aa["minimum"]:.3f}–{aa["maximum"]:.3f}' if aa else '—'} |")
    lines+=['','Blocking outcomes (including unmet S6 benefit and evidence gaps):','','```json',json.dumps(s['blocking_outcomes'],indent=2),'```']
    lines+=['','## Coverage and retained gaps','','Only complete, usable, clean blocks enter selected statistics. Missing, fatal and unresolved attempts remain below and in the raw archive. An incomplete row is not a pass.','', '| Stage/workload | Clean blocks / expected | Status |','| --- | --- | --- |']
    groups={}
    for record in inventory:
        groups.setdefault((record['stage'],record['workload']),[]).append(record)
    for (stage,workload),records in sorted(groups.items()):
        clean=sum(r['status']=='clean' for r in records)
        status='complete observations' if clean==len(records) else 'incomplete; evidence gap'
        lines.append(f'| {stage}/{workload} | {clean}/{len(records)} | {status} |')
    gaps=[r for r in inventory if r['status']!='clean']
    lines+=['', 'Retained gaps:', '', '```json',json.dumps(gaps,indent=2),'```', '',
            'Conditional selection and omissions: pairs in `not_run_presrss` and workloads in `not_run_preslat` are **not run (selection limit), evidence gap**. Their raised preservation values remain blocking.', '', '```json',json.dumps(plan,indent=2),'```','']
    for path in ['loopback','S5','S6']:
        for workload in ['stream','datagram']:
            arms=['cand-bbrv3']+(['cand-reno'] if path!='S6' else [])
            for arm in arms:
                if not any((e['path'],e['workload'],e['arm'])==(path,workload,arm) for e in s['readiness']):
                    lines.append(f'{path}/{workload}/{arm}: incomplete (0 of 5); all readiness metrics are evidence gaps.')
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
    lines+=['','## Timeline, preservation and loss','','These raw-derived structures retain the registered classifications and their supporting values. Follow-up outcomes never automatically clear a raised readiness cell.','', '```json',json.dumps(attribution_digest(a),indent=2),'```','', 'DATAGRAM loss is descriptive:','', '```json',json.dumps(loss['cells'],indent=2),'```','']
    return '\n'.join(lines)


def attribution_digest(a):
    required={'s5timeline','presrss','preslat','cpu_split'}
    assert required <= set(a), 'missing required attribution outputs'
    timeline=a['s5timeline']
    if 'calculation_error' in timeline:
        t=timeline
    else:
        t={k:timeline[k] for k in ['workloads','verdict','instrumented_over_readiness_goodput'] if k in timeline}
        t['observations']=[{k:o[k] for k in ['id','workload','block','goodput_mbps','decomposition','verdict'] if k in o} for o in timeline['observations']]
    cpu=a['cpu_split']
    if 'calculation_error' not in cpu:
        cpu={cell:{k:value[k] for k in ['flag_raised','registered','window_ratio','outside_share','outcome'] if k in value} for cell,value in cpu.items()}
    return dict(s5timeline=t,presrss=a['presrss'],preslat=a['preslat'],cpu_split=cpu)


def derive_outputs():
    """Derive every decisive cache from current selected raw evidence."""
    import dgloss
    S.readiness()
    for key,fn in [('receiver',S.receiver),('s5timeline',S.s5timeline),('presrss',S.presrss),('preslat',S.preslat)]:
        try:
            fn()
        except (AssertionError, OSError, ValueError, KeyError, TypeError, ZeroDivisionError) as exc:
            # A missing diagnostic instrument is a reported gap, never a stale
            # classification. It does not make any measurement rerunnable.
            S.save('cpu_split' if key=='receiver' else key, dict(outcome='evidence gap',calculation_error=f'{type(exc).__name__}: {exc}'))
    dgloss.main()


def cache_values():
    values={}
    for name in ['summary.json','attribution.json','datagram-loss.json']:
        p=HERE/name
        values[name]=json.loads(p.read_text()) if p.exists() else None
    return values


def verify_raw_correspondence():
    before=cache_values()
    derive_outputs()
    after=cache_values()
    keys=['s5timeline','presrss','preslat','cpu_split']
    old_a=before['attribution.json'] or {}
    new_a=after['attribution.json'] or {}
    mismatch=(before['summary.json']!=after['summary.json'] or before['datagram-loss.json']!=after['datagram-loss.json']
              or any(k not in old_a or k not in new_a or old_a[k]!=new_a[k] for k in keys))
    if mismatch:
        import time
        target=C.run.ART/f'cache-mismatch-{time.time_ns()}.json'
        target.write_text(json.dumps(dict(before=before,after=after),indent=2)+'\n')
        raise AssertionError('decisive cache differs from raw reload; mismatch retained')
    return after


def main():
    s=S.readiness_summary()
    if '--plan' in sys.argv:
        C.selection('main') # Only complete readiness authorizes conditional execution.
        plan=conditional_plan();p=C.run.ART/'conditional-plan.json'
        if p.exists():assert json.loads(p.read_text())==plan,'conditional plan changed'
        else:p.write_text(json.dumps(plan,indent=2)+'\n')
        print(json.dumps(plan));return
    caches=verify_raw_correspondence()
    s=caches['summary.json']
    plan_path=C.run.ART/'conditional-plan.json'
    if plan_path.exists():
        plan=conditional_plan()
        assert json.loads(plan_path.read_text())==plan, 'conditional plan changed'
    else:
        # A gap report is still possible before readiness can authorize a plan.
        _,records=C.reporting_inventory('main')
        assert any(r['status']!='clean' for r in records), 'complete readiness requires conditional plan'
        plan=dict(selected=[],status='not registered: incomplete readiness; evidence gap')
    inventory=[]
    for phase in ['main','s5timeline','presrss','preslat']:
        inventory+=C.reporting_inventory(phase)[1]
    result=render(s,caches['attribution.json'],caches['datagram-loss.json'],json.loads(gzip.decompress((HERE/'historical-context.json.gz').read_bytes())),inventory,plan)
    output=HERE/'result.md'
    if '--check' in sys.argv:assert output.read_text()==result,'decisive prose differs from JSON'
    else:output.write_text(result)


if __name__=='__main__':main()
