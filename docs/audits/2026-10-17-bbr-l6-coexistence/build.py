#!/usr/bin/env python3
"""Finite BBRv3 L6 coexistence builds (#777); never edits a tracked transport source file.

Adapted from the #748 slice 6 readiness record's build.py (9b38f0ab), which
built the same two fixtures. Recorded changes: only the plain `reno` (frozen
matched reference and the competitor) and `cand` (the advanced port, `abfa5456`)
fixtures are built, and each must be byte-identical to slice 6's binary
(EXPECTED), so this record runs exactly the binaries slice 6 measured; the
`-diag`, timeline and counting overlays are not used; the relay target builds
this record's two-flow relay (`relay2/`), its calibration aid (`calibrate2/`)
and slice 6's launcher from the frozen tree, with the read-only model overlays
`model-overlay/next.go` (copied) and `model-overlay/flows.go` (new); the
artifact directory is this record's; and an `aidtest` target assembles the same
module without building, for running the aid tests (`go test ./relay2 ./model`).

Each variant is an exported source tree under .local with a copied campaign
module whose quic-go replacement points at that tree.

Usage: build.py [reno|cand|relay|aidtest ...]   (default: reno cand relay)
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
ART = ROOT / '.local/bbr-l6-coexistence'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
REVISIONS = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',  # frozen matched reference and the competitor
    'cand': 'abfa5456d99a9299825d04454a2e7ed12dff3326',  # the advanced port on main (#748): BBRv3 selected per run
}
# Copied byte-identical from the #748 slice 6 readiness record.
SLICE6 = '9b38f0ab'
SLICE6_DIR = 'docs/audits/2026-10-16-bbr-port-readiness'
SLICE6_COPIED = ['launch/main.go', 'model-overlay/next.go']
# Binaries rebuilt from a revision that slice 6 already built (SHA-256).
EXPECTED = {
    ('reno', 'fixture'): 'cda8feb1b2fb004d72425adee5edeffb64a4c437f3ef07f9eb4e686af3ab5c8b',  # #715, #736, #738, #739, slice 6
    ('cand', 'fixture'): 'd3f05ad685f92732c5354ee683c01c30f17aa067c400b725d9a30029b3410c15',  # slice 6
    ('relay', 'launch'): 'a6146d62e16339f247a7e5fdac29ff47a66be7d8b6fc8d5e647d0479a58d5917',  # #712 onwards, slice 6
}
# This record's own aids, as registered (built on the Mac and, identically, on minimax).
REGISTERED = {
    ('relay', 'relay2'): 'bff3ba4f86d4c71c40c0979e748fe698ddac22492fbf63158fa7a889a79de8fc',
    ('relay', 'calibrate2'): 'f4773dc9b53ae600b60dfaa6716247eef45bba78fdde546d5df36cfdb65b909d',
}
LINUX = {'GOOS': 'linux', 'GOARCH': 'amd64'}
ENV = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod', 'CGO_ENABLED': '0', 'GOWORK': 'off'}


def git(*args, **kw):
    return subprocess.check_output(['git', *args], cwd=ROOT, **kw)


def replace(path, old, new):
    s = path.read_text()
    assert s.count(old) == 1, (path, old)
    path.write_text(s.replace(old, new))


def go_build(module, pkg, binary, label, platform=LINUX):
    assert not binary.exists(), f'{binary} exists; builds never overwrite'
    # -buildvcs=false: binaries must not depend on the enclosing worktree's HEAD.
    cmd = ['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags', '-X main.sourceRevision=' + label, '-o', str(binary), pkg]
    subprocess.run(cmd, cwd=module, env={**ENV, **platform}, check=True)
    modules = subprocess.check_output(['go', 'version', '-m', str(binary)], env=ENV, text=True).splitlines()
    return dict(binary=str(binary.relative_to(ART)), sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), command=cmd,
                platform=f"{platform['GOOS']}/{platform['GOARCH']}",
                modules=[line.split('\t')[1:4] for line in modules if line.startswith('\tdep\t') or line.startswith('\t=>\t')])


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


def aids(module):
    """Add this record's relay, calibration aid, launcher and the per-flow model overlay."""
    shutil.copy(HERE / 'model-overlay/flows.go', module / 'model/flows.go')
    for d, files in [('relay2', ['main.go', 'main_test.go']), ('calibrate2', ['main.go']), ('launch', ['main.go'])]:
        (module / d).mkdir()
        for f in files:
            if (HERE / d / f).exists():
                shutil.copy(HERE / d / f, module / d / f)


def build(name):
    (ART / 'bin').mkdir(parents=True, exist_ok=True)
    for rel in SLICE6_COPIED:
        assert (HERE / rel).read_bytes() == git('show', f'{SLICE6}:{SLICE6_DIR}/{rel}'), rel
    if name == 'aidtest':
        _, _, module = export('aidtest', REVISIONS['reno'])
        aids(module)
        print(module)
        return
    if name == 'relay':
        # The relay model is the frozen Q1 model; build from the frozen tree.
        full_rev, tree, module = export('relay', REVISIONS['reno'])
        aids(module)
        out = {label: go_build(module, './' + pkg, ART / 'bin' / label, full_rev)
               for label, pkg in [('relay2', 'relay2'), ('calibrate2', 'calibrate2'), ('launch', 'launch')]}
        receipt = dict(variant='relay', revision=full_rev, overlays=['model-overlay/next.go', 'model-overlay/flows.go'], builds=out)
    else:
        full_rev, tree, module = export(name, REVISIONS[name])
        receipt = dict(variant=name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH),
                       builds={'fixture': go_build(module, './fixture', ART / 'bin' / f'{name}-fixture', full_rev)})
    for key, b in receipt['builds'].items():
        if (name, key) in EXPECTED:
            assert b['sha256'] == EXPECTED[(name, key)], (name, key, b['sha256'])
            b['byte_identical_to_slice6'] = True
        if (name, key) in REGISTERED:
            assert b['sha256'] == REGISTERED[(name, key)], (name, key, b['sha256'])
            b['registered'] = True
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(name, 'built')


if __name__ == '__main__':
    for v in sys.argv[1:] or ['reno', 'cand', 'relay']:
        build(v)
