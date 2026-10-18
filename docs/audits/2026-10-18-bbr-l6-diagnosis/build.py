#!/usr/bin/env python3
"""Finite BBRv3 L6 diagnosis builds (#792); never edits a tracked transport source file.

Adapted from the #777 L6 coexistence record's build.py (71e88789), which built
the same fixtures and relay. Recorded changes:
- #777's aids are not copied: relay2, calibrate2, the launcher and both model
  overlays are read from #777's record directory in this tree, after a check
  that each is byte-identical to its file at 71e88789.
- One more fixture variant, `cand-trace`: the `cand` revision (`abfa5456`) with
  this record's measurement-only overlay (trace/bbr_trace.go, and its test file,
  which does not enter the binary) and six patch sites in BBR-only code, each
  asserted to occur exactly once. `reno`, `cand` and the relay target must stay
  byte-identical to #777's binaries (EXPECTED).
- The artifact directory is this record's (.local/bbr-l6-diagnosis).
- The build is split in two steps so that the Go compile can run on another
  host from the same exported module: `export` (needs this git checkout) writes
  each tree and its build plan; `compile` (needs only the tree and Go) builds
  from the plan and checks the hashes. `all` does both here. Go builds are
  reproducible for a fixed toolchain, target, -trimpath and -buildvcs=false.

Each variant is an exported source tree under .local with a copied campaign
module whose quic-go replacement points at that tree.

Usage: build.py export|compile|all [reno|cand|cand-trace|relay|aidtest ...]
       (default variants: reno cand cand-trace relay)
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
ART = ROOT / '.local/bbr-l6-diagnosis'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
REVISIONS = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',  # frozen Reno: the competitor
    'cand': 'abfa5456d99a9299825d04454a2e7ed12dff3326',  # the advanced port on main (#748), BBRv3 selected per run
    'cand-trace': 'abfa5456d99a9299825d04454a2e7ed12dff3326',  # the same, with this record's overlay
}
# #777's aids, used in place and checked against #777's record.
L6 = '71e88789e8b89dcfec960d4f54d20027ef1f77b9'
L6_DIR = 'docs/audits/2026-10-17-bbr-l6-coexistence'
L6_AIDS = ['relay2/main.go', 'relay2/main_test.go', 'calibrate2/main.go', 'launch/main.go', 'model-overlay/next.go', 'model-overlay/flows.go']
# Binaries #777 built and ran (SHA-256); each rebuild must be byte-identical.
EXPECTED = {
    ('reno', 'fixture'): 'cda8feb1b2fb004d72425adee5edeffb64a4c437f3ef07f9eb4e686af3ab5c8b',
    ('cand', 'fixture'): 'd3f05ad685f92732c5354ee683c01c30f17aa067c400b725d9a30029b3410c15',
    ('relay', 'relay2'): 'bff3ba4f86d4c71c40c0979e748fe698ddac22492fbf63158fa7a889a79de8fc',
    ('relay', 'calibrate2'): 'f4773dc9b53ae600b60dfaa6716247eef45bba78fdde546d5df36cfdb65b909d',
    ('relay', 'launch'): 'a6146d62e16339f247a7e5fdac29ff47a66be7d8b6fc8d5e647d0479a58d5917',
}
# This record's own binary, registered after its first build (None until then).
REGISTERED = {
    ('cand-trace', 'fixture'): '57d794d47b123e28c436e029ca86c3d5953f664edf9f987b68c32281d921a7b2',
}
LINUX = {'GOOS': 'linux', 'GOARCH': 'amd64'}
ENV = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod', 'CGO_ENABLED': '0', 'GOWORK': 'off'}

# The overlay's hooks: (file, exact text, replacement). Each text occurs once.
TRACE_SITES = [
    ('internal/congestion/bbr_sender.go', 'func (b *BBRSender) Feedback(e FeedbackEvent) {\n',
     'func (b *BBRSender) Feedback(e FeedbackEvent) {\n\ttraceFeedbackStart(b, &e)\n\tdefer traceFeedbackEnd(b, &e)\n'),
    ('internal/congestion/bbr_sender.go', '\troundStart := roundEvidence && b.startRound(anchor.Delivery.Delivered, s.Delivered)\n',
     '\troundStart := roundEvidence && b.startRound(anchor.Delivery.Delivered, s.Delivered)\n\tif roundStart {\n\t\ttraceRoundStart(b, &e)\n\t}\n'),
    ('internal/congestion/bbr_sender.go', '\tif lossRoundStart {\n\t\tif b.lossPending && b.phase == bbrStartup',
     '\tif lossRoundStart {\n\t\ttraceLossRoundPre(b, &e)\n\t\tif b.lossPending && b.phase == bbrStartup'),
    ('internal/congestion/bbr_sender.go', '\t\tb.lossRanges = b.lossRanges[:0]\n\t\tb.lossBytes, b.lossFlight = 0, 0\n\t\tb.lossPending, b.lossOverflow = false, false\n\t\tif !b.recoveryStarted {\n',
     '\t\ttraceLossRoundPost(b, &e)\n\t\tb.lossRanges = b.lossRanges[:0]\n\t\tb.lossBytes, b.lossFlight = 0, 0\n\t\tb.lossPending, b.lossOverflow = false, false\n\t\tif !b.recoveryStarted {\n'),
    ('internal/congestion/bbr_sender.go', 'func (b *BBRSender) Reset(path, sample, delivered uint64) {\n',
     'func (b *BBRSender) Reset(path, sample, delivered uint64) {\n\ttraceReset(b, path, sample, delivered)\n'),
    ('internal/congestion/bbr_probe.go', 'func (b *BBRSender) probeLoss(e FeedbackEvent, p PacketInfo, remaining uint64) {\n',
     'func (b *BBRSender) probeLoss(e FeedbackEvent, p PacketInfo, remaining uint64) {\n\ttraceProbeLossEntry(b)\n'),
    ('internal/congestion/bbr_probe.go', '\tif lost <= flight/50 || lost < uint64(p.Length) || p.Delivery.PostInFlight < p.Length {\n',
     '\ttraceProbeLoss(b, &e, &p, lost, flight, remaining)\n\tif lost <= flight/50 || lost < uint64(p.Length) || p.Delivery.PostInFlight < p.Length {\n'),
]


def git(*args, **kw):
    return subprocess.check_output(['git', *args], cwd=ROOT, **kw)


def replace(path, old, new):
    s = path.read_text()
    assert s.count(old) == 1, (path, old)
    path.write_text(s.replace(old, new))


def check_aids():
    for rel in L6_AIDS:
        assert (ROOT / L6_DIR / rel).read_bytes() == git('show', f'{L6}:{L6_DIR}/{rel}'), rel


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
    shutil.copy(ROOT / L6_DIR / 'model-overlay/next.go', module / 'model/next.go')
    return full_rev, tree, module


def aids(module):
    """Add #777's relay, calibration aid, launcher and the per-flow model overlay."""
    shutil.copy(ROOT / L6_DIR / 'model-overlay/flows.go', module / 'model/flows.go')
    for d, files in [('relay2', ['main.go', 'main_test.go']), ('calibrate2', ['main.go']), ('launch', ['main.go'])]:
        (module / d).mkdir()
        for f in files:
            if (ROOT / L6_DIR / d / f).exists():
                shutil.copy(ROOT / L6_DIR / d / f, module / d / f)


