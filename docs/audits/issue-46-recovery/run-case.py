"""Run one explicitly named diagnostic case and retain its receipt. No loops/retries."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
from datetime import datetime, timezone

case, test = sys.argv[1:]
root = Path(__file__).resolve().parent
receipt = root / 'raw' / f'{case}-command.json'
if receipt.exists():
    raise SystemExit(f'{case} already attempted; refusing to overwrite')
binary = Path('/tmp/quic-go-issue46-controlled.test')
env = os.environ.copy()
env.update(TIMESCALE_FACTOR='1', ISSUE46_OUTPUT=str(root / 'raw' / f'{case}.jsonl'))
command = [str(binary), '-test.run=' + test, '-test.count=1', '-test.v', '-test.timeout=30s', '-version=1']
meta = {
    'command': command, 'cwd': str(Path.cwd()), 'utc': datetime.now(timezone.utc).isoformat(),
    'environment': {k: env.get(k) for k in ['TIMESCALE_FACTOR','ISSUE46_OUTPUT','QUIC_GO_DISABLE_GSO','QUIC_GO_DISABLE_ECN','GODEBUG','GOMAXPROCS']},
    'toolchain': subprocess.check_output(['go','version'], text=True).strip(),
    'platform': subprocess.check_output(['uname','-a'], text=True).strip(),
    'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
    'source_hashes': {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in [Path('integrationtests/self/handshake_corruption_test.go'), Path('integrationtests/self/issue46_diagnostic_test.go')]},
}
receipt.write_text(json.dumps(meta, indent=2)+'\n')
with (root / 'LEDGER.md').open('a') as f:
    f.write(f'\n{case}: starting the case predeclared in PLAN.md; `{test}`. Receipt: `raw/{case}-command.json`.\n')
with (root/'raw'/f'{case}.log').open('w') as f:
    result = subprocess.run(command, env=env, stdout=f, stderr=subprocess.STDOUT)
with (root/'LEDGER.md').open('a') as f:
    f.write(f'\n{case}: exit {result.returncode}; `raw/{case}.log` and `raw/{case}.jsonl`.\n')
print((root/'raw'/f'{case}.log').read_text())
