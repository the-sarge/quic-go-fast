#!/usr/bin/env python3
"""Finite Q2 continuation/full Linux aid; preserved prefix, fail closed, no retries."""
import argparse
import concurrent.futures
import copy
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import shlex
import subprocess
import time

HERE = Path(__file__).resolve().parent
SOURCE = HERE.parent / '2026-09-25-bbrv3-q1-campaign'
CONTROLLER = Path('/Users/josh/.local/share/infra/bbr-q635-controller')
RECORDS = CONTROLLER / '.local/cloud/bbr-campaign'
REVISION = 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0'
CLEANUP_SECONDS = 660
# Worst cleanup waits: clock90+TERM5+reap5+3*chrony10, destroy420+TERM10+
# reap5, 3*monitor10, 2*inventory30 = 655 seconds, within the existing reserve.
ROOT = None
STAGE = None
SSH = None

def save(path, data):
    with path.open('x') as f:
        json.dump(data, f, indent=2)
        f.write('\n')

def configure(attempt):
    global ROOT, STAGE, SSH
    if not re.fullmatch(r'q2-linux-[0-9]{8}-[a-z0-9]+(?:-[a-z0-9]+)*', attempt):
        raise ValueError('fresh Linux attempt identity required')
    ROOT = RECORDS / attempt
    STAGE = '/tmp/' + attempt
    SSH = ['gcloud', 'compute', 'ssh', '--project=bbr-q635-20260925', '--zone=us-east1-b', '--tunnel-through-iap', '--quiet', '--ssh-flag=-oConnectTimeout=15', '--ssh-flag=-oServerAliveInterval=15', '--ssh-flag=-oServerAliveCountMax=3', '--ssh-flag=-oControlMaster=auto', '--ssh-flag=-oControlPath=/tmp/' + attempt + '-%C', '--ssh-flag=-oControlPersist=10h']

def run(host, command, directory, label, bound=30):
    began = time.time_ns()
    try:
        p = subprocess.run(SSH + [host, '--command=' + command], capture_output=True, text=True, timeout=bound)
        code, stdout, stderr = p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired as exc:
        code = 124
        stdout, stderr = exc.stdout or b'', exc.stderr or b''
        if isinstance(stdout, bytes): stdout = stdout.decode(errors='replace')
        if isinstance(stderr, bytes): stderr = stderr.decode(errors='replace')
    with (directory / (label + '.stdout')).open('x') as f: f.write(stdout)
    with (directory / (label + '.stderr')).open('x') as f: f.write(stderr)
    save(directory / (label + '-exit.json'), dict(host=host, command=command, started_unix_ns=began, ended_unix_ns=time.time_ns(), exit_code=code))
    try: data = json.loads(stdout)
    except ValueError: data = None
    return code, data

def require(host, command, directory, label, bound=30):
    code, data = run(host, command, directory, label, bound)
    if code: raise RuntimeError(f'{label}: exit{code}; raw output retained')
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
            if data.get('source') != REVISION or data.get('platform') != 'linux/amd64' or data.get('gomaxprocs') != 4 or data.get('managed_direct') is not True:
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


def local(args, label, bound):
    began = time.time_ns()
    with (ROOT / (label + '.txt')).open('x') as out:
        p = subprocess.Popen(args, cwd=CONTROLLER, stdout=out, stderr=subprocess.STDOUT, start_new_session=True)
        try: code = p.wait(timeout=bound)
        except subprocess.TimeoutExpired:
            # Normal destroy gets seven minutes before interruption. A genuine
            # timeout stays failed; never automatically unlock or retry state.
            import signal
            os.killpg(p.pid, signal.SIGTERM)
            try: p.wait(timeout=240 if label == 'apply' else 10 if label == 'destroy' else 5)
            except subprocess.TimeoutExpired:
                os.killpg(p.pid, signal.SIGKILL)
                try: p.wait(timeout=5)
                except subprocess.TimeoutExpired: pass  # Failed/unreaped; explicit cleanup follow-up required.
            code = 124
    save(ROOT / (label + '-exit.json'), dict(started_unix_ns=began, ended_unix_ns=time.time_ns(), exit_code=code))
    if code: raise RuntimeError(f'{label}: exit{code}')


