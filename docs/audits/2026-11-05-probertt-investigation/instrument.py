"""Disposable observation overlay for the reconstructed historical issue #1210 build."""
from pathlib import Path
import subprocess
import sys

root = Path(sys.argv[1]).resolve()
assert root == Path('/Volumes/worktrees/quic-go-fast/investigate-1210-native')
assert subprocess.check_output(['git', 'branch', '--show-current'], cwd=root, text=True).strip() == 'codex/investigate-1210-native'


def edit(name, old, new):
    p = root / name
    text = p.read_text()
    assert text.count(old) == 1, (name, old, text.count(old))
    p.write_text(text.replace(old, new))


def imp(name):
    edit(name, 'import (', 'import (\n "github.com/quic-go/quic-go/internal/diag1210"')


p = root / 'internal/diag1210/record.go'
p.parent.mkdir()
p.write_text(r'''// Temporary diagnostic for issue #1210. Not a production API.
package diag1210

import (
 "bufio"
 "encoding/binary"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "sync"
 "time"
 "github.com/quic-go/quic-go/internal/monotime"
)

var Enabled = os.Getenv("Q1210_TRACE") != ""
var mu sync.Mutex
var rows [][16]int64
var dropped int64
var flushed bool
var originMono = monotime.Now()
var originWall = time.Now().UnixNano()
const capacity = 2000000

func init() { if Enabled { rows = make([][16]int64, 0, capacity) } }
func Pick(pn int64) bool { return Enabled && pn >= 0 && uint64(pn)*11400714819323198485 >> 59 == 0 }
func Add(kind int64, values ...int64) {
 if !Enabled { return }
 var row [16]int64
 row[0] = kind
 copy(row[1:], values)
 mu.Lock()
 defer mu.Unlock()
 if flushed || len(rows) == capacity { dropped++; return }
 rows = append(rows, row)
}
func Drop(n int64) { mu.Lock(); dropped += n; mu.Unlock() }
func Flush() {
 if !Enabled { return }
 mu.Lock()
 defer mu.Unlock()
 if flushed { return }
 flushed = true
 path := filepath.Join(os.Getenv("Q1210_OUTPUT_DIR"), fmt.Sprintf("diag1210-%d.bin", os.Getpid()))
 f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
 if err != nil { fmt.Fprintf(os.Stderr, "[DEBUG-1210] flush: %v\n", err); return }
 w := bufio.NewWriterSize(f, 1024*1024)
 err = binary.Write(w, binary.LittleEndian, rows)
 if err == nil { err = w.Flush() }
 if e := f.Close(); err == nil { err = e }
 endMono := monotime.Now()
 endWall := time.Now().UnixNano()
 header := map[string]any{"diag1210": path, "records": len(rows), "dropped": dropped, "record_int64s": 16, "origin_mono_ns": int64(originMono), "origin_unix_ns": originWall, "end_mono_ns": int64(endMono), "end_unix_ns": endWall, "error": fmt.Sprint(err), "clock": "monotime; Unix anchor is a sequential observation, never cross-host one-way time"}
 b, _ := json.Marshal(header)
 fmt.Fprintf(os.Stderr, "[DEBUG-1210] %s\n", b)
}
''')

imp('connection.go')
edit('connection.go', 'func (c *Conn) run() (err error) {', '''func (c *Conn) run() (err error) {
 if diag1210.Enabled { diag1210.Add(10, int64(monotime.Now()), int64(c.perspective)); defer diag1210.Flush() }''')
edit('connection.go', 'isNonProbing, pathChallenge, err := c.handleUnpackedShortHeaderPacket', '''if diag1210.Pick(int64(pn)) { diag1210.Add(9, int64(monotime.Now()), int64(pn), int64(p.rcvTime), int64(p.Size())) }
 isNonProbing, pathChallenge, err := c.handleUnpackedShortHeaderPacket''')
edit('connection.go', 'acked1RTTPacket, err := c.sentPacketHandler.ReceivedAck(frame, encLevel, c.lastPacketReceivedTime)', '''if encLevel == protocol.Encryption1RTT && diag1210.Pick(int64(frame.LargestAcked())) {
  diag1210.Add(3, int64(monotime.Now()), int64(frame.LargestAcked()), int64(rcvTime), int64(frame.DelayTime), int64(c.lastPacketReceivedTime))
 }
 acked1RTTPacket, err := c.sentPacketHandler.ReceivedAck(frame, encLevel, c.lastPacketReceivedTime)''')

