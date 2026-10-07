#!/usr/bin/env python3
"""Finite #738 builds; never edits a tracked transport source file.

Adapted from #734's build.py (04094bd7). Fixture variants are exported source
trees under .local with a copied campaign module whose quic-go replacement
points at that tree, exactly as #715, #734 and #736 built them. `reno`, `cand`
(r8) and `prev` (`d0fabc4d`) must rebuild byte-identical to the prior records'
binaries. New here:

- `cand-hand-timeline`: r8 plus this record's hand-off recorder
  (hand/handrec.go, hand/hand_hooks.go), which extends #734's loopback
  recorder (`cand-lb-timeline`): it computes #734's window aggregates from the
  same two clock reads per opportunity and adds the credit/worker hand-off.
  The `-timeline` suffix makes #715's unchanged run_case_x set
  TIMELINE_OUTPUT for the sender, which enables it.
- `prev-hand-timeline`: the same on `d0fabc4d`.
- `cand4q-hand-timeline`: the diagnostic pending-bound arm, r8 with ordinary
  pending local work bounded at 4Q instead of 2Q (both `2*e.bbr.quantum` in
  packet_emission_bbr.go), instrumented; handBoundQ = 4. Never kept.
- `syn-*-timeline`: the Stage 0 synthetic cases, `cand-hand-timeline` plus one
  injection each (hand/synth.go; SYNTH below).
- `gate-r8`, `gate-4q`, `gate-m1`: exported test trees for the diagnostic
  arm's gates (r8 with the variant tests at 2Q; the arm with them at 4Q and
  r8's four 2Q-encoding tests adapted; the arm plus an injected second
  violation, which the gates must detect). gates.sh runs them.
- `relay`: #715's relay-linux and launcher, rebuilt from the frozen tree.

Usage: build.py [variant ...]   (default: every variant, relay and gate tree)
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
ART = ROOT / '.local/bbr-loopback-handoff'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
REVISIONS = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',  # frozen matched reference
    'cand': '95f5b6b7a0689a035c29d859f4e026469e20d20d',  # surviving revision r8 (#734, kept by #735 and #737)
    'prev': 'd0fabc4dbab24a68af9a1960348423dbf0ce8388',  # r6 (#714), r8 without the Linux kick
}
# Stage 0 synthetic injections (README, "Synthetic cases"): spin and delay nanoseconds, cwnd cap bytes.
SYNTH = {
    'slowworker': dict(synSlowWorkerNS=20_000),
    'busyloop': dict(synBusyLoopNS=20_000),
    'delaysignal': dict(synSignalDelayNS=50_000),
    'cwnd': dict(cwnd=12_000),
    'mixed': dict(synSignalDelayNS=100_000, cwnd=8_000, synAlternate=True, synPhaseNS=2_000_000_000),
}
# (revision, recorders, pending bound in Q, synthetic case)
VARIANTS = {name: (rev, False, 2, None) for name, rev in REVISIONS.items()}
VARIANTS['cand-hand-timeline'] = (REVISIONS['cand'], True, 2, None)
VARIANTS['prev-hand-timeline'] = (REVISIONS['prev'], True, 2, None)
VARIANTS['cand4q-hand-timeline'] = (REVISIONS['cand'], True, 4, None)
for case in SYNTH:
    VARIANTS[f'syn-{case}-timeline'] = (REVISIONS['cand'], True, 2, case)
# Development only (excluded, disclosed): recorder parts switched off to locate its cost.
DEV_PARTS = {'dev-p1-timeline': 1, 'dev-p3-timeline': 3, 'dev-p7-timeline': 7, 'dev-p9-timeline': 9}
DEV_MASK = {'dev-m0-timeline': 0}
for name in [*DEV_PARTS, *DEV_MASK]:
    VARIANTS[name] = (REVISIONS['cand'], True, 2, None)
DEV_SYNTH = {'devmixedb': dict(synSignalDelayNS=100_000, cwnd=9_000, synAlternate=True, synPhaseNS=1_000_000_000)}
SYNTH.update(DEV_SYNTH)
for case in DEV_SYNTH:
    VARIANTS[f'syn-{case}-timeline'] = (REVISIONS['cand'], True, 2, case)
GATE_TREES = ['gate-r8', 'gate-4q', 'gate-m1']
# Aids copied byte-identical from #736's record (3bf2245e), which copied them from #712/#715.
RECORD = '3bf2245e'
RECORD_DIR = 'docs/audits/2026-10-06-bbr-r8-linux-redemonstration'
COPIED = ['run.py', 'stage_run.py', 'localize.py', 'pack.py', 'launch/main.go', 'relay/main.go', 'relay/main-v1.go.src',
          'relay/linux-ecn.patch']
MODEL_OVERLAY = ('36f7ce01', 'docs/audits/2026-10-05-bbr-linux-redemonstration/model-overlay/next.go')  # as #734 built
# Prior records' build receipts: rebuilt binaries must match.
EXPECTED = {
    'reno': 'cda8feb1',         # #715, #734, #736
    'cand': 'b7171142',         # r8 as #734, #735 and #736 built it
    'prev': 'b5defc02',         # d0fabc4d as #715 and #734 built it
    'relay-linux': '24ee941d',  # #715, #734, #736
    'launch': 'a6146d62',       # #715, #734, #736
}
LINUX = {'GOOS': 'linux', 'GOARCH': 'amd64'}
ENV = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod', 'CGO_ENABLED': '0', 'GOWORK': 'off'}


def git(*args, **kw):
    return subprocess.check_output(['git', *args], cwd=ROOT, **kw)


def replace(path, old, new, count=1):
    s = path.read_text()
    assert s.count(old) == count, (path, old, s.count(old))
    path.write_text(s.replace(old, new))


def four_q(tree):
    """The diagnostic arm's one deviation from D08: ordinary pending local work bounded at 4Q."""
    replace(tree / 'packet_emission_bbr.go', '2*e.bbr.quantum', '4*e.bbr.quantum', count=2)