def check_interruption_prefix():
    # This is the one operator-authorized interruption, not a generic skip list.
    prefix = RECORDS / 'q2-linux-20260929-lease01-retry2'
    authorization = json.loads((ROOT / 'interruption-authorization.json').read_text())
    plan_path = prefix / 'continuation-0-11-preserved-plan.json'
    if (authorization.get('experiment_ceiling_hours') != 192
            or authorization.get('cloud_ceiling_usd') != 150
            or authorization.get('preparation_ceiling_hours') != 72
            or authorization.get('preserved_indices') != list(range(12))
            or authorization.get('continuation_indices') != list(range(12, 108))
            or authorization.get('missing_closing_clock_disclosure_accepted') is not True
            or authorization.get('prefix_plan_sha256') != hashlib.sha256(plan_path.read_bytes()).hexdigest()):
        raise ValueError('continuation authorization missing or changed')
    plan = json.loads(plan_path.read_text())
    if plan['preserved_indices'] != list(range(12)) or plan['continuation_indices'] != list(range(12, 108)):
        raise ValueError('prefix boundary changed')
    if (json.loads((prefix / 'lease-result.json').read_text())['status'] != 'failed'
            or json.loads((prefix / 'independent-cleanup-verification.json').read_text())['verified'] is not True):
        raise ValueError('interrupted prefix not failed and cleaned up')
    rows = [json.loads(line) for line in (SOURCE / 'case-inventory.jsonl').read_text().splitlines()]
    actual_files = {str(path.relative_to(prefix)) for i in range(12)
                    for path in (prefix / ('case-' + str(i))).iterdir() if path.is_file()}
    if actual_files != set(plan['prefix_files_sha256']):
        raise ValueError('prefix file inventory changed')
    for filename in actual_files:
        path = prefix / filename
        if path.is_symlink() or hashlib.sha256(path.read_bytes()).hexdigest() != plan['prefix_files_sha256'][filename]:
            raise ValueError('prefix receipt changed: ' + filename)
    for index in range(12):
        directory = prefix / ('case-' + str(index))
        d = json.loads((directory / 'commands.json').read_text())
        verdict = json.loads((directory / 'verdict.json').read_text())
        labels = ['native-path' if rows[index]['scenario'] == 'S1' else 'router', 'focal-receive', 'focal-send']
        if (d['case'] != rows[index] or [c['label'] for c in d['commands']] != labels
                or verdict['case_index'] != index or verdict['valid'] is not True):
            raise ValueError('prefix case identity or verdict changed')
        roles = {label: (json.loads((directory / (label + '-exit.json')).read_text())['exit_code'],
                         json.loads((directory / (label + '.stdout')).read_text())) for label in labels}
        if validate(d, roles):
            raise ValueError('prefix raw role replay failed')
    for index in range(0, 108, 2):
        a, b = rows[index:index + 2]
        if (any(a[k] != b[k] for k in ('platform', 'group', 'scenario', 'pair', 'seed'))
                or (a['order_in_pair'], b['order_in_pair']) != (1, 2)):
            raise ValueError('original pair boundary changed')
    return authorization


