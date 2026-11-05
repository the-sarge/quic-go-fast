"""Restore a recorded disposable issue #1210 overlay into a caller-owned clean feature worktree."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('worktree', type=Path)
args = parser.parse_args()
root = args.worktree.resolve()
assert root.is_relative_to('/Volumes/worktrees/quic-go-fast'), root
assert subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip() == str(root)
branch = subprocess.check_output(['git', 'branch', '--show-current'], cwd=root, text=True).strip()
assert branch and branch not in ('main', 'master', 'trunk'), branch
assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip() == 'bc4915ea0de0c238ecfdc51d827f7da67639a789'
assert subprocess.check_output(['git', 'status', '--porcelain'], cwd=root) == b'', 'worktree must be clean'
source = Path(__file__).resolve().parent / 'source'
manifest = json.loads((source / 'manifest.json').read_text())
for name, digest in manifest.items():
    assert not Path(name).is_absolute() and '..' not in Path(name).parts
    data = (source / (name + '.txt')).read_bytes()
    assert hashlib.sha256(data).hexdigest() == digest, name
for name in manifest:
    target = root / name
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes((source / (name + '.txt')).read_bytes())
print(f'Installed kernel timestamp observer in {root} on {branch}; temporary instrumentation only.')
