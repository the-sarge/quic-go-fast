#!/usr/bin/env python3
"""Finite #734 builds; never edits a tracked transport source file.

Adapted from the #715 re-demonstration's build.py (36f7ce01). Fixture variants
are exported source trees under .local with a copied campaign module whose
quic-go replacement points at that tree, exactly as #715 built them; `reno`
and `cand` must rebuild byte-identical to #715's binaries. New here:

- `cand-wake-timeline`: `d0fabc4d` plus #715's timeline overlay (unchanged)
  plus the #734 wake-path recorder (wake/wakerec.go, wake/wake_hooks.go) with
  the Go execution trace over the measured window. It is the Stage 0
  instrument candidate. The `-timeline` suffix makes #715's unchanged
  run_case_x set TIMELINE_OUTPUT for the sender, which also enables the
  recorder.
- `cand-notrace-timeline`: the same with the trace off (recorder events only),
  for describing the trace's own perturbation.
- `cand-lb-timeline`: the recorder alone with the trace off and without #715's
  overlay, for the loopback rule.
- `harness`: the synthetic harness (wake/harness) with the recorder,
  byte-identical except for its package clause.
- `ana`: the analysis (wake/ana), for linux/amd64 and darwin/arm64.
- `relay`: #715's relay-linux and launcher, rebuilt from the frozen tree.

Usage: build.py [variant ...]   (default: every variant)
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
ART = ROOT / '.local/bbr-pacing-wake'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
REVISIONS = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',  # frozen matched reference
    'cand': 'd0fabc4d',                                  # surviving revision r6 (#714)
}
# (revision, timeline overlay, wake recorder, trace on)
VARIANTS = {name: (rev, False, False, False) for name, rev in REVISIONS.items()}
VARIANTS['cand-wake-timeline'] = (REVISIONS['cand'], True, True, True)
VARIANTS['cand-notrace-timeline'] = (REVISIONS['cand'], True, True, False)
# Loopback: the recorder alone (aggregates and events), trace off, without #715's overlay, whose
# growing JSON snapshot every 500 ms perturbs loopback goodput. The suffix only makes run_case_x set
# TIMELINE_OUTPUT, which enables the recorder.
VARIANTS['cand-lb-timeline'] = (REVISIONS['cand'], False, True, False)
RECORD = '36f7ce01'  # #715
RECORD_DIR = 'docs/audits/2026-10-05-bbr-linux-redemonstration'
COPIED = ['run.py', 'stage_run.py', 'localize.py', 'pack.py', 'launch/main.go', 'relay/main.go', 'relay/main-v1.go.src',
          'relay/linux-ecn.patch', 'timeline/bbr_timeline.go', 'timeline/emission_timeline.go']
# #715's build receipts (raw.tar.gz bin/*-build.json): rebuilt binaries must match.
EXPECTED = {
    'reno': 'cda8feb1',
    'cand': 'b5defc02',
    'relay-linux': '24ee941d',
    'launch': 'a6146d62',
}
LINUX = {'GOOS': 'linux', 'GOARCH': 'amd64'}
DARWIN = {'GOOS': 'darwin', 'GOARCH': 'arm64'}
ENV = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod', 'CGO_ENABLED': '0', 'GOWORK': 'off'}


def git(*args, **kw):
    return subprocess.check_output(['git', *args], cwd=ROOT, **kw)


def replace(path, old, new):
    s = path.read_text()
    assert s.count(old) == 1, (path, old)
    path.write_text(s.replace(old, new))


# Verbatim from #715's build.py.
def timeline(tree):
    """#715 measurement-only overlay: hooks only BBR-selected code (Feedback, sendBounded, the BBR handoff)."""
    shutil.copy(HERE / 'timeline/bbr_timeline.go', tree / 'internal/congestion/bbr_timeline.go')
    shutil.copy(HERE / 'timeline/emission_timeline.go', tree / 'emission_timeline.go')
    snd = tree / 'internal/congestion/bbr_sender.go'
    replace(snd, 'func (b *BBRSender) Feedback(e FeedbackEvent) {\n',
            'func (b *BBRSender) Feedback(e FeedbackEvent) {\n\tdefer func() { timelineFeedback(b, int64(e.PriorInFlight)) }()\n')
    em = tree / 'packet_emission_bbr.go'
    replace(em, 'func (e *packetEmission) sendBounded(now monotime.Time, confirmed bool) (result emissionResult) {\n',
            'func (e *packetEmission) sendBounded(now monotime.Time, confirmed bool) (result emissionResult) {\n'
            '\ttimelineEnter(now)\n\tdefer func() { timelineExit(result) }()\n')
    replace(em, '\t\tmetadata.credit = e.reservation\n',
            '\t\ttimelineSent(int(buf.Len()), int(gso))\n\t\tmetadata.credit = e.reservation\n')
    subprocess.run(['gofmt', '-l', '-w', '.'], cwd=tree, check=True)


