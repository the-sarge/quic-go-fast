#!/usr/bin/env python3
"""Finite #740 memory-attribution builds; never edits a tracked transport source file.

Adapted from #739's build.py (138223ac). Each variant is an exported source
tree under .local with a copied campaign module whose quic-go replacement
points at that tree. Changes, all recorded:
- variants: `reno` and `cand` (plain, rebuilt only to assert byte-identity
  with #739's binaries), `reno-mem` and `cand-mem` (the #740 memory accounting
  overlay, mem/), and `cand-mem-ring` (cand-mem with the diagnostic 4,096-entry
  outcome ring, never kept);
- the memory overlay adds the #710 occupancy hooks (overlay/diag_occupancy.go,
  unchanged) without #710's 10 ms in-memory series, a streaming per-second
  sampler (mem/mem_series.go), the receiver-controller switch, and, in
  candidate builds only, the BBR structure accounting (mem/mem_ackhandler.go)
  and the phase log (mem/mem_phase.go);
- every aid copied unchanged from #739's record is checked byte-identical to it.

Usage: build.py [variant ...]   (default: every variant, then synthetic and relay)
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
ART = ROOT / '.local/bbr-r9-memory-attribution'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
REVISIONS = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',  # frozen matched reference
    'cand': '81e9dc8c7154fe13227386866f83c48dcc75c330',  # surviving revision r9 (#738 deviation 4): BBRv3 and default Reno
}
# name -> (revision, overlays)
VARIANTS = {'reno': ('reno', ()), 'cand': ('cand', ()), 'reno-mem': ('reno', ('mem',)), 'cand-mem': ('cand', ('mem',)),
            'cand-mem-ring': ('cand', ('mem', 'ring'))}
RING_ENTRIES = 4096  # the registered diagnostic ring size: the smallest the indexes support (power of two, >= 4096)
RELAY_V1_SOURCE = '19635ff6847efd3b778184b2eddae2cbb7baeb29de844cd25d906e4c0045ec61'
# Copied byte-identical from #739's record (138223ac).
R9 = '138223ac'
R9_DIR = 'docs/audits/2026-10-08-bbr-r9-linux-redemonstration'
R9_COPIED = ['run.py', 'analyze.py', 'attribution.py', 'localize.py', 'localize_test.py', 'prereq.py', 'pack.py', 'rules.py',
             'rules_test.py', 'stage_run.py', 'go.mod', 'harness.py', 'harness_test.py', 'calibrate/main.go', 'launch/main.go',
             'relay/main.go', 'relay/main-v1.go.src', 'relay/linux-ecn.patch', 'overlay/diag_occupancy.go', 'model-overlay/next.go',
             'stages.py', 'receiver.py', 'receiver_test.py']
# Binaries this record rebuilds from a revision a prior record already built (SHA-256): #739's.
EXPECTED = {
    ('reno', 'fixture'): 'cda8feb1b2fb004d72425adee5edeffb64a4c437f3ef07f9eb4e686af3ab5c8b',  # #715, #736, #738
    ('cand', 'fixture'): '4fa753223fdc777265e3ba1306f025e82fdf382a5d22f8a8992b8d0a72c5c1b6',  # #738's `new` (r9)
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


def mem(tree, module, candidate):
    """#740 measurement-only memory overlay (see the module docstring)."""
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
    shutil.copy(HERE / 'mem/mem_series.go', module / 'fixture/mem_series.go')
    main = module / 'fixture/main.go'
    replace(main, '\tstarted := time.Now()\n', '\tstarted := time.Now()\n\tstopMem := startMemSeries()\n')
    replace(main, '\t<-heapDone\n\tcloseTransport()\n', '\t<-heapDone\n\tstopMem()\n\tcloseTransport()\n')
    replace(module / 'fixture/session.go', '\t_ = c.SetCongestionControlV1(r.Controller)\n',
            '\t_ = c.SetCongestionControlV1(memController(r.Controller))\n')
    for role in ('receiveSession', 'sendSession'):
        replace(module / 'fixture/session.go',
                f'func {role}(ctx context.Context, conn *quic.Conn, cfg Run) (Result, error) {{\n',
                f'func {role}(ctx context.Context, conn *quic.Conn, cfg Run) (Result, error) {{\n\tmemConn.Store(conn)\n')
    if not candidate:
        shutil.copy(HERE / 'mem/mem_root_reno.go', tree / 'mem_root.go')
        return
    shutil.copy(HERE / 'mem/mem_root.go', tree / 'mem_root.go')
    shutil.copy(HERE / 'mem/mem_ackhandler.go', tree / 'internal/ackhandler/mem_ackhandler.go')
    shutil.copy(HERE / 'mem/mem_phase.go', tree / 'internal/congestion/mem_phase.go')
    replace(tree / 'internal/ackhandler/congestion_dispatch.go',
            '\td.sink.Sent(congestion.SendEvent{Packet: info, PriorInFlight: prior, PostInFlight: h.bytesInFlight})\n',
            '\td.sink.Sent(congestion.SendEvent{Packet: info, PriorInFlight: prior, PostInFlight: h.bytesInFlight})\n\tmemNote(d)\n')
    replace(tree / 'internal/congestion/bbr_sender.go', 'func (b *BBRSender) Feedback(e FeedbackEvent) {\n',
            'func (b *BBRSender) Feedback(e FeedbackEvent) {\n\tdefer func() { memPhase(b.phase) }()\n')


