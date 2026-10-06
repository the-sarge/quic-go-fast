#!/usr/bin/env python3
"""#735 descriptive profile and registered lead-selection rule.

Written, with synthetic cases in lead_profile_test.py, before any profile observation
existed. A profile selects leads; it never attributes a cause.

- Groups: #712's `classify_stack` and `localize` (localize.py, byte-identical),
  applied to r8 against frozen Reno at both endpoints. Descriptive only.
- Sites: every BBR-only function (BBR_ONLY below: #712's `BBR` pattern plus the
  root package's BBR-only emission, pacing and credit code). Its cost is its
  inclusive cycles per useful GiB at the r8 sender (each sample counts once per
  site on its stack). Reno never executes these functions, so this is their
  whole cost, not a difference.
- Ranking and selection (`select_leads`): see README.md, "Registration: profile
  and lead selection".

Usage (on minimax, where the root-owned perf.data lives): lead_profile.py fold
       anywhere holding the folded stacks: lead_profile.py > profile.json
"""
import gzip
import json
import re
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402

run.ART = HERE.parents[2] / '.local/bbr-per-packet-cpu-leads'
run.OBS = run.ART / 'observations'
import localize  # noqa: E402
from localize import BBR, GROUPS, fold, group_cycles  # noqa: E402

localize.OBS = run.OBS
# Functions defined in the root package's BBR-only files at r8 (bbr_*.go, local_send_credit.go,
# packet_emission_bbr.go, pacing_kick_linux.go), including their closures.
ROOT_BBR_ONLY = re.compile(
    r'quic-go\.\(\*packetEmission\)\.(sendBounded|boundedDatagrams|handoff|localBlocked|reserveLocal|resetLocalPath'
    r'|opportunityCapabilities|waitForReservation|enableBBR|enableBBRECN)(\.func\d+)*$'
    r'|quic-go\.\(\*(bbrSendPolicy|sendReservation|localSendCredit|pacingKick)\)\.'
    r'|quic-go\.(newBBRSendPolicy|ceilSendDelay|newLocalSendCredit|newPacingKick)$'
    r'|quic-go\.\(\*Conn\)\.traceBBR')
CONTAINER_SHARE = 0.25   # a site holding more than this share of r8's sender cycles in every block is a container, not a lead
FLOOR = 0.15e9           # cycles per useful GiB: a lead's median cost must reach this
OVERLAP = 0.5            # a site with this share of its cycles under an already-selected lead overlaps it
MAX_LEADS = 3


def bbr_only(frame):
    return bool(BBR.search(frame) or ROOT_BBR_ONLY.search(frame))


def site_cycles(folded, gib):
    """{site: inclusive cycles per useful GiB} for BBR-only functions, and the total."""
    out, total = {}, 0.0
    for key, cycles in folded.items():
        total += cycles
        for f in {f for f in (key.split(';') if key else []) if bbr_only(f)}:
            out[f] = out.get(f, 0.0) + cycles
    return {k: v / gib for k, v in out.items()}, total / gib


def co_cycles(folded, gib, site, others):
    """Cycles per GiB of samples containing `site` and any of `others` (overlap with selected leads)."""
    s = 0.0
    for key, cycles in folded.items():
        frames = set(key.split(';')) if key else set()
        if site in frames and frames & set(others):
            s += cycles
    return s / gib


def select_leads(blocks, eligible):
    """Registered selection.

    blocks: [{'sites': {site: cyc/GiB}, 'total': cyc/GiB, 'overlap': callable(site, selected) -> cyc/GiB}], one per
    workload-block of the r8 sender. eligible: {site: (bool, reason)} from the source-reading eligibility judgement
    (README); a site absent from it is not yet judged and stops selection there.
    Returns the ranked table and the selected sites.
    """
    sites = sorted({s for b in blocks for s in b['sites']})
    table = []
    for s in sites:
        vals = [b['sites'].get(s, 0.0) for b in blocks]
        shares = [b['sites'].get(s, 0.0) / b['total'] for b in blocks]
        table.append(dict(site=s, median=statistics.median(vals), min=min(vals), blocks_positive=sum(v > 0 for v in vals),
                          container=all(x > CONTAINER_SHARE for x in shares)))
    table.sort(key=lambda r: -r['median'])
    selected, decisions = [], []
    for r in table:
        if len(selected) == MAX_LEADS:
            break
        s = r['site']
        if r['median'] < FLOOR:
            decisions.append((s, 'below the floor; selection ends'))
            break
        if r['container']:
            decisions.append((s, 'container'))
            continue
        if r['blocks_positive'] < len(blocks):
            decisions.append((s, 'not present in every block'))
            continue
        if selected:
            ov = statistics.median(b['overlap'](s, selected) / b['sites'][s] for b in blocks)
            if ov >= OVERLAP:
                decisions.append((s, f'overlaps a selected lead ({ov:.2f})'))
                continue
        if s not in eligible:
            decisions.append((s, 'eligibility not yet judged; selection pauses'))
            break
        ok, why = eligible[s]
        if not ok:
            decisions.append((s, 'ineligible: ' + why))
            continue
        selected.append(s)
        decisions.append((s, 'selected: ' + why))
    return dict(table=table, decisions=decisions, selected=selected)


