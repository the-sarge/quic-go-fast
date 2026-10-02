#!/usr/bin/env python3
"""Finite demonstration builds; never edits a tracked transport source file.

Each variant is an exported source tree under .local with a copied campaign
module whose quic-go replacement points at that tree. Counted builds add the
demonstration overlays in counting/ and use the bbrworkcount tag.
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
ART = ROOT / '.local/bbr-correction-demonstration'
FROZEN = 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0'
SERVICE = 'c6bb13b72e2d5de55af36543aa27c8cf06812a11'
FULL = '8a0e0962'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
DIAG_PATCH = ('d785f3b82a82380e7b36c87dd29cd6291ae418a3', 'docs/audits/2026-10-02-bbr-recovery-service/diagnostic-workcount.patch')
VARIANTS = {
    'frozen': (FROZEN, False), 'service': (SERVICE, False), 'full': (FULL, False),
    'full-quantum': (FULL, False),
    'diagnostic-counted': (FROZEN, True), 'service-counted': (SERVICE, True), 'full-counted': (FULL, True),
}


def git(*args, **kw):
    return subprocess.check_output(['git', *args], cwd=ROOT, **kw)


def replace(path, old, new):
    s = path.read_text()
    assert s.count(old) == 1, (path, old)
    path.write_text(s.replace(old, new))


def c4_hooks(tree):
    rec = tree / 'internal/congestion/bbr_recovery.go'
    replace(rec, '\t\tif b.undo.exited && !b.ce.active {\n', '\t\tif b.undo.exited && !b.ce.active {\n\t\t\tc4Count(7)\n')
    replace(rec, '\t\t\t\tb.probeRTTReturnStartup = true\n', '\t\t\t\tb.probeRTTReturnStartup = true\n\t\t\t\tc4Count(1)\n')
    replace(rec, '\t\t\t\tb.phase = bbrStartup\n', '\t\t\t\tb.phase = bbrStartup\n\t\t\t\tc4Count(0)\n')
    replace(rec, '\t\t\t\tb.startProbeRefill(b.delivered)\n', '\t\t\t\tb.startProbeRefill(b.delivered)\n\t\t\t\tc4Count(2)\n')
    replace(rec, '\tif b.undo.valid {\n\t\tb.undo.exited, b.undo.phase = true, b.phase\n', '\tif b.undo.valid {\n\t\tc4Count(8)\n\t\tb.undo.exited, b.undo.phase = true, b.phase\n')
    snd = tree / 'internal/congestion/bbr_sender.go'
    replace(snd, 'func (b *BBRSender) Feedback(e FeedbackEvent) {\n', 'func (b *BBRSender) Feedback(e FeedbackEvent) {\n\tdefer func() { c4Phase(b.phase) }()\n')
    replace(snd, '\t\treturn clockValid && e.HasAck && b.updateRTT(e.Time, e.RawRTT)\n',
            '\t\tbefore, probeBefore := b.minimumRTT, b.probeRTTMinimum\n'
            '\t\texpired := clockValid && e.HasAck && b.updateRTT(e.Time, e.RawRTT)\n'
            '\t\tif (before > 0 && b.minimumRTT < before) || (probeBefore > 0 && b.probeRTTMinimum < probeBefore) {\n\t\t\tc4Count(3)\n\t\t}\n'
            '\t\treturn expired\n')
    replace(snd, '\troundStart := roundEvidence && b.startRound(anchor.Delivery.Delivered, s.Delivered)\n',
            '\troundStart := roundEvidence && b.startRound(anchor.Delivery.Delivered, s.Delivered)\n'
            '\tif roundStart && (!s.Valid || s.Interval <= 0) {\n\t\tc4Count(4)\n\t}\n')
    replace(snd, '\t\t\tb.inflightLong = max(b.bdp(1), bbrBytes(b.latestVolume))\n',
            '\t\t\tc4Count(5)\n'
            '\t\t\tif max(b.bdp(1), bbrBytes(b.latestVolume)) != max(b.inflight(1), bbrBytes(b.latestVolume)) {\n\t\t\t\tc4Count(6)\n\t\t\t}\n'
            '\t\t\tb.inflightLong = max(b.bdp(1), bbrBytes(b.latestVolume))\n')


def build(variant):
    rev, counted = VARIANTS[variant]
    full_rev = git('rev-parse', rev, text=True).strip()
    tree = ART / 'src' / variant
    assert not tree.exists(), f'{tree} exists; builds never overwrite a prior tree'
    tree.mkdir(parents=True)
    archive = git('archive', '--format=tar', full_rev)
    subprocess.run(['tar', '-x', '-C', str(tree)], input=archive, check=True)
    module = tree / 'campaign'
    module.mkdir()
    for name in ['go.mod', 'go.sum']:
        shutil.copy(tree / CAMPAIGN / name, module / name)
    for name in ['fixture', 'model']:
        shutil.copytree(tree / CAMPAIGN / name, module / name)
    replace(module / 'go.mod', 'replace github.com/quic-go/quic-go => ../../..', 'replace github.com/quic-go/quic-go => ..')
    (module / 'heap.patch').write_bytes(git('show', ':'.join(HEAP_PATCH)))
    subprocess.run(['patch', '-p1', '--forward', '--batch', '-i', 'heap.patch'], cwd=module, check=True)
    shutil.copy(HERE / 'model-overlay/next.go', module / 'model/next.go')
    shutil.copytree(HERE / 'relay', module / 'relay')
    applied = []
    if variant == 'full-quantum':
        # Diagnostic intervention only: once credit is short, sleep until a full
        # quantum accrues instead of one datagram's credit. Bursts stay <= Q.
        replace(tree / 'bbr_send_policy.go',
                '\treturn now.Add(time.Duration(ceilSendDelay(uint64(int64(size)*1e9-p.tokens), p.rate)))\n',
                '\treturn now.Add(time.Duration(ceilSendDelay(uint64(int64(max(size, p.quantum))*1e9-p.tokens), p.rate)))\n')
        applied.append('full-quantum deadline intervention')
    if counted:
        if variant == 'diagnostic-counted':
            (tree / 'diag.patch').write_bytes(git('show', ':'.join(DIAG_PATCH)))
            subprocess.run(['patch', '-p1', '--forward', '--batch', '-i', 'diag.patch'], cwd=tree, check=True)
            applied.append(':'.join(DIAG_PATCH))
        shutil.copy(HERE / 'counting/service_work_on.go', tree / 'internal/ackhandler/service_work_on.go')
        shutil.copy(HERE / 'counting/c4_work_on.go', tree / 'internal/congestion/c4_work_on.go')
        shutil.copy(HERE / 'counting/c4_work_off.go', tree / 'internal/congestion/c4_work_off.go')
        if variant == 'full-counted':
            c4_hooks(tree)
        subprocess.run(['gofmt', '-l', '-w', 'internal/ackhandler', 'internal/congestion'], cwd=tree, check=True)
    env = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod'}
    tags = ['-tags', 'bbrworkcount'] if counted else []
    label = full_rev + ('+' + variant if variant not in ('frozen', 'service', 'full') else '')
    out = {}
    for name, pkg in [('fixture', './fixture'), ('relay', './relay')]:
        binary = ART / 'bin' / f'{variant}-{name}'
        binary.parent.mkdir(exist_ok=True)
        assert not binary.exists(), binary
        cmd = ['go', 'build', '-trimpath', *tags, '-ldflags', '-X main.sourceRevision=' + label, '-o', str(binary), pkg]
        subprocess.run(cmd, cwd=module, env=env, check=True)
        out[name] = dict(binary=str(binary.relative_to(ART)), sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), command=cmd)
    receipt = dict(variant=variant, revision=full_rev, counted=counted, patches=applied,
                   overlays=sorted(str(p.relative_to(HERE)) for p in (HERE / 'counting').glob('*.go')) if counted else [],
                   c4_hooks=variant == 'full-counted', heap_fixture=':'.join(HEAP_PATCH), builds=out,
                   go=subprocess.check_output(['go', 'version'], cwd=module, env=env, text=True).strip())
    (ART / 'bin' / f'{variant}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(dict(variant=variant, fixture=out['fixture']['sha256'][:16], relay=out['relay']['sha256'][:16])))


for v in sys.argv[1:] or list(VARIANTS):
    build(v)
