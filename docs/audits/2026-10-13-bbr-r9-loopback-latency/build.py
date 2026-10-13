#!/usr/bin/env python3
"""Finite #744 builds; never edits a tracked transport source file.

Adapted from #740's build.py (ddbac0a3). Each variant is an exported source
tree under .local with a copied campaign module whose quic-go replacement
points at that tree. Variants, all recorded:
- `reno` and `cand` (plain frozen Reno and plain r9), rebuilt only to assert
  byte-identity with #740's binaries;
- `cand8`: r9 with `bbrSendQueueCapacity = 8`, the one-constant depth control
  (D3; diagnostic, never kept);
- `cand-ctl` and `cand8-ctl`: the same two revisions with the control-path
  recorder (ctl/ctlrec.go and the hooks in `ctl_hooks`);
- `syn-*-ctl`: `cand-ctl` with one registered Stage 0 injection each (SYNTH).
Every aid copied unchanged from #740's record is checked byte-identical to it.

Usage: build.py [variant ...]   (default: every variant, then launch)
       build.py identity        (the depth-identity tests in the exported plain trees)
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
ART = ROOT / '.local/bbr-r9-loopback-latency'
CAMPAIGN = 'docs/audits/2026-09-25-bbrv3-q1-campaign'
HEAP_PATCH = ('798f499878290fc70a0df9db1c067c326ec3b3af', 'docs/audits/2026-09-30-bbr-causal-diagnosis/fixture-heap-diagnostics.patch')
REVISIONS = {
    'reno': 'e4f322cbbfd4225a4b714e08ec19c958cccadcb0',  # frozen matched reference
    'cand': '81e9dc8c7154fe13227386866f83c48dcc75c330',  # surviving revision r9 (#738 deviation 4)
}
# Registered Stage 0 injections (microseconds): constant -> value. Each case is cand-ctl plus one injection.
SYNTH = {
    'syn-sq-ctl': ('ctlSynSenderHold', 300),    # deep sender queue: the tagged entry waits 300 us more in the sender's queue
    'syn-rq-ctl': ('ctlSynReceiverHold', 300),  # deep receiver queue: the same in the receiver's queue
    'syn-sw-ctl': ('ctlSynSlowWorker', 20),     # slow worker: the sender's worker spins 20 us in every submission
    'syn-rl-ctl': ('ctlSynBusyLoop', 1),        # busy receiver loop: 1 us per received packet in the receiver's loop
    'syn-rw-ctl': ('ctlSynReplyDelay', 300),    # delayed reply write: 300 us between reading a request and writing its reply
    'syn-un-ctl': ('ctlSynUnplaced', 300),      # unplaceable delay: 300 us after the tagged entry's submission stamp
}
# name -> (revision, depth, recorder, synthetic)
VARIANTS = {'reno': ('reno', None, False, None), 'cand': ('cand', 32, False, None), 'cand8': ('cand', 8, False, None),
            'cand-ctl': ('cand', 32, True, None), 'cand8-ctl': ('cand', 8, True, None),
            **{name: ('cand', 32, True, name) for name in SYNTH}}
# Copied byte-identical from #740's record (ddbac0a3).
R740 = 'ddbac0a3'
R740_DIR = 'docs/audits/2026-10-08-bbr-r9-memory-attribution'
R740_COPIED = ['run.py', 'stage_run.py', 'localize.py', 'localize_test.py', 'pack.py', 'rules.py', 'rules_test.py', 'go.mod',
               'launch/main.go', 'model-overlay/next.go']
# Binaries this record rebuilds from a revision a prior record already built (SHA-256): #740's.
EXPECTED = {
    ('reno', 'fixture'): 'cda8feb1b2fb004d72425adee5edeffb64a4c437f3ef07f9eb4e686af3ab5c8b',  # #715, #736, #738, #739, #740
    ('cand', 'fixture'): '4fa753223fdc777265e3ba1306f025e82fdf382a5d22f8a8992b8d0a72c5c1b6',  # #738's `new` (r9), #739, #740
    ('launch', 'launch'): 'a6146d62e16339f247a7e5fdac29ff47a66be7d8b6fc8d5e647d0479a58d5917',
}
# Files the recorder writes or patches (relative to the exported tree).
CTL_FILES = ['ctlrec.go', 'send_stream.go', 'send_queue.go', 'receive_stream.go', 'connection.go',
             'campaign/fixture/session.go', 'campaign/fixture/main.go']
LINUX = {'GOOS': 'linux', 'GOARCH': 'amd64'}
ENV = {**os.environ, 'GOTOOLCHAIN': 'go1.27.0', 'GOFLAGS': '-mod=mod', 'CGO_ENABLED': '0', 'GOWORK': 'off'}


def git(*args, **kw):
    return subprocess.check_output(['git', *args], cwd=ROOT, **kw)


def replace(path, old, new):
    s = path.read_text()
    assert s.count(old) == 1, (path, old)
    path.write_text(s.replace(old, new))


def depth(tree, entries):
    """The depth control: r9's source with one constant changed."""
    replace(tree / 'send_queue.go', 'const bbrSendQueueCapacity = 32\n', f'const bbrSendQueueCapacity = {entries}\n')


