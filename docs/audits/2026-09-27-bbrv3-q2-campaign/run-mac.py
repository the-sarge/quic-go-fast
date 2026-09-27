#!/usr/bin/env python3
"""Frozen Q2 Mac lease orchestration; finite inventory, no automatic retries."""
import concurrent.futures
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import time

SOURCE = Path(__file__).resolve().parents[1] / '2026-09-25-bbrv3-q1-campaign'
ROOT = Path('/Users/josh/.local/share/infra/bbr-q635-controller/.local/cloud/bbr-campaign/q2-mac-20260927')
STAGE = '/tmp/q2-mac-20260927'
SSH = ['ssh', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes', '-o', 'ConnectTimeout=10', '-o', 'ServerAliveInterval=15', '-o', 'ServerAliveCountMax=4']
REVISION = 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0'
PROVIDER = '/Volumes/worktrees/infra/wellspring-minimax-deploy/scripts/garm/local-mac-provider.sh'
PROVIDER_CONFIG = '/Volumes/worktrees/infra/wellspring-minimax-deploy/.local/garm/local-mac-provider.conf'
IDENTITY = ['q2-mac-20260927', 'minimax', 'q2-campaign', 'mac-20260927']


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
    (directory / (label + '.stdout')).write_text(stdout)
    (directory / (label + '.stderr')).write_text(stderr)
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
        code, data = role_results[label]
        if code or data is None:
            errors.append(f'{label}: exit={code}, JSON={data is not None}')
            continue
        if label == 'native-path':
            if data != command['config']:
                errors.append('native forwarding receipt mismatch')
        elif label == 'router':
            if data.get('Source') != REVISION or data.get('PacketVersion') != 2 or data.get('Config') != d['router_config']:
                errors.append('gateway identity/configuration mismatch')
            observations = data.get('Observations', [])
            if len(observations) != 2:
                errors.append('missing directional gateway observations')
            for obs in observations:
                if any(obs.get(k, 0) for k in ['Error', 'SocketDrops', 'NonCanonical', 'SendErrors', 'MissingTimestamps']):
                    errors.append('gateway packet/timestamp/error gate')
                if obs.get('Stats', {}).get('PropagationOverflow', 0):
                    errors.append('gateway propagation storage gate')
                if any(obs.get(k, 0) > 5000000 for k in ['MaxIngressLagNS', 'MaxEgressLagNS']):
                    errors.append('gateway five-millisecond timing gate')
        else:
            result = data.get('result', {})
            if data.get('error') or result.get('errors') or data.get('resources', {}).get('error'):
                errors.append(f'{label}: fixture/resource error')
            if data.get('source') != REVISION or data.get('platform') != 'darwin/arm64' or data.get('gomaxprocs') != 4 or data.get('managed_direct') is not True:
                errors.append(f'{label}: native source/resource identity mismatch')
            if result.get('run') != d['focal_config'] or result.get('controller') != d['focal_config']['controller']:
                errors.append(f'{label}: case/controller mismatch')
            if result.get('receiver', {}).get('corrupt', 0):
                errors.append(f'{label}: corrupt useful delivery')
            if not data.get('resource_samples') or not data.get('resources'):
                errors.append(f'{label}: missing resources')
            if label == 'focal-receive' and d['case']['group'] == 'completion':
                verified, censored = result.get('completion_verified'), result.get('censored')
                if not (bool(verified) ^ bool(censored)):
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
    assert d['case']['platform'] == 'darwin/arm64'
    assert d['gateway_end_unix_ns'] / 1e9 + 30 < expiry
    for command in d['commands']:
        command['command'] = command['command'].replace('/tmp/q1-campaign', STAGE)
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
                outcomes[label] = job.result()
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
    indices = json.loads((ROOT / 'mac-case-indices.json').read_text())
    assert indices == list(range(340, 400)) + list(range(500, 510))
    assert not list(ROOT.glob('case-*')), 'New attempt needs a new directory/ports/reservation; never overwrite receipts'
    began = time.time()
    expiry = began + 338 * 60
    save(ROOT / 'mac-lease.json', {'id': 'Q2-06', 'started_unix': began, 'expires_unix': expiry, 'reserved_minutes': 338, 'cloud_usd': 0, 'indices': indices, 'source': REVISION})
    ledger = json.loads((ROOT / 'ledger.json').read_text())
    ledger['reservations'].append({'id': 'Q2-06', 'hours': 338 / 60, 'usd': 0, 'expires_unix': expiry, 'status': 'reserved before execution; unused time remains charged'})
    assert ledger['baseline_experiment_hours'] + sum(x['hours'] for x in ledger['reservations']) <= ledger['experiment_ceiling_hours']
    temporary = ROOT / 'ledger-updated.json'
    save(temporary, ledger)
    temporary.replace(ROOT / 'ledger.json')
    monitors = []
    status, error = 'failed', None
    try:
        out = (ROOT / 'mini-awake.stdout').open('x')
        err = (ROOT / 'mini-awake.stderr').open('x')
        awake = subprocess.Popen(SSH + ['m4mini-eth', 'caffeinate -i -t 20280'], stdout=out, stderr=err)
        monitors.append((awake, out, err))
        for host in ('m4mini-eth', 'mbp128', 'minimax'):
            count = 338 * 60 // 5
            command = f'iostat -C -w 5 -c {count}' if host != 'minimax' else f'vmstat 5 {count}'
            out = (ROOT / (host + '-cpu-during.txt')).open('x')
            err = (ROOT / (host + '-cpu-during.stderr')).open('x')
            p = subprocess.Popen(SSH + [host, command], stdout=out, stderr=err)
            monitors.append((p, out, err))
        for index in indices:
            one_case(index, expiry)
        status = 'completed'
    except BaseException as exc:
        error = repr(exc)
        print(error, flush=True)
    finally:
        for p, out, err in monitors:
            p.terminate()
            try:
                p.wait(timeout=10)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait()
            out.close()
            err.close()
        clock = subprocess.run(['python3', str(ROOT / 'followup-clock-observe.py'), 'mac', str(ROOT / 'clock-after.json')], capture_output=True, text=True, timeout=60)
        (ROOT / 'clock-after-driver.txt').write_text(clock.stdout + '\n' + clock.stderr)
        code, _ = run('minimax', 'sudo -n bash /tmp/q1-gateway-network.sh down && test -z "$(sudo -n ip netns list | grep q1-mac-20260925)"', ROOT, 'gateway-cleanup', 40)
        clean = code == 0
        action = '--release-host-admission-consumer' if clean else '--fail-host-admission-consumer'
        args = [PROVIDER, action, *IDENTITY]
        if not clean:
            args.append('Q2 gateway cleanup failed; inspect the retained controller receipt')
        command = 'GARM_PROVIDER_CONFIG_FILE=' + shlex.quote(PROVIDER_CONFIG) + ' ' + shlex.join(args)
        release, _ = run('m4mini-eth', command, ROOT, 'admission-cleanup', 60)
        if not clean or release or clock.returncode:
            status = 'failed'
        save(ROOT / 'lease-result.json', {'status': status, 'error': error, 'gateway_cleanup_ok': clean, 'admission_disposition_exit': release, 'clock_after_exit': clock.returncode, 'ended_at': datetime.now(timezone.utc).isoformat()})
    return 0 if status == 'completed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