def hand(tree, bound_q, case):
    """#738 hand-off recorder, and a synthetic injection for syn-* variants."""
    src = (HERE / 'hand/handrec.go').read_text()
    if bound_q != 2:
        assert src.count('const handBoundQ = 2\n') == 1
        src = src.replace('const handBoundQ = 2\n', f'const handBoundQ = {bound_q}\n')
    if name_parts := DEV_PARTS.get(tree.name):
        assert src.count('const handParts = 15\n') == 1
        src = src.replace('const handParts = 15\n', f'const handParts = {name_parts}\n')
    if (mask := DEV_MASK.get(tree.name)) is not None:
        assert src.count('const handMask = 7\n') == 1
        src = src.replace('const handMask = 7\n', f'const handMask = {mask}\n')
    (tree / 'handrec.go').write_text(src)
    shutil.copy(HERE / 'hand/hand_hooks.go', tree / 'hand_hooks.go')
    syn = SYNTH[case] if case else {}
    # Connection loop: blocked in the run-loop select, its wakes, and the run loop's own full-queue check.
    conn = tree / 'connection.go'
    sel = '\t\t\tselect {\n\t\t\tcase <-c.closeChan:\n\t\t\t\tbreak runLoop\n\t\t\tcase <-c.timer.C:\n'
    replace(conn, sel, '\t\t\thandBlocked()\n' + sel)
    for case_line, k in [('case <-c.timer.C:\n', 1), ('case <-c.sendingScheduled:\n', 2), ('case <-c.handshakeSendFeedback.wakeup:\n', 3),
                         ('case <-sendQueueAvailable:\n', 4), ('case <-c.notifyReceivedPacket:\n', 5)]:
        replace(conn, '\t\t\t' + case_line, '\t\t\t' + case_line + f'\t\t\t\thandWoken({k})\n')
    replace(conn, '\t\tif available := c.emission.capacity(); available != nil {\n',
            '\t\tif available := c.emission.capacity(); available != nil {\n\t\t\thandSetReason(hcQueue)\n')
    # Emission: opportunities, their stops, and handed-off entries.
    em = tree / 'packet_emission_bbr.go'
    anchor = 'func (e *packetEmission) sendBounded(now monotime.Time, confirmed bool) (result emissionResult) {\n'
    spin = '\tsynSpin(synBusyLoopNS)\n' if syn.get('synBusyLoopNS') else ''
    replace(em, anchor, anchor + '\thandOpp(now)\n\tdefer func() { handExit(result, e) }()\n' + spin)
    replace(em, '\t\te.reservation.resize(buf.Len())\n', '\t\thandEntry(int64(buf.Len()), int64(gso))\n\t\te.reservation.resize(buf.Len())\n')
    # Local send credit: admissions, refusals, completions and their signal.
    cr = tree / 'local_send_credit.go'
    replace(cr, '\tc.pending += n\n\tc.current += n\n', '\tc.pending += n\n\tc.current += n\n\thandPending(int64(c.pending), int64(limit))\n')
    refuse = '\tif !c.canReserve(n, limit, ordinary, isolated) {\n\t\t// Test capacity and consume stale wakeups under the completion lock.\n'
    replace(cr, refuse, refuse + '\t\thandRefused(int64(c.pending))\n')
    signal = '\tif signal && released != 0 {\n\t\tselect {\n\t\tcase c.available <- struct{}{}:\n\t\tdefault:\n\t\t}\n\t}\n'
    replace(cr, signal, '\tif signal && released != 0 {\n\t\tselect {\n\t\tcase c.available <- struct{}{}:\n'
                        '\t\t\thandCreditSignaled()\n\t\tdefault:\n\t\t}\n\t}\n')
    # Send worker: idle/busy clock, sampled group timing, and the freed-slot signal.
    sq = tree / 'send_queue.go'
    replace(sq, '\t\tselect {\n\t\tcase <-h.closeCalled:\n', '\t\thandWorkerIdle(len(h.queue))\n\t\tselect {\n\t\tcase <-h.closeCalled:\n')
    held = ''
    if syn.get('synSignalDelayNS'):
        held = '\t\t\tsynDequeued(len(h.queue) == sendQueueCapacity-1, h.available)\n'
        replace(sq, '\treturn len(h.queue) == sendQueueCapacity\n}\n', '\treturn len(h.queue) == sendQueueCapacity || synHeld()\n}\n')
    replace(sq, '\t\tcase e := <-h.queue:\n', '\t\tcase e := <-h.queue:\n\t\t\thandWorkerDequeued()\n' + held)
    replace(sq, '\te.buf.Release()\n\te.metadata.credit.complete()\n}\n', '\te.buf.Release()\n\te.metadata.credit.complete()\n\thandCompleted()\n}\n')
    run_signal = '\t\t\te.release()\n\t\t\tselect {\n\t\t\tcase h.available <- struct{}{}:\n\t\t\tdefault:\n\t\t\t}\n'
    if syn.get('synSignalDelayNS'):
        replace(sq, run_signal, '\t\t\te.release()\n\t\t\tif !synHeld() {\n\t\t\t\tselect {\n\t\t\t\tcase h.available <- struct{}{}:\n'
                                '\t\t\t\t\thandQueueSignaled()\n\t\t\t\tdefault:\n\t\t\t\t}\n\t\t\t}\n')
    else:
        replace(sq, run_signal, '\t\t\te.release()\n\t\t\tselect {\n\t\t\tcase h.available <- struct{}{}:\n\t\t\t\thandQueueSignaled()\n'
                                '\t\t\tdefault:\n\t\t\t}\n')
    slow = '\tsynSpin(synSlowWorkerNS)\n' if syn.get('synSlowWorkerNS') else ''
    replace(sq, '\tif err := h.conn.Write(e.buf.Data, e.gsoSize, e.ecn); err != nil {\n',
            '\thandSubmit()\n' + slow + '\terr := h.conn.Write(e.buf.Data, e.gsoSize, e.ecn)\n\thandSubmitted()\n\tif err != nil {\n')
    if case:
        s = (HERE / 'hand/synth.go').read_text()
        lines = []
        for line in s.splitlines(keepends=True):
            name = line.strip().split(' ')[0]
            if name in ('synSlowWorkerNS', 'synBusyLoopNS', 'synSignalDelayNS') and line.rstrip().endswith('= 0'):
                line = f'\t{name} = {syn.get(name, 0)}\n'
            if name == 'synAlternate' and line.rstrip().endswith('= false'):
                line = f"\tsynAlternate = {'true' if syn.get('synAlternate') else 'false'}\n"
            if name == 'synPhaseNS' and line.rstrip().endswith('= 1_000_000_000'):
                line = f"\tsynPhaseNS = {syn.get('synPhaseNS', 1_000_000_000)}\n"
            lines.append(line)
        out = ''.join(lines)
        for k in ['synSlowWorkerNS', 'synBusyLoopNS', 'synSignalDelayNS']:
            assert out.count(f'\t{k} = {syn.get(k, 0)}\n') == 1, k
        assert out.count(f"\tsynAlternate = {'true' if syn.get('synAlternate') else 'false'}\n") == 1
        (tree / 'synth.go').write_text(out)
        if 'cwnd' in syn:
            sph = tree / 'internal/ackhandler/sent_packet_handler.go'
            replace(sph, '\tallowance := max(0, h.congestion.GetCongestionWindow()-h.bytesInFlight)\n',
                    '\tallowance := max(0, min(h.congestion.GetCongestionWindow(), synCwnd())-h.bytesInFlight)\n')
            alt = 'true' if syn.get('synAlternate') else 'false'
            (tree / 'internal/ackhandler/zz_synth_cwnd.go').write_text(
                'package ackhandler\n\nimport (\n\t"math"\n\n\t"github.com/quic-go/quic-go/internal/monotime"\n'
                '\t"github.com/quic-go/quic-go/internal/protocol"\n)\n\n'
                '// Measurement-only #738 synthetic case: the BBR send allowance sees at most this congestion window\n'
                '// (in the mixed case, only in even seconds of the monotonic clock).\n'
                f'const synCwndCap protocol.ByteCount = {syn["cwnd"]}\n\nconst synAlternate = {alt}\n\n'
                f'const synPhaseNS = {syn.get("synPhaseNS", 1_000_000_000)}\n\n'
                'func synCwnd() protocol.ByteCount {\n\tif synAlternate && (int64(monotime.Now())/synPhaseNS)&1 == 1 {\n'
                '\t\treturn math.MaxInt64\n\t}\n\treturn synCwndCap\n}\n')
    subprocess.run(['gofmt', '-l', '-w', '.'], cwd=tree, check=True, capture_output=True)


