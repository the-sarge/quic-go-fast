#!/usr/bin/env python3
"""Prepared corrected 30-minute diagnostic; explicit fresh retry authorization required."""
import argparse
import concurrent.futures
import copy
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import shlex
import signal
import time

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('frozen_v5', HERE / 'run-linux-v5.py')
cloud = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cloud)
WINDOW_SECONDS = 260


def check_time(expiry, remaining):
    if time.time() + remaining * WINDOW_SECONDS + cloud.CLEANUP_SECONDS + 30 >= expiry:
        raise ValueError('remaining diagnostic windows/export/cleanup exceed immutable expiry')


def diagnostic_errors(commands, outcomes):
    # Keep raw exit/gate observations. Only timing rejection is admissible
    # diagnostic data; never promote it to a valid campaign case.
    errors = []
    for c in commands['commands']:
        label = c['label']
        outcome = outcomes.get(label)
        if outcome is None:
            errors.append('missing role ' + label)
            continue
        code, data = outcome
        if label == 'router':
            if code not in (0, 1) or not isinstance(data, dict):
                errors.append('router execution/receipt failed')
                continue
            replay = cloud.validate(dict(commands, commands=[c]), {label: (0, data)})
            errors.extend(e for e in replay if e != 'gateway five-millisecond timing gate')
            timing_failed = any(e == 'gateway five-millisecond timing gate' for e in replay)
            if (code == 1) != timing_failed:
                errors.append('router exit does not match unchanged timing observation')
        else:
            errors.extend(cloud.validate(dict(commands, commands=[c]), {label: outcome}))
    return errors


def preflight(attempt):
    if attempt != 'q2-linux-20260930-router-diagnostic2':
        raise ValueError('only the single operator-authorized diagnostic identity is admitted')
    cloud.configure(attempt)
    root = cloud.ROOT
    if (root / 'dispatch-started.json').exists():
        raise ValueError('diagnostic already dispatched; no duplicate or retry')
    request = json.loads((root / 'request.json').read_text())
    ledger = json.loads((root / 'ledger.json').read_text())
    auth = json.loads((root / 'operator-authorization.json').read_text())
    if (auth.get('attempt') != attempt or auth.get('diagnostic_retry_authorized') is not True
            or auth.get('failed_attempt') != 'q2-linux-20260930-router-diagnostic1'):
        raise ValueError('fresh diagnostic retry requires explicit operator authorization')
    if (request['lifetime_minutes'] != 30 or request['platform'] != 'linux'
            or request['competitors'] is not False or request['gateway_cores'] != 4
            or auth['cloud_cap_usd'] != 200 or ledger['cloud_ceiling_usd'] != 200):
        raise ValueError('diagnostic scope or operator budget changed')
    used = ledger['baseline_experiment_hours'] + sum(x['hours'] for x in ledger['reservations'])
    cost = ledger['baseline_reserved_usd'] + sum(x['usd'] for x in ledger['reservations'])
    if abs(request['experiment_hours_used'] - used) > 1e-8 or abs(request['spent_usd'] - cost) > 1e-8:
        raise ValueError('incomplete historical ledger')
    if (request['preparation_hours_used'] != ledger['preparation_budget_guard_hours']
            or request['preparation_hours_used'] >= 72 or used + .5 > 192):
        raise ValueError('preparation/experiment allowance exhausted')
    if ledger['forecast_experiment_hours'] > 192 or ledger['forecast_reserved_usd'] > 200:
        raise ValueError('full campaign forecast exceeds operator-authorized caps')
    if cost + request['hourly_usd'] * .5 + request['reserve_usd'] > 150:
        raise ValueError('this dispatch exceeds retained stricter controller spend guard')
    stage = json.loads((root / 'stage-manifest.json').read_text())
    for relative, expected in stage['local_files'].items():
        if hashlib.sha256((root / relative).read_bytes()).hexdigest() != expected:
            raise ValueError('local stage identity changed: ' + relative)
    for filename, expected in stage['source_files'].items():
        if hashlib.sha256(Path(filename).read_bytes()).hexdigest() != expected:
            raise ValueError('source identity changed: ' + filename)
    prior = json.loads((cloud.RECORDS / request['project_id'] / 'launch.json').read_text())
    if prior['status'] != 'destroyed':
        raise ValueError('another launch exists')
    facts = json.loads((root / 'fresh-admission-facts.json').read_text())
    age = time.time() - datetime.fromisoformat(facts['recorded_at']).timestamp()
    if not 0 <= age <= 1800 or facts['inventories_empty'] is not True or facts['human_adc_no_credential_overrides'] is not True:
        raise ValueError('fresh admission facts incomplete/stale')
    return request, ledger, stage