imp('internal/ackhandler/received_packet_tracker.go')
edit('internal/ackhandler/received_packet_tracker.go', 'ack.DelayTime = max(0, now.Sub(h.largestObservedRcvdTime))', '''ack.DelayTime = max(0, now.Sub(h.largestObservedRcvdTime))
 if diag1210.Pick(int64(ack.LargestAcked())) { diag1210.Add(2, int64(monotime.Now()), int64(ack.LargestAcked()), int64(now), int64(h.largestObservedRcvdTime), int64(ack.DelayTime), int64(h.ackElicitingPacketsReceivedSinceLastAck), int64(h.oldestUnackedRcvdTime), int64(h.ackAlarm)) }''')

imp('internal/congestion/bbr_sender.go')
edit('internal/congestion/bbr_sender.go', 'func (b *BBRSender) Feedback(e FeedbackEvent) {', '''func (b *BBRSender) Feedback(e FeedbackEvent) {
 if diag1210.Enabled {
  prePhase := b.phase
  observe := b.InProbeRTT() || diag1210.Pick(int64(e.LargestAcked))
  defer func() {
   if observe || b.InProbeRTT() {
    var acked int64
    for _, p := range e.Acked { acked += int64(p.Length) }
    diag1210.Add(4, int64(monotime.Now()), int64(e.LargestAcked), int64(e.Time), int64(prePhase), int64(b.phase), int64(b.GetCongestionWindow()), int64(e.PriorInFlight), int64(e.PostInFlight), int64(e.RawRTT), int64(b.minimumRTT), int64(e.PendingLocal), acked, int64(e.Delivery.Delivered), int64(b.bandwidth), int64(b.rate))
   }
  }()
  for _, p := range e.Acked {
   if diag1210.Pick(int64(p.PacketNumber)) { diag1210.Add(11, int64(monotime.Now()), int64(p.PacketNumber), int64(e.Time), int64(p.SendTime), int64(p.Length), int64(e.LargestAcked), int64(b.phase)) }
  }
 }''')

edit('internal/congestion/bbr_sender.go', 'func (b *BBRSender) Sent(e SendEvent) {', '''func (b *BBRSender) Sent(e SendEvent) {
 if diag1210.Enabled && b.InProbeRTT() { diag1210.Add(15, int64(monotime.Now()), int64(e.Packet.PacketNumber), int64(e.Packet.SendTime), int64(e.PriorInFlight), int64(e.PostInFlight), int64(b.GetCongestionWindow()), int64(e.Packet.Length)) }''')
edit('internal/congestion/bbr_sender.go', 'var acked int64', '''var acked, rawResidence, processResidence, lostBytes int64
    processTime := monotime.Now()
    for _, p := range e.Acked {
     rawResidence += int64(p.Length) * int64(e.Time.Sub(p.SendTime))
     processResidence += int64(p.Length) * int64(processTime.Sub(p.SendTime))
    }
    for _, p := range e.Lost { lostBytes += int64(p.Length) }
    diag1210.Add(14, int64(processTime), int64(e.LargestAcked), int64(e.Time), rawResidence, processResidence, lostBytes)''')

imp('packet_emission.go')
edit('packet_emission.go', 'type packetEmission struct {', 'type packetEmission struct {\n diagPNs [8]int64\n diagN int\n')
edit('packet_emission.go', 'func (e *packetEmission) registerPacket(p shortHeaderPacket, ecn protocol.ECN, now monotime.Time) {', '''func (e *packetEmission) registerPacket(p shortHeaderPacket, ecn protocol.ECN, now monotime.Time) {
 chosen := diag1210.Pick(int64(p.PacketNumber))
 if p.Ack != nil && diag1210.Pick(int64(p.Ack.LargestAcked())) {
  chosen = true
  diag1210.Add(12, int64(monotime.Now()), int64(p.PacketNumber), int64(p.Ack.LargestAcked()), int64(p.Ack.DelayTime))
 }
 if chosen {
  diag1210.Add(1, int64(monotime.Now()), int64(p.PacketNumber), int64(now), int64(p.Length))
  if e.diagN < len(e.diagPNs) { e.diagPNs[e.diagN] = int64(p.PacketNumber); e.diagN++ } else { diag1210.Drop(1) }
 }''')