def recorder_source(package):
    s = (HERE / 'wake/wakerec.go').read_text()
    assert s.count('\npackage quic\n') == 0 and s.startswith('package quic\n')
    return s.replace('package quic\n', f'package {package}\n', 1)


def wake(tree, trace_on):
    """#734 wake-path recorder: every timer arm, run-loop wake case, opportunity and opportunity exit."""
    src = recorder_source('quic')
    if not trace_on:
        assert src.count('const wakeTrace = true\n') == 1
        src = src.replace('const wakeTrace = true\n', 'const wakeTrace = false\n')
    (tree / 'wakerec.go').write_text(src)
    shutil.copy(HERE / 'wake/wake_hooks.go', tree / 'wake_hooks.go')
    conn = tree / 'connection.go'
    replace(conn, '\tif c.blocked == blockModeHardBlocked {\n\t\tc.timer.Reset(monotime.Until(deadline))\n',
            '\tif c.blocked == blockModeHardBlocked {\n\t\twakeArm(deadline, c.pacingDeadline, 1)\n\t\tc.timer.Reset(monotime.Until(deadline))\n')
    replace(conn, '\tif c.blocked == blockModeCongestionLimited {\n\t\tc.timer.Reset(monotime.Until(deadline))\n',
            '\tif c.blocked == blockModeCongestionLimited {\n\t\twakeArm(deadline, c.pacingDeadline, 2)\n\t\tc.timer.Reset(monotime.Until(deadline))\n')
    replace(conn, '\t\tdeadline = c.pacingDeadline\n\t}\n\tc.timer.Reset(monotime.Until(deadline))\n',
            '\t\tdeadline = c.pacingDeadline\n\t}\n\twakeArm(deadline, c.pacingDeadline, 0)\n\tc.timer.Reset(monotime.Until(deadline))\n')
    for case, k in [('case <-c.timer.C:\n', 1), ('case <-c.sendingScheduled:\n', 2), ('case <-c.handshakeSendFeedback.wakeup:\n', 3),
                    ('case <-sendQueueAvailable:\n', 4), ('case <-c.notifyReceivedPacket:\n', 5)]:
        replace(conn, '\t\t\t' + case, '\t\t\t' + case + f'\t\t\t\twakeWoke({k})\n')
    em = tree / 'packet_emission_bbr.go'
    head = 'func (e *packetEmission) sendBounded(now monotime.Time, confirmed bool) (result emissionResult) {\n'
    if '\ttimelineEnter(now)\n' in em.read_text():
        head += '\ttimelineEnter(now)\n\tdefer func() { timelineExit(result) }()\n'
    replace(em, head, head + '\twakeOpp(now)\n\tdefer func() { wakeExit(result, e.bbr) }()\n')
    subprocess.run(['gofmt', '-l', '-w', '.'], cwd=tree, check=True)


