#!/usr/bin/env python3
"""Compare a #792 BBR trace with draft-06's expected transitions (verification aid).

Each rule recomputes, from the logged inputs, what draft-ietf-ccwg-bbr-06
(archive text SHA-256 ba4f90e9...a891, pinned source 6db913f5) says the state
should become, using the design's declared translations (docs/designs/bbrv3.md
at abfa5456: D02 ProbeRTT cap, D11 packet-equivalent Reno rounds, integer
floors for the draft's real-valued gains and the 0.7 retention, emission's 1%
pacing margin). It never imports or restates the port's Go code; where the
port's arithmetic is a declared translation, the rule says so.

Rules (draft section):
  C0  Startup and Drain exits (5.3.1.2, 5.3.1.3, 5.3.2)
  C1  ProbeBW probe timing: wall clock or Reno-coexistence rounds (5.3.3.8.3)
  C2  ProbeBW_REFILL: short-term reset, one round, then UP (5.3.3.3, 5.3.3.9)
  C3  ProbeBW_UP exits: loss, precautionary, plateau, CE, ProbeRTT (5.3.3.7)
  C4  The two-percent check and inflight_longterm (5.5.10.2)
  C5  Short-term bounds outside probing (5.5.10.3)
  C6  cwnd upper bound; a recovery-exit restore by the volume caps (5.6.4.2, 5.6.4.4, 5.6.4.6, 5.6.4.7)
  C7  Pacing rate by phase (5.6.1, 5.6.2)
  C8  max_bw filter: upper bound and aging only at a cycle advance (5.5.3-5.5.5)
  C9  ProbeRTT entry, hold and exit (5.3.4.3, D02)

Usage: draft06check.py TRACE.json [--measure-start-unix-ns N] [--out RESULT.json]
"""
import argparse
import json
import math
import sys

STARTUP, DRAIN, DOWN, CRUISE, REFILL, UP, PROBERTT = range(7)
PHASES = ['Startup', 'Drain', 'Down', 'Cruise', 'Refill', 'Up', 'ProbeRTT']
ROUND, PRE, POST, LOSSPRE, LOSSPOST, PROBELOSS, OUTPRE, OUTPOST, RESET = range(1, 10)
ACKS_STOPPING = 4
INF = -1
PROBERTT_LATE_NS = 500_000_000
MAX_CWND_PACKETS = 10000      # protocol.MaxCongestionWindowPackets at abfa5456
LIMITED = {1, 2, 3}           # application, flow control, ProbeRTT (SendLimitation values)
PERIODS = {'equal': (0, 100), 'short': (100, 200), 'back': (200, 240), 'absent': (240, 270), 'resumed': (270, 300)}


class Trace:
    def __init__(self, data, measure_start_ns=None):
        self.data = data
        self.c = {name: i for i, name in enumerate(data['columns'])}
        self.f = {name: 1 << i for i, name in enumerate(data['flags'])}
        self.rows = data['rows']
        self.measure_start_ns = measure_start_ns
        self.by_seq = {}
        for i, r in enumerate(self.rows):
            self.by_seq.setdefault(r[self.c['seq']], []).append(i)

    def v(self, r, name):
        return r[self.c[name]]

    def flag(self, r, name):
        return bool(r[self.c['flags']] & self.f[name])

    def period(self, r):
        if self.measure_start_ns is None:
            return None
        s = (self.v(r, 'unix_ns') - self.measure_start_ns) / 1e9
        if s < 0:
            return 'warmup'
        for name, (a, b) in PERIODS.items():
            if a <= s < b:
                return name
        return 'after'

    def pairs(self, kinds=((PRE, POST), (OUTPRE, OUTPOST))):
        for i in range(len(self.rows) - 1):
            a, b = self.rows[i], self.rows[i + 1]
            for kp, kq in kinds:
                if a[0] == kp and b[0] == kq and a[self.c['seq']] == b[self.c['seq']]:
                    yield a, b

    def same_seq(self, r, kind):
        return [self.rows[i] for i in self.by_seq.get(r[self.c['seq']], []) if self.rows[i][0] == kind]

    def end_of_event(self, r):
        """The end-of-event POST row of r's event, if the event changed something."""
        posts = self.same_seq(r, POST)
        return posts[-1] if posts else None


