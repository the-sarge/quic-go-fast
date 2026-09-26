#!/usr/bin/env python3
"""Freeze this campaign's finite lease partition; retire with Q2 evidence."""
import hashlib
import json
import math
from pathlib import Path

root = Path(__file__).resolve().parent
inventory = root / 'case-inventory.jsonl'
rows = [json.loads(line) for line in inventory.read_text().splitlines()]
# Preserve every frozen controller pair; partition whole pairs by native platform.
# Linux completion cases use spare time in the second competitor lease, avoiding
# another bootstrap/teardown reservation. Idle competitor VMs are still charged.
groups = {name: [] for name in ('linux-core', 'linux-competitors-and-completion', 'mac', 'windows')}
for index in range(0, len(rows), 2):
    pair = rows[index:index + 2]
    assert len(pair) == 2
    a, b = pair
    assert all(a[k] == b[k] for k in ('platform', 'group', 'scenario', 'pair', 'seed'))
    assert a['fixture']['workload'] == b['fixture']['workload']
    assert (a['order_in_pair'], b['order_in_pair']) == (1, 2)
    assert {a['fixture']['controller'], b['fixture']['controller']} == {'reno', 'bbrv3'}
    platform = a['platform']
    if platform == 'linux/amd64':
        group = 'linux-competitors-and-completion' if a['group'] == 'completion' or a['scenario'] in ('L4', 'L6', 'L7', 'L8') else 'linux-core'
    else:
        group = {'darwin/arm64': 'mac', 'windows/amd64': 'windows'}[platform]
    groups[group].append([(i, rows[i]) for i in (index, index + 1)])

def seconds(row):
    # Future start25s plus gateway tail20s; completion measure max60s.
    return (row['fixture']['warmup_ms'] + row['fixture']['measure_ms']) // 1000 + 45

leases = []
for group, pairs in groups.items():
    current = []
    duration = 0
    for pair in pairs:
        pair_seconds = sum(seconds(row) for _, row in pair)
        if current and duration + pair_seconds + 900 > 480 * 60:
            leases.append((group, current, duration))
            current, duration = [], 0
        current.extend(pair)
        duration += pair_seconds
    if current:
        leases.append((group, current, duration))

result = {'status': 'frozen bounded lease partition; fresh native preflight required',
          'inventory_sha256': hashlib.sha256(inventory.read_bytes()).hexdigest(),
          'retained_reservations': {'experiment_hours': 24.718, 'usd': 30.02},
          'remaining_preparation_measurements': {'linux_hours': 0, 'linux_usd': 0, 'windows_hours': 0, 'windows_usd': 0, 'mac_hours': 0, 'mac_usd': 0},
          'setup_cleanup_seconds_per_lease': 900,
          'case_overhead_seconds': {'future_start': 25, 'gateway_tail': 20},
          'order_contract': 'Execute leases and case indices in this recorded order. Original controller order, seed and pairing remain unchanged; full pairs are partitioned by platform/topology. Linux completions reuse a competitor lease with idle extra endpoints.',
          'deadline_contract': 'Each reservation starts before apply/setup and includes export/cleanup. Never start a case unless its complete command envelope plus remaining cleanup allowance fits immutable expiry. Reserved unused time stays charged; overruns require available contingency.',
          'leases': []}
for n, (group, items, duration) in enumerate(leases, 1):
    cloud = group != 'mac'
    competitors = group == 'linux-competitors-and-completion'
    hourly = 0 if not cloud else 2.1 if competitors or group == 'windows' else 1.3
    minutes = math.ceil((duration + 900) / 60)
    result['leases'].append({'id': f'Q2-{n:02d}', 'platform': items[0][1]['platform'],
                             'competitors': competitors, 'gateway_cores': 4,
                             'case_indices': [index for index, _ in items],
                             'case_command_seconds': duration, 'lifetime_minutes': minutes,
                             'hourly_usd': hourly, 'export_storage_reserve_usd': 0.25 if cloud else 0,
                             'reserved_usd': round(minutes / 60 * hourly + (0.25 if cloud else 0), 6)})
indices = [i for lease in result['leases'] for i in lease['case_indices']]
assert len(indices) == len(rows) == 520 and sorted(indices) == list(range(520))
assert all(lease['lifetime_minutes'] <= 480 for lease in result['leases'])
retained = result['retained_reservations']
remaining = result['remaining_preparation_measurements']
result['forecast_hours'] = round(retained['experiment_hours'] + remaining['linux_hours'] + remaining['windows_hours'] + remaining['mac_hours'] + sum(lease['lifetime_minutes'] for lease in result['leases']) / 60, 6)
result['forecast_usd'] = round(retained['usd'] + remaining['linux_usd'] + remaining['windows_usd'] + remaining['mac_usd'] + sum(lease['reserved_usd'] for lease in result['leases']), 6)
result['remaining_hours'] = round(72 - result['forecast_hours'], 6)
result['remaining_usd'] = round(100 - result['forecast_usd'], 6)
assert result['remaining_hours'] >= 0 and result['remaining_usd'] >= 0
(root / 'lease-plan-candidate.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({k: v for k, v in result.items() if k not in ('leases',)}, indent=2))
print('leases:', [(x['id'], len(x['case_indices']), x['lifetime_minutes']) for x in result['leases']])
