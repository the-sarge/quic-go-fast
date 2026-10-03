#!/usr/bin/env python3
"""Finite #714 intervention builds; never edits a tracked transport source file.

Copied from the #712 Linux diagnostic's build.py (4323dae8) with three
recorded changes: only plain fixture builds exist (no diagnostic overlay or
counting variants); a variant is any named revision given on the command line
as NAME=REV, so each intervention revision builds the same way as the frozen
references; and every copied aid is checked byte-identical against the #712
record. Each variant is an exported source tree under .local with a copied
campaign module whose quic-go replacement points at that tree.

Usage: build.py relay reno=e4f322cb r0=fc4c1bf1 r1=<sha> ...
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
ART = ROOT / '.local/bbr-per-packet-interventions'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
FROZEN = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',  # frozen matched reference
    'r0': 'fc4c1bf13e6e46a832dc479d7f6279feedbfb847',    # adopted D1+D2 candidate, the predecessor of intervention 1
}
DIAGNOSTIC = '4323dae8'
DIAGNOSTIC_DIR = 'docs/audits/2026-10-03-bbr-linux-diagnostic'
COPIED = ['run.py', 'localize.py', 'launch/main.go', 'relay/main.go', 'relay/main-v1.go.src', 'relay/linux-ecn.patch',
          'model-overlay/next.go']
ENV = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod', 'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0',
       'GOWORK': 'off'}


def git(*args, **kw):
    return subprocess.check_output(['git', *args], cwd=ROOT, **kw)


def replace(path, old, new):
    s = path.read_text()
    assert s.count(old) == 1, (path, old)
    path.write_text(s.replace(old, new))


def go_build(module, pkg, binary, label):
    assert not binary.exists(), f'{binary} exists; builds never overwrite'
    # -buildvcs=false: binaries must not depend on the enclosing worktree's HEAD.
    cmd = ['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags', '-X main.sourceRevision=' + label, '-o', str(binary), pkg]
    subprocess.run(cmd, cwd=module, env=ENV, check=True)
    return dict(binary=str(binary.relative_to(ART)), sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), command=cmd)


def export(name, rev):
    full_rev = git('rev-parse', rev, text=True).strip()
    tree = ART / 'src' / name
    assert not tree.exists(), f'{tree} exists; builds never overwrite a prior tree'
    tree.mkdir(parents=True)
    subprocess.run(['tar', '-x', '-C', str(tree)], input=git('archive', '--format=tar', full_rev), check=True)
    module = tree / 'campaign'
    module.mkdir()
    for f in ['go.mod', 'go.sum']:
        shutil.copy(tree / CAMPAIGN / f, module / f)
    for d in ['fixture', 'model']:
        shutil.copytree(tree / CAMPAIGN / d, module / d)
    replace(module / 'go.mod', 'replace github.com/quic-go/quic-go => ../../..', 'replace github.com/quic-go/quic-go => ..')
    (module / 'heap.patch').write_bytes(git('show', ':'.join(HEAP_PATCH)))
    subprocess.run(['patch', '-p1', '--forward', '--batch', '-i', 'heap.patch'], cwd=module, check=True)
    shutil.copy(HERE / 'model-overlay/next.go', module / 'model/next.go')
    return full_rev, tree, module


def build(arg):
    (ART / 'bin').mkdir(parents=True, exist_ok=True)
    for rel in COPIED:
        assert (HERE / rel).read_bytes() == git('show', f'{DIAGNOSTIC}:{DIAGNOSTIC_DIR}/{rel}'), rel
    if arg == 'relay':
        # The relay model is the frozen Q1 model; build from the frozen tree, with #712's Linux ECN adapter.
        out = {}
        full_rev, tree, module = export('relay-linux', FROZEN['reno'])
        (module / 'relay').mkdir()
        shutil.copy(HERE / 'relay/main.go', module / 'relay/main.go')
        subprocess.run(['patch', '-p1', '--forward', '--batch', '-i', str(HERE / 'relay/linux-ecn.patch')], cwd=module, check=True)
        out['relay-linux'] = go_build(module, './relay', ART / 'bin' / 'relay-linux', full_rev)
        (module / 'launch').mkdir()
        shutil.copy(HERE / 'launch/main.go', module / 'launch/main.go')
        out['launch'] = go_build(module, './launch', ART / 'bin' / 'launch', full_rev)
        name, receipt = 'relay', dict(variant='relay', revision=full_rev, builds=out)
    else:
        name, rev = arg.split('=', 1) if '=' in arg else (arg, FROZEN[arg])
        if name in FROZEN:
            assert git('rev-parse', rev, text=True).strip() == FROZEN[name], (name, rev)
        full_rev, tree, module = export(name, rev)
        receipt = dict(variant=name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH),
                       builds={'fixture': go_build(module, './fixture', ART / 'bin' / f'{name}-fixture', full_rev)})
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps({name: {k: v['sha256'][:16] for k, v in receipt['builds'].items()}}))


for a in sys.argv[1:] or ['relay', 'reno', 'r0']:
    build(a)