class Report:
    def __init__(self, t):
        self.t, self.rules = t, {}

    def rule(self, name):
        return self.rules.setdefault(name, dict(checked=0, mismatches=[], notes={}))

    def check(self, name, ok, r, what, **detail):
        x = self.rule(name)
        x['checked'] += 1
        if not ok:
            x['mismatches'].append(dict(seq=self.t.v(r, 'seq'), unix_ns=self.t.v(r, 'unix_ns'), period=self.t.period(r),
                                        phase=PHASES[self.t.v(r, 'phase')], what=what, **detail))

    def event(self, name, r, **detail):
        self.rule(name).setdefault('events', []).append(dict(unix_ns=self.t.v(r, 'unix_ns'), period=self.t.period(r), **detail))

    def note(self, name, key, n=1):
        notes = self.rule(name)['notes']
        notes[key] = notes.get(key, 0) + n


def lo(a, b):
    """min where INF (-1) is unbounded."""
    if a == INF:
        return b
    if b == INF:
        return a
    return min(a, b)


def model_bw(t, r):
    return lo(t.v(r, 'bandwidth'), t.v(r, 'bandwidth_short'))


def bdp(t, r, num, den):
    if t.v(r, 'min_rtt_ns') == 0:
        return None
    return (model_bw(t, r) * t.v(r, 'min_rtt_ns') // 1_000_000_000) * num // den


def headroom(t, r):
    hi = t.v(r, 'inflight_long')
    if hi == INF:
        return INF
    size = t.v(r, 'size')
    return max(4 * size, hi - max(size, hi * 15 // 100))


def reno_rounds(t, r, bdp1=None, window=None):
    b = bdp(t, r, 1, 1) if bdp1 is None else bdp1
    w = t.v(r, 'window') if window is None else window
    target = w if b is None else min(b, w)
    size = t.v(r, 'size')
    return min(63, max(1, math.ceil(target / size)))


def mid_bdp1(t, pre, post):
    """BDP when ProbeBW decides an event's phase: after the event's bandwidth
    sample and loss-round close, before its RTT update and window update."""
    loss = t.same_seq(pre, LOSSPOST)
    if loss:
        return t.v(loss[-1], 'bdp1')
    if t.v(pre, 'min_rtt_ns') == 0:
        return None
    return lo(t.v(post, 'bandwidth'), t.v(pre, 'bandwidth_short')) * t.v(pre, 'min_rtt_ns') // 1_000_000_000


def refill_entries(t):
    """Events that entered Refill: Down/Cruise to Refill, or Down/Cruise through
    Refill into ProbeRTT in one event (the short-term reset shows the Refill)."""
    for pre, post in t.pairs(((PRE, POST),)):
        a, b = t.v(pre, 'phase'), t.v(post, 'phase')
        through = b == PROBERTT and t.v(post, 'bandwidth_short') == INF and t.v(post, 'inflight_short') == INF \
            and not (t.v(pre, 'bandwidth_short') == INF and t.v(pre, 'inflight_short') == INF)
        if a in (DOWN, CRUISE) and (b == REFILL or through):
            yield pre, post


def check_startup_drain(t, rep):
    drain_round = None
    for pre, post in t.pairs():
        a, b = t.v(pre, 'phase'), t.v(post, 'phase')
        if a == STARTUP and b == DRAIN:
            drain_round = t.v(post, 'round')
            loss_exit = bool(t.same_seq(pre, LOSSPRE)) and t.v(pre, 'inflight_long') == INF and t.v(post, 'inflight_long') != INF
            ok = t.v(post, 'plateau') >= 3 or loss_exit or t.flag(post, 'ce_active')
            rep.check('C0', ok, post, 'Startup exit without plateau, loss or CE')
        if a == DRAIN and b in (DOWN, CRUISE):
            target = bdp(t, post, 1, 1)
            inflight_ok = target is not None and t.v(pre, 'post_in_flight') <= quantize(t, post, target)
            ok = inflight_ok or (drain_round is not None and t.v(post, 'round') > drain_round + 3)
            rep.check('C0', ok, post, 'Drain exit before inflight <= Inflight(1.0) or three rounds')


def quantize(t, r, x, up=False):
    size = t.v(r, 'size')
    x = max(2 * t.v(r, 'quantum'), 4 * size, x)
    return x + 2 * size if up else x


def check_probe_timing(t, rep):
    for pre, post in refill_entries(t):
        elapsed = t.v(pre, 'mono_ns') - t.v(pre, 'cycle_stamp_mono')
        time_ok = elapsed > t.v(pre, 'probe_wait_ns')
        rsp = t.v(pre, 'rounds_since_probe') + (t.v(post, 'round') - t.v(pre, 'round'))
        # The window is the event's pre-update window; the BDP is the mid-event one.
        rounds_ok = rsp >= reno_rounds(t, pre, mid_bdp1(t, pre, post), t.v(pre, 'window'))
        undo = t.flag(post, 'undo_eligible')
        precautionary = (t.flag(pre, 'previous_probe_precautionary') and not t.flag(pre, 'previous_probe_too_high')
                         and t.v(post, 'round') > t.v(pre, 'round') and t.v(pre, 'ack_phase') == ACKS_STOPPING)
        reason = 'undo' if undo else 'precautionary' if precautionary else 'rounds' if rounds_ok else 'time' if time_ok else None
        rep.check('C1', reason is not None, post, 'Refill before its wall-clock or round trigger', elapsed_ns=elapsed,
                  probe_wait_ns=t.v(pre, 'probe_wait_ns'), rounds=rsp, rounds_needed=reno_rounds(t, pre, mid_bdp1(t, pre, post), t.v(pre, 'window')))
        if reason:
            rep.note('C1', 'refill_by_' + reason)
        rep.event('C1', post, reason=reason, elapsed_ns=elapsed)
        if reason == 'time':
            # Lateness is bounded by the event gap; a missed trigger is caught at the next round below.
            x = rep.rule('C1')['notes']
            x['time_trigger_max_late_ns'] = max(x.get('time_trigger_max_late_ns', 0), elapsed - t.v(pre, 'probe_wait_ns'))
        rep.check('C1', not t.flag(pre, 'ce_active') or undo, post, 'Refill while a CE response is active (D03)')
    for r in t.rows:
        if r[0] != ROUND or t.v(r, 'phase') not in (DOWN, CRUISE) or t.flag(r, 'ce_active'):
            continue
        post = t.end_of_event(r)
        moved = post is not None and t.v(post, 'phase') not in (DOWN, CRUISE)
        # The largest BDP this event can reach (its own sample raises max_bw) needs the most rounds.
        if t.v(r, 'min_rtt_ns') > 0:
            high = lo(max(t.v(r, 'bandwidth'), t.v(r, 'sample_rate')), t.v(r, 'bandwidth_short')) * t.v(r, 'min_rtt_ns') // 1_000_000_000
            needed = reno_rounds(t, r, high, t.v(r, 'window'))
            rep.check('C1', moved or t.v(r, 'rounds_since_probe') < needed, r,
                      'no Refill at a round meeting the Reno-coexistence count', rounds=t.v(r, 'rounds_since_probe'), rounds_needed=needed)
        elapsed = t.v(r, 'mono_ns') - t.v(r, 'cycle_stamp_mono')
        if elapsed > t.v(r, 'probe_wait_ns'):  # the event's own decision follows this mid-event row
            rep.check('C1', moved, r, 'still in Down/Cruise after the wall-clock trigger', elapsed_ns=elapsed,
                      probe_wait_ns=t.v(r, 'probe_wait_ns'))


def check_refill(t, rep):
    entry = None
    for pre, post in t.pairs():
        a, b = t.v(pre, 'phase'), t.v(post, 'phase')
        if b == REFILL and a != REFILL:
            entry = t.v(post, 'round')
            rep.check('C2', t.v(post, 'bandwidth_short') == INF and t.v(post, 'inflight_short') == INF, post,
                      'Refill did not reset the short-term model')
            rep.check('C2', not t.flag(post, 'previous_probe_precautionary'), post, 'Refill kept prev_probe_precautionary')
        if a == REFILL and b != REFILL:
            if b == UP:
                rep.check('C2', entry is not None and t.v(post, 'round') == entry + 1, post, 'Refill did not last exactly one round',
                          entry_round=entry, up_round=t.v(post, 'round'))
                rep.check('C2', t.v(post, 'plateau') == 0 and t.flag(post, 'probe_sample'), post,
                          'Up entry without ResetFullBW or is_bw_probe_sample')
            else:
                rep.check('C2', t.flag(post, 'ce_active') or b == PROBERTT, post, 'Refill left for neither Up, CE nor ProbeRTT')
            entry = None


def crosses(t, r):
    lost, flight, length, sent_post = (r[t.c[k]] for k in ('a0', 'a1', 'a2', 'a3'))
    return lost > flight // 50 and lost >= length and sent_post >= length


def check_up_exits(t, rep):
    for pre, post in t.pairs():
        if t.v(pre, 'phase') != UP or t.v(post, 'phase') == UP:
            continue
        b = t.v(post, 'phase')
        loss = any(crosses(t, r) for r in t.same_seq(pre, PROBELOSS))
        precautionary = (t.flag(pre, 'previous_probe_too_high') and t.flag(post, 'previous_probe_precautionary')
                         and t.v(pre, 'post_in_flight') >= t.v(pre, 'inflight_long'))
        plateau = t.v(post, 'plateau') >= 3
        reason = ('loss' if loss else 'precautionary' if precautionary else 'ce' if t.flag(post, 'ce_active')
                  else 'probertt' if b == PROBERTT else 'plateau' if plateau else None)
        rep.check('C3', reason is not None and b in (DOWN, PROBERTT), post, 'Up exit without a draft exit condition', to=PHASES[b])
        if reason:
            rep.note('C3', 'up_exit_' + reason)
        rep.event('C3', post, reason=reason)
        if reason == 'plateau':
            # At the decision: the event's round usage (unchanged by the window update), the
            # pre-update window, and inflight_longterm after this event's upward adjustment.
            hi = t.v(post, 'inflight_long')
            limited = ((t.flag(post, 'window_used_in_round') or t.flag(post, 'window_used_last_round'))
                       and hi != INF and t.v(pre, 'window') >= hi)
            rep.check('C3', not limited, post, 'plateau exit while cwnd-limited at inflight_longterm (ResetFullBW expected)')


def inflight_at_loss(lost, flight, length, clamp):
    prev_flight, prev_lost = flight - length, lost - length
    if clamp:  # the port's integer form: a negative prefix is clamped to zero
        prefix = (prev_flight - prev_lost * 50) // 49 if prev_lost <= prev_flight // 50 else 0
        return prev_flight + prefix
    return prev_flight + (0.02 * prev_flight - prev_lost) / 0.98


def check_probe_losses(t, rep):
    rule = 'C4'
    for r in t.rows:
        if r[0] != PROBELOSS:
            continue
        lost, flight, length, sent_post, limited = (r[t.c[k]] for k in ('a0', 'a1', 'a2', 'a3', 'a4'))
        port, draft = crosses(t, r), lost > 0.02 * flight
        rep.note(rule, 'checks')
        if port != draft:
            rep.note(rule, 'threshold_translation_differs')
            rep.check(rule, port or lost <= 0.02 * flight + 1, r, "draft's loss check crosses where the port's does not",
                      lost=lost, flight=flight, length=length)
        if not port:
            continue
        rep.note(rule, 'crossings')
        rep.event(rule, r, lost=lost, flight=flight)
        post = t.end_of_event(r)
        rep.check(rule, post is not None, r, 'a crossing changed no recorded state')
        if post is None:
            continue
        rep.check(rule, t.flag(post, 'previous_probe_too_high'), post, 'crossing did not set prev_probe_too_high')
        if t.v(r, 'phase') == UP:
            rep.check(rule, t.v(post, 'phase') in (DOWN, PROBERTT), post, 'crossing in Up did not start ProbeBW_DOWN')
        if limited in LIMITED:
            rep.note(rule, 'crossing_limited')
            continue
        b = bdp(t, r, 1, 1)
        target = t.v(r, 'window') if b is None else min(b, t.v(r, 'window'))
        floor = target * 7 // 10
        port_hi = max(inflight_at_loss(lost, flight, length, True), floor)
        draft_hi = max(inflight_at_loss(lost, flight, length, False), 0.7 * target)
        got = t.v(post, 'inflight_long')
        has_ack = t.flag(post, 'has_ack')
        rep.check(rule, got == port_hi or (has_ack and got >= port_hi), post, 'inflight_longterm differs from the crossing',
                  expected=port_hi, got=got)
        if got != port_hi:
            rep.note(rule, 'raised_later_in_event')
        if port_hi - draft_hi >= 1:
            rep.note(rule, 'clamp_cases')
            rep.note(rule, 'clamp_excess_bytes', round(port_hi - draft_hi))
    # Every decrease, reset or first setting of inflight_longterm needs a cause.
    for pre, post in t.pairs():
        m = post[t.c['a0']]
        if not m & (1 << 3 | 1 << 4):
            continue
        cause = (any(crosses(t, r) for r in t.same_seq(pre, PROBELOSS)) or bool(t.same_seq(pre, RESET))
                 or (t.v(pre, 'phase') == STARTUP and t.v(post, 'phase') == DRAIN and bool(t.same_seq(pre, LOSSPRE))))
        rep.check(rule, cause, post, 'inflight_longterm lowered, set or reset without a crossing, Startup loss exit or reset',
                  before=t.v(pre, 'inflight_long'), after=t.v(post, 'inflight_long'))


def check_lower_bounds(t, rep):
    rule = 'C5'
    rows = t.rows
    for i in range(len(rows) - 1):
        p, q = rows[i], rows[i + 1]
        if p[0] != LOSSPRE or q[0] != LOSSPOST or p[t.c['seq']] != q[t.c['seq']]:
            continue
        phase = t.v(p, 'phase')
        if phase == STARTUP:
            continue
        if phase in (REFILL, UP):
            rep.check(rule, t.v(q, 'bandwidth_short') == t.v(p, 'bandwidth_short') and t.v(q, 'inflight_short') == t.v(p, 'inflight_short'),
                      q, 'short-term bounds changed while probing')
            continue
        bw0 = t.v(p, 'bandwidth') if t.v(p, 'bandwidth_short') == INF else t.v(p, 'bandwidth_short')
        if0 = t.v(p, 'window') if t.v(p, 'inflight_short') == INF else t.v(p, 'inflight_short')
        want_bw, want_if = max(t.v(p, 'latest_rate'), bw0 * 7 // 10), max(t.v(p, 'latest_volume'), if0 * 7 // 10)
        rep.check(rule, t.v(q, 'bandwidth_short') == want_bw and t.v(q, 'inflight_short') == want_if, q,
                  'short-term bounds differ from max(latest, 0.7 x bound)', expected=[want_bw, want_if],
                  got=[t.v(q, 'bandwidth_short'), t.v(q, 'inflight_short')])
        rep.note(rule, 'binding_bw' if want_bw < t.v(p, 'bandwidth') else 'nonbinding_bw')
    for pre, post in t.pairs():
        m = post[t.c['a0']]
        if not m & (1 << 1 | 1 << 2):
            continue
        for name in ('bandwidth_short', 'inflight_short'):
            a, b = t.v(pre, name), t.v(post, name)
            if a == b:
                continue
            if b == INF:
                refill = t.v(pre, 'phase') in (DOWN, CRUISE) and t.v(post, 'phase') in (REFILL, PROBERTT)  # Refill, possibly then ProbeRTT
                ok = refill or (t.v(pre, 'phase') == PROBERTT and t.v(post, 'phase') != PROBERTT) or bool(t.same_seq(pre, RESET))
                what = 'short-term bound reset outside Refill, ProbeRTT exit or reset'
            else:
                # max(latest, 0.7 x bound) can lower, set or raise a bound; an undo can raise it.
                ok = bool(t.same_seq(pre, LOSSPRE)) or (a != INF and b > a and t.flag(post, 'undo_eligible'))
                what = 'short-term bound changed without a loss-round close or an undo'
            rep.check(rule, ok, post, what, bound=name, before=a, after=b)


def check_window(t, rep):
    rule = 'C6'
    for r in t.rows:
        if r[0] != POST or not t.flag(r, 'has_ack') or t.v(r, 'phase') == STARTUP or t.v(r, 'min_rtt_ns') == 0:
            continue
        size, phase = t.v(r, 'size'), t.v(r, 'phase')
        if t.flag(r, 'persistent_waiting'):
            rep.check(rule, t.v(r, 'window') == 2 * size, r, 'persistent-congestion restart window is not 2 M')
            continue
        up = phase == UP
        target = quantize(t, r, (bdp(t, r, 9, 4) if up else bdp(t, r, 2, 1)) + t.v(r, 'extra'), up)
        cap = t.v(r, 'inflight_short')
        if phase >= DOWN:
            cap = lo(cap, t.v(r, 'inflight_long'))
        if phase in (CRUISE, PROBERTT):
            cap = lo(cap, headroom(t, r))
        if t.flag(r, 'ce_active'):
            cap = lo(cap, t.v(r, 'ce_flight'))
        bound = min(size * MAX_CWND_PACKETS, max(4 * size, lo(cap, target)))
        if t.flag(r, 'episode_exited') or t.flag(r, 'undo_eligible'):
            # RestoreCwnd (5.6.4.4): the restored window is bounded by the volume caps
            # (BoundCwndForModel); the draft leaves its order against this ACK's
            # max_inflight open, so max_inflight applies from the next ACK.
            restored = min(size * MAX_CWND_PACKETS, max(4 * size, cap)) if cap != INF else size * MAX_CWND_PACKETS
            rep.check(rule, t.v(r, 'window') <= restored, r, 'restored cwnd above the volume caps', window=t.v(r, 'window'), bound=restored)
            if t.v(r, 'window') > bound:
                rep.note(rule, 'restored_above_max_inflight')
            continue
        rep.check(rule, t.v(r, 'window') <= bound, r, 'cwnd above BoundCwndForModel(max_inflight)', window=t.v(r, 'window'), bound=bound)
        rep.note(rule, 'at_bound' if t.v(r, 'window') == bound else 'below_bound')


def gain_rate(t, r):
    bw = model_bw(t, r)
    return {DRAIN: bw // 2, DOWN: bw * 9 // 10, CRUISE: bw, REFILL: bw, PROBERTT: bw, UP: bw * 5 // 4}.get(t.v(r, 'phase'))


def check_pacing(t, rep):
    for r in t.rows:
        if r[0] != POST or not t.flag(r, 'has_ack') or t.v(r, 'phase') == STARTUP or t.v(r, 'bandwidth') <= 0:
            continue
        want = max(1, gain_rate(t, r))
        if t.flag(r, 'ce_active'):
            rep.check('C7', t.v(r, 'rate') <= want, r, 'pacing rate above the phase rate under CE', want=want, got=t.v(r, 'rate'))
        else:
            rep.check('C7', t.v(r, 'rate') == want, r, 'pacing rate is not pacing_gain x bw', want=want, got=t.v(r, 'rate'))


def check_bandwidth_filter(t, rep):
    """The filter keeps the largest sample of the current and previous cycle.
    A cycle advance takes effect at the next sample, as in the draft, so a ROUND
    row (written before its event's sample) may show a value the event drops."""
    history = []  # (cycle, max sample rate) per finished round, in order

    def window(c, extra=0):
        w = [m for cy, m in history if c - 1 <= cy <= c]
        return max(w + [extra])

    for r in t.rows:
        if r[0] == ROUND:
            c = t.v(r, 'cycle')
            bound = window(c, r[t.c['a4']])  # includes this event's own sample
            history.append((c, r[t.c['a4']]))
            if t.v(r, 'bandwidth') > bound:
                post = t.end_of_event(r)
                ok = post is not None and t.v(post, 'bandwidth') <= bound
                rep.check('C8', ok, r, 'max_bw above every sample of the current and previous cycle, not dropped by this event',
                          bandwidth=t.v(r, 'bandwidth'), samples_max=bound)
                rep.note('C8', 'stale_until_next_sample')
            else:
                rep.check('C8', True, r, '')
        elif r[0] == POST and r[t.c['a0']] & (1 << 9) and not t.same_seq(r, RESET):
            # A decrease keeps the largest sample of its window (this event's sample included).
            c = t.v(t.same_seq(r, PRE)[-1], 'cycle')
            low = window(c)
            rep.check('C8', t.v(r, 'bandwidth') >= low or t.flag(r, 'sample_limited'), r,
                      'max_bw dropped below a sample of the current or previous cycle', bandwidth=t.v(r, 'bandwidth'), samples_max=low)
            rep.note('C8', 'aging_events')


def check_probertt(t, rep):
    hold = None
    for pre, post in t.pairs():
        a, b = t.v(pre, 'phase'), t.v(post, 'phase')
        if b == PROBERTT and a != PROBERTT:
            expired = t.v(pre, 'probe_rtt_min_ns') > 0 and t.v(pre, 'mono_ns') - t.v(pre, 'probe_rtt_stamp_mono') > 5_000_000_000
            rep.check('C9', expired and not t.flag(pre, 'idle_restart'), post, 'ProbeRTT entered without an expired 5 s filter')
            hold = None
        if b == PROBERTT and t.flag(post, 'probe_rtt_holding') and not t.flag(pre, 'probe_rtt_holding'):
            hold = t.v(post, 'mono_ns')
        if a == PROBERTT and b != PROBERTT:
            dur = None if hold is None else t.v(post, 'mono_ns') - hold
            rep.check('C9', dur is not None and dur > 200_000_000, post, 'ProbeRTT exit before 200 ms of hold', hold_ns=dur)
            rep.check('C9', b in (CRUISE, STARTUP), post, 'ProbeRTT exit to neither ProbeBW (Cruise) nor Startup', to=PHASES[b])
            rep.check('C9', t.v(post, 'bandwidth_short') == INF and t.v(post, 'inflight_short') == INF, post,
                      'ProbeRTT exit kept the short-term model')
            hold = None
    for r in t.rows:
        if r[0] != ROUND or t.v(r, 'phase') == PROBERTT or t.flag(r, 'idle_restart') or t.v(r, 'probe_rtt_min_ns') == 0:
            continue
        if t.v(r, 'mono_ns') - t.v(r, 'probe_rtt_stamp_mono') > 5_000_000_000 + PROBERTT_LATE_NS:
            post = t.end_of_event(r)
            rep.check('C9', post is not None and t.v(post, 'phase') == PROBERTT, r, 'ProbeRTT overdue by more than 0.5 s')


def coverage(t):
    kinds = {}
    for r in t.rows:
        kinds[r[0]] = kinds.get(r[0], 0) + 1
    probe_rows = kinds.get(PROBELOSS, 0)
    complete = (t.data.get('overflow', 0) == 0 and len(t.rows) > 0 and t.data.get('foreign_events', 0) == 0
                and t.data.get('probe_loss_entries', 0) >= probe_rows)
    return dict(rows=len(t.rows), by_kind={str(k): n for k, n in sorted(kinds.items())}, overflow=t.data.get('overflow'),
                foreign_events=t.data.get('foreign_events'), probe_loss_entries=t.data.get('probe_loss_entries'),
                probe_loss_rows=probe_rows, events=t.data.get('events'), complete=complete)


CHECKS = [check_startup_drain, check_probe_timing, check_refill, check_up_exits, check_probe_losses, check_lower_bounds,
          check_window, check_pacing, check_bandwidth_filter, check_probertt]


def run(data, measure_start_ns=None):
    t = Trace(data, measure_start_ns)
    rep = Report(t)
    for name in ['C0', 'C1', 'C2', 'C3', 'C4', 'C5', 'C6', 'C7', 'C8', 'C9']:
        rep.rule(name)
    for check in CHECKS:
        check(t, rep)
    for x in rep.rules.values():
        by = {}
        for m in x['mismatches']:
            by[m['period']] = by.get(m['period'], 0) + 1
        x['mismatches_by_period'] = by
    total = sum(len(x['mismatches']) for x in rep.rules.values())
    return dict(coverage=coverage(t), rules=rep.rules, mismatches=total)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('trace')
    ap.add_argument('--measure-start-unix-ns', type=int)
    ap.add_argument('--out')
    args = ap.parse_args()
    with open(args.trace) as f:
        result = run(json.load(f), args.measure_start_unix_ns)
    text = json.dumps(result, indent=1, sort_keys=True)
    if args.out:
        with open(args.out, 'w') as f:
            f.write(text + '\n')
    for name, x in sorted(result['rules'].items()):
        print(f"{name}: checked {x['checked']}, mismatches {len(x['mismatches'])} {x['notes']}")
    print('coverage', result['coverage'])
    return 0


if __name__ == '__main__':
    sys.exit(main())
