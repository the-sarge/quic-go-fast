#!/usr/bin/env python3
"""#777 path calibration: the two-flow relay against the registered criteria.

Each check starts relay2 with a chosen model time zero, drives it with
calibrate2's stand-in endpoints and judges the reports. The criteria were
written before any calibration run of this record. Every run here is excluded
from every statistic. A failed check ends the record as a prerequisite gap.

Checks (zero = the relay's model time zero; D = the model's one-way delay):
- ecn: every codepoint read as sent, per flow and direction; nothing at the
  other flow's endpoint.
- delay-warmup (zero = now + 1 h): both flows, both directions, D = 50 ms.
- delay-short (zero = now - 150 s): competitor D = 12.5 ms, focal D = 50 ms.
- switch-100 (zero = now - 99 s) and switch-200 (zero = now - 199 s):
  competitor probes sent more than 20 ms before the boundary see the old D
  and those sent more than 20 ms after it the new D; focal D = 50 ms.
  For the delay checks, every probe must arrive, the median one-way delay must
  lie in [D, D + 2 ms] and every probe in [D, D + 5 ms].
- rate-forward and rate-reverse (zero = now + 1 h): both flows offer 75 Mbit/s
  of 1,400-byte payloads for 4 s; the delivered rate in charged IP bytes over
  the 2 s starting 1.5 s after the first arrival is within 1% of 100 Mbit/s,
  the relay's largest queue reaches capacity less one packet, and both flows
  lose packets to overflow.
- relay health in every check: no relay error, send error, truncated read or
  propagation overflow.
"""
import json
import statistics
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import run  # noqa: E402

OUT = run.ART / 'prereq'
CAPACITY = 1251220            # 857 x 1460, both directions
PACKET = 1428                 # 1,400-byte payload plus IPv4 and UDP headers
CALIBRATE_CPUS = '2,3,6,7'    # the competitor's cores, unused by any endpoint here


def addresses(prefix=''):
    args = []
    for who in ['focal', 'competitor']:
        a = run.ADDR[who]
        args += [f'-{who}-front', a['front'], f'-{who}-back', a['back'], f'-{who}-sender', a['sender'], f'-{who}-target', a['receiver']]
    return args


def calibrate(name, zero_ns, mode_args, until_s=30):
    """Run one relay instance and one calibrate2 mode; return both reports."""
    relay_out, calib_out = OUT / f'{name}-relay.json', OUT / f'{name}-calibrate.json'
    err = (OUT / f'{name}-relay.stderr').open('w')
    relay = subprocess.Popen(['taskset', '-c', run.AFFINITY['relay'], str(run.ART / 'bin/relay2'), '-scenario', 'L6', '-seed', '9997',
                              *addresses(), '-start-unix-ns', str(zero_ns), '-until', f'{until_s}s', '-output', str(relay_out)],
                             stdout=subprocess.DEVNULL, stderr=err)
    try:
        run.wait_ready(relay, OUT / f'{name}-relay.stderr', 'relay ready')
        subprocess.run(['taskset', '-c', CALIBRATE_CPUS, str(run.ART / 'bin/calibrate2'), *mode_args, *addresses(),
                        '-output', str(calib_out)], check=True, timeout=until_s)
    finally:
        relay.terminate()
        relay.wait(timeout=5)
        err.close()
    return json.loads(relay_out.read_text()), json.loads(calib_out.read_text())


def relay_healthy(rep):
    problems = [rep['Error']] if rep.get('Error') else []
    for d in ['Forward', 'Reverse']:
        if rep[d]['Stats']['PropagationOverflow']:
            problems.append(f'{d} propagation overflow')
        for f in ['Focal', 'Competitor']:
            if rep[d][f]['SendErrors'] or rep[d][f]['TruncatedRead']:
                problems.append(f'{d} {f} send errors or truncated reads')
    return problems


def leg_ok(leg, expected_ms, count, boundary=None):
    """Judge one probe leg; expected_ms is a constant or a function of the send model time (s)."""
    problems = []
    if leg.get('error') or leg['foreign'] or leg['unparsed']:
        problems.append(f"error {leg.get('error')}, foreign {leg['foreign']}, unparsed {leg['unparsed']}")
    if len(leg['samples']) != count:
        problems.append(f"{len(leg['samples'])} of {count} probes arrived")
    groups = {}
    for sent, delay in leg['samples']:
        d = expected_ms(sent) if callable(expected_ms) else expected_ms
        if d is not None:
            groups.setdefault(d, []).append(delay / 1e6)
    for d, delays in groups.items():
        med = statistics.median(delays)
        if not d <= med <= d + 2:
            problems.append(f'median {med:.3f} ms outside [{d}, {d + 2}]')
        if min(delays) < d or max(delays) > d + 5:
            problems.append(f'range {min(delays):.3f}-{max(delays):.3f} ms outside [{d}, {d + 5}]')
    return problems, {d: dict(probes=len(v), median_ms=statistics.median(v), min_ms=min(v), max_ms=max(v)) for d, v in groups.items()}