def stage_helper():
    # Frozen V5 records primary staging under <host>-stage. Give this second
    # transfer its own exclusive receipt; keep the same SSH stream and deadline.
    host, archive = 'bbr-gateway', 'diagnostic-helper.tar.gz'
    destination = shlex.quote(cloud.STAGE + '/' + archive)
    cloud.local(cloud.SSH + [host, '--command=set -C; cat > ' + destination],
                host + '-diagnostic-helper-stage', 90, stdin_path=cloud.ROOT / archive)


def window(kind, expiry, remaining):
    check_time(expiry, remaining)
    directory = cloud.ROOT / ('diagnostic-' + kind)
    directory.mkdir(mode=0o700)
    parent_stage = cloud.STAGE
    child_stage = parent_stage + '/' + kind
    for host in ('bbr-a', 'bbr-b', 'bbr-gateway'):
        files = ['q1router'] if host == 'bbr-gateway' else ['q1fixture', 'campaign.pem', 'campaign-key.pem']
        command = 'mkdir ' + shlex.quote(child_stage) + ' && ' + ' && '.join(
            'ln ' + shlex.quote(parent_stage + '/' + f) + ' ' + shlex.quote(child_stage + '/' + f) for f in files)
        cloud.require(host, command, directory, host + '-window-stage', 10)
    cloud.STAGE = child_stage
    commands = cloud.expand(39, time.time_ns() + 25_000_000_000)
    if kind == 'traced':
        commands['commands'][0]['command'] = commands['commands'][0]['command'].replace(
            'env GOGC=400 GOMEMLIMIT=4GiB ', 'env GOGC=400 GOMEMLIMIT=4GiB GODEBUG=gctrace=1 ')
    cloud.save(directory / 'commands.json', dict(commands, diagnostic_only=True,
        qualification_eligible=False, instrumentation=(kind == 'traced')))
    outcomes, pending, failure, trace_future = {}, [], None, None
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        try:
            if kind == 'traced':
                command = 'sudo -n python3 ' + shlex.quote(parent_stage + '/collect-kernel-timers.py')
                command += ' --attempt ' + cloud.ROOT.name + ' --stage ' + shlex.quote(parent_stage)
                trace_future = pool.submit(cloud.run, 'bbr-gateway', command, directory, 'kernel-trace', 285)
                cloud.require('bbr-gateway', 'for i in $(seq 1 30); do test -s ' +
                    shlex.quote(parent_stage + '/trace-ready.json') + ' && exit 0; sleep .1; done; exit 1',
                    directory, 'trace-ready', 10)
            for c in commands['commands']:
                label, host = c['label'], c['host']
                pending.append((label, pool.submit(cloud.run, host, c['command'], directory, label, WINDOW_SECONDS)))
                if label == 'router':
                    cloud.require(host, 'for i in $(seq 1 30); do pgrep -f "^./q1router " >/dev/null && test "$(cat /proc/sys/net/ipv4/ip_forward)" = 0 && exit 0; sleep .2; done; exit 1', directory, 'router-ready', 12)
                if c['readiness_command']:
                    ready = False
                    for n in range(20):
                        code, _ = cloud.run(host, c['readiness_command'], directory, label + '-ready-' + str(n), 5)
                        if code == 0:
                            ready = True
                            break
                        if time.time_ns() >= commands['start_unix_ns'] - 1_000_000_000:
                            break
                        time.sleep(.2)
                    if not ready:
                        raise ValueError('native receiver readiness failed')
        except BaseException as exc:
            failure = repr(exc)
        finally:
            for label, future in pending:
                try:
                    outcomes[label] = future.result()
                except BaseException as exc:
                    outcomes[label] = (125, None)
                    failure = repr(exc)
            cloud.STAGE = parent_stage
            if trace_future is not None:
                try:
                    cloud.require('bbr-gateway', 'touch ' + shlex.quote(parent_stage + '/trace-stop'), directory, 'trace-stop', 10)
                except BaseException as exc:
                    failure = repr(exc)
                code, _ = trace_future.result()
                if code:
                    failure = 'kernel tracing incomplete/lost: exit' + str(code)
    errors = ([failure] if failure else []) + diagnostic_errors(commands, outcomes)
    calibration_errors = cloud.validate(commands, outcomes)
    cloud.save(directory / 'diagnostic-result.json', dict(diagnostic_only=True,
        qualification_eligible=False, original_case_template=39, hard_errors=errors,
        unchanged_calibration_errors=calibration_errors, ended_at=datetime.now(timezone.utc).isoformat()))
    print(json.dumps(dict(window=kind, hard_errors=errors, unchanged_calibration_errors=calibration_errors)), flush=True)
    if errors:
        raise RuntimeError('diagnostic hard gate: ' + '; '.join(errors))