def check_prefix():
    # Exact second continuation selected by the operator; no arbitrary skip list.
    check_interruption_prefix()
    authorization = json.loads((ROOT / 'operator-authorization.json').read_text())
    prefix = RECORDS / 'q2-linux-20260929-lease01-continuation2'
    manifest_path = prefix / 'failure-evidence-hashes.json'
    if (authorization.get('experiment_ceiling_hours') != 192
            or authorization.get('cloud_ceiling_usd') != 150
            or authorization.get('preparation_ceiling_hours') != 72
            or authorization.get('preserved_indices') != list(range(36))
            or authorization.get('continuation_indices') != list(range(36, 108))
            or authorization.get('missing_closing_clock_disclosure_accepted') is not True
            or authorization.get('no_automatic_retry') is not True
            or authorization.get('suffix_prefix_manifest_sha256') != hashlib.sha256(manifest_path.read_bytes()).hexdigest()):
        raise ValueError('second continuation authorization missing or changed')
    manifest = json.loads(manifest_path.read_text())
    for filename, facts in manifest['files'].items():
        path = prefix / filename
        if path.is_symlink() or path.stat().st_size != facts['bytes'] or hashlib.sha256(path.read_bytes()).hexdigest() != facts['sha256']:
            raise ValueError('prior failed attempt receipt changed: ' + filename)
    if (json.loads((prefix / 'lease-result.json').read_text())['status'] != 'failed'
            or json.loads((prefix / 'independent-cleanup-verification.json').read_text())['cleanup_verified'] is not True
            or json.loads((prefix / 'case-36/verdict.json').read_text())['valid'] is not False):
        raise ValueError('prior attempt not failed and independently cleaned')
    for boundary in ('before', 'after'):
        data = json.loads((prefix / ('clock-' + boundary + '.json')).read_text())
        if json.loads((prefix / ('clock-' + boundary + '-exit.json')).read_text())['exit_code'] != 0:
            raise ValueError('preserved suffix clock observation failed')
        for host in ('bbr-a', 'bbr-b', 'bbr-gateway'):
            facts = data['hosts'][host]
            samples = facts['samples']
            lower = max(s['remote'] - s['controller_after'] for s in samples)
            upper = min(s['remote'] - s['controller_before'] for s in samples)
            if (facts['consistent'] is not True or lower > upper
                    or lower != facts['intersection_lower_ns'] or upper != facts['intersection_upper_ns']):
                raise ValueError('preserved suffix clock intersection failed')
            if json.loads((prefix / (host + '-chrony-' + boundary + '-exit.json')).read_text())['exit_code'] != 0:
                raise ValueError('preserved suffix clock source failed')
    rows = [json.loads(line) for line in (SOURCE / 'case-inventory.jsonl').read_text().splitlines()]
    for index in range(12, 36):
        directory = prefix / ('case-' + str(index))
        actual = {str(path.relative_to(prefix)) for path in directory.iterdir() if path.is_file()}
        pinned = {name for name in manifest['files'] if name.startswith('case-' + str(index) + '/')}
        if actual != pinned:
            raise ValueError('preserved suffix file inventory changed')
        d = json.loads((directory / 'commands.json').read_text())
        verdict = json.loads((directory / 'verdict.json').read_text())
        labels = ['native-path' if rows[index]['scenario'] == 'S1' else 'router', 'focal-receive', 'focal-send']
        if (d['case'] != rows[index] or [c['label'] for c in d['commands']] != labels
                or verdict['case_index'] != index or verdict['valid'] is not True):
            raise ValueError('preserved suffix case identity or verdict changed')
        roles = {label: (json.loads((directory / (label + '-exit.json')).read_text())['exit_code'],
                         json.loads((directory / (label + '.stdout')).read_text())) for label in labels}
        if validate(d, roles):
            raise ValueError('preserved suffix raw role replay failed')
    return authorization


