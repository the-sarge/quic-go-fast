#!/usr/bin/env python3
"""Finite #710 attribution builds; never edits a tracked transport source file.

Each variant is an exported source tree under .local with a copied campaign
module whose quic-go replacement points at that tree. Every variant, Reno
included, receives the same measurement-only occupancy overlay and fixture
series sampler, so the compared arms differ only in their source revision.
The relay is the demonstration's frozen v1 emulator plus delivery
observability (relay/main.go); relay/main-v1.go.txt is kept for comparison.
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
ART = ROOT / '.local/bbr-quantum-pacing'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
VARIANTS = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',         # frozen matched reference
    'perdatagram': '777ce739',                                # D1 ECN-corrected candidate
    'quantum': 'fc4c1bf1',                                    # D1 + D2 quantum-release pacing
}
# Relay sources used by the correction demonstration at 80466857. Its binaries
# embed that worktree's VCS stamp, so identity is checked at source level.
DEMONSTRATION = '80466857'
DEMONSTRATION_DIR = 'docs/audits/2026-10-02-bbr-correction-demonstration'
RELAY_V1_SOURCE = '19635ff6847efd3b778184b2eddae2cbb7baeb29de844cd25d906e4c0045ec61'
ENV = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod'}


def git(*args, **kw):
    return subprocess.check_output(['git', *args], cwd=ROOT, **kw)


def replace(path, old, new):
    s = path.read_text()
    assert s.count(old) == 1, (path, old)
    path.write_text(s.replace(old, new))


def overlay(tree, module):
    shutil.copy(HERE / 'overlay/diag_occupancy.go', tree / 'diag_occupancy.go')
    conn = tree / 'connection.go'
    replace(conn, '\tc.receivedPackets.PushBack(p)\n\tc.receivedPacketMx.Unlock()\n',
            '\tc.receivedPackets.PushBack(p)\n\tdiagRxQueue(c.receivedPackets.Len())\n\tc.receivedPacketMx.Unlock()\n')
    replace(conn, '\t\tp := c.receivedPackets.PopFront()\n\t\tc.receivedPacketMx.Unlock()\n',
            '\t\tp := c.receivedPackets.PopFront()\n\t\tdiagRxQueue(c.receivedPackets.Len())\n\t\tc.receivedPacketMx.Unlock()\n')
    fc = tree / 'flow_controller_connection.go'
    replace(fc, '\tc.highestReceived += increment\n', '\tc.highestReceived += increment\n\tdiagConnReceived(int64(c.highestReceived))\n')
    replace(fc, '\tc.addBytesRead(n)\n\treturn c.hasWindowUpdate()\n',
            '\tc.addBytesRead(n)\n\tdiagConnRead(int64(c.bytesRead))\n\treturn c.hasWindowUpdate()\n')
    shutil.copy(HERE / 'overlay/diag_series.go', module / 'fixture/diag_series.go')
    main = module / 'fixture/main.go'
    replace(main, '\tstarted := time.Now()\n', '\tstarted := time.Now()\n\tstopSeries := startDiagSeries()\n')
    replace(main, '\t<-heapDone\n\tcloseTransport()\n', '\t<-heapDone\n\tstopSeries()\n\tcloseTransport()\n')


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


def build(name):
    (ART / 'bin').mkdir(parents=True, exist_ok=True)
    if name == 'relay':
        # The relay model is the frozen Q1 model; build from the frozen tree.
        # Each relay builds as package ./relay so v1 rebuilds byte-identically.
        out = {}
        for label, src in [('relay-v1', 'main-v1.go.src'), ('relay-v3', 'main.go')]:
            full_rev, tree, module = export(label, VARIANTS['reno'])
            (module / 'relay').mkdir()
            shutil.copy(HERE / 'relay' / src, module / 'relay/main.go')
            out[label] = go_build(module, './relay', ART / 'bin' / label, full_rev)
        # The demonstration committed next.go, but its relay/main-v1.go.txt was
        # dropped by the repository's *.txt ignore rule; pin its on-disk hash.
        assert (HERE / 'model-overlay/next.go').read_bytes() == git('show', f'{DEMONSTRATION}:{DEMONSTRATION_DIR}/model-overlay/next.go')
        assert hashlib.sha256((HERE / 'relay/main-v1.go.src').read_bytes()).hexdigest() == RELAY_V1_SOURCE
        receipt = dict(variant='relay', revision=full_rev, builds=out)
    else:
        full_rev, tree, module = export(name, VARIANTS[name])
        overlay(tree, module)
        subprocess.run(['gofmt', '-l', '.'], cwd=tree, check=True)
        receipt = dict(variant=name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH),
                       overlays=['overlay/diag_occupancy.go', 'overlay/diag_series.go'],
                       builds={'fixture': go_build(module, './fixture', ART / 'bin' / f'{name}-fixture', full_rev + '+diag')})
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps({k: v['sha256'][:16] for k, v in receipt['builds'].items()}))


for v in sys.argv[1:] or [*VARIANTS, 'relay']:
    build(v)
