#!/usr/bin/env python3
"""#712 registered localization, preservation and contamination rules.

Registered in README.md ("Registration: attribution stages") and exercised on
synthetic cases in localize_test.py before any attribution observation existed.
Localization says where the candidate's extra work executes; it never
classifies a cause. Usage on minimax after a stage: `localize.py fold` (turns
root-owned perf.data into folded stacks), then anywhere: `localize.py`.
"""
import gzip
import json
import re
import statistics
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import FIXTURE_CPUS, OBS, SIBLING_CPUS, summarize  # noqa: E402

HERE = Path(__file__).resolve().parent
USER_HZ = 100

# ---- stack classification ----------------------------------------------------
# Disjoint groups. D3 names the first five; the last three keep the partition
# exhaustive. Kernel samples are classified by their kernel frames, user
# samples by their user frames, each with a fixed precedence.
GROUPS = ['bbr', 'alloc_gc', 'user_sched', 'kernel_send', 'kernel_wake_sched', 'kernel_recv', 'kernel_other', 'user_other']
USER_GROUPS = {'bbr', 'alloc_gc', 'user_sched', 'user_other'}
KERNEL_GROUPS = set(GROUPS) - USER_GROUPS

K_WAKE_SCHED = re.compile(r'^(try_to_wake_up|wake_up_q|wake_up_process|__wake_up\w*|autoremove_wake_function|ep_autoremove_wake_function'
                          r'|ep_poll_callback|default_wake_function|futex_wake\w*|__schedule|schedule\w*|finish_task_switch\S*'
                          r'|pick_next_task\w*|context_switch|do_nanosleep|hrtimer_nanosleep|futex_wait\w*|ep_poll|do_epoll_wait'
                          r'|ttwu_\w+|enqueue_task\w*|dequeue_task\w*|activate_task|select_task_rq\w*|resched_curr\w*)$')
K_SEND = re.compile(r'^(__sys_sendmsg|__sys_sendmmsg|___sys_sendmsg|__sys_sendto|udp_sendmsg|udp_send_skb|ip_send_skb|ip_output|____sys_sendmsg)$')
K_RECV = re.compile(r'^(__sys_recvmsg|__sys_recvmmsg|do_recvmmsg|___sys_recvmsg|____sys_recvmsg|udp_recvmsg|__sys_recvfrom)$')
KERNEL_ADDR = re.compile(r'^ffffffff')
KERNEL_SUFFIX = re.compile(r'(\.(isra|part|constprop|cold|llvm)(\.\d+)?)+$')

U_ALLOC_GC = re.compile(r'^runtime\.(mallocgc\w*|gcBgMarkWorker|bgsweep|bgscavenge|gcAssistAlloc\w*|gcStart|gcMarkDone|gcMarkTermination'
                        r'|gcDrain\w*|scanobject|greyobject|scanblock|markroot\w*|sweepone|wbBufFlush\w*|gcWriteBarrier\w*)$')
U_SCHED = re.compile(r'^runtime\.(schedule|findRunnable|park_m|stopm|startm|notesleep|notewakeup|futexsleep|futexwakeup|netpoll|wakep'
                     r'|ready|goready|osyield|usleep|procyield|runqsteal|stealWork|injectglist|sysmon|mPark|gopark|goexit0|mcall'
                     r'|checkTimers|handoffp|resetspinning|semasleep|semawakeup|lock2|unlock2|chansend|chanrecv\w*|selectgo)$')
BBR = re.compile(r'internal/congestion\.(\(\*BBRSender\)|\(\*bbr\w*\)|NewBBRSender|bbr\w*)'
                 r'|internal/ackhandler\.\(\*(recoveryEvidence|recoveryIndex|congestionDispatch|deliverySampler|bbrECNTracker|retained\w*|spaceSlots)\)'
                 r'|internal/ackhandler\.\(\*sentPacketHandler\)\.(captureCongestion\w*|captureBBRECN|appendRetainedAck|retire\w*'
                 r'|beginCongestionFeedback|finishCongestionFeedback)'
                 r'|internal/ackhandler\.(congestionKey|EnableBBR|EnableDeliverySampling|newRecovery\w*|retained\w+)'
                 r'|quic-go\.\(\*localSendCredit\)')