def go_build(module, pkg, binary, label, platform=LINUX):
    assert not binary.exists(), f'{binary} exists; builds never overwrite'
    # -buildvcs=false: binaries must not depend on the enclosing worktree's HEAD.
    cmd = ['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags', '-X main.sourceRevision=' + label, '-o', str(binary), pkg]
    subprocess.run(cmd, cwd=module, env={**ENV, **platform}, check=True)
    return dict(binary=str(binary.relative_to(ART)), sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), command=cmd,
                platform=f"{platform['GOOS']}/{platform['GOARCH']}")


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
    shutil.copy(git_path_model_overlay(), module / 'model/next.go')
    return full_rev, tree, module


def git_path_model_overlay():
    """#715's model-overlay/next.go (#712's), materialized from the #715 record."""
    p = ART / 'model-overlay-next.go'
    if not p.exists():
        p.write_bytes(git('show', f'{RECORD}:{RECORD_DIR}/model-overlay/next.go'))
    return p


def fixture(name):
    rev, tl, wk, trace_on = VARIANTS[name]
    full_rev, tree, module = export(name, rev)
    if tl:
        timeline(tree)
    if wk:
        wake(tree, trace_on)
    label = full_rev + ('+timeline' if tl else '') + ('+wake' if wk else '') + ('' if trace_on or not wk else '-notrace')
    return dict(variant=name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH),
                overlays=(['timeline/bbr_timeline.go', 'timeline/emission_timeline.go'] if tl else [])
                + (['wake/wakerec.go', 'wake/wake_hooks.go'] if wk else []), trace=trace_on,
                builds={'fixture': go_build(module, './fixture', ART / 'bin' / f'{name}-fixture', label)})


def build(name):
    (ART / 'bin').mkdir(parents=True, exist_ok=True)
    for rel in COPIED:
        assert (HERE / rel).read_bytes() == git('show', f'{RECORD}:{RECORD_DIR}/{rel}'), rel
    if name == 'relay':
        out = {}
        full_rev, tree, module = export('relay-linux', REVISIONS['reno'])
        (module / 'relay').mkdir()
        shutil.copy(HERE / 'relay/main.go', module / 'relay/main.go')
        subprocess.run(['patch', '-p1', '--forward', '--batch', '-i', str(HERE / 'relay/linux-ecn.patch')], cwd=module, check=True)
        out['relay-linux'] = go_build(module, './relay', ART / 'bin' / 'relay-linux', full_rev)
        (module / 'launch').mkdir()
        shutil.copy(HERE / 'launch/main.go', module / 'launch/main.go')
        out['launch'] = go_build(module, './launch', ART / 'bin' / 'launch', full_rev)
        receipt = dict(variant='relay', revision=full_rev, builds=out)
    elif name == 'harness':
        module = ART / 'src' / 'harness'
        assert not module.exists()
        module.mkdir(parents=True)
        shutil.copy(HERE / 'wake/harness/main.go', module / 'main.go')
        (module / 'wakerec.go').write_text(recorder_source('main'))
        (module / 'go.mod').write_text('module harness\n\ngo 1.27\n')
        receipt = dict(variant='harness', builds={'harness': go_build(module, '.', ART / 'bin' / 'harness', 'harness')})
    elif name == 'ana':
        module = HERE / 'wake/ana'
        receipt = dict(variant='ana', builds={p['GOOS']: go_build(module, '.', ART / 'bin' / f"wakeana-{p['GOOS']}", 'wakeana', p)
                                              for p in [LINUX, DARWIN]})
    else:
        receipt = fixture(name)
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    for k, b in receipt['builds'].items():
        key = name if k == 'fixture' else k
        if key in EXPECTED:
            receipt.setdefault('matches_715', {})[key] = b['sha256'].startswith(EXPECTED[key])
            assert b['sha256'].startswith(EXPECTED[key]), (key, b['sha256'])
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(name, 'built')


for v in sys.argv[1:] or [*VARIANTS, 'relay', 'harness', 'ana']:
    build(v)