def preflight(lease_id):
    if ROOT is None or not ROOT.is_dir() or ROOT.is_symlink(): raise ValueError('fresh directory required')
    if any((ROOT / f).exists() for f in ['dispatch-started.json', 'lease-result.json']) or list(ROOT.glob('case-*')):
        raise ValueError('attempt already used')
    plan = json.loads((SOURCE / 'lease-plan-candidate.json').read_text())
    leases = plan['leases']
    lease = copy.deepcopy(next(x for x in leases if x['id'] == lease_id))
    if lease_id not in ('Q2-01', 'Q2-02', 'Q2-03'): raise ValueError('only original non-competitor Linux leases supported')
    request = json.loads((ROOT / 'request.json').read_text())
    if request['platform'] != 'linux' or request['competitors'] is not False or request['gateway_cores'] != 4:
        raise ValueError('wrong topology')
    if lease_id == 'Q2-01':
        check_prefix()
        lease['case_indices'] = list(range(36, 108))
    expected_minutes = 327 if lease_id == 'Q2-01' else lease['lifetime_minutes']
    if request['lifetime_minutes'] != expected_minutes: raise ValueError('unrecorded lease envelope change')
    ledger = json.loads((ROOT / 'ledger.json').read_text())
    used = ledger['baseline_experiment_hours'] + sum(x['hours'] for x in ledger['reservations'])
    cost = ledger['baseline_reserved_usd'] + sum(x['usd'] for x in ledger['reservations'])
    if abs(request['experiment_hours_used'] - used) > 1e-8 or abs(request['spent_usd'] - cost) > 1e-8:
        raise ValueError('request omits or duplicates historical charges')
    if request['preparation_hours_used'] < ledger['preparation_budget_guard_hours'] or request['preparation_hours_used'] >= 72:
        raise ValueError('preparation ledger incomplete/exhausted')
    if ledger.get('experiment_ceiling_hours') != 192: raise ValueError('192-hour authorization not carried')
    if used + request['lifetime_minutes'] / 60 > 192 or cost + request['hourly_usd'] * request['lifetime_minutes'] / 60 + request['reserve_usd'] > 150:
        raise ValueError('budget exhausted')
    if ledger['forecast_experiment_hours'] > 192 or ledger['forecast_reserved_usd'] > 150: raise ValueError('forecast exceeds caps')
    stage = json.loads((ROOT / 'stage-manifest.json').read_text())
    for filename, digest in stage['local_files'].items():
        if hashlib.sha256((ROOT / filename).read_bytes()).hexdigest() != digest: raise ValueError('stage hash changed: ' + filename)
    for filename, digest in stage['source_files'].items():
        if hashlib.sha256(Path(filename).read_bytes()).hexdigest() != digest: raise ValueError('source hash changed: ' + filename)
    prior = json.loads((RECORDS / request['project_id'] / 'launch.json').read_text())
    if prior['status'] != 'destroyed': raise ValueError('another planned/live launch exists')
    return lease, request, ledger, stage


def clock(which):
    local(['python3', str(ROOT / 'clock-observe.py'), 'linux', str(ROOT / ('clock-' + which + '.json'))], 'clock-' + which, 90)
    data = json.loads((ROOT / ('clock-' + which + '.json')).read_text())
    for host in ['bbr-a', 'bbr-b', 'bbr-gateway']:
        facts = data['hosts'][host]
        if facts['consistent'] is not True or not facts['samples'] or facts['intersection_lower_ns'] > facts['intersection_upper_ns']:
            raise ValueError('inconsistent native clock: ' + host)
        require(host, "chronyc tracking && chronyc tracking | grep -Eq 'Leap status[[:space:]]*:[[:space:]]*Normal'", ROOT, host + '-chrony-' + which, 10)