def classify_stack(frames):
    """frames: symbol names, leaf first. Returns one group name."""
    kernel = [f for f in frames if f.startswith('k:')]
    if kernel:
        names = [f[2:] for f in kernel]
        for n in names:  # innermost wake or scheduling frame wins inside any syscall
            if K_WAKE_SCHED.match(n):
                return 'kernel_wake_sched'
        if any(K_SEND.match(n) for n in names):
            return 'kernel_send'
        if any(K_RECV.match(n) for n in names):
            return 'kernel_recv'
        return 'kernel_other'
    if any(U_ALLOC_GC.match(f) for f in frames):
        return 'alloc_gc'
    if any(U_SCHED.match(f) for f in frames):
        return 'user_sched'
    if any(BBR.search(f) for f in frames):
        return 'bbr'
    return 'user_other'


def fold(perf_data):
    """Folded stacks {'k:leaf;...;user root': summed cycles} from a root-owned perf.data."""
    out = subprocess.run(['sudo', '-n', 'perf', 'script', '-i', str(perf_data), '-F', 'period,ip,sym', '--no-demangle'],
                         capture_output=True, text=True, check=True).stdout
    folded, period, frames = {}, None, []

    def flush():
        if period is not None:
            key = ';'.join(frames)
            folded[key] = folded.get(key, 0) + period
    for line in out.splitlines():
        if not line.strip():
            flush()
            period, frames = None, []
        elif not line.startswith('\t'):
            flush()
            period, frames = int(line.split()[0]), []
        else:
            parts = line.split(None, 1)
            addr, sym = parts[0], (parts[1] if len(parts) > 1 else '[unknown]')
            sym = sym.split('+0x')[0].strip()
            if KERNEL_ADDR.match(addr):
                sym = 'k:' + KERNEL_SUFFIX.sub('', sym)
            frames.append(sym)
    flush()
    return folded


def group_cycles(folded):
    g = dict.fromkeys(GROUPS, 0.0)
    for key, cycles in folded.items():
        g[classify_stack(key.split(';') if key else [])] += cycles
    return g


# ---- localization rule ---------------------------------------------------------
def block_excess(cand, reno):
    """Per-group excess per useful GiB, its positive pool and shares of that pool."""
    delta = {k: cand.get(k, 0.0) - reno.get(k, 0.0) for k in GROUPS}
    pool = sum(v for v in delta.values() if v > 0)
    share = {k: (v / pool if pool > 0 and v > 0 else 0.0) for k, v in delta.items()}
    return dict(delta=delta, total=sum(delta.values()), positive_pool=pool, share=share)


def domain_split(du, dk):
    """Share of a positive user/kernel excess held by user code; None when neither part is positive."""
    pu, pk = max(du, 0.0), max(dk, 0.0)
    return pu / (pu + pk) if pu + pk > 0 else None