def adapt_tests_4q(tree):
    """r8's four tests that encode the 2Q number, restated for the tree's bound (variantBoundQ)."""
    t = tree / 'bbr_send_policy_test.go'
    replace(t, '\t\t\t\tc.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)\n\t\t\t\t// Full datagrams make the physical-byte bound visible at the real packer.\n',
            '\t\t\t\tc.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)\n'
            '\t\t\t\tif variantBoundQ != 2 {\n\t\t\t\t\tt.Skip("restated for the variant bound by TestBBRVariantPendingBoundDrainRefill")\n\t\t\t\t}\n'
            '\t\t\t\t// Full datagrams make the physical-byte bound visible at the real packer.\n')
    replace(t, 'held := c.emission.bbr.credit.reserve(3601, 4800, false, isolated)',
            'held := c.emission.bbr.credit.reserve(variantBoundQ*2400-1199, variantBoundQ*2400, false, isolated)')
    replace(t, 'held := c.emission.bbr.credit.reserve(3000, 4800, true, false)',
            'held := c.emission.bbr.credit.reserve(variantBoundQ*2400-1800, variantBoundQ*2400, true, false)')
    replace(tree / 'delivery_sampling_test.go', 'r := c.emission.bbr.credit.reserve(4800, 4800, true, false)',
            'r := c.emission.bbr.credit.reserve(variantBoundQ*2400, variantBoundQ*2400, true, false)')


