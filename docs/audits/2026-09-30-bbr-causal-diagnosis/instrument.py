#!/usr/bin/env python3
"""Bounded diagnostic patch, used separately from performance comparisons."""
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent

def edit(file, old, new):
    p = ROOT / file
    s = p.read_text()
    assert s.count(old) == 1, old
    p.write_text(s.replace(old, new))

(ROOT / 'causal_diagnostics.go').write_text('''package quic
import("encoding/json";"fmt";"os";"sync/atomic";"time";"github.com/quic-go/quic-go/internal/monotime")
var causal = struct {
 Loops, Waits, Opportunities, Progress, Exhausted uint64
 Stops [9]uint64
 PolicyRateMin, PolicyRateMax uint64
 PendingMax int64
 SingleWrites, BatchCalls, BatchEntries, BatchAccepted atomic.Uint64
 QueueResidence [8]atomic.Uint64
 DeadlineLateness [8]uint64
}{PolicyRateMin:^uint64(0)}
func causalBucket(ns int64) int { for i,v:=range []int64{1000,10000,100000,500000,1000000,5000000,20000000} { if ns<=v{return i} }; return 7 }
func (c *Conn) causalOpportunity(now monotime.Time, result emissionResult) {
 causal.Opportunities++
 if result.progress {causal.Progress++}; if result.supplyExhausted {causal.Exhausted++}
 causal.Stops[result.stop]++
 if p:=c.emission.bbr; p!=nil {
 causal.PolicyRateMin=min(causal.PolicyRateMin,p.rate); causal.PolicyRateMax=max(causal.PolicyRateMax,p.rate)
 if causal.Opportunities%256==0 {p.credit.mu.Lock(); causal.PendingMax=max(causal.PendingMax,int64(p.credit.pending));p.credit.mu.Unlock()}
 }
}
func causalQueue(e queueEntry) {if e.metadata.causalAt!=0 { causal.QueueResidence[causalBucket(int64(monotime.Now()-e.metadata.causalAt))].Add(1) }}
func (c *Conn) causalDump() {
 residence:=make([]uint64,8); for i:=range residence {residence[i]=causal.QueueResidence[i].Load()}
 d:=map[string]any{"loops":causal.Loops,"waits":causal.Waits,"opportunities":causal.Opportunities,"progress":causal.Progress,"exhausted":causal.Exhausted,"stops":causal.Stops,"policy_rate_min":causal.PolicyRateMin,"policy_rate_max":causal.PolicyRateMax,"sampled_pending_max":causal.PendingMax,"single_writes":causal.SingleWrites.Load(),"batch_calls":causal.BatchCalls.Load(),"batch_entries":causal.BatchEntries.Load(),"batch_accepted":causal.BatchAccepted.Load(),"queue_residence_histogram":residence,"deadline_lateness_histogram":causal.DeadlineLateness,"histogram_upper_ns":[]int64{1000,10000,100000,500000,1000000,5000000,20000000},"sample_every":256}
 if h,ok:=c.sentPacketHandler.(interface{CausalDiagnostics() any});ok {d["recovery"]=h.CausalDiagnostics()}
 b,_:=json.Marshal(d);fmt.Fprintln(os.Stderr,"[BBR-CAUSAL]",string(b))
}
var _ = time.Second
''')
edit('connection.go','\tfor {\n\t\tif c.framer.QueuedTooManyControlFrames()', '\tfor {\n\t\tcausal.Loops++\n\t\tif c.framer.QueuedTooManyControlFrames()')
edit('connection.go','\t\tif !shouldProceedImmediately {','\t\tif !shouldProceedImmediately {\n\t\t\tcausal.Waits++')
edit('connection.go','\t\tnow := monotime.Now()\n\t\tif h, ok := c.sentPacketHandler.(deliveryLifecycle);', '\t\tnow := monotime.Now()\n\t\tif c.pacingDeadline > 0 && c.pacingDeadline != deadlineSendImmediately && now > c.pacingDeadline {causal.DeadlineLateness[causalBucket(int64(now-c.pacingDeadline))]++}\n\t\tif h, ok := c.sentPacketHandler.(deliveryLifecycle);')
edit('connection.go','\tc.emission.drain() // close the send queue before sending the CONNECTION_CLOSE','\tc.emission.drain() // close the send queue before sending the CONNECTION_CLOSE\n\tc.causalDump()')
edit('connection.go','\tresult := c.emission.send(now, c.handshakeConfirmed)','\tresult := c.emission.send(now, c.handshakeConfirmed)\n\tc.causalOpportunity(now,result)')
edit('send_queue.go','\tcredit         *sendReservation','\tcausalAt monotime.Time\n\tcredit         *sendReservation')
edit('send_queue.go','"github.com/quic-go/quic-go/internal/protocol"','"github.com/quic-go/quic-go/internal/protocol"\n"github.com/quic-go/quic-go/internal/monotime"')
edit('send_queue.go','\tselect {\n\tcase h.queue <- queueEntry{','\tif causal.Opportunities%256==0 {metadata.causalAt=monotime.Now()}\n\tselect {\n\tcase h.queue <- queueEntry{')
edit('send_queue.go','func (h *sendQueue) writeEntry(e queueEntry) error {','func (h *sendQueue) writeEntry(e queueEntry) error {\n causal.SingleWrites.Add(1);causalQueue(e)')
edit('send_queue.go','\t\t\taccepted, err := bs.sendBatch(bufs, remaining[0].ecn)','\t\t\tcausal.BatchCalls.Add(1);causal.BatchEntries.Add(uint64(len(remaining)));for _,entry:=range remaining {causalQueue(entry)}\n\t\t\taccepted, err := bs.sendBatch(bufs, remaining[0].ecn)\n\t\t\tif accepted>0 {causal.BatchAccepted.Add(uint64(accepted))}')
edit('internal/ackhandler/congestion_dispatch.go','\tscratch          []congestion.PacketInfo','\tscratch          []congestion.PacketInfo\n causalFeedback uint64\n causalValid, causalLimited uint64\n causalSamples []causalSample\n causalIdleCalls, causalIdleOneRate uint64')
edit('internal/ackhandler/congestion_dispatch.go','\td.sampler.feedback(&d.event)','''\td.causalFeedback++
 beforeDelivered,beforeLost,beforeTime:=d.sampler.delivered,d.sampler.lost,d.sampler.deliveredTime
 d.sampler.feedback(&d.event)
 if d.event.Delivery.Valid {d.causalValid++};if d.event.Delivery.Limited!=0 {d.causalLimited++}
 if d.causalFeedback%128==0 && len(d.causalSamples)<256 && len(d.event.Acked)<=512 && len(d.event.Lost)<=512 {
 d.causalSamples=append(d.causalSamples,causalSample{beforeDelivered,beforeLost,beforeTime,d.sampler.minimumRTT,d.event.Time,d.event.PathGeneration,d.event.SampleGeneration,append([]congestion.PacketInfo(nil),d.event.Acked...),append([]congestion.PacketInfo(nil),d.event.Lost...),d.event.Delivery})
 }''')