def ring(tree):
    """The diagnostic ring treatment: one deviation, a smaller outcome ring (earlier eviction)."""
    replace(tree / 'internal/ackhandler/bbr_recovery.go', '\tmaxRecoveryOutcomes = 32768\n', f'\tmaxRecoveryOutcomes = {RING_ENTRIES}\n')
    # The one test literal that restates the production capacity follows the constant.
    pto = tree / 'internal/ackhandler/bbr_pto_test.go'
    replace(pto, '\t\t\t\tfor range 32768 {\n', '\t\t\t\tfor range maxRecoveryOutcomes {\n')
    replace(pto, 'require.Equal(t, 32768, h.DeliveryStats().OutcomeEntries)', 'require.Equal(t, maxRecoveryOutcomes, h.DeliveryStats().OutcomeEntries)')
    # Declared-domain adaptations (README, "Ring treatment"), test-only:
    # - "retained eviction" registers about 2 x maxDeliveryRetained outcomes after the persistent-congestion
    #   evidence; a ring that small evicts that evidence, which is the treatment's one deviation, so the
    #   case asserts the eviction and the absent span instead of the span.
    replace(pto, '\t\t\twant := mode == "optional live full" || mode == "retained eviction" || mode == "surviving suffix"\n',
            '\t\t\tdeviation := mode == "retained eviction" && maxRecoveryOutcomes <= 2*maxDeliveryRetained\n'
            '\t\t\tif deviation {\n\t\t\t\trequire.Positive(t, h.DeliveryStats().OutcomeEvicted)\n\t\t\t}\n'
            '\t\t\twant := mode == "optional live full" || (mode == "retained eviction" && !deviation) || mode == "surviving suffix"\n')
    # - the slot-set suffix searches start at fixed offsets below the capacity; offsets below zero are outside
    #   a 4,096-entry ring's index domain and are skipped.
    eq = tree / 'internal/ackhandler/recovery_equivalence_test.go'
    replace(eq, '\t\tvar full slotSet\n\t\tfor i := lo; i < maxRecoveryOutcomes; i++ {\n',
            '\t\tif lo < 0 {\n\t\t\tcontinue\n\t\t}\n\t\tvar full slotSet\n\t\tfor i := lo; i < maxRecoveryOutcomes; i++ {\n')


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


def fixture(name, platform=LINUX):
    rev, overlays = VARIANTS[name]
    full_rev, tree, module = export(name, REVISIONS[rev])
    if 'ring' in overlays:
        ring(tree)
    if 'mem' in overlays:
        mem(tree, module, rev == 'cand')
    if overlays:
        subprocess.run(['gofmt', '-l', '-w', '.'], cwd=tree, check=True)
    label = full_rev + ''.join('+' + o for o in overlays)
    files = {'mem': ['overlay/diag_occupancy.go', 'mem/mem_series.go']
             + (['mem/mem_root.go', 'mem/mem_ackhandler.go', 'mem/mem_phase.go'] if rev == 'cand' else ['mem/mem_root_reno.go'])}
    return dict(variant=name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH), overlays=list(overlays),
                overlay_files=[f for o in overlays for f in files.get(o, [])],
                ring_entries=RING_ENTRIES if 'ring' in overlays else 32768,
                builds={'fixture': go_build(module, './fixture', ART / 'bin' / f'{name}-fixture', label, platform)})


def build(name):
    (ART / 'bin').mkdir(parents=True, exist_ok=True)
    # Aids copied from #739's record must be byte-identical to it.
    for rel in R9_COPIED:
        assert (HERE / rel).read_bytes() == git('show', f'{R9}:{R9_DIR}/{rel}'), rel
    assert hashlib.sha256((HERE / 'relay/main-v1.go.src').read_bytes()).hexdigest() == RELAY_V1_SOURCE
    if name == 'synthetic':
        # D6's synthetic accounting cases: the sampler, unchanged, beside a known-cause program, in cand-mem's module.
        module = ART / 'src/cand-mem/campaign'
        (module / 'synthetic').mkdir()
        shutil.copy(HERE / 'mem/mem_series.go', module / 'synthetic/mem_series.go')
        shutil.copy(HERE / 'mem/synthetic/main.go', module / 'synthetic/main.go')
        receipt = dict(variant='synthetic', builds={'synthetic': go_build(module, './synthetic', ART / 'bin/synthetic', 'synthetic')})
    elif name == 'relay':
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
    for key, b in receipt['builds'].items():
        if (name, key) in EXPECTED:
            assert b['sha256'] == EXPECTED[(name, key)], (name, key, b['sha256'])
            b['byte_identical_to_prior_record'] = True
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(name, 'built')


if __name__ == '__main__':
    for v in sys.argv[1:] or [*VARIANTS, 'relay']:
        build(v)
