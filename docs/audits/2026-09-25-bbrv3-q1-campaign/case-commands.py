#!/usr/bin/env python3
"""Expand one frozen Q2 case into finite native commands; not a service."""
import argparse
import base64
import copy
import hashlib
import json
import shlex
from pathlib import Path

root = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('index', type=int, help='zero-based frozen inventory index')
parser.add_argument('--start-unix-ns', required=True, type=int, help='warm-up start, normally now plus25s')
args = parser.parse_args()
raw = (root / 'case-inventory.jsonl').read_bytes()
assert hashlib.sha256(raw).hexdigest() == 'bab8af515337f2fa32bdf3765e658a1d102ad601125112100b7043c23ef28fd1'
rows = [json.loads(line) for line in raw.splitlines()]
if not 0 <= args.index < len(rows) or args.start_unix_ns <= 0:
    parser.error('finite inventory index and positive start required')
row = rows[args.index]
mac = row['platform'] == 'darwin/arm64'
windows = row['platform'] == 'windows/amd64'
port = 20000 + 8 * args.index
cfg = dict(row['fixture'], start_unix_ns=args.start_unix_ns)
start = cfg['start_unix_ns']
warm_ns = cfg['warmup_ms'] * 1000000
end = start + (cfg['warmup_ms'] + cfg['measure_ms']) * 1000000
name = row['id']
ips = ['10.64.1.10', '10.64.2.10'] if mac else ['10.63.1.10', '10.63.2.10']
hosts = ['m4mini-eth', 'mbp128'] if mac else ['bbr-a', 'bbr-b']
interfaces = ['q1a', 'q1b'] if mac else ['ens4', 'ens5']
macs = ['02:63:65:01:00:01', '02:63:65:02:00:01'] if mac else ['42:01:0a:3f:01:01', '42:01:0a:3f:02:01']
competitors = row['group'] == 'core' and row['scenario'] in ('L4', 'L6', 'L7', 'L8')
router = dict(A=dict(Interface=interfaces[0], PeerMAC=macs[0], Focal=ips[0]),
              B=dict(Interface=interfaces[1], PeerMAC=macs[1], Focal=ips[1]),
              Scenario=row['scenario'], StartUnixNS=start + warm_ns,
              MeasureSeconds=cfg['measure_ms'] // 1000,
              Phase=row['event_phase_seconds'], Seed=row['seed'])
if competitors:
    router['A']['Competitor'], router['B']['Competitor'] = '10.63.1.11', '10.63.2.11'
result = {'case': row, 'focal_config': cfg, 'router_config': router,
          'start_unix_ns': start, 'measurement_start_unix_ns': start + warm_ns,
          'measurement_end_unix_ns': end, 'gateway_end_unix_ns': end + 20000000000,
          'native_window_contract': 'Before execution, require qualified profile, clock/path/resource preflight, sufficient frozen lease time, no overlap with prior case, and ledger reservation. Endpoint start must be within120s; receivers ready before senders. Collect every role even on failure; any missing/nonzero result invalidates the case.',
          'commands': []}

def command(host, label, text, launch, config=None, ready=None):
    result['commands'].append(dict(host=host, label=label, launch_not_before_unix_ns=launch,
                                    command=text, config=config, readiness_command=ready))

workdir = '/tmp/q1-campaign'
router_bound = (cfg['warmup_ms'] + cfg['measure_ms']) // 1000 + 50
router_prefix = 'sudo ip netns exec q1-mac-20260925 ' if mac else 'sudo '
router_env = 'env GOGC=400 GOMEMLIMIT=4GiB ' + ('taskset -c 0-3 ' if mac else '')
router_command = 'cd '+workdir+' && printf %s '+shlex.quote(json.dumps(router))+' > '+name+'-router-config.json && '+router_prefix+router_env+'timeout --signal=TERM --kill-after=5 '+str(router_bound)+' ./q1router -config '+name+'-router-config.json -output '+name+'-router.json; rc=$?; sudo cat '+workdir+'/'+name+'-router.json; exit $rc'
command('minimax' if mac else 'bbr-gateway', 'router', router_command, start - 25000000000, router)