def mutant_m1(tree):
    """The injected second violation: credit returned at dequeue, not after local acceptance or rejection."""
    replace(tree / 'send_queue.go', '\t\tcase e := <-h.queue:\n',
            '\t\tcase e := <-h.queue:\n\t\t\te.metadata.credit.complete() // #738 mutant m1: credit returned at dequeue\n\t\t\te.metadata.credit = nil\n')


def gate_tree(name):
    full_rev = git('rev-parse', REVISIONS['cand'], text=True).strip()
    tree = ART / 'src' / name
    assert not tree.exists(), f'{tree} exists; builds never overwrite a prior tree'
    tree.mkdir(parents=True)
    subprocess.run(['tar', '-x', '-C', str(tree)], input=git('archive', '--format=tar', full_rev), check=True)
    bound = 2 if name == 'gate-r8' else 4
    if bound == 4:
        four_q(tree)
        adapt_tests_4q(tree)
    if name == 'gate-m1':
        mutant_m1(tree)
    shutil.copy(HERE / 'gates/pending_bound_variant_test.go', tree / 'zz_pending_bound_variant_test.go')
    (tree / 'zz_variant_bound_test.go').write_text(f'package quic\n\nconst variantBoundQ = {bound}\n')
    receipt = dict(variant=name, revision=full_rev, bound_q=bound, adapted_2q_tests=bound == 4, mutant='m1' if name == 'gate-m1' else None,
                   files={p: hashlib.sha256((tree / p).read_bytes()).hexdigest() for p in
                          ['packet_emission_bbr.go', 'send_queue.go', 'bbr_send_policy_test.go', 'delivery_sampling_test.go',
                           'zz_pending_bound_variant_test.go', 'zz_variant_bound_test.go']})
    return receipt