def load_profile():
    rows = {}
    for p in sorted(run.OBS.glob('profile*-S5-*/receipt.json')):
        d = p.parent
        r = run.summarize(d)
        meta = json.loads((d / 'meta.json').read_text())
        rows.setdefault((meta['phase'], r['workload'], meta['pair']), {})[meta['variant']] = (d, r)
    return rows


def main():
    eligible = {k: tuple(v) for k, v in json.loads((HERE / 'eligibility.json').read_text()).items()} \
        if (HERE / 'eligibility.json').exists() else {}
    rows = load_profile()
    groups, blocks, folds = {}, [], {}
    for (phase, wl, block), arms in sorted(rows.items()):
        if phase != 'profile' or set(arms) != {'reno', 'r8'}:
            continue
        g = {}
        for role in ['send', 'receive']:
            g[role] = {}
            for name, (d, r) in arms.items():
                folded = json.loads(gzip.decompress((d / f'{role}.folded.json.gz').read_bytes()))
                gib = r['useful_bytes'] / 2**30
                g[role][name] = {k: v / gib for k, v in group_cycles(folded).items()}
                if role == 'send' and name == 'r8':
                    folds[(wl, block)] = (folded, gib)
        groups.setdefault(wl, []).append(dict(block=block, g=g))
    out = dict(groups={})
    for wl, bl in groups.items():
        for role in ['send', 'receive']:
            res = localize.localize([dict(cand=b['g'][role]['r8'], reno=b['g'][role]['reno']) for b in bl])
            out['groups'][f'{wl}/{role}'] = dict(res, group_cycles_per_gib=[dict(block=b['block'], **b['g'][role]) for b in bl])
    for (wl, block), (folded, gib) in sorted(folds.items()):
        sites, total = site_cycles(folded, gib)
        blocks.append(dict(workload=wl, block=block, sites=sites, total=total,
                           overlap=lambda s, sel, f=folded, g=gib: co_cycles(f, g, s, sel)))
    sel = select_leads(blocks, eligible)
    out['sites'] = dict(blocks=[dict(workload=b['workload'], block=b['block'], total=b['total']) for b in blocks],
                        table=sel['table'][:40], decisions=sel['decisions'], selected=sel['selected'])
    out['leaf_excess'] = leaf_excess(rows)
    print(json.dumps(out, indent=2))


def leaf_excess(rows):
    """Descriptive: the 30 leaf functions with the largest median sender excess (r8 minus Reno), any code."""
    per = {}
    for (phase, wl, block), arms in rows.items():
        if phase != 'profile' or set(arms) != {'reno', 'r8'}:
            continue
        v = {}
        for name, (d, r) in arms.items():
            folded = json.loads(gzip.decompress((d / 'send.folded.json.gz').read_bytes()))
            gib = r['useful_bytes'] / 2**30
            leaf = {}
            for key, cycles in folded.items():
                f = key.split(';')[0] if key else '[unknown]'
                leaf[f] = leaf.get(f, 0.0) + cycles / gib
            v[name] = leaf
        for f in set(v['r8']) | set(v['reno']):
            per.setdefault(f, []).append(v['r8'].get(f, 0.0) - v['reno'].get(f, 0.0))
    n = max((len(x) for x in per.values()), default=0)
    med = {f: statistics.median(x + [0.0] * (n - len(x))) for f, x in per.items()}
    return [dict(leaf=f, median_excess=m) for f, m in sorted(med.items(), key=lambda kv: -kv[1])[:30]]


if __name__ == '__main__':
    if sys.argv[1:] == ['fold']:
        for p in sorted(run.OBS.glob('profile*/*.perf.data')):
            target = p.with_name(p.name.replace('.perf.data', '.folded.json.gz'))
            if not target.exists():
                target.write_bytes(gzip.compress(json.dumps(fold(p)).encode()))
                print('folded', target.relative_to(run.OBS))
        sys.exit()
    main()
