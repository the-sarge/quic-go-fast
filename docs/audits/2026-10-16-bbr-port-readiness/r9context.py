#!/usr/bin/env python3
"""Descriptive context for the BBR production-port readiness record (#748 slice 6).

Written before any observation of this record. It reads this record's
`summary.json` and `attribution.json` (after `stages.py all`) and #739's r9
record at 138223ac (through `git show`), and writes `r9-context.json`. It sets
no flag, changes no registered rule and reads no observation directly.

- `readiness`: for every cell (path, workload, arm, measure) in this record,
  this record's median, raised flag, blocks crossing and A/A range, and beside
  it r9's median, raised flag and blocks crossing for the same cell; the S6
  useful benefit likewise. r9's values come from different runs on a different
  day, so the comparison is descriptive, not paired.
- `absolute`: each arm's medians (goodput, CPU seconds per GiB, peak RSS,
  measured-window heap peak, control p95 and p50, relay overflow rate) beside
  r9's.
- `s5timeline`: the stage and workload verdicts, and per workload the ranges
  over blocks of the decomposition, the lateness figures (late arrivals,
  total and maximum lateness per run), the phase shares and the
  connection-goroutine state shares, beside r9's.
- `cand_over_reno_on_cand`: only for a raised Reno-on-candidate cell, the
  median (minimum, maximum) over blocks of the per-block ratio candidate ÷
  Reno on candidate for the same measure. Both arms are ratios against the
  same block's frozen Reno, so frozen Reno cancels and the figure compares
  BBR with Reno on the candidate's own root code.

Usage: r9context.py
"""
import hashlib
import json
import statistics
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
R9 = '138223ac'
R9_DIR = 'docs/audits/2026-10-08-bbr-r9-linux-redemonstration'
MEASURES = ['goodput', 'send_cpu', 'receive_cpu', 'send_rss', 'receive_rss', 'control_p95']
DECOMPOSITION = ['deficit', 'idle', 'model', 'timing', 'unexplained']
LATENESS = ['late_count', 'late_ms_total', 'late_max_ms']
SHARES = ['phase_share', 'state_share']


def stats(v):
    v = [x for x in v if x is not None]
    return dict(median=statistics.median(v), minimum=min(v), maximum=max(v), n=len(v)) if v else None


def cell_key(e):
    return (e['path'], e['workload'], e['arm'])


def readiness(this, r9):
    """Every cell of this record beside r9's value for the same cell (None where r9 has no such cell)."""
    r9_cells = {cell_key(e): e for e in r9['readiness']}
    out = []
    for e in this['readiness']:
        old = r9_cells.get(cell_key(e))
        row = dict(path=e['path'], workload=e['workload'], arm=e['arm'], blocks=e['blocks'], measures={})
        for k, f in e['flags'].items():
            r = old['flags'].get(k) if old else None
            row['measures'][k] = dict(median=f['median'], raised=f['raised'], blocks_crossing=f['blocks_crossing'], aa=f.get('aa'),
                                      r9=dict(median=r['median'], raised=r['raised'], blocks_crossing=r['blocks_crossing']) if r else None)
        if 'useful_benefit' in e:
            ub = old.get('useful_benefit') if old else None
            row['useful_benefit'] = dict(e['useful_benefit'], r9=ub)
        out.append(row)
    return out


def group_medians(g):
    m = lambda s: s['median'] if s else None
    out = dict(goodput_mbps=m(g['goodput_mbps']), control_p95_ms=m(g['control_p95_ms']), control_p50_ms=m(g['control_p50_ms']))
    for role in ['send', 'receive']:
        out[role] = {k: m(g[role][k]) for k in ['cpu_seconds_per_gib', 'peak_rss_mib', 'heap_peak_measured_mib']}
    if g.get('relay'):
        out['overflow_rate'] = m(g['relay']['overflow_rate'])
    return out


def absolute(this, r9):
    r9_groups = {cell_key(g): g for g in r9['groups']}
    return [dict(path=g['path'], workload=g['workload'], arm=g['arm'], n=g['n'], medians=group_medians(g),
                 r9=group_medians(r9_groups[cell_key(g)]) if cell_key(g) in r9_groups else None)
            for g in this['groups']]


def raised_reno_on_cand(summary):
    return [(e['path'], e['workload'], k) for e in summary['readiness'] if e['arm'] == 'cand-reno'
            for k, f in e['flags'].items() if f['raised']]


def cand_over_reno_on_cand(paired, cells):
    """Per-block candidate ÷ Reno-on-candidate for each raised Reno-on-candidate cell."""
    out = []
    for path, workload, measure in cells:
        vals = [p['vs_reno']['cand-bbrv3'][measure] / p['vs_reno']['cand-reno'][measure] for p in paired
                if (p['path'], p['workload']) == (path, workload) and 'cand-bbrv3' in p['vs_reno'] and 'cand-reno' in p['vs_reno']]
        out.append(dict(path=path, workload=workload, measure=measure, ratio=stats(vals)))
    return out


def timeline_ranges(t):
    if not t or not t.get('observations'):
        return None
    out = {}
    for w in ['stream', 'datagram']:
        obs = [o for o in t['observations'] if o['workload'] == w and o.get('decomposition')]
        out[w] = dict(verdicts=[o.get('verdict') for o in obs],
                      **{k: stats([o['decomposition'][k] for o in obs]) for k in DECOMPOSITION},
                      **{k: stats([o.get(k) for o in obs]) for k in LATENESS})
        for k in SHARES:
            keys = sorted({x for o in obs for x in (o.get(k) or {})})
            out[w][k] = {x: stats([(o.get(k) or {}).get(x) for o in obs]) for x in keys}
    return dict(verdict=t.get('verdict'), workloads=t.get('workloads'), blocks=out,
                instrumented_over_readiness_goodput=t.get('instrumented_over_readiness_goodput'))


def git_json(rel):
    raw = subprocess.check_output(['git', 'show', f'{R9}:{R9_DIR}/{rel}'], cwd=ROOT)
    return json.loads(raw), hashlib.sha256(raw).hexdigest()


def main():
    this = json.loads((HERE / 'summary.json').read_text())
    attribution = json.loads((HERE / 'attribution.json').read_text())
    r9, r9_summary_sha = git_json('summary.json')
    r9_attr, r9_attr_sha = git_json('attribution.json')
    cells = raised_reno_on_cand(this)
    out = dict(sources=dict(r9_record=R9, r9_summary_sha256=r9_summary_sha, r9_attribution_sha256=r9_attr_sha),
               readiness=readiness(this, r9), absolute=absolute(this, r9),
               s5timeline=dict(this=timeline_ranges(attribution.get('s5timeline')), r9=timeline_ranges(r9_attr.get('s5timeline'))),
               raised_reno_on_cand=cells, cand_over_reno_on_cand=cand_over_reno_on_cand(this['paired'], cells))
    (HERE / 'r9-context.json').write_text(json.dumps(out, indent=1) + '\n')
    for row in out['readiness']:
        cells_txt = ' '.join(f"{k}={m['median']:.3f}{'*' if m['raised'] else ''}"
                             + (f"[r9 {m['r9']['median']:.3f}{'*' if m['r9']['raised'] else ''}]" if m['r9'] else '[r9 -]')
                             for k, m in row['measures'].items())
        print(f"{row['path']:8} {row['workload']:8} {row['arm']:13} {cells_txt}")
    for c in out['cand_over_reno_on_cand']:
        print('cand/reno-on-cand', c)


if __name__ == '__main__':
    sys.exit(main())
