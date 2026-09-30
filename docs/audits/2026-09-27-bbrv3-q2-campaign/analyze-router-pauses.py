#!/usr/bin/env python3
"""Read-only, finite replay of three stopped Q2 attempts; no launch capability."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
PERIOD_NS = 61_440_000_000  # Exploratory hypothesis, not a qualification gate.
ATTEMPTS = [
    ('q2-linux-20260929-lease01-retry1', 'failure-evidence-key-sha256.json'),
    ('q2-linux-20260929-lease01-continuation2', 'failure-evidence-hashes.json'),
    ('q2-linux-20260929-lease01-continuation4', 'failure-evidence-hashes.json'),
]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fit(passing):
    # Only passing, bidirectional >=2ms maxima within 1ms of each other train
    # the grid. Include all other rows in output; hold every failed row out.
    anchor = passing[0]['mean_peak_unix_ns']
    points = [(round((r['mean_peak_unix_ns'] - anchor) / PERIOD_NS),
               r['mean_peak_unix_ns'] - anchor) for r in passing]
    if len(points) == 1:
        return anchor, float(PERIOD_NS)
    mean_n = sum(n for n, _ in points) / len(points)
    mean_y = sum(y for _, y in points) / len(points)
    period = sum((n - mean_n) * (y - mean_y) for n, y in points) / sum(
        (n - mean_n) ** 2 for n, _ in points)
    offset = mean_y - period * mean_n
    # Preserve large Unix timestamp as an integer; use small deltas for fit.
    return anchor + round(offset), period


def analyze(base):
    source = HERE / 'run-linux-v5.py'
    if digest(source) != 'c8ebc2d0b925a1abc89dbe022af789d49f2b1e6a35e74e929a926e5fd6207c1f':
        raise ValueError('frozen validator source changed')
    spec = importlib.util.spec_from_file_location('frozen_q2_v5', source)
    driver = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(driver)
    result = {'period_hypothesis_ns': PERIOD_NS, 'attempts': [],
              'limitation': 'Post-hoc exploratory analysis of maxima, not full event traces or proof of cause.'}
    for name, manifest_name in ATTEMPTS:
        root = base / name
        manifest = json.loads((root / manifest_name).read_text())
        for relative, entry in manifest['files'].items():
            path = root / relative
            expected = entry['sha256'] if isinstance(entry, dict) else entry
            if digest(path) != expected:
                raise ValueError(f'frozen evidence changed: {name}/{relative}')
            if isinstance(entry, dict) and path.stat().st_size != entry['bytes']:
                raise ValueError(f'frozen size changed: {name}/{relative}')
        rows, receipts, input_hashes = [], [], {}
        for directory in sorted(root.glob('case-*'), key=lambda p: int(p.name[5:])):
            verdict = json.loads((directory / 'verdict.json').read_text())
            commands = json.loads((directory / 'commands.json').read_text())
            roles = {}
            for c in commands['commands']:
                label = c['label']
                exit_path = directory / (label + '-exit.json')
                stdout = directory / (label + '.stdout')
                code = json.loads(exit_path.read_text())['exit_code']
                try:
                    data = json.loads(stdout.read_text())
                except ValueError:
                    data = None
                roles[label] = (code, data)
                for path in (exit_path, stdout):
                    input_hashes[str(path.relative_to(root))] = digest(path)
            errors = driver.validate(commands, roles)
            if verdict['valid'] != (not errors) or verdict['errors'] != errors:
                raise ValueError(f'verdict replay changed: {name}/{directory.name}')
            for path in (directory / 'commands.json', directory / 'verdict.json'):
                input_hashes[str(path.relative_to(root))] = digest(path)
            receipts.append({'case': verdict['case_index'], 'valid': verdict['valid'], 'errors': errors})
            if 'router' not in roles:
                continue
            _, router = roles['router']
            observations = router['Observations']
            peaks = [o['MaxEgressAtNS'] for o in observations]
            row = {'case': verdict['case_index'], 'id': verdict['id'], 'valid': verdict['valid'],
                   'mean_peak_unix_ns': sum(peaks) // 2,
                   'peak_separation_ns': abs(peaks[1] - peaks[0]),
                   'directions': [{k: o[k] for k in ('Direction', 'MaxEgressLagNS', 'MaxEgressAtNS',
                       'MaxIngressLagNS', 'SocketDrops', 'NonCanonical', 'SendErrors', 'MissingTimestamps')}
                       for o in observations]}
            row['peak_after_measurement_start_ns'] = row['mean_peak_unix_ns'] - router['Config']['StartUnixNS']
            row['grid_candidate'] = row['peak_separation_ns'] < 1_000_000 and all(
                o['MaxEgressLagNS'] >= 2_000_000 for o in observations)
            rows.append(row)
        training = [r for r in rows if r['valid'] and r['grid_candidate']]
        anchor, period = fit(training)
        for row in rows:
            delta = row['mean_peak_unix_ns'] - anchor
            cycle = round(delta / period)
            row['nearest_grid_cycle'] = cycle
            row['grid_residual_ns'] = round(delta - cycle * period)
        result['attempts'].append({'id': name, 'original_manifest_sha256': digest(root / manifest_name),
            'original_manifest_files_verified': len(manifest['files']),
            'replayed_verdicts': receipts, 'read_input_sha256': input_hashes,
            'training_cases': [r['case'] for r in training], 'fitted_period_ns': period,
            'fit_basis': 'passing rows only; single training row uses the fixed hypothesis period',
            'rows': rows})
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--records', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    data = analyze(args.records)
    with args.output.open('x') as out:
        json.dump(data, out, indent=2)
        out.write('\n')
