"""Rebuild one retained diagnostic source overlay without launching traffic."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('worktree', type=Path)
parser.add_argument('--source', choices=['source-trace1', 'source-ablation'], default='source-ablation')
args = parser.parse_args()
root = args.worktree.resolve()
evidence = Path(__file__).resolve().parent
assert root.is_relative_to('/Volumes/worktrees/quic-go-fast')
for name, digest in json.loads((evidence/args.source/'manifest.json').read_text()).items():
    assert hashlib.sha256((root/name).read_bytes()).hexdigest() == digest, name
env = dict(os.environ, GOTOOLCHAIN='local', GOPROXY='off', GOFLAGS='', CGO_ENABLED='0', GOMAXPROCS='4')
version = subprocess.check_output(['go', 'version'], env=env, text=True).strip()
assert version.startswith('go version go1.27.3 '), version
scratch = root/'.cache/1210-replay'
scratch.mkdir(parents=True, exist_ok=False)
archive = evidence/'harness-source.tar.gz'
assert hashlib.sha256(archive.read_bytes()).hexdigest() == json.loads((evidence/'harness-source.json').read_text())['sha256']
with tarfile.open(archive) as packed:
    packed.extractall(scratch, filter='data')
(scratch/'fork-r').symlink_to(root, target_is_directory=True)
receipts = json.loads((evidence/('trace-builds.json' if args.source == 'source-trace1' else 'ablation-builds.json')).read_text())
for receipt in receipts:
    platform = receipt['platform']
    goos, goarch = platform.split('-')
    flags = [x for x in receipt['argv'] if x.startswith('-') and x != '-o']
    output = scratch/('diagnostic-'+platform)
    subprocess.run(['go', 'build', *flags, '-o', str(output), './cmd/wiremux-measure'], cwd=scratch/'src', env=dict(env, GOOS=goos, GOARCH=goarch), check=True)
    digest = hashlib.sha256(output.read_bytes()).hexdigest()
    assert digest == receipt['sha256'], (platform, digest, receipt['sha256'])
    print(platform, digest, 'matches retained diagnostic binary', flush=True)
