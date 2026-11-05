"""Copy the relevant captured bytes into replay bundles, preserving source roots."""
import gzip
import hashlib
import json
from pathlib import Path
import sys
import tarfile

source = Path(sys.argv[1])
destination = Path(__file__).resolve().parent/'observations'


def keep(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        assert path.read_bytes() == data, path
    else:
        with path.open('xb') as f:
            f.write(data)


for result in sorted(source.glob('run-q1210-*/controller/*/result.json')):
    arm = result.parent.name
    root = result.parents[2]
    state = json.loads((root/'ops/execution-state.json').read_text())
    if state['status'] != 'released':
        continue
    out = destination/arm
    for name in ['result.json', 'send-stderr.log', 'receive-stderr.log']:
        keep(out/'controller'/arm/name, (result.parent/name).read_bytes())
    keep(out/'controller'/arm/'send-datagram-ledger.bin.gz', gzip.compress((result.parent/'send-datagram-ledger.bin').read_bytes(), mtime=0))
    for role in ['send', 'receive']:
        text = (result.parent/(role+'-stderr.log')).read_text()
        headers = [json.loads(x.split('[DEBUG-1210] ', 1)[1]) for x in text.splitlines() if x.startswith('[DEBUG-1210] {')]
        if not headers:
            continue
        assert len(headers) == 1
        h = headers[0]
        name = Path(h['diag1210']).name
        if role == 'send':
            with tarfile.open(root/'remote-worker-files.tar.gz') as archive:
                member = next(x for x in archive.getmembers() if Path(x.name).name == name)
                data = archive.extractfile(member).read()
            target = out/(name+'.gz')
        else:
            data = (root/'local'/name).read_bytes()
            target = out/'local'/(name+'.gz')
        assert len(data) == h['records']*128
        keep(target, gzip.compress(data, mtime=0))
    # Independent lease release and measured host load, not a qualification claim.
    names = ['execution-state.json', 'execution-errors.json', 'remote-custody.json', 'local-worker-absence.json']
    names += [p.name for p in (root/'ops').glob('*mac-cores.stdout')]
    names += [p.name for p in (root/'ops').glob('*linux-host.stdout')]
    for name in names:
        p = root/'ops'/name
        if p.exists():
            keep(out/'receipts'/name, p.read_bytes())
    print('packaged', arm, flush=True)

manifest = {str(p.relative_to(destination)): dict(bytes=p.stat().st_size, sha256=hashlib.sha256(p.read_bytes()).hexdigest()) for p in sorted(destination.rglob('*')) if p.is_file() and p.name != 'manifest.json'}
# The index grows when another completed arm is added; its referenced files do not change.
(destination/'manifest.json').write_text(json.dumps(manifest, indent=2)+'\n')