def check_ecn():
    rep, cal = calibrate('ecn', time.time_ns() + 3600 * 10**9, ['-mode', 'ecn', '-count', '200'])
    problems = relay_healthy(rep)
    for leg in cal['legs']:
        if leg.get('error') or leg['foreign'] or leg['unparsed']:
            problems.append(f"{leg['flow']} forward={leg['forward']}: error {leg.get('error')}, foreign {leg['foreign']}, unparsed {leg['unparsed']}")
        for cp in range(4):
            if leg['received'][cp][cp] != leg['sent'][cp] or leg['sent'][cp] != 200:
                problems.append(f"{leg['flow']} forward={leg['forward']} codepoint {cp}: sent {leg['sent'][cp]}, read {leg['received'][cp]}")
    return problems, dict(legs=[{k: v for k, v in l.items() if k != 'samples'} for l in cal['legs']])


def check_delay(name, offset_s, expected):
    """expected(flow, model_seconds) -> D in ms or None (ignored near a boundary)."""
    zero = time.time_ns() - int(offset_s * 1e9)
    count = 250
    rep, cal = calibrate(name, zero, ['-mode', 'probe', '-count', str(count), '-interval', '10ms'])
    problems, details = relay_healthy(rep), {}
    for leg in cal['legs']:
        p, d = leg_ok(leg, lambda sent: expected(leg['flow'], (sent - zero) / 1e9), count)
        key = f"{leg['flow']} {'forward' if leg['forward'] else 'reverse'}"
        problems += [f'{key}: {x}' for x in p]
        details[key] = d
    return problems, details


def boundary(at, before, after):
    def expected(flow, model_s):
        if flow == 'focal':
            return 50.0
        if model_s < at - 0.02:
            return before
        if model_s > at + 0.02:
            return after
        return None
    return expected


def check_rate(reverse):
    name = 'rate-' + ('reverse' if reverse else 'forward')
    rep, cal = calibrate(name, time.time_ns() + 3600 * 10**9,
                         ['-mode', 'blast', '-mbps', '75', '-duration', '4s', '-payload', '1400'] + (['-reverse'] if reverse else []))
    problems = relay_healthy(rep)
    arrivals = sorted((t, n, leg['flow']) for leg in cal['legs'] for t, n in leg['samples'])
    for leg in cal['legs']:
        if leg.get('error') or leg['foreign'] or leg['unparsed']:
            problems.append(f"{leg['flow']}: error {leg.get('error')}, foreign {leg['foreign']}, unparsed {leg['unparsed']}")
    t0 = arrivals[0][0] + 1.5e9
    window = [(n, f) for t, n, f in arrivals if t0 <= t < t0 + 2e9]
    mbps = sum((n + 28) * 8 for n, _ in window) / 2 / 1e6
    shares = {f: sum((n + 28) * 8 for n, g in window if g == f) / 2 / 1e6 for f in ['focal', 'competitor']}
    d = rep['Reverse' if reverse else 'Forward']
    if not 99.0 <= mbps <= 101.0:
        problems.append(f'delivered {mbps:.3f} Mbit/s outside 100 +/- 1%')
    if d['Stats']['MaxQueueBytes'] < CAPACITY - PACKET or d['Stats']['MaxQueueBytes'] > CAPACITY:
        problems.append(f"largest queue {d['Stats']['MaxQueueBytes']} bytes")
    if not d['Focal']['Dropped'] or not d['Competitor']['Dropped']:
        problems.append(f"drops focal {d['Focal']['Dropped']}, competitor {d['Competitor']['Dropped']}")
    return problems, dict(delivered_mbps=mbps, per_flow_mbps=shares, max_queue_bytes=d['Stats']['MaxQueueBytes'],
                          drops=dict(focal=d['Focal']['Dropped'], competitor=d['Competitor']['Dropped']))


def main():
    OUT.mkdir(parents=True, exist_ok=False)
    checks = {
        'ecn': check_ecn,
        'delay-warmup': lambda: check_delay('delay-warmup', -3600, lambda flow, m: 50.0),
        'delay-short': lambda: check_delay('delay-short', 150, lambda flow, m: 12.5 if flow == 'competitor' else 50.0),
        'switch-100': lambda: check_delay('switch-100', 99, boundary(100, 50.0, 12.5)),
        'switch-200': lambda: check_delay('switch-200', 199, boundary(200, 12.5, 50.0)),
        'rate-forward': lambda: check_rate(False),
        'rate-reverse': lambda: check_rate(True),
    }
    results = {}
    for name, check in checks.items():
        problems, details = check()
        results[name] = dict(passed=not problems, problems=problems, details=details)
        print(name, 'pass' if not problems else problems, flush=True)
    passed = all(r['passed'] for r in results.values())
    run.write(OUT / 'prereq.json', dict(passed=passed, checks=results))
    if not passed:
        raise SystemExit('calibration failed: a prerequisite gap')


if __name__ == '__main__':
    main()