def ctl_hooks(tree, module, synthetic):
    """The #744 control-path recorder (ctl/ctlrec.go) and its hooks; measurement-only."""
    src = (HERE / 'ctl/ctlrec.go').read_text()
    if synthetic:
        const, value = SYNTH[synthetic]
        src, n = re.subn(rf'\b{const}(\s+)= 0\b', rf'{const}\g<1>= {value}', src)
        assert n == 1, synthetic
    (tree / 'ctlrec.go').write_text(src)
    ss = tree / 'send_stream.go'
    replace(ss, 'func (s *SendStream) Write(p []byte) (int, error) {\n\treturn s.WriteWithLimit(p, nil)\n',
            'func (s *SendStream) Write(p []byte) (int, error) {\n\tif s.streamID == ctlStream {\n\t\tctlWrite(s, p)\n\t}\n'
            '\treturn s.WriteWithLimit(p, nil)\n')
    replace(ss, '\tf, blocked, hasMoreData := s.popNewStreamFrameForPacket(maxBytes, v)\n\tif f != nil {\n\t\ts.numOutstandingFrames++\n\t}\n'
                '\ts.mutex.Unlock()\n',
            '\tf, blocked, hasMoreData := s.popNewStreamFrameForPacket(maxBytes, v)\n\tif f != nil {\n\t\ts.numOutstandingFrames++\n\t}\n'
            '\ts.mutex.Unlock()\n\tif f != nil && s.streamID == ctlStream {\n\t\tctlPopped(f.Offset, f.Offset+f.DataLen(), false)\n\t}\n')
    replace(ss, '\tif f == nil {\n\t\treturn ackhandler.StreamFrame{}, hasMore\n\t}\n',
            '\tif f == nil {\n\t\treturn ackhandler.StreamFrame{}, hasMore\n\t}\n'
            '\tif s.streamID == ctlStream {\n\t\tctlPopped(f.Offset, f.Offset+f.DataLen(), true)\n\t}\n')
    sq = tree / 'send_queue.go'
    replace(sq, 'func (h *sendQueue) Send(p *packetBuffer, gsoSize uint16, ecn protocol.ECN, metadata sendMetadata) {\n\tselect {\n',
            'func (h *sendQueue) Send(p *packetBuffer, gsoSize uint16, ecn protocol.ECN, metadata sendMetadata) {\n\tctlEnqueue(h, p)\n\tselect {\n')
    replace(sq, '\t\tcase e := <-h.queue:\n', '\t\tcase e := <-h.queue:\n\t\t\tctlDequeued(e)\n')
    replace(sq, '\t\tnext := <-h.queue\n', '\t\tnext := <-h.queue\n\t\tctlDequeued(next)\n')
    replace(sq, 'func (e queueEntry) release() {\n\te.buf.Release()\n', 'func (e queueEntry) release() {\n\tctlReleased(e)\n\te.buf.Release()\n')
    replace(sq, '\tif err := h.conn.Write(e.buf.Data, e.gsoSize, e.ecn); err != nil {\n',
            '\tctlBeforeWrite(e)\n\tif err := h.conn.Write(e.buf.Data, e.gsoSize, e.ecn); err != nil {\n')
    rs = tree / 'receive_stream.go'
    replace(rs, 'func (s *ReceiveStream) handleStreamFrame(frame *wire.StreamFrame, now monotime.Time) error {\n',
            'func (s *ReceiveStream) handleStreamFrame(frame *wire.StreamFrame, now monotime.Time) error {\n'
            '\tif s.streamID == ctlStream {\n\t\tctlArrived(frame.Offset, frame.Offset+frame.DataLen(), now)\n\t}\n')
    replace(rs, '\tqueuedStreamWindowUpdate, queuedConnWindowUpdate, n, err := s.readImpl(p)\n',
            '\tqueuedStreamWindowUpdate, queuedConnWindowUpdate, n, err := s.readImpl(p)\n'
            '\tif s.streamID == ctlStream {\n\t\tctlRead(s.readPos, n)\n\t}\n')
    replace(tree / 'connection.go', '\t\tp := c.receivedPackets.PopFront()\n\t\tc.receivedPacketMx.Unlock()\n',
            '\t\tp := c.receivedPackets.PopFront()\n\t\tc.receivedPacketMx.Unlock()\n\t\tctlLoop()\n')
    session = module / 'fixture/session.go'
    replace(session, '\t\t\treceived := time.Now().UnixNano()\n\t\t\tbinary.BigEndian.PutUint64(p[16:24], uint64(received))\n',
            '\t\t\treceived := time.Now().UnixNano()\n\t\t\tquic.CtlReplyDelay()\n\t\t\tbinary.BigEndian.PutUint64(p[16:24], uint64(received))\n')
    replace(session, '\t\t\treplies <- response{received - sent,',
            '\t\t\tquic.CtlNote(id, measured, sent, received)\n\t\t\treplies <- response{received - sent,')
    main = module / 'fixture/main.go'
    replace(main, '\t<-heapDone\n\tcloseTransport()\n', '\t<-heapDone\n\tquic.CtlFlush()\n\tcloseTransport()\n')
    if 'quic "github.com/quic-go/quic-go"' not in main.read_text():
        replace(main, 'import (\n', 'import (\n\tquic "github.com/quic-go/quic-go"\n')


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
    shutil.copy(HERE / 'model-overlay/next.go', module / 'model/next.go')
    return full_rev, tree, module