def go_build(module, pkg, binary, label, platform=LINUX):
    assert not binary.exists(), f'{binary} exists; builds never overwrite'
    # -buildvcs=false: binaries must not depend on the enclosing worktree's HEAD.
    cmd = ['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags', '-X main.sourceRevision=' + label, '-o', str(binary), pkg]
    subprocess.run(cmd, cwd=module, env={**ENV, **platform}, check=True)
    return dict(binary=str(binary.relative_to(ART)), sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), command=cmd,
                platform=f"{platform['GOOS']}/{platform['GOARCH']}")


def model_overlay():
    p = ART / 'model-overlay-next.go'
    if not p.exists():
        p.write_bytes(git('show', ':'.join(MODEL_OVERLAY)))
    return p


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
    subprocess.run(['patch', '-p1', '--forward', '--batch', '-i', 'heap.patch'], cwd=module, check=True, capture_output=True)
    shutil.copy(model_overlay(), module / 'model/next.go')
    return full_rev, tree, module


def fixture(name):
    rev, rec, bound_q, case = VARIANTS[name]
    full_rev, tree, module = export(name, rev)
    if bound_q != 2:
        four_q(tree)
    if rec:
        hand(tree, bound_q, case)
    label = full_rev + (f'+{bound_q}q' if bound_q != 2 else '') + ('+hand' if rec else '') + (f'+syn-{case}' if case else '')
    return dict(variant=name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH), bound_q=bound_q, synthetic=case,
                synthetic_parameters=SYNTH.get(case), recorders=['hand/handrec.go', 'hand/hand_hooks.go'] + (['hand/synth.go'] if case else []) if rec else [],
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
        subprocess.run(['patch', '-p1', '--forward', '--batch', '-i', str(HERE / 'relay/linux-ecn.patch')], cwd=module, check=True, capture_output=True)
        out['relay-linux'] = go_build(module, './relay', ART / 'bin' / 'relay-linux', full_rev)
        (module / 'launch').mkdir()
        shutil.copy(HERE / 'launch/main.go', module / 'launch/main.go')
        out['launch'] = go_build(module, './launch', ART / 'bin' / 'launch', full_rev)
        receipt = dict(variant='relay', revision=full_rev, builds=out)
    elif name in GATE_TREES:
        receipt = gate_tree(name)
        receipt['builds'] = {}
    else:
        receipt = fixture(name)
    receipt['go'] = subprocess.check_output(['go', 'version'], cwd=ART, env=ENV, text=True).strip()
    for k, b in receipt['builds'].items():
        key = name if k == 'fixture' else k
        if key in EXPECTED:
            receipt.setdefault('matches_prior', {})[key] = b['sha256'].startswith(EXPECTED[key])
            assert b['sha256'].startswith(EXPECTED[key]), (key, b['sha256'])
    (ART / 'bin' / f'{name}-build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(name, 'built')


DEV = {*DEV_PARTS, *DEV_MASK, *(f'syn-{c}-timeline' for c in DEV_SYNTH)}
for v in sys.argv[1:] or [*(v for v in VARIANTS if v not in DEV), 'relay', *GATE_TREES]:
    build(v)
