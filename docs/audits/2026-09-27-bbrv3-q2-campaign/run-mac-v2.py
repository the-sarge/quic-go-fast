#!/usr/bin/env python3
"""Q2 Mac retry candidate: explicit fresh attempt, finite inventory, no retries."""
import argparse
import concurrent.futures
import copy
from datetime import datetime, timezone
import hashlib
import json
import os
import re
from pathlib import Path
import shlex
import subprocess
import time

SOURCE = Path(__file__).resolve().parents[1] / '2026-09-25-bbrv3-q1-campaign'
RECORDS = Path('/Users/josh/.local/share/infra/bbr-q635-controller/.local/cloud/bbr-campaign')
ROOT = None
STAGE = None
SSH = ['ssh', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes', '-o', 'ConnectTimeout=10', '-o', 'ServerAliveInterval=15', '-o', 'ServerAliveCountMax=4']
REVISION = 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0'
PROVIDER = '/Volumes/worktrees/infra/wellspring-minimax-deploy/scripts/garm/local-mac-provider.sh'
PROVIDER_CONFIG = '/Volumes/worktrees/infra/wellspring-minimax-deploy/.local/garm/local-mac-provider.conf'
IDENTITY = None
CLEANUP_SECONDS = 360


def configure(attempt):
    global ROOT, STAGE, IDENTITY
    if not re.fullmatch(r'q2-mac-[0-9]{8}-[a-z0-9]+(?:-[a-z0-9]+)*', attempt):
        raise ValueError('fresh attempt must be q2-mac-YYYYMMDD-unique-suffix')
    ROOT, STAGE = RECORDS / attempt, '/tmp/' + attempt
    IDENTITY = [attempt, 'minimax', 'q2-campaign', attempt]


def preflight():
    if ROOT is None or not ROOT.is_dir() or ROOT.is_symlink():
        raise ValueError('prepare a fresh record directory before dispatch')
    if list(ROOT.glob('case-*')) or any((ROOT / n).exists() for n in ['mac-lease.json', 'lease-result.json', 'dispatch-started.json']):
        raise ValueError('attempt already used; never overwrite receipts or resume implicitly')
    indices = json.loads((ROOT / 'mac-case-indices.json').read_text())
    if indices != list(range(340, 400)) + list(range(500, 510)):
        raise ValueError('Mac inventory/order changed')
    lease = json.loads((ROOT / 'host-lease.json').read_text())
    if [lease.get(k) for k in ['instance_id', 'host', 'consumer_kind', 'consumer_id']] != IDENTITY or lease.get('exclusive_host') is not True:
        raise ValueError('exact exclusive admission receipt required')
    validate_clock(json.loads((ROOT / 'clock-before.json').read_text()))
    if not (ROOT / 'followup-clock-observe.py').is_file():
        raise ValueError('post-run clock observer missing')
    ledger = json.loads((ROOT / 'ledger.json').read_text())
    if ledger['experiment_ceiling_hours'] != 96 or ledger['cloud_ceiling_usd'] != 150 or ledger['preparation_ceiling_hours'] != 72:
        raise ValueError('ledger must reflect the approved ceilings')
    hours = ledger['baseline_experiment_hours'] + sum(x['hours'] for x in ledger['reservations'])
    dollars = ledger['baseline_reserved_usd'] + sum(x['usd'] for x in ledger['reservations'])
    if hours + 338 / 60 > 96 or dollars > 150:
        raise ValueError('insufficient remaining ledger allowance')
    if ledger['forecast_experiment_hours'] > 96 or ledger['forecast_reserved_usd'] > 150:
        raise ValueError('remaining campaign forecast exceeds ceiling')
    return indices, ledger


def save(path, value):
    with path.open('x') as out:
        json.dump(value, out, indent=2)
        out.write('\n')
    path.chmod(0o600)


def run(host, command, directory, label, bound=30):
    started = time.time_ns()
    try:
        p = subprocess.run(SSH + [host, command], capture_output=True, text=True, timeout=bound)
        code, stdout, stderr = p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired as exc:
        code = 124
        stdout = exc.stdout or b''
        stderr = exc.stderr or b''
        if isinstance(stdout, bytes):
            stdout = stdout.decode(errors='replace')
        if isinstance(stderr, bytes):
            stderr = stderr.decode(errors='replace')
        stderr += '\nController command timeout; native process disposition requires observation.\n'
    with (directory / (label + '.stdout')).open('x') as out:
        out.write(stdout)
    with (directory / (label + '.stderr')).open('x') as out:
        out.write(stderr)
    save(directory / (label + '-exit.json'), dict(host=host, command=command, started_unix_ns=started, ended_unix_ns=time.time_ns(), exit_code=code))
    try:
        data = json.loads(stdout)
    except ValueError:
        data = None
    return code, data


def require(host, command, directory, label, bound=30):
    code, data = run(host, command, directory, label, bound)
    if code:
        raise RuntimeError(f'{label}: exit {code}; raw output retained')
    return data


def validate(d, role_results):
    errors = []
    for command in d['commands']:
        label = command['label']
        code, data = role_results.get(label, (None, None))
        if code != 0 or not isinstance(data, dict):
            errors.append(f'{label}: exit={code}, JSON object={isinstance(data, dict)}')
            continue
        if label == 'native-path':
            if data != command['config']:
                errors.append('native forwarding receipt mismatch')
        elif label == 'router':
            expected = copy.deepcopy(d['router_config'])
            # Pinned Go side struct emits its zero-valued string (no omitempty).
            # Normalize the generated expectation only; receipts stay untouched.
            for side in ('A', 'B'):
                expected[side].setdefault('Competitor', '')
            if data.get('Source') != REVISION or data.get('PacketVersion') != 2 or data.get('Config') != expected:
                errors.append('gateway identity/configuration mismatch')
            if data.get('Profiled') is not False:
                errors.append('gateway profiling status must be explicitly false')
            observations = data.get('Observations', [])
            directions = {expected['A']['Interface'] + '->' + expected['B']['Interface'],
                          expected['B']['Interface'] + '->' + expected['A']['Interface']}
            if not isinstance(observations, list) or len(observations) != 2 or any(not isinstance(o, dict) for o in observations):
                errors.append('missing directional gateway observations')
                continue
            if {o.get('Direction') for o in observations} != directions:
                errors.append('gateway directions do not cover both paths')
            for obs in observations:
                counters = ['SocketDrops', 'NonCanonical', 'SendErrors', 'MissingTimestamps']
                if obs.get('Error') != '' or any(type(obs.get(k)) is not int or obs[k] != 0 for k in counters):
                    errors.append('gateway packet/timestamp/error gate')
                stats = obs.get('Stats')
                if not isinstance(stats, dict) or type(stats.get('PropagationOverflow')) is not int or stats['PropagationOverflow'] != 0:
                    errors.append('gateway propagation storage gate')
                if any(type(obs.get(k)) is not int or not 0 <= obs[k] <= 5000000 for k in ['MaxIngressLagNS', 'MaxEgressLagNS']):
                    errors.append('gateway five-millisecond timing gate')
        else:
            result = data.get('result')
            if not isinstance(result, dict) or not isinstance(result.get('receiver'), dict) or not isinstance(result.get('control'), dict) or not isinstance(data.get('resources'), dict):
                errors.append(f'{label}: missing result/receiver/control/resources')
                continue
            if data.get('error') or result.get('errors') or data.get('resources', {}).get('error'):
                errors.append(f'{label}: fixture/resource error')
            if result['control'].get('error'):
                errors.append(f'{label}: control error')
            if data.get('source') != REVISION or data.get('platform') != 'darwin/arm64' or data.get('gomaxprocs') != 4 or data.get('managed_direct') is not True:
                errors.append(f'{label}: native source/resource identity mismatch')
            if result.get('run') != d['focal_config'] or result.get('controller') != d['focal_config']['controller']:
                errors.append(f'{label}: case/controller mismatch')
            if type(result['receiver'].get('corrupt')) is not int or result['receiver']['corrupt'] != 0:
                errors.append(f'{label}: corrupt useful delivery')
            trace = data.get('trace')
            if not isinstance(trace, dict) or any(not isinstance(trace.get(k), dict) for k in ['sent_ecn', 'received_ecn']):
                errors.append(f'{label}: missing native ECN record')
            if not data.get('resource_samples') or not data.get('resources'):
                errors.append(f'{label}: missing resources')
            if label == 'focal-receive' and d['case']['group'] == 'completion':
                verified, censored = result.get('completion_verified'), result.get('censored')
                if type(verified) is not bool or type(censored) is not bool or verified == censored:
                    errors.append('completion must be verified or explicitly censored')
                if verified and result.get('receiver', {}).get('useful_bytes') != d['focal_config']['completion_bytes']:
                    errors.append('verified completion byte count mismatch')
    return errors


def one_case(index, expiry):
    directory = ROOT / ('case-' + str(index))
    directory.mkdir(mode=0o700)
    require('minimax', 'test -z "$(sudo -n virsh list --name --state-running)" && test -z "$(podman ps -q)"', directory, 'gateway-availability')
    start = time.time_ns() + 25000000000
    raw = subprocess.check_output(['python3', str(SOURCE / 'case-commands.py'), str(index), '--start-unix-ns', str(start)], text=True)
    d = json.loads(raw)
    if d['case']['platform'] != 'darwin/arm64' or d['gateway_end_unix_ns'] / 1e9 + CLEANUP_SECONDS >= expiry:
        raise ValueError('native platform or remaining lease envelope invalid')
    for command in d['commands']:
        command['command'] = command['command'].replace('/tmp/q1-campaign', STAGE)
        command['command'] = command['command'].replace('./q1fixture ', STAGE + '/q1fixture ')
        command['command'] = command['command'].replace('sudo ', 'sudo -n ')
        if command['readiness_command']:
            command['readiness_command'] = command['readiness_command'].replace('/tmp/q1-campaign', STAGE)
    save(directory / 'commands.json', d)
    outcomes, pending = {}, []
    failure = None
    bound = (d['gateway_end_unix_ns'] - time.time_ns()) / 1e9 + 30
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        try:
            for command in d['commands']:
                label, host, text = command['label'], command['host'], command['command']
                if label == 'native-path':
                    outcomes[label] = run(host, text, directory, label)
                    if outcomes[label][0]:
                        raise RuntimeError('native-path setup failed')
                    continue
                pending.append((label, pool.submit(run, host, text, directory, label, bound)))
                if label == 'router':
                    require(host, 'for i in $(seq 1 30); do pgrep -f "^./q1router " >/dev/null && test "$(sudo -n ip netns exec q1-mac-20260925 cat /proc/sys/net/ipv4/ip_forward)" = 0 && exit 0; sleep 0.2; done; exit 1', directory, 'router-ready')
                if command['readiness_command']:
                    ready = False
                    for attempt in range(40):
                        code, _ = run(host, command['readiness_command'], directory, f'{label}-ready-{attempt}', 5)
                        if code == 0:
                            ready = True
                            break
                        if time.time_ns() >= start - 1000000000:
                            break
                        time.sleep(0.2)
                    if not ready:
                        raise RuntimeError('receiver did not become ready before scheduled start')
        except Exception as exc:
            failure = str(exc)
        finally:
            for label, job in pending:
                try:
                    outcomes[label] = job.result()
                except Exception as exc:
                    outcomes[label] = (125, None)
                    failure = (failure or '') + f'; {label}: {exc!r}'
    errors = [failure] if failure else []
    if len(outcomes) != len(d['commands']):
        errors.append('not all required roles launched')
    else:
        errors.extend(validate(d, outcomes))
    save(directory / 'verdict.json', {'case_index': index, 'id': d['case']['id'], 'valid': not errors, 'errors': errors, 'ended_at': datetime.now(timezone.utc).isoformat()})
    print(json.dumps({'case_index': index, 'id': d['case']['id'], 'valid': not errors, 'errors': errors}), flush=True)
    if errors:
        raise RuntimeError('hard gate failed: ' + '; '.join(errors))


def main():
    os.umask(0o077)
    indices, ledger = preflight()
    # Exclusive creation also prevents simultaneous dispatch of the same attempt.
    save(ROOT / 'dispatch-started.json', {'started_at': datetime.now(timezone.utc).isoformat(), 'runner_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
    began = time.time()
    expiry = began + 338 * 60
    save(ROOT / 'mac-lease.json', {'id': 'Q2-06', 'started_unix': began, 'expires_unix': expiry, 'reserved_minutes': 338, 'cloud_usd': 0, 'indices': indices, 'source': REVISION})
    save(ROOT / 'ledger-before.json', ledger)
    ledger['reservations'].append({'id': IDENTITY[0], 'lease_family': 'Q2-06', 'hours': 338 / 60, 'usd': 0, 'expires_unix': expiry, 'status': 'reserved before execution; unused time remains charged'})
    temporary = ROOT / 'ledger-updated.json'
    save(temporary, ledger)
    temporary.replace(ROOT / 'ledger.json')
    monitors = []
    status, error = 'failed', None
    try:
        for host in ('m4mini-eth', 'mbp128', 'minimax'):
            count = 338 * 60 // 5
            command = f'iostat -C -w 5 -c {count}' if host != 'minimax' else f'vmstat 5 {count}'
            script = monitor_script(command, host == 'm4mini-eth')
            command = 'cd ' + shlex.quote(STAGE) + ' && test ! -e monitor.sh && printf %s ' + shlex.quote(script) + ' > monitor.sh && exec /bin/sh ' + shlex.quote(STAGE + '/monitor.sh')
            out = (ROOT / (host + '-cpu-during.txt')).open('x')
            err = (ROOT / (host + '-cpu-during.stderr')).open('x')
            p = subprocess.Popen(SSH + [host, command], stdout=out, stderr=err)
            monitors.append((p, out, err, host))
            require(host, 'for i in 1 2 3 4 5; do test -s ' + shlex.quote(STAGE + '/monitor-child.pid') + ' && kill -0 "$(cat ' + shlex.quote(STAGE + '/monitor-child.pid') + ')" && exit 0; sleep 0.2; done; exit 1', ROOT, host + '-monitor-ready', 5)
        for index in indices:
            one_case(index, expiry)
        status = 'completed'
    except BaseException as exc:
        error = repr(exc)
        print(error, flush=True)
    finally:
        result = finish_lease(monitors, status, error)
    return 0 if result['status'] == 'completed' else 1


def monitor_script(command, awake):
    # A named, attempt-owned shell owns and reaps both children. Closing SSH
    # alone does not prove remote process termination.
    return f'''#!/bin/sh
set -eu
cd {shlex.quote(STAGE)}
child= awake=
cleanup() {{
  trap - EXIT HUP INT TERM
  [ -z "$child" ] || kill "$child" 2>/dev/null || true
  [ -z "$awake" ] || kill "$awake" 2>/dev/null || true
  [ -z "$child" ] || wait "$child" 2>/dev/null || true
  [ -z "$awake" ] || wait "$awake" 2>/dev/null || true
}}
trap cleanup EXIT
trap 'exit 0' HUP INT TERM
echo $$ > monitor.pid
{command} &
child=$!
echo "$child" > monitor-child.pid
{'caffeinate -i -w $$ & awake=$!' if awake else ':'}
wait "$child"
'''


def stop_monitor_command():
    script = f'''
set -eu
cd {shlex.quote(STAGE)}
test -f monitor.pid
pid=$(cat monitor.pid)
case "$pid" in ''|*[!0-9]*) exit 1;; esac
expected={shlex.quote('/bin/sh ' + STAGE + '/monitor.sh')}
actual=$(ps -p "$pid" -o command= 2>/dev/null || true)
test -n "$actual" || exit 0
test "$actual" = "$expected" || exit 1
kill -TERM "$pid"
for i in 1 2 3 4 5; do
  actual=$(ps -p "$pid" -o command= 2>/dev/null || true)
  test "$actual" = "$expected" || exit 0
  sleep 1
done
exit 1
'''
    return '/bin/sh -c ' + shlex.quote(script)


def finish_lease(monitors, status, error):
    # Each cleanup step is independent. A clock/SSH failure cannot skip teardown
    # or the admission disposition. Every failure stays in the final receipt.
    cleanup_errors = []
    remote_monitors_clean = True
    for p, out, err, host in monitors:
        try:
            code, _ = run(host, stop_monitor_command(), ROOT, host + '-monitor-cleanup', 15)
            if code:
                raise RuntimeError(f'{host}: remote monitor cleanup exit {code}')
        except Exception as exc:
            remote_monitors_clean = False
            cleanup_errors.append('remote monitor cleanup: ' + repr(exc))
        try:
            p.terminate()
            try:
                p.wait(timeout=10)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait(timeout=5)
        except Exception as exc:
            cleanup_errors.append('monitor shutdown: ' + repr(exc))
        finally:
            out.close()
            err.close()
    clock_exit = None
    endpoints_clean = True
    for host in ('m4mini-eth', 'mbp128'):
        try:
            # Only processes launched with this fresh attempt's absolute binary
            # path are eligible. Never use a generic fixture-name kill.
            pattern = shlex.quote('^' + STAGE + '/q1fixture ')
            command = f'set -e; pkill -TERM -f {pattern} || test "$?" = 1; for i in 1 2 3 4 5; do rc=0; pgrep -f {pattern} >/dev/null || rc=$?; test "$rc" = 1 && exit 0; test "$rc" = 0 || exit 1; sleep 1; done; exit 1'
            code, _ = run(host, command, ROOT, host + '-endpoint-cleanup', 10)
            if code:
                raise RuntimeError(f'{host}: endpoint termination not proved, exit {code}')
        except Exception as exc:
            endpoints_clean = False
            cleanup_errors.append('endpoint cleanup: ' + repr(exc))
    try:
        clock = subprocess.run(['python3', str(ROOT / 'followup-clock-observe.py'), 'mac', str(ROOT / 'clock-after.json')], capture_output=True, text=True, timeout=60)
        clock_exit = clock.returncode
        with (ROOT / 'clock-after-driver.txt').open('x') as out:
            out.write(clock.stdout + '\n' + clock.stderr)
        if clock_exit:
            raise RuntimeError(f'clock observer exit {clock_exit}')
        validate_clock(json.loads((ROOT / 'clock-after.json').read_text()))
    except Exception as exc:
        cleanup_errors.append('clock observation: ' + repr(exc))
    clean = False
    try:
        code, _ = run('minimax', 'sudo -n bash /tmp/q1-gateway-network.sh down && test -z "$(sudo -n ip netns list | grep q1-mac-20260925)"', ROOT, 'gateway-cleanup', 40)
        clean = code == 0
        if not clean:
            cleanup_errors.append(f'gateway cleanup exit {code}')
    except Exception as exc:
        cleanup_errors.append('gateway cleanup: ' + repr(exc))
    release = None
    try:
        owned_clean = clean and remote_monitors_clean and endpoints_clean
        action = '--release-host-admission-consumer' if owned_clean else '--fail-host-admission-consumer'
        args = [PROVIDER, action, *IDENTITY]
        if not owned_clean:
            args.append('Q2 owned cleanup failed; inspect the retained controller receipt')
        command = 'GARM_PROVIDER_CONFIG_FILE=' + shlex.quote(PROVIDER_CONFIG) + ' ' + shlex.join(args)
        release, _ = run('m4mini-eth', command, ROOT, 'admission-cleanup', 60)
        if release:
            cleanup_errors.append(f'admission disposition exit {release}')
    except Exception as exc:
        cleanup_errors.append('admission disposition: ' + repr(exc))
    if cleanup_errors:
        status = 'failed'
    result = {'status': status, 'error': error, 'cleanup_errors': cleanup_errors, 'gateway_cleanup_ok': clean, 'remote_monitors_cleanup_ok': remote_monitors_clean, 'endpoint_cleanup_ok': endpoints_clean, 'admission_disposition_exit': release, 'clock_after_exit': clock_exit, 'ended_at': datetime.now(timezone.utc).isoformat()}
    save(ROOT / 'lease-result.json', result)
    return result


def validate_clock(data):
    for host in ('m4mini-eth', 'mbp128', 'minimax'):
        observation = data['hosts'][host]
        if observation['consistent'] is not True or not observation['samples'] or observation['intersection_lower_ns'] > observation['intersection_upper_ns']:
            raise ValueError(f'{host}: inconsistent clock observation')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attempt', required=True)
    parser.add_argument('--execute', action='store_true', help='launch only after fresh native/path/resource/price/ledger preflight')
    args = parser.parse_args()
    configure(args.attempt)
    if args.execute:
        raise SystemExit(main())
    preflight()
    print('Local prepared-attempt checks passed; no native process launched.')
