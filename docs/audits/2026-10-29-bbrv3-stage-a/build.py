#!/usr/bin/env python3
"""Build the finite Stage A aid; never dispatch or allocate resources."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import io
import time
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[2]
PINS = {'candidate': '23c83b9896980511286ca8b81cd632f7ec56afed',
        'reference': '8095c72244755f0c1e24f4158253bf09cfbe6e17'}


def run(args, cwd=ROOT, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()


def digest():
    files = sorted(p for folder in ('fixture', 'model', 'probe', 'router')
                   for p in (ROOT / folder).glob('*.go'))
    files += [ROOT / 'go.mod', ROOT / 'go.sum']
    hashes = {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
    return hashlib.sha256(json.dumps(hashes, sort_keys=True, separators=(',', ':')).encode()).hexdigest(), hashes


def main():
    if not run(['go', 'version']).startswith('go version go1.27.3 '):
        raise SystemExit('Go 1.27.3 required')
    build = ROOT / 'build'
    build.mkdir(exist_ok=True)
    overlay, hashes = digest()
    receipt = {'schema': 'stage-a-build-v1', 'started_at': datetime.now(timezone.utc).isoformat(),
               'overlay_sha256': overlay, 'input_sha256': hashes, 'build_host': run(['go', 'env', 'GOHOSTOS', 'GOHOSTARCH']),
               'toolchain': run(['go', 'version']), 'execution_evidence': False, 'builds': {}}
    started = time.monotonic()
    for name, revision in PINS.items():
        source = build / 'sources' / name
        # Always extract the pinned Git object into an empty non-checkout directory.
        # Never use the containing checkout as the linked library.
        if source.exists():
            raise SystemExit(f'{source} exists; retain prior build and use a fresh copy for rebuilding')
        source.mkdir(parents=True)
        archive = subprocess.check_output(['git', 'archive', revision], cwd=REPO)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter='data')
        modfile = build / f'{name}.mod'
        modfile.write_text((ROOT / 'go.mod').read_text().replace('./build/sources/candidate', str(source)))
        modfile.with_suffix('.sum').write_bytes((ROOT / 'go.sum').read_bytes())
        env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOOS='linux', GOARCH='amd64', CGO_ENABLED='0')
        graph = run(['go', 'list', '-mod=readonly', f'-modfile={modfile}', '-m', '-json', 'all'], env=env)
        (build / f'{name}-modules.json').write_text(graph+'\n')
        output = build / f'{name}-fixture'
        command = ['go', 'build', '-mod=readonly', f'-modfile={modfile}', '-trimpath', '-buildvcs=false',
                   '-ldflags', f'-X main.sourceRevision={revision} -X main.overlayDigest={overlay}', '-o', str(output), './fixture']
        run(command, env=env)
        receipt['builds'][name] = {'library_revision': revision, 'command': command,
            'module_graph_sha256': hashlib.sha256((graph+'\n').encode()).hexdigest(),
            'binary_sha256': hashlib.sha256(output.read_bytes()).hexdigest(), 'go_version_m': run(['go', 'version', '-m', str(output)])}
    env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOOS='linux', GOARCH='amd64', CGO_ENABLED='0')
    run(['go','build','-mod=readonly','-trimpath','-buildvcs=false','-ldflags',f'-X main.sourceRevision={PINS["candidate"]}', '-o', str(build/'probe'), './probe'],env=env)
    receipt['probe_sha256'] = hashlib.sha256((build/'probe').read_bytes()).hexdigest()
    receipt['finished_at'] = datetime.now(timezone.utc).isoformat()
    receipt['elapsed_seconds'] = time.monotonic()-started
    (ROOT/'build-receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')


if __name__ == '__main__':
    main()
