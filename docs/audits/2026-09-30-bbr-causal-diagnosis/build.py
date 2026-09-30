#!/usr/bin/env python3
"""Frozen finite interventions; use only in an owned experimental worktree."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
ART = ROOT / '.local/bbr-causal-diagnosis'
FILES = ['internal/congestion/bbr_sender.go', 'internal/ackhandler/bbr_recovery.go', 'internal/ackhandler/bbr_ecn.go', 'connection.go']

def replace(s, old, new):
    assert s.count(old) == 1, old
    return s.replace(old, new)

variant = sys.argv[1]
assert variant in ['baseline', 'allocation', 'recovery', 'both', 'continuation', 'combined', 'recovery-ack', 'recovery-pto', 'recovery-feedback', 'ecn-reuse']
for file in FILES:
    s = subprocess.check_output(['git', 'show', '2383659ce0781b358fe098f32637d41767b7e9d0:' + file], cwd=ROOT, text=True)
    if file.endswith('bbr_sender.go') and variant in ['allocation','both','combined']:
        s = replace(s, '\tfresh := NewBBRSender(size)\n\tb.size, b.initialWindow = size, fresh.initialWindow', '\tif size <= 0 || size > protocol.MaxPacketBufferSize { panic("invalid BBR packet size") }\n\tb.size, b.initialWindow = size, min(10*size, max(14720, 2*size))')
    if file.endswith('bbr_recovery.go') and (variant in ['recovery','both','combined'] or variant.startswith('recovery-')):
        s = replace(s, '\tunconfirmedLoss bool', '\tunconfirmedLoss bool\n\tdiagnosticLostEver, diagnosticPTOEver bool')
        s = replace(s, '\t\tr.outcomes[i].state = outcomeLost', '\t\tr.outcomes[i].state = outcomeLost\n\t\tr.diagnosticLostEver = true')
        s = replace(s, '\t\tr.outcomes[i].ptoRetired = true', '\t\tr.outcomes[i].ptoRetired = true\n\t\tr.diagnosticPTOEver = true')
        s = replace(s, '\tif witness == protocol.InvalidPacketNumber {', '\tif witness == protocol.InvalidPacketNumber || !r.diagnosticPTOEver {')
        s = replace(s, '\t\t\to.state = outcomeLost', '\t\t\to.state = outcomeLost\n\t\t\tr.diagnosticLostEver = true')
        old = '\twitness := protocol.InvalidPacketNumber\n\tfor i := 0; i < r.count; i++ {'
        new = '''\twitness := protocol.InvalidPacketNumber
\t// Bound range enumeration by the original ring scan's record count.
\tn := 0
\tfor _, a := range ack.AckRanges {
\t\tspan := a.Largest-a.Smallest+1
\t\tif span <= 0 || span > protocol.PacketNumber(r.count-n) { n = r.count+1; break }
\t\tn += int(span)
\t}
\tif n <= r.count {
\t\tfor _, a := range ack.AckRanges {
\t\t\tfor pn := a.Smallest; pn <= a.Largest; pn++ {
\t\t\t\tif index, ok := r.keys[congestionPacketKey{space:space,number:pn}]; ok {
\t\t\t\t\to := &r.outcomes[index]
\t\t\t\t\tif o.state == outcomeDisposed { continue }
\t\t\t\t\tif o.receiptEligible {
\t\t\t\t\t\twitness = max(witness, pn)
\t\t\t\t\t\tif o.state != outcomeAcked { r.ackOrdinal = max(r.ackOrdinal,o.ordinal) }
\t\t\t\t\t}
\t\t\t\t\to.state = outcomeAcked
\t\t\t\t}
\t\t\t}
\t\t}
\t\treturn witness
\t}
\tfor i := 0; i < r.count; i++ {'''
        s = replace(s, old, new)
        s = replace(s, '\tvar first *recoveryOutcome', '\tif !r.diagnosticLostEver { return }\n\tvar first *recoveryOutcome')
        if variant.startswith('recovery-'):
            original = subprocess.check_output(['git','show','2383659ce0781b358fe098f32637d41767b7e9d0:'+file],cwd=ROOT,text=True)
            selected = {'recovery-ack':'ack','recovery-pto':'confirmPTO','recovery-feedback':'feedback'}[variant]
            for name in ['ack','confirmPTO','feedback']:
                if name == selected: continue
                prefix = 'func (r *recoveryEvidence) '+name+'('
                a = s.index(prefix); b = s.index('\n}',a)+2
                x = original.index(prefix); y = original.index('\n}',x)+2
                restored = original[x:y]
                if name == 'confirmPTO':
                    restored = restored.replace('o.state = outcomeLost','o.state = outcomeLost\n\t\t\tr.diagnosticLostEver = true')
                s = s[:a]+restored+s[b:]
    if file.endswith('bbr_ecn.go') and variant == 'ecn-reuse':
        s = replace(s, '\tranges           []ecnMarkRange', '\tranges           []ecnMarkRange\n\tdiagnosticScratch []ecnMarkRange')
        s = replace(s, '\tvar next []ecnMarkRange', '\tnext := e.diagnosticScratch[:0]')
        s = replace(s, '\te.ranges = next', '\te.ranges, e.diagnosticScratch = next, e.ranges[:0]')
    if file == 'connection.go' and variant in ['continuation','combined']:
        s = replace(s, '\t\tresult := c.triggerSending(now)', '''\t\tresult := c.triggerSending(now)
\t\t// Experimental maximum of eight opportunities before loop service.
\t\tfor i := 1; c.emission.bbr != nil && i < 8 && result.err == nil && result.retry && result.available == nil; i++ {
\t\t\tif c.receivedPackets.Len() > 0 || c.closeErr.Load() != nil { break }
\t\t\tresult = c.triggerSending(monotime.Now())
\t\t}''')
    (ROOT / file).write_text(s)
subprocess.run(['gofmt','-w',*FILES],cwd=ROOT,check=True)
patch = subprocess.check_output(['git','diff','--',*FILES],cwd=ROOT)
(HERE / (variant+'.patch')).write_bytes(patch)
binary = ART / variant
env = {**os.environ, 'GOTOOLCHAIN':'go1.27.0'}
cmd = ['go','build','-trimpath','-ldflags','-X main.sourceRevision=e4f322cbbfd4225a4b714e08ec19c958cccadcb0+'+variant,'-o',str(binary),'./fixture']
subprocess.run(cmd,cwd=ART/'diagnostic-source',env=env,check=True)
receipt = dict(variant=variant,component='e4f322cbbfd4225a4b714e08ec19c958cccadcb0',base='2383659ce0781b358fe098f32637d41767b7e9d0',patch_sha256=hashlib.sha256(patch).hexdigest(),binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),command=cmd)
(ART/(variant+'-build.json')).write_text(json.dumps(receipt,indent=2)+'\n')
print(json.dumps(receipt))