def prepare(stage):
    started = time.time_ns()
    # Readiness polling is bounded startup observation, never a comparative retry.
    for host in ['bbr-a', 'bbr-b', 'bbr-gateway']:
        ready = False
        for n in range(8):
            code, _ = run(host, 'test "$(nproc)" = 4 && command -v chronyc && command -v python3 && sudo -n true', ROOT, host + '-startup-' + str(n), 30)
            if code == 0:
                ready = True
                break
            time.sleep(3)
        if not ready: raise RuntimeError('native startup not ready: ' + host)
        require(host, 'mkdir ' + shlex.quote(STAGE) + ' && uname -a && lscpu && cat /etc/os-release && ip -details link && ip route && vmstat 1 3 && sysctl net.ipv4.tcp_available_congestion_control net.ipv4.tcp_ecn net.ipv4.tcp_timestamps', ROOT, host + '-facts', 35)
        archive = 'gateway.tar.gz' if host == 'bbr-gateway' else 'endpoints.tar.gz'
        local(['gcloud', 'compute', 'scp', '--project=bbr-q635-20260925', '--zone=us-east1-b', '--tunnel-through-iap', '--quiet', '--scp-flag=-O', '--scp-flag=-oControlMaster=no', '--scp-flag=-oControlPath=none', str(ROOT / archive), host + ':' + STAGE + '/'], host + '-stage', 90)
        files = stage['gateway_files'] if host == 'bbr-gateway' else stage['endpoint_files']
        checks = ''.join(digest + '  ' + name + '\n' for name, digest in files.items())
        require(host, 'cd ' + shlex.quote(STAGE) + ' && tar -xzf ' + archive + ' && chmod 700 q1* && printf %s ' + shlex.quote(checks) + ' | sha256sum -c -', ROOT, host + '-hashes')
        require(host, 'lscpu -p=CPU,CORE,SOCKET | python3 -c ' + shlex.quote('import sys; x=[l.strip().split(",") for l in sys.stdin if not l.startswith("#")]; assert len(x)==4 and len({tuple(r[1:]) for r in x})==4'), ROOT, host + '-four-cores')
        require(host, "chronyc tracking && chronyc tracking | grep -Eq 'Leap status[[:space:]]*:[[:space:]]*Normal'", ROOT, host + '-clock-source')
    require('bbr-gateway', 'set -e; for i in ens4 ens5; do sudo -n ethtool -K "$i" gro off lro off gso off tso off rx-gro-hw off; sudo -n ethtool -k "$i"; done; ip -j addr; ip -j route; ping -c 1 -W 2 10.63.1.1 >/dev/null || test "$?" = 1; ping -c 1 -W 2 10.63.2.1 >/dev/null || test "$?" = 1; ip -j neigh', ROOT, 'gateway-native-path', 35)
    # GCE virtual gateways do not reply to ICMP. Exit1 only warms ARP; other
    # ping errors still fail, and exact MAC plus real end-to-end gates follow.
    # Both GCE virtual-router MACs are pinned by the frozen command generator.
    require('bbr-gateway', 'ip -j neigh | python3 -c ' + shlex.quote('import json,sys; d={x["dst"]:x.get("lladdr") for x in json.load(sys.stdin)}; assert d.get("10.63.1.1")=="42:01:0a:3f:01:01" and d.get("10.63.2.1")=="42:01:0a:3f:02:01"'), ROOT, 'gateway-peer-identities')
    require('bbr-gateway', 'sudo -n sysctl -w net.ipv4.ip_forward=0', ROOT, 'path-disable')
    require('bbr-a', 'if ping -c 2 -W 1 10.63.2.10; then exit 1; else exit 0; fi', ROOT, 'path-negative', 10)
    require('bbr-gateway', 'sudo -n sysctl -w net.ipv4.ip_forward=1', ROOT, 'path-enable')
    require('bbr-a', 'ping -c 3 -W 2 -M do -s 1432 10.63.2.10', ROOT, 'path-forward', 10)
    require('bbr-b', 'ping -c 3 -W 2 -M do -s 1432 10.63.1.10', ROOT, 'path-reverse', 10)
    require('bbr-gateway', 'sudo -n sysctl -w net.ipv4.ip_forward=0', ROOT, 'path-restore')
    clock('before')
    save(ROOT / 'native-preflight.json', dict(started_unix_ns=started, ended_unix_ns=time.time_ns(), passed=True))


def expand(index, start):
    d = json.loads(subprocess.check_output(['python3', str(SOURCE / 'case-commands.py'), str(index), '--start-unix-ns', str(start)], text=True))
    if d['case']['platform'] != 'linux/amd64' or len(d['commands']) != 3:
        raise ValueError('unsupported native case domain')
    for c in d['commands']:
        c['command'] = c['command'].replace('/tmp/q1-campaign', STAGE).replace('./q1fixture ', STAGE + '/q1fixture ').replace('sudo ', 'sudo -n ')
        if c['readiness_command']: c['readiness_command'] = c['readiness_command'].replace('/tmp/q1-campaign', STAGE)
    return d