def fixture(name, platform=LINUX):
    rev, entries, recorder, synthetic = VARIANTS[name]
    full_rev, tree, module = export(name, REVISIONS[rev])
    changes = []
    if entries == 8:
        depth(tree, entries)
        changes.append('depth8')
    if recorder:
        ctl_hooks(tree, module, synthetic)
        changes.append('ctl' + (f'+{synthetic}' if synthetic else ''))
    if recorder:  # format only the files the recorder touched; the depth change needs none
        subprocess.run(['gofmt', '-l', '-w', *CTL_FILES], cwd=tree, check=True)
    label = full_rev + ''.join('+' + c for c in changes)
    tree_sources = {p: hashlib.sha256((tree / p).read_bytes()).hexdigest()
                    for p in ['send_queue.go', 'send_stream.go', 'receive_stream.go', 'connection.go', 'ctlrec.go']
                    if (tree / p).exists()}
    return dict(variant=name, revision=full_rev, heap_fixture=':'.join(HEAP_PATCH), bbr_send_queue_capacity=entries,
                recorder=recorder, synthetic=dict(zip(('constant', 'microseconds'), SYNTH[synthetic])) if synthetic else None,
                changes=changes, patched_source_sha256=tree_sources,
                builds={'fixture': go_build(module, './fixture', ART / 'bin' / f'{name}-fixture', label, platform)})


def identity():
    """D3: each plain build's queue capacity, by the depth tests in its exported tree (cand: 32, cand8: 8)."""
    out = {}
    for name, want in [('cand', 32), ('cand8', 8)]:
        tree = ART / 'src' / name
        test = tree / 'ctl_depth_identity_test.go'
        test.write_text('package quic\n\nimport "testing"\n\n'
                        '// #744 identity check: the BBR queue depth this exported tree selects.\n'
                        'func TestCtlDepthIdentity(t *testing.T) {\n'
                        f'\tif bbrSendQueueCapacity != {want} || sendQueueCapacityFor(true) != {want} || sendQueueCapacityFor(false) != 8 {{\n'
                        '\t\tt.Fatal(bbrSendQueueCapacity, sendQueueCapacityFor(true), sendQueueCapacityFor(false))\n\t}\n}\n')
        cmd = ['go', 'test', '-count=1', '-run', '^(TestCtlDepthIdentity|TestBBRSendQueueDepth.*)$', '-v', '.']
        res = subprocess.run(cmd, cwd=tree, env=ENV, capture_output=True, text=True)
        test.unlink()
        out[name] = dict(want=want, command=cmd, exit=res.returncode,
                         passed=re.findall(r'--- PASS: (\S+)', res.stdout), failed=re.findall(r'--- FAIL: (\S+)', res.stdout))
        print(name, 'exit', res.returncode, 'passed', len(out[name]['passed']), 'failed', out[name]['failed'])
    (ART / 'identity.json').write_text(json.dumps(out, indent=2) + '\n')


def build(name):
    (ART / 'bin').mkdir(parents=True, exist_ok=True)
    # Aids copied from #740's record must be byte-identical to it.
    for rel in R740_COPIED:
        assert (HERE / rel).read_bytes() == git('show', f'{R740}:{R740_DIR}/{rel}'), rel
    if name == 'launch':
        full_rev, tree, module = export('launch', REVISIONS['reno'])
        (module / 'launch').mkdir()
        shutil.copy(HERE / 'launch/main.go', module / 'launch/main.go')
        receipt = dict(variant='launch', revision=full_rev,
                       builds={'launch': go_build(module, './launch', ART / 'bin' / 'launch', full_rev)})
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
    if sys.argv[1:] == ['identity']:
        identity()
    else:
        for v in sys.argv[1:] or [*VARIANTS, 'launch']:
            build(v)