(ROOT/'internal/ackhandler/causal_diagnostics.go').write_text('''package ackhandler
import("time";"github.com/quic-go/quic-go/internal/congestion";"github.com/quic-go/quic-go/internal/monotime")
type causalSample struct {BeforeDelivered,BeforeLost uint64;BeforeTime monotime.Time;MinimumRTT time.Duration;Time monotime.Time;Path,Generation uint64;Acked,Lost []congestion.PacketInfo;Sample congestion.DeliverySample}
func(h *sentPacketHandler) CausalDiagnostics() any {
 d:=h.congestionEvents;if d==nil{return nil}
 out:=map[string]any{"idle_calls":d.causalIdleCalls,"idle_one_rate":d.causalIdleOneRate,"feedback_events":d.causalFeedback,"valid_samples":d.causalValid,"limited_samples":d.causalLimited,"ring_count":d.recovery.count,"ring_evictions":d.recovery.evicted,"outstanding":d.sampler.outstanding,"delivered":d.sampler.delivered,"lost_bytes":d.sampler.lost,"evidence_lost":d.sampler.evidenceLost,"missing":d.sampler.missing,"expired":d.sampler.expired,"samples":d.causalSamples,"sample_every":128,"sample_cap":256,"diagnostics":h.BBRDiagnostics()}
 if e:=h.bbrECN;e!=nil {out["ecn_ranges"]=len(e.ranges);out["ecn_sent"]=e.sent;out["ecn_accepted"]=e.accepted;out["ecn_failed"]=e.counterFailed||e.evidenceLost}
 return out
}
''')
edit('internal/ackhandler/congestion_dispatch.go','\t\tb.BeforeSend(now, h.DeliveryIdle())','\t\tidle:=h.DeliveryIdle(); b.BeforeSend(now,idle); if idle && h.congestionEvents!=nil {h.congestionEvents.causalIdleCalls++;if b.PacingRate()==1 {h.congestionEvents.causalIdleOneRate++}}')
files=['connection.go','send_queue.go','causal_diagnostics.go','internal/ackhandler/congestion_dispatch.go','internal/ackhandler/causal_diagnostics.go']
subprocess.run(['gofmt','-w',*files],cwd=ROOT,check=True)
patch=subprocess.check_output(['git','diff','--',*files],cwd=ROOT)
for f in ['causal_diagnostics.go','internal/ackhandler/causal_diagnostics.go']:
 p=subprocess.run(['git','diff','--no-index','--','/dev/null',f],cwd=ROOT,capture_output=True);assert p.returncode==1;patch+=p.stdout
(HERE/'instrumentation.patch').write_bytes(patch)
print('bounded instrumentation applied')