def localize(blocks, counters_domain=None):
    """blocks: [{'cand': {group: cycles/GiB}, 'reno': {...}}], one per block.

    counters_domain: {'cycles_user_share': x, 'instructions_user_share': y} from the counters stage (medians).
    """
    ex = [block_excess(b['cand'], b['reno']) for b in blocks]
    n = len(ex)
    res = dict(blocks=ex, median_share={k: statistics.median(e['share'][k] for e in ex) for k in GROUPS},
               median_delta={k: statistics.median(e['delta'][k] for e in ex) for k in GROUPS},
               blocks_with_excess=sum(e['total'] > 0 for e in ex))
    if res['blocks_with_excess'] < (n // 2 + 1):
        return dict(res, outcome='no repeatable excess', lead=None)
    lead = [k for k in GROUPS if res['median_share'][k] >= 0.5 and all(e['delta'][k] > 0 for e in ex)]
    if not lead:
        return dict(res, outcome='inconclusive: no group holds half the excess in every block', lead=None)
    lead = lead[0]
    res['lead'] = lead
    if counters_domain:
        cyc, ins = counters_domain.get('cycles_user_share'), counters_domain.get('instructions_user_share')
        res['counters_domain'] = counters_domain
        if cyc is not None and ins is not None and (cyc >= 0.5) != (ins >= 0.5):
            res['instructions_cycles_disagree'] = True
        if cyc is not None and (cyc >= 0.5) != (lead in USER_GROUPS):
            return dict(res, outcome='inconclusive: call-graph lead and counter domain conflict')
    return dict(res, outcome='localized')


# ---- counters stage -----------------------------------------------------------
COUNTER_EVENTS = ['instructions:u', 'instructions:k', 'cycles:u', 'cycles:k', 'context-switches', 'cpu-migrations', 'page-faults',
                  'sched:sched_wakeup', 'syscalls:sys_enter_sendmsg', 'syscalls:sys_enter_sendmmsg', 'syscalls:sys_enter_recvmsg',
                  'syscalls:sys_enter_recvmmsg', 'syscalls:sys_enter_futex', 'syscalls:sys_enter_epoll_pwait',
                  'syscalls:sys_enter_nanosleep', 'net:net_dev_xmit']


def counter_quality(c):
    """Counters are usable when every event counted and none was multiplexed below 95% running."""
    return all(e in c and c[e]['value'] is not None and (c[e]['running_pct'] or 0) >= 95 for e in COUNTER_EVENTS)


def counters_block(cand, reno, gib_cand, gib_reno):
    """Per-GiB ratios and user/kernel excess for one endpoint in one block (raw perf values)."""
    per = lambda c, gib: {e: c[e]['value'] / gib for e in COUNTER_EVENTS}
    a, b = per(cand, gib_cand), per(reno, gib_reno)
    ratio = {e: (a[e] / b[e] if b[e] else None) for e in COUNTER_EVENTS}
    for kind in ['instructions', 'cycles']:
        ta, tb = a[f'{kind}:u'] + a[f'{kind}:k'], b[f'{kind}:u'] + b[f'{kind}:k']
        ratio[kind] = ta / tb
        du, dk = a[f'{kind}:u'] - b[f'{kind}:u'], a[f'{kind}:k'] - b[f'{kind}:k']
        ratio[f'{kind}_delta_user_per_gib'], ratio[f'{kind}_delta_kernel_per_gib'] = du, dk
        ratio[f'{kind}_user_share'] = domain_split(du, dk)
    return ratio


def preservation(instr_ratios, readiness_ratio, aa_cpu_ratios):
    """#711's registered rule, unchanged: noise attribution needs equal work and a readiness ratio inside A/A."""
    med = statistics.median(instr_ratios)
    inside = min(aa_cpu_ratios) <= readiness_ratio <= max(aa_cpu_ratios)
    equal = 0.97 <= med <= 1.03
    return dict(instructions_median=med, equal_work=equal, readiness_ratio=readiness_ratio, aa_range=[min(aa_cpu_ratios), max(aa_cpu_ratios)],
                inside_aa=inside, attribution='measurement noise (flag stays raised)' if equal and inside else 'unattributed preservation flag')


# ---- contamination rule ---------------------------------------------------------
def contamination(samples, window):
    """Foreign CPU on fixture cores and their SMT siblings during the measured window.

    samples: host-cpu.json rows (1 s); window: [start_ns, end_ns]. Foreign CPU is the busy
    time on those CPUs minus the fixture processes' own CPU time, in cores.
    """
    sel = [s for s in samples if window[0] <= s['unix_ns'] <= window[1]]
    cpus = FIXTURE_CPUS + SIBLING_CPUS
    per, sib = [], []
    for a, b in zip(sel, sel[1:]):
        dt = (b['unix_ns'] - a['unix_ns']) / 1e9
        busy = sum(b['cpus'][str(c)]['busy'] - a['cpus'][str(c)]['busy'] for c in cpus) / USER_HZ
        own = sum((b['fixture_ticks'][r] or a['fixture_ticks'][r]) - a['fixture_ticks'][r]
                  for r in a['fixture_ticks'] if a['fixture_ticks'][r] is not None) / USER_HZ
        per.append(max(0.0, (busy - own) / dt))
        sib.append(sum(b['cpus'][str(c)]['busy'] - a['cpus'][str(c)]['busy'] for c in SIBLING_CPUS) / USER_HZ / dt)
    if not per:
        return dict(contaminated=None, reason='no samples in window')
    mean, peak = statistics.fmean(per), max(per)
    return dict(foreign_mean_cores=mean, foreign_max_cores=peak, sibling_mean_cores=statistics.fmean(sib), samples=len(per),
                contaminated=mean > 0.10 or peak > 0.50)


# ---- stage drivers --------------------------------------------------------------
def load(phase):
    out = []
    for p in sorted(OBS.glob(f'{phase}-*/receipt.json')):
        out.append((p.parent, summarize(p.parent)))
    return out


def arm(r):
    return '-'.join([r['variant'], r['controller']] + ([r['tag']] if r.get('tag') else []))


def counters_stage():
    rows = load('counters')
    res = {}
    for workload in ['stream', 'datagram']:
        sel = [(d, r) for d, r in rows if r['workload'] == workload]
        by = {(r['pair'], arm(r)): r for _, r in sel}
        quality = {r['id']: all(counter_quality(r[role]['counters']) for role in ['send', 'receive']) for _, r in sel}
        out = dict(quality_failures=[k for k, v in quality.items() if not v])
        for name in ['reno-reno-aa', 'cand-bbrv3', 'cand-reno']:
            for role in ['send', 'receive']:
                blocks = []
                for block in sorted({r['pair'] for _, r in sel}):
                    c, ref = by.get((block, name)), by.get((block, 'reno-reno'))
                    if not c or not ref or not quality[c['id']] or not quality[ref['id']]:
                        continue
                    gib = lambda r: r['useful_bytes'] / 2**30
                    b = counters_block(c[role]['counters'], ref[role]['counters'], gib(c), gib(ref))
                    b['cpu_seconds'] = c[role]['cpu_seconds_per_gib'] / ref[role]['cpu_seconds_per_gib']
                    b['block'] = block
                    blocks.append(b)
                keys = [k for k in blocks[0] if k != 'block'] if blocks else []
                out[f'{name}/{role}'] = dict(blocks=blocks, median={k: statistics.median(x[k] for x in blocks if x[k] is not None)
                                                                     for k in keys if any(x[k] is not None for x in blocks)})
        res[workload] = out
    return res


def callgraph_stage(counters=None):
    rows = load('callgraphs')
    res = {}
    for workload in ['stream', 'datagram']:
        sel = [(d, r) for d, r in rows if r['workload'] == workload]
        for role in ['send', 'receive']:
            blocks = []
            for block in sorted({r['pair'] for _, r in sel}):
                pair = {arm(r): (d, r) for d, r in sel if r['pair'] == block}
                if 'cand-bbrv3' not in pair or 'reno-reno' not in pair:
                    continue
                g = {}
                for name, (d, r) in pair.items():
                    folded = json.loads(gzip.decompress((d / f'{role}.folded.json.gz').read_bytes()))
                    gib = r['useful_bytes'] / 2**30
                    g[name] = {k: v / gib for k, v in group_cycles(folded).items()}
                blocks.append(dict(block=block, cand=g['cand-bbrv3'], reno=g['reno-reno']))
            dom = None
            if counters:
                m = counters[workload][f'cand-bbrv3/{role}']['median']
                dom = dict(cycles_user_share=m.get('cycles_user_share'), instructions_user_share=m.get('instructions_user_share'))
            res[f'{workload}/{role}'] = dict(localize(blocks, dom), group_cycles_per_gib=blocks)
    return res


if __name__ == '__main__':
    if sys.argv[1:] == ['fold']:
        for p in sorted(OBS.glob('*/*.perf.data')):
            target = p.with_name(p.name.replace('.perf.data', '.folded.json.gz'))
            if not target.exists():
                target.write_bytes(gzip.compress(json.dumps(fold(p)).encode()))
                print('folded', target.relative_to(OBS))
        sys.exit()
    out = dict(counters=counters_stage())
    out['callgraphs'] = callgraph_stage(out['counters'])
    (HERE / 'localization.json').write_text(json.dumps(out, indent=2) + '\n')
    for k, v in out['callgraphs'].items():
        print(k, v['outcome'], v.get('lead'), {g: round(s, 2) for g, s in v['median_share'].items() if s})
