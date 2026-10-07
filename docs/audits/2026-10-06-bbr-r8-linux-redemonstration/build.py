#!/usr/bin/env python3
"""Finite #736 re-demonstration builds; never edits a tracked transport source file.

Copied from #715's build.py (36f7ce01) with four recorded changes: `cand` is the
surviving revision r8 (`95f5b6b7`, #734's survivor, unchanged by #735); `prev`
and the macOS build are dropped (no loopback-bottleneck stage and no macOS
stage run here); the artifact directory is this ticket's; and the aids this
record copies from #715 and #735 are checked byte-identical against those
records, beside #715's existing checks against #712. Each variant is an
exported source tree under .local with a copied campaign module whose quic-go
replacement points at that tree. Readiness stages use plain builds. The `-diag`
variants add the #710 measurement-only occupancy overlay and 10 ms runtime
series to every arm alike, for attribution stages only.

Usage: build.py [variant ...]   (default: every variant, then relay)
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
ART = ROOT / '.local/bbr-r8-linux-redemonstration'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
REVISIONS = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',  # frozen matched reference
    'cand': '95f5b6b7a0689a035c29d859f4e026469e20d20d',  # surviving revision r8 (#734, kept by #735): BBRv3 and default Reno
}
# (revision, diag overlay, timeline overlay)
VARIANTS = {name: (rev, False, False) for name, rev in REVISIONS.items()}
VARIANTS.update({'reno-diag': (REVISIONS['reno'], True, False), 'cand-diag': (REVISIONS['cand'], True, False)})
# Attribution only: diag overlay plus the demonstration's phase-timeline counting overlay.
VARIANTS['cand-diag-counted'] = (REVISIONS['cand'], True, False)
# S5 goodput stage only: plain build plus the #715 timeline overlay.
VARIANTS['cand-timeline'] = (REVISIONS['cand'], False, True)
RELAY_V1_SOURCE = '19635ff6847efd3b778184b2eddae2cbb7baeb29de844cd25d906e4c0045ec61'
DIAGNOSTIC = '4323dae8'
DIAGNOSTIC_DIR = 'docs/audits/2026-10-03-bbr-linux-diagnostic'
COPIED = ['run.py', 'analyze.py', 'attribution.py', 'localize.py', 'localize_test.py', 'prereq.py', 'pack.py',
          'calibrate/main.go', 'launch/main.go', 'relay/main.go', 'relay/main-v1.go.src', 'relay/linux-ecn.patch',
          'overlay/diag_occupancy.go', 'overlay/diag_series.go', 'counting/service_work_on.go', 'counting/c4_work_on.go',
          'counting/c4_work_off.go', 'model-overlay/next.go']
# Copied byte-identical from #715's record (36f7ce01).
PRIOR = '36f7ce01'
PRIOR_DIR = 'docs/audits/2026-10-05-bbr-linux-redemonstration'
PRIOR_COPIED = ['rules.py', 'rules_test.py', 'stage_run.py', 'timeline/bbr_timeline.go', 'timeline/emission_timeline.go', 'go.mod']
# Copied byte-identical from #735's record (f3840c83): the receiver measure.
LEADS = 'f3840c83'
LEADS_DIR = 'docs/audits/2026-10-06-bbr-per-packet-cpu-leads'
LEADS_COPIED = ['receiver.py', 'receiver_test.py']
LINUX = {'GOOS': 'linux', 'GOARCH': 'amd64'}
ENV = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod', 'CGO_ENABLED': '0', 'GOWORK': 'off'}


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


# Verbatim from the demonstration's build.py at 80466857.
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


def counting(tree):
    shutil.copy(HERE / 'counting/service_work_on.go', tree / 'internal/ackhandler/service_work_on.go')
    shutil.copy(HERE / 'counting/c4_work_on.go', tree / 'internal/congestion/c4_work_on.go')
    shutil.copy(HERE / 'counting/c4_work_off.go', tree / 'internal/congestion/c4_work_off.go')
    c4_hooks(tree)
    subprocess.run(['gofmt', '-l', '-w', 'internal/ackhandler', 'internal/congestion'], cwd=tree, check=True)


def go_build(module, pkg, binary, label, platform=LINUX, tags=()):
    assert not binary.exists(), f'{binary} exists; builds never overwrite'
    # -buildvcs=false: binaries must not depend on the enclosing worktree's HEAD.
    cmd = ['go', 'build', '-trimpath', '-buildvcs=false', *tags, '-ldflags', '-X main.sourceRevision=' + label, '-o', str(binary), pkg]
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
    shutil.copy(HERE / 'model-overlay/next.go', module / 'model/next.go')
    return full_rev, tree, module


def fixture(name, platform=LINUX, out_name=None):
    rev, diag, tl = VARIANTS[name]
    full_rev, tree, module = export(out_name or name, rev)
    counted = name.endswith('-counted')
    if diag:
        overlay(tree, module)
        subprocess.run(['gofmt', '-l', '.'], cwd=tree, check=True)
    if counted:
        counting(tree)
    if tl:
        timeline(tree)
    label = full_rev + ('+diag' if diag else '') + ('+counted' if counted else '') + ('+timeline' if tl else '')
    return dict(variant=out_name or name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH),
                overlays=(['overlay/diag_occupancy.go', 'overlay/diag_series.go'] if diag else [])
                + (['counting/service_work_on.go', 'counting/c4_work_on.go', 'counting/c4_work_off.go'] if counted else [])
                + (['timeline/bbr_timeline.go', 'timeline/emission_timeline.go'] if tl else []),
                c4_hooks=counted,
                builds={'fixture': go_build(module, './fixture', ART / 'bin' / f'{out_name or name}-fixture', label, platform,
                                            ('-tags', 'bbrworkcount') if counted else ())})


def build(name):
    (ART / 'bin').mkdir(parents=True, exist_ok=True)
    # Aids copied from the #712 record must be byte-identical to it.
    for rel in COPIED:
        assert (HERE / rel).read_bytes() == git('show', f'{DIAGNOSTIC}:{DIAGNOSTIC_DIR}/{rel}'), rel
    for rel in PRIOR_COPIED:
        assert (HERE / rel).read_bytes() == git('show', f'{PRIOR}:{PRIOR_DIR}/{rel}'), rel
    for rel in LEADS_COPIED:
        assert (HERE / rel).read_bytes() == git('show', f'{LEADS}:{LEADS_DIR}/{rel}'), rel
    assert hashlib.sha256((HERE / 'relay/main-v1.go.src').read_bytes()).hexdigest() == RELAY_V1_SOURCE
    if name == 'relay':
        # The relay model is the frozen Q1 model; build from the frozen tree.
        # relay-v3: #711's relay, unadapted (calibration negative control only).
        # relay-linux: relay-v3 plus the Linux ECN adapter, used for every WAN arm.
        out = {}
        for label in ['relay-v3', 'relay-linux']:
            full_rev, tree, module = export(label, REVISIONS['reno'])
            (module / 'relay').mkdir()
            shutil.copy(HERE / 'relay/main.go', module / 'relay/main.go')
            if label == 'relay-linux':
                subprocess.run(['patch', '-p1', '--forward', '--batch', '-i', str(HERE / 'relay/linux-ecn.patch')], cwd=module, check=True)
            out[label] = go_build(module, './relay', ART / 'bin' / label, full_rev)
        (module / 'calibrate').mkdir()
        shutil.copy(HERE / 'calibrate/main.go', module / 'calibrate/main.go')
        out['calibrate'] = go_build(module, './calibrate', ART / 'bin' / 'calibrate', full_rev)
        (module / 'launch').mkdir()
        shutil.copy(HERE / 'launch/main.go', module / 'launch/main.go')
        out['launch'] = go_build(module, './launch', ART / 'bin' / 'launch', full_rev)
        receipt = dict(variant='relay', revision=full_rev, builds=out)
    else:
        receipt = fixture(name)
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(name, 'built')


for v in sys.argv[1:] or [*VARIANTS, 'relay']:
    build(v)