def trace_overlay(tree):
    """#792 measurement-only overlay: hooks only BBR-selected controller code."""
    shutil.copy(HERE / 'trace/bbr_trace.go', tree / 'internal/congestion/bbr_trace.go')
    shutil.copy(HERE / 'trace/bbr_trace_test.go', tree / 'internal/congestion/bbr_trace_test.go')
    for rel, old, new in TRACE_SITES:
        replace(tree / rel, old, new)


def export_variant(name):
    check_aids()
    if name in ('relay', 'aidtest'):
        # The relay model is the frozen Q1 model; build from the frozen tree.
        full_rev, tree, module = export(name, REVISIONS['reno'])
        aids(module)
        plan = dict(variant=name, revision=full_rev, overlays=['model-overlay/next.go', 'model-overlay/flows.go'],
                    builds=[] if name == 'aidtest' else [['relay2', './relay2'], ['calibrate2', './calibrate2'], ['launch', './launch']])
    else:
        full_rev, tree, module = export(name, REVISIONS[name])
        overlays = []
        if name == 'cand-trace':
            trace_overlay(tree)
            overlays = ['trace/bbr_trace.go', 'trace/bbr_trace_test.go']
        plan = dict(variant=name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH), overlays=overlays,
                    overlay_sha256={rel: hashlib.sha256((HERE / rel).read_bytes()).hexdigest() for rel in overlays},
                    builds=[['fixture', './fixture']])
    (tree / 'build-plan.json').write_text(json.dumps(plan, indent=2) + '\n')
    print(module)


def go_build(module, pkg, binary, label, platform=LINUX):
    assert not binary.exists(), f'{binary} exists; builds never overwrite'
    # -buildvcs=false: binaries must not depend on the enclosing worktree's HEAD.
    cmd = ['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags', '-X main.sourceRevision=' + label, '-o', str(binary), pkg]
    subprocess.run(cmd, cwd=module, env={**ENV, **platform}, check=True)
    modules = subprocess.check_output(['go', 'version', '-m', str(binary)], env=ENV, text=True).splitlines()
    return dict(binary=str(binary.relative_to(ART)), sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), command=cmd,
                platform=f"{platform['GOOS']}/{platform['GOARCH']}",
                modules=[line.split('\t')[1:4] for line in modules if line.startswith('\tdep\t') or line.startswith('\t=>\t')])


def compile_variant(name):
    tree = ART / 'src' / name
    plan = json.loads((tree / 'build-plan.json').read_text())
    (ART / 'bin').mkdir(parents=True, exist_ok=True)
    out = {}
    for key, pkg in plan['builds']:
        label = key if name == 'relay' else f'{name}-fixture'
        out[key] = go_build(tree / 'campaign', pkg, ART / 'bin' / label, plan['revision'])
    receipt = dict(plan, builds=out, host=os.uname().nodename)
    for key, b in out.items():
        if (name, key) in EXPECTED:
            assert b['sha256'] == EXPECTED[(name, key)], (name, key, b['sha256'])
            b['byte_identical_to_777'] = True
        if REGISTERED.get((name, key)):
            assert b['sha256'] == REGISTERED[(name, key)], (name, key, b['sha256'])
            b['registered'] = True
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(name, 'built', {k: b['sha256'][:16] for k, b in out.items()})


if __name__ == '__main__':
    step, variants = sys.argv[1], sys.argv[2:] or ['reno', 'cand', 'cand-trace', 'relay']
    assert step in ('export', 'compile', 'all'), step
    for v in variants:
        if step in ('export', 'all'):
            export_variant(v)
        if step in ('compile', 'all') and v != 'aidtest':
            compile_variant(v)
