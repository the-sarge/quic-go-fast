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
args = parser.parse_args()
root = args.worktree.resolve()
evidence = Path(__file__).resolve().parent
assert root.is_relative_to('/Volumes/worktrees/quic-go-fast')
for name, digest in json.loads((evidence/'source/manifest.json').read_text()).items():
    assert hashlib.sha256((root/name).read_bytes()).hexdigest() == digest, name
env = dict(os.environ, GOTOOLCHAIN='local', GOPROXY='off', GOFLAGS='', CGO_ENABLED='0', GOMAXPROCS='4')
version = subprocess.check_output(['go', 'version'], env=env, text=True).strip()
assert version.startswith('go version go1.27.3 '), version
scratch = root/'.cache/1210-replay'
scratch.mkdir(parents=True, exist_ok=False)
historical = evidence.parent/'2026-11-05-probertt-investigation'
archive = historical/'harness-source.tar.gz'
assert hashlib.sha256(archive.read_bytes()).hexdigest() == json.loads((historical/'harness-source.json').read_text())['sha256']
with tarfile.open(archive) as packed:
    packed.extractall(scratch, filter='data')
for name, digest in json.loads((evidence/'harness-overlay/manifest.json').read_text()).items():
    data = (evidence/'harness-overlay'/(name+'.txt')).read_bytes()
    assert hashlib.sha256(data).hexdigest() == digest, name
    (scratch/'src'/name).write_bytes(data)
(scratch/'fork-r').symlink_to(root, target_is_directory=True)
receipts = [json.loads((evidence/('build-'+platform+'.json')).read_text()) for platform in ['linux-amd64', 'darwin-arm64']]
for receipt in receipts:
    platform = receipt['platform']
    goos, goarch = platform.split('-')
    flags = [x for x in receipt['argv'] if x.startswith('-') and x != '-o']
    output = scratch/('diagnostic-'+platform)
    subprocess.run(['go', 'build', *flags, '-o', str(output), './cmd/wiremux-measure'], cwd=scratch/'src', env=dict(env, GOOS=goos, GOARCH=goarch), check=True)
    digest = hashlib.sha256(output.read_bytes()).hexdigest()
    assert digest == receipt['sha256'], (platform, digest, receipt['sha256'])
    print(platform, digest, 'matches retained diagnostic binary', flush=True)