def endpoints(label, config, pair_hosts, pair_ips, base_port, cubic=False):
    launch = config['start_unix_ns'] - (45000000000 if label.startswith('competitor-') else 25000000000)
    seconds = (config['warmup_ms'] + config['measure_ms']) // 1000 + (70 if cubic else 50)
    for role, i in [('receive', 1), ('send', 0)]:
        ident = name+'-'+label+'-'+role
        config_name = ident+'-config.json'
        output = ident+'.json'
        cli = '-role '+role+' -config '+config_name+' -local '+pair_ips[i]+':'+str(base_port)+' -output '+output
        if role == 'send': cli += ' -peer '+pair_ips[1]+':'+str(base_port)
        if cubic: cli += ' -tcp-cubic'
        if windows:
            data = base64.b64encode(json.dumps(config).encode()).decode()
            script = f"""$ErrorActionPreference='Stop'
Set-Location C:/bbr/q1
[IO.File]::WriteAllBytes('C:/bbr/q1/{config_name}',[Convert]::FromBase64String('{data}'))
$p=Start-Process -FilePath C:/bbr/q1/q1fixture.exe -WorkingDirectory C:/bbr/q1 -ArgumentList '{cli}' -PassThru -NoNewWindow -RedirectStandardOutput '{ident}.stdout' -RedirectStandardError '{ident}.stderr'
if (-not $p.WaitForExit({seconds*1000})) {{ $p.Kill(); throw 'fixture deadline' }}
$p.Refresh(); $rc=$p.ExitCode
if (Test-Path '{output}') {{ [Convert]::ToBase64String([IO.File]::ReadAllBytes('C:/bbr/q1/{output}')) }} else {{ Get-Content -Raw '{ident}.stderr' | Write-Error; exit 1 }}
exit $rc
"""
            text = 'powershell.exe -NoProfile -NonInteractive -EncodedCommand '+base64.b64encode(script.encode('utf-16le')).decode()
            ready = "powershell.exe -NoProfile -NonInteractive -Command \"if (Select-String -Quiet -Pattern ready -Path C:/bbr/q1/"+ident+".stderr -ErrorAction SilentlyContinue) {exit 0}; exit 1\""
        else:
            # macOS process lifetime is bounded inside the native fixture.
            timeout = '' if mac else 'timeout --signal=TERM --kill-after=5 '+str(seconds)+' '
            text = 'cd '+workdir+' && printf %s '+shlex.quote(json.dumps(config))+' > '+config_name+' && '+timeout+'./q1fixture '+cli+' 2>'+ident+'.stderr; rc=$?; cat '+workdir+'/'+output+'; cat '+workdir+'/'+ident+'.stderr >&2; exit $rc'
            ready = 'grep -q ready '+workdir+'/'+ident+'.stderr'
        command(pair_hosts[i], label+'-'+role, text, launch, config, ready if role == 'receive' else None)

endpoints('focal', cfg, hosts, ips, port)
if competitors:
    schedule = json.loads((root / 'competitor-schedule-candidate.json').read_text())['scenarios'][row['scenario']]
    for n, spec in enumerate(schedule.get('flows', [schedule]), 1):
        c = copy.deepcopy(cfg)
        c.update(id=name+'-competitor-'+str(n), controller=schedule['controller'], workload='stream', payload_bytes=16384,
                 start_unix_ns=start+warm_ns+spec['start_measured_seconds']*1000000000,
                 warmup_ms=spec['warmup_ms'], measure_ms=spec['measure_ms'])
        if row['scenario'] != 'L4':
            c.update(bulk_pause_start_ms=240000, bulk_pause_end_ms=270000)
        endpoints('competitor-'+str(n), c, ['bbr-competitor-a', 'bbr-competitor-b'], ['10.63.1.11', '10.63.2.11'], port+spec['port_offset'], schedule['tcp_cubic'])
print(json.dumps(result, indent=2))
