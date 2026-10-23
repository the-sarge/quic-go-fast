#!/usr/bin/env python3
"""#712 D3 prerequisites on minimax: host facts and relay ECN calibration.

Records the CPU topology, the host settings this ticket must leave unchanged,
and a bidirectional codepoint calibration through the unadapted #711 relay
(negative control: Linux strips ECN) and the adapted relay used for every WAN
arm. Endpoint ECN engagement and the instrument smoke check run as the
excluded `smoke` and `perfsmoke` matrix stages. Never changes host state.
"""
import json
import subprocess
import time

from run import AFFINITY, ART, BACK, FRONT, RECEIVER, SENDER, write


def sh(*cmd):
    return subprocess.run(cmd, capture_output=True, text=True).stdout


def calibrate(relay, scenario='S5'):
    out = ART / 'prereq' / f'calibrate-{relay}.json'
    start = time.time_ns() + 500_000_000
    err = (ART / 'prereq' / f'calibrate-{relay}.relay.stderr').open('w')
    proc = subprocess.Popen(['taskset', '-c', AFFINITY['relay'], str(ART / 'bin' / relay), '-scenario', scenario,
                             '-seed', '9997', '-front', FRONT, '-back', BACK, '-sender', SENDER, '-target', RECEIVER,
                             '-start-unix-ns', str(start), '-until', '30s',
                             '-output', str(ART / 'prereq' / f'calibrate-{relay}.relay.json')],
                            stdout=subprocess.DEVNULL, stderr=err)
    time.sleep(1)
    res = subprocess.run(['taskset', '-c', AFFINITY['send'], str(ART / 'bin/calibrate'), '-sender', SENDER, '-target',
                          RECEIVER, '-front', FRONT, '-back', BACK, '-output', str(out)], capture_output=True, text=True)
    proc.terminate()
    proc.wait(timeout=5)
    err.close()
    return dict(relay=relay, scenario=scenario, exit=res.returncode, report=json.loads(out.read_text()),
                relay_report=json.loads((ART / 'prereq' / f'calibrate-{relay}.relay.json').read_text()))


(ART / 'prereq').mkdir(parents=True, exist_ok=True)
facts = dict(
    uname=sh('uname', '-a'), lscpu=sh('lscpu'), lscpu_e=sh('lscpu', '-e=CPU,CORE,SOCKET,NODE,CACHE'),
    l3=sh('sh', '-c', 'for c in 0 8; do cat /sys/devices/system/cpu/cpu$c/cache/index3/shared_cpu_list; done'),
    governor=sh('sh', '-c', 'cat /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor | sort | uniq -c'),
    sysctl=sh('sysctl', 'kernel.perf_event_paranoid', 'net.core.rmem_max', 'net.core.wmem_max', 'net.core.rmem_default',
              'net.core.wmem_default', 'net.ipv4.udp_mem', 'kernel.sched_autogroup_enabled'),
    kernel_accounting=sh('sh', '-c', 'grep -E "IRQ_TIME_ACCOUNTING|VIRT_CPU_ACCOUNTING|NO_HZ_FULL=|CONFIG_HZ=" /boot/config-$(uname -r)'),
    lo_offloads=sh('sh', '-c', 'ethtool -k lo | grep -E "udp|generic"'), perf=sh('perf', '--version'),
    go=sh('sh', '-c', 'GOTOOLCHAIN=go1.27.0 go version'), id=sh('id'), uptime=sh('uptime'), affinity=AFFINITY,
    calibration=[calibrate('relay-v3'), calibrate('relay-linux')])
write(ART / 'prereq' / 'host-facts.json', facts)
for c in facts['calibration']:
    r = c['report']
    print(c['relay'], 'pass' if r['pass'] else 'FAIL', 'forward', r['forward']['received'], 'reverse', r['reverse']['received'],
          'relay ECNIn', c['relay_report']['Forward']['ECNIn'], c['relay_report']['Reverse']['ECNIn'])