imp('packet_emission_bbr.go')
edit('packet_emission_bbr.go', 'func (e *packetEmission) handoff(buf *packetBuffer, gso uint16, ecn protocol.ECN, metadata sendMetadata) {', '''func (e *packetEmission) handoff(buf *packetBuffer, gso uint16, ecn protocol.ECN, metadata sendMetadata) {
 if diag1210.Enabled {
  metadata.diagPNs, metadata.diagN = e.diagPNs, e.diagN
  e.diagN = 0
  for _, pn := range metadata.diagPNs[:metadata.diagN] { diag1210.Add(7, int64(monotime.Now()), pn, int64(buf.Len()), int64(gso)) }
 }''')
edit('packet_emission_bbr.go', 'func (e *packetEmission) sendBounded(now monotime.Time, confirmed bool) (result emissionResult) {', '''func (e *packetEmission) sendBounded(now monotime.Time, confirmed bool) (result emissionResult) {
 if diag1210.Enabled && e.bbr.controller != nil && e.bbr.controller.InProbeRTT() {
  diag1210.Add(5, int64(monotime.Now()), int64(now))
  defer func() {
   e.bbr.credit.mu.Lock(); pending := e.bbr.credit.pending; e.bbr.credit.mu.Unlock()
   diag1210.Add(6, int64(monotime.Now()), int64(now), int64(result.stop), int64(e.bbr.tokens), int64(e.bbr.quantum), int64(pending), int64(e.bbr.controller.GetCongestionWindow()), int64(e.bbr.supplyGap))
  }()
 }''')
edit('packet_emission_bbr.go', '\td1460.allowance = int64(allowance)', '''if diag1460On { d1460.allowance = int64(allowance) }
 if diag1210.Enabled && e.bbr.controller != nil && e.bbr.controller.InProbeRTT() { diag1210.Add(13, int64(monotime.Now()), int64(now), int64(mode), int64(allowance), int64(size), int64(e.bbr.tokens)) }''')

imp('send_queue.go')
edit('send_queue.go', 'type sendMetadata struct {', 'type sendMetadata struct {\n diagPNs [8]int64\n diagN int\n')
edit('send_queue.go', '\tvar err error\n\tif h.authorized != nil {', ''' var started monotime.Time
 if diag1210.Enabled && e.metadata.diagN > 0 { started = monotime.Now() }
 var err error
 if h.authorized != nil {''')
edit('send_queue.go', '\tif err == nil {\n\t\tfor _, receipt := range e.metadata.terminalReceipts {', ''' if started != 0 {
  ended := monotime.Now()
  for _, pn := range e.metadata.diagPNs[:e.metadata.diagN] { diag1210.Add(8, int64(ended), pn, int64(started), int64(e.buf.Len()), 1) }
 }
 if err == nil {
  for _, receipt := range e.metadata.terminalReceipts {''')
edit('send_queue.go', '\t\t\taccepted, err := bs.sendBatch(bufs, remaining[0].ecn)', ''' var started monotime.Time
   if diag1210.Enabled { started = monotime.Now() }
   accepted, err := bs.sendBatch(bufs, remaining[0].ecn)
   if diag1210.Enabled {
    ended := monotime.Now()
    for j, entry := range remaining {
     if j >= accepted { break }
     for _, pn := range entry.metadata.diagPNs[:entry.metadata.diagN] { diag1210.Add(8, int64(ended), pn, int64(started), int64(entry.buf.Len()), int64(accepted)) }
    }
   }''')

files = subprocess.check_output(['git', 'diff', '--name-only'], cwd=root, text=True).splitlines()
files += ['internal/diag1210/record.go']
subprocess.run(['gofmt', '-w', *[x for x in files if x.endswith('.go')]], cwd=root, check=True)
print('Installed temporary issue #1210 observation overlay.')
