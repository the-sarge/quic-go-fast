#!/usr/bin/env python3
"""Finite #990 builds from immutable source exports, never tracked product files.

Adapted from #808: old retains frozen Reno; reno and cand both pin the intended
bundle. New plain/diag/timeline hashes are registered below. The relay remains
built from old frozen source. Copied aids have explicit hashes; the ECN patch
is compressed losslessly. Historical overlay functions are retained unchanged.
"""
import hashlib
import gzip
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
ART = ROOT / '.local/bundle-990-readiness'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
REVISIONS = {
    'old': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',
    'reno': '8095c72244755f0c1e24f4158253bf09cfbe6e17',  # frozen matched reference
    'cand': '8095c72244755f0c1e24f4158253bf09cfbe6e17',  # combined revision on main (#808): BBRv3 and default Reno
}
# (revision, diag overlay, timeline overlay)
VARIANTS = {name: (rev, False, False) for name, rev in REVISIONS.items()}
VARIANTS.update({'reno-diag': (REVISIONS['reno'], True, False), 'cand-diag': (REVISIONS['cand'], True, False)})
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
# Copied byte-identical from #736's record (3bf2245e): every aid this record does not adapt.
R8 = '3bf2245e'
R8_DIR = 'docs/audits/2026-10-06-bbr-r8-linux-redemonstration'
R8_COPIED = ['harness.py', 'harness_test.py']
# Copied byte-identical from #739's record (138223ac): every aid, including #739's adapted order and analysis.
R9 = '138223ac'
R9_DIR = 'docs/audits/2026-10-08-bbr-r9-linux-redemonstration'
R9_COPIED = COPIED + PRIOR_COPIED + LEADS_COPIED + R8_COPIED + ['matrix.py', 'follow.py']
# Copied byte-identical from slice 6's record (9b38f0ab): every aid this record does not adapt.
SLICE6 = '9b38f0ab2f7e7a88ecb45dc66a46f895a8aad0cb'
SLICE6_DIR = 'docs/audits/2026-10-16-bbr-port-readiness'
SLICE6_COPIED = R9_COPIED + ['stages.py', 'resume.py', 'resume_test.py']
# This record's own binaries, registered after their first build (None until then).
REGISTERED = {('reno-diag', 'fixture'): 'cfb922ad245c03fe56d61e6f2a38eb28f982c325e27c0c8c9c0d0d9401e4ee59', ('cand-timeline', 'fixture'): 'fc2f51e095241b7a8a5ade8c7dd3f60dfb227a8a9a54c2cf79da46dcdee88d8c', ('cand', 'fixture'): '6ad82807ef4eefa837b082d7bcc758a97eee57f720fb45e71720c6283b215854', ('cand-diag', 'fixture'): 'cfb922ad245c03fe56d61e6f2a38eb28f982c325e27c0c8c9c0d0d9401e4ee59', ('reno', 'fixture'): '6ad82807ef4eefa837b082d7bcc758a97eee57f720fb45e71720c6283b215854'}

# Binaries this record rebuilds from a revision and overlays a prior record already built (SHA-256).
EXPECTED = {
    ('old', 'fixture'): 'cda8feb1b2fb004d72425adee5edeffb64a4c437f3ef07f9eb4e686af3ab5c8b',  # #715, #736, #738, #739
    ('relay', 'relay-v3'): '033d467c36157a04ca82e60d213ebae1e12ca9900193a6d1cdb6fbcb5d9299d6',
    ('relay', 'relay-linux'): '24ee941d2c63c2595b62fcd8abdf00cb7aec5d346b82d67cec4d2a1dc1fcd41e',
    ('relay', 'calibrate'): '4218b433f3d7d04c65521d503a84cc693987f28c0fe2a175d8f69cdfcff069ff',
    ('relay', 'launch'): 'a6146d62e16339f247a7e5fdac29ff47a66be7d8b6fc8d5e647d0479a58d5917',
}
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
    manifest = json.loads((HERE / 'copied-aids.json').read_text())
    for rel, digest in manifest.items():
        assert hashlib.sha256((HERE / rel).read_bytes()).hexdigest() == digest, rel
    assert hashlib.sha256((HERE / 'relay/main-v1.go.src').read_bytes()).hexdigest() == RELAY_V1_SOURCE
    if name == 'relay':
        # The relay model is the frozen Q1 model; build from the frozen tree.
        # relay-v3: #711's relay, unadapted (calibration negative control only).
        # relay-linux: relay-v3 plus the Linux ECN adapter, used for every WAN arm.
        out = {}
        for label in ['relay-v3', 'relay-linux']:
            full_rev, tree, module = export(label, REVISIONS['old'])
            (module / 'relay').mkdir()
            shutil.copy(HERE / 'relay/main.go', module / 'relay/main.go')
            if label == 'relay-linux':
                subprocess.run(['patch', '-p1', '--forward', '--batch'], input=gzip.decompress((HERE / 'relay/linux-ecn.patch.gz').read_bytes()), cwd=module, check=True)
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
    for key, b in receipt['builds'].items():
        if (name, key) in EXPECTED:
            assert b['sha256'] == EXPECTED[(name, key)], (name, key, b['sha256'])
            b['byte_identical_to_prior_record'] = True
        if REGISTERED.get((name, key)):
            assert b['sha256'] == REGISTERED[(name, key)], (name, key, b['sha256'])
            b['registered'] = True
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(name, 'built')


if __name__ == '__main__':
    for v in sys.argv[1:] or [*VARIANTS, 'relay']:
        build(v)