def main(attempt):
    os.umask(0o077)
    request, ledger, stage = preflight(attempt)
    cloud.save(cloud.ROOT / 'dispatch-started.json', dict(started_at=datetime.now(timezone.utc).isoformat(),
        diagnostic_only=True, windows=['untraced', 'traced'], original_case_template=39,
        automatic_retry=False, runner_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest()))
    status, error, clock_started = 'failed', None, False
    try:
        cloud.save(cloud.ROOT / 'ledger-before.json', ledger)
        ledger['reservations'].append(dict(id=attempt, lease_family='Q2-router-diagnostic',
            hours=.5, usd=request['hourly_usd'] * .5 + request['reserve_usd'],
            status='full diagnostic reservation retained before apply, including failed/unused time'))
        cloud.save(cloud.ROOT / 'ledger-reserved.json', ledger)
        signal.alarm(30 * 60 - cloud.CLEANUP_SECONDS)
        cloud.local(['python3', 'scripts/cloud/bbr-campaign/campaign.py', 'apply', '--config',
                     str(cloud.ROOT / 'request.json')], 'apply', 300)
        launch = json.loads((cloud.RECORDS / request['project_id'] / 'launch.json').read_text())
        cloud.save(cloud.ROOT / 'launch-receipt.json', launch)
        expiry = datetime.fromisoformat(launch['expires_at']).timestamp()
        signal.alarm(max(1, math.floor(expiry - cloud.CLEANUP_SECONDS - time.time())))
        clock_started = True
        cloud.prepare(stage)
        stage_helper()
        cloud.require('bbr-gateway', 'cd ' + shlex.quote(cloud.STAGE) + ' && tar -xzf diagnostic-helper.tar.gz && printf %s ' +
            shlex.quote(stage['diagnostic_helper_sha256'] + '  collect-kernel-timers.py\n') + ' | sha256sum -c -', cloud.ROOT, 'helper-identity', 10)
        facts = 'uname -a; cat /boot/config-$(uname -r); cat /sys/devices/system/clocksource/clocksource0/current_clocksource; cat /sys/devices/system/clocksource/clocksource0/available_clocksource'
        cloud.require('bbr-gateway', 'set -e; ' + facts, cloud.ROOT, 'kernel-trace-facts', 10)
        cloud.require('bbr-gateway', 'sudo -n python3 ' + shlex.quote(cloud.STAGE + '/collect-kernel-timers.py') +
            ' --attempt ' + attempt + ' --stage ' + shlex.quote(cloud.STAGE) + ' --probe', cloud.ROOT, 'trace-native-probe', 20)
        cloud.save(cloud.ROOT / 'diagnostic-native-preflight.json', dict(passed=True,
            qualification_eligible=False, ended_at=datetime.now(timezone.utc).isoformat()))
        window('untraced', expiry, 2)
        window('traced', expiry, 1)
        status = 'completed'
    except BaseException as exc:
        error = repr(exc)
        print(error, flush=True)
    finally:
        signal.alarm(0)
        result = cloud.finish(request, status, error, [], clock_started)
    return 0 if result['status'] == 'completed' else 1


if __name__ == '__main__':
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGALRM):
        signal.signal(sig, cloud.interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attempt', required=True)
    parser.add_argument('--execute', action='store_true')
    args = parser.parse_args()
    if args.execute:
        raise SystemExit(main(args.attempt))
    preflight(args.attempt)
    print('Prepared finite diagnostic; no resource or native traffic launched.')
