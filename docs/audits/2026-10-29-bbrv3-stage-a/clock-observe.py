"""Finite Q1 management-channel clock observations; never adjusts a clock."""
import base64
import json
import pathlib
import select
import subprocess
import sys
import time

project, zone, output = sys.argv[1:]
platform = "linux"
hosts = ['m4mini-eth', 'mbp128', 'minimax'] if platform == 'mac' else ['bbr-a', 'bbr-b', 'bbr-gateway']
result = {'contract': 'For each request, remote timestamp R was sampled between controller C0 and C1. Offset remote-minus-controller is bounded by [R-C1,R-C0]. Intersect samples only within this short observation; record sample spread and do not assume the bound persists through a later lease. No clock adjustment. Durations use their original denominators; wall-clock RTTs require no clock step.',
          'controller_start_unix_ns': time.time_ns(), 'hosts': {}}
for host in hosts:
    windows = platform == 'windows' and host != 'bbr-gateway'
    if windows:
        code = "[Console]::WriteLine('READY'); while ($null -ne [Console]::ReadLine()) { [Console]::WriteLine(([DateTime]::UtcNow.Ticks - 621355968000000000L) * 100L) }"
        command = 'powershell.exe -NoProfile -NonInteractive -EncodedCommand ' + base64.b64encode(code.encode('utf-16le')).decode()
    else:
        command = "python3 -u -c 'import sys,time; print(\"READY\",flush=True); [(print(time.time_ns(),flush=True)) for line in sys.stdin]'"
    args = ['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', host, command] if platform == 'mac' else [
        'gcloud', 'compute', 'ssh', host, '--project=' + project, '--zone=' + zone,
        '--tunnel-through-iap', '--quiet', '--ssh-flag=-T', '--ssh-flag=-oConnectTimeout=15',
        '--ssh-flag=-oControlMaster=auto', '--ssh-flag=-oControlPath=/tmp/' + pathlib.Path(output).parent.name + '-%C',
        '--ssh-flag=-oControlPersist=30m', '--command=' + command]
    stderr = pathlib.Path(output).with_name(pathlib.Path(output).stem + '-' + host + '.stderr')
    with stderr.open('x') as err:
        process = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=err, bufsize=0)
        def line(bound):
            # Read bytes directly so select and a text buffer cannot disagree.
            data = bytearray()
            deadline = time.monotonic() + bound
            while not data.endswith(b'\n'):
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not select.select([process.stdout], [], [], remaining)[0]:
                    raise TimeoutError('clock response deadline: ' + host)
                byte = process.stdout.read(1)
                if not byte:
                    raise RuntimeError('clock channel closed: ' + host)
                data.extend(byte)
            return data.strip()
        try:
            if line(40) != b'READY':
                raise RuntimeError('clock channel did not become ready')
            samples = []
            for _ in range(10):
                before = time.time_ns()
                process.stdin.write(b'now\n')
                remote = int(line(3))
                after = time.time_ns()
                samples.append({'controller_before': before, 'remote': remote, 'controller_after': after,
                                'offset_lower_ns': remote - after, 'offset_upper_ns': remote - before})
                time.sleep(0.1)
            lower = max(x['offset_lower_ns'] for x in samples)
            upper = min(x['offset_upper_ns'] for x in samples)
            result['hosts'][host] = {'samples': samples, 'intersection_lower_ns': lower,
                                     'intersection_upper_ns': upper, 'consistent': lower <= upper}
        finally:
            process.stdin.close()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
result['controller_end_unix_ns'] = time.time_ns()
with pathlib.Path(output).open('x') as receipt:
    receipt.write(json.dumps(result, indent=2) + '\n')
print(json.dumps({host: {k: v for k, v in facts.items() if k != 'samples'} for host, facts in result['hosts'].items()}, indent=2))
if any(not facts['consistent'] for facts in result['hosts'].values()):
    raise SystemExit('Clock changed or observations do not support one offset interval')