def one_case(index, expiry):
    began = time.time_ns()
    directory = ROOT / ('case-' + str(index))
    directory.mkdir(mode=0o700)
    d = expand(index, time.time_ns() + 25_000_000_000)
    if d['gateway_end_unix_ns'] / 1e9 + CLEANUP_SECONDS + 30 >= expiry:
        raise ValueError('remaining immutable expiry cannot fit case/export/cleanup')
    save(directory / 'commands.json', d)
    pending, outcomes, failure = [], {}, None
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        try:
            for c in d['commands']:
                label, host = c['label'], c['host']
                if label == 'native-path':
                    outcomes[label] = run(host, c['command'], directory, label)
                    if outcomes[label][0]: raise RuntimeError('native forwarding setup failed')
                    continue
                bound = max(1, d['gateway_end_unix_ns'] / 1e9 - time.time() + 30)
                pending.append((label, pool.submit(run, host, c['command'], directory, label, bound)))
                if label == 'router':
                    require(host, 'for i in $(seq 1 30); do pgrep -f "^./q1router " >/dev/null && test "$(cat /proc/sys/net/ipv4/ip_forward)" = 0 && exit 0; sleep 0.2; done; exit 1', directory, 'router-ready', 12)
                if c['readiness_command']:
                    ready = False
                    for n in range(20):
                        code, _ = run(host, c['readiness_command'], directory, label + '-ready-' + str(n), 5)
                        if code == 0:
                            ready = True
                            break
                        if time.time_ns() >= d['start_unix_ns'] - 1_000_000_000: break
                        time.sleep(0.2)
                    if not ready: raise RuntimeError('receiver readiness deadline')
        except Exception as exc: failure = repr(exc)
        finally:
            for label, future in pending:
                try: outcomes[label] = future.result()
                except Exception as exc:
                    outcomes[label] = (125, None)
                    failure = (failure or '') + '; ' + repr(exc)
    errors = [failure] if failure else []
    if len(outcomes) != len(d['commands']): errors.append('not all roles launched')
    else: errors.extend(validate(d, outcomes))
    elapsed = (time.time_ns() - began) / 1e9
    nominal = (d['gateway_end_unix_ns'] - d['start_unix_ns']) / 1e9 + 25
    save(directory / 'orchestration.json', dict(started_unix_ns=began, ended_unix_ns=time.time_ns(), elapsed_seconds=elapsed, nominal_case_envelope_seconds=nominal, additional_serialized_seconds=max(0, elapsed - nominal), classification={'clock_path_native_resource_preflight':'per-lease serialized, recorded in native-preflight.json', 'role_readiness':'per-case overlapped with25s future start', 'role_exports':'per-case collected within role wait; excess beyond20s tail is serialized', 'system_cpu_monitor':'per-lease continuous, overlapped with cases'}))
    save(directory / 'verdict.json', dict(case_index=index, id=d['case']['id'], valid=not errors, errors=errors, ended_at=datetime.now(timezone.utc).isoformat()))
    print(json.dumps(dict(case_index=index, valid=not errors, errors=errors)), flush=True)
    if errors: raise RuntimeError('hard case gate failed: ' + '; '.join(errors))
    return max(0, elapsed - nominal)


def destroy_owned(request):
    receipt = RECORDS / request['project_id'] / 'launch.json'
    if not receipt.exists(): return
    record = json.loads(receipt.read_text())
    if record['status'] == 'destroyed': return
    if record['request'] != request:
        raise RuntimeError('live launch identity differs; refusing unrelated teardown')
    local(['python3', 'scripts/cloud/bbr-campaign/campaign.py', 'destroy', '--config', str(ROOT / 'request.json')], 'destroy', 420)


def finish(request, status, error, monitor_processes, clock_started):
    cleanup_errors = []
    # Destroy removes every native process too. Clock/export failure cannot skip it.
    if clock_started:
        try: clock('after')
        except Exception as exc: cleanup_errors.append('clock-after: ' + repr(exc))
    try: destroy_owned(request)
    except Exception as exc: cleanup_errors.append('destroy: ' + repr(exc))
    for p, out, err in monitor_processes:
        try:
            p.terminate()
            try: p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                p.kill(); p.wait(timeout=5)
        except Exception as exc: cleanup_errors.append('monitor SSH close: ' + repr(exc))
        finally: out.close(); err.close()
    for resource in ['instances', 'disks']:
        try:
            p = subprocess.run(['gcloud', 'compute', resource, 'list', '--project=' + request['project_id'], '--format=json'], capture_output=True, text=True, timeout=30)
            with (ROOT / ('final-' + resource + '.stdout')).open('x') as f: f.write(p.stdout)
            with (ROOT / ('final-' + resource + '.stderr')).open('x') as f: f.write(p.stderr)
            if p.returncode or json.loads(p.stdout): raise RuntimeError(resource + ' inventory not empty/verified')
        except Exception as exc: cleanup_errors.append('independent cleanup: ' + repr(exc))
    result = dict(status='completed' if status == 'completed' and not cleanup_errors else 'failed', error=error, cleanup_errors=cleanup_errors, ended_at=datetime.now(timezone.utc).isoformat())
    save(ROOT / 'lease-result.json', result)
    return result


def main(lease_id):
    os.umask(0o077)
    lease, request, ledger, stage = preflight(lease_id)
    save(ROOT / 'dispatch-started.json', dict(started_at=datetime.now(timezone.utc).isoformat(), runner_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), lease=lease_id, case_indices=lease['case_indices']))
    status, error, monitors, clock_started = 'failed', None, [], False
    try:
        save(ROOT / 'ledger-before.json', ledger)
        ledger['reservations'].append(dict(id=ROOT.name, lease_family=lease_id, hours=request['lifetime_minutes'] / 60, usd=request['hourly_usd'] * request['lifetime_minutes'] / 60 + request['reserve_usd'], status='full reservation retained before apply, including failed/unused time'))
        save(ROOT / 'ledger-reserved.json', ledger)
        local(['python3', 'scripts/cloud/bbr-campaign/campaign.py', 'apply', '--config', str(ROOT / 'request.json')], 'apply', 600)
        launch = json.loads((RECORDS / request['project_id'] / 'launch.json').read_text())
        save(ROOT / 'launch-receipt.json', launch)
        expiry = datetime.fromisoformat(launch['expires_at']).timestamp()
        clock_started = True
        prepare(stage)
        for host in ['bbr-a', 'bbr-b', 'bbr-gateway']:
            out = (ROOT / (host + '-cpu-during.txt')).open('x'); err = (ROOT / (host + '-cpu-during.stderr')).open('x')
            p = subprocess.Popen(SSH + [host, '--command=vmstat 5 ' + str(request['lifetime_minutes'] * 12)], stdout=out, stderr=err)
            monitors.append((p, out, err))
        rows = [json.loads(line) for line in (SOURCE / 'case-inventory.jsonl').read_text().splitlines()]
        extra = 0
        for n, index in enumerate(lease['case_indices']):
            remaining = lease['case_indices'][n:]
            forecast = sum((rows[i]['fixture']['warmup_ms'] + rows[i]['fixture']['measure_ms']) / 1000 + 45 + extra for i in remaining)
            if time.time() + forecast + CLEANUP_SECONDS + 30 >= expiry:
                raise RuntimeError('remaining partition exceeds immutable lease; stop before next case')
            extra = max(extra, one_case(index, expiry))
        status = 'completed'
    except BaseException as exc:
        error = repr(exc)
        print(error, flush=True)
    finally:
        result = finish(request, status, error, monitors, clock_started)
    return 0 if result['status'] == 'completed' else 1


def interrupted(signum, frame):
    raise KeyboardInterrupt('interrupted by signal ' + str(signum))


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attempt', required=True)
    parser.add_argument('--lease', required=True, choices=['Q2-01', 'Q2-02', 'Q2-03'])
    parser.add_argument('--execute', action='store_true')
    args = parser.parse_args()
    configure(args.attempt)
    if args.execute: raise SystemExit(main(args.lease))
    preflight(args.lease)
    print('Local prepared-attempt checks passed; no resource or comparative case launched.')
