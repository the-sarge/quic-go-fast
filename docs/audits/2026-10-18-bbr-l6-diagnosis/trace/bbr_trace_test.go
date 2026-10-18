package congestion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// Tests for the measurement-only #792 overlay, run in the exported tree next to
// the port's own tests (bbrProbeTrace is bbr_probe_test.go's driver).

func traceRows(kind int64) [][traceCols]int64 {
	var out [][traceCols]int64
	for _, r := range trace.rows[:trace.n] {
		if r[0] == kind {
			out = append(out, r)
		}
	}
	return out
}

func traceEnable(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "trace.json")
	traceConfigure([]string{"fixture", "-role", "send"}, out, strconv.FormatInt(time.Now().Add(time.Hour).UnixNano(), 10))
	t.Cleanup(func() { traceConfigure(nil, "", "") })
	require.True(t, trace.on)
	return out
}

func TestTraceConfigure(t *testing.T) {
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixNano(), 10)
	t.Cleanup(func() { traceConfigure(nil, "", "") })
	for _, c := range []struct {
		args          []string
		output, until string
		on            bool
	}{
		{[]string{"x", "-role", "send"}, "out", future, true},
		{[]string{"x", "-role=send"}, "out", future, true},
		{[]string{"x", "--role", "send"}, "out", future, true},
		{[]string{"x", "-role", "receive"}, "out", future, false},
		{[]string{"x"}, "out", future, false},
		{[]string{"x", "-role", "send"}, "", future, false},
		{[]string{"x", "-role", "send"}, "out", "", false},
		{[]string{"x", "-role", "send"}, "out", "0", false},
		{[]string{"x", "-role", "send"}, "out", "soon", false},
	} {
		traceConfigure(c.args, c.output, c.until)
		require.Equal(t, c.on, trace.on, c)
	}
}

func TestTraceDisabledRecordsNothing(t *testing.T) {
	traceConfigure(nil, "", "")
	x := newBBRProbeTrace()
	x.up(t)
	x.lose(20000, SendUnknown)
	require.Zero(t, trace.n)
	require.Nil(t, trace.target)
}

func TestTraceRecordsProbeCycleAndLoss(t *testing.T) {
	out := traceEnable(t)
	x := newBBRProbeTrace()
	x.up(t)
	x.lose(20000, SendUnknown)
	x.ackFlight(100000, 20000, 18000, SendUnknown)

	// Phase changes are recorded as PRE/POST pairs of the same event.
	pre, post := traceRows(traceKindPre), traceRows(traceKindPost)
	require.Equal(t, len(pre), len(post))
	var phases [][2]int64
	for i := range pre {
		require.Equal(t, pre[i][3], post[i][3], "pair shares the event ordinal")
		if pre[i][5] != post[i][5] {
			require.NotZero(t, post[i][40]&1)
			phases = append(phases, [2]int64{pre[i][5], post[i][5]})
		}
	}
	require.Equal(t, [][2]int64{
		{int64(bbrStartup), int64(bbrDrain)}, {int64(bbrDrain), int64(bbrCruise)},
		{int64(bbrCruise), int64(bbrRefill)}, {int64(bbrRefill), int64(bbrUp)}, {int64(bbrUp), int64(bbrDown)},
	}, phases)

	// The loss in Up is one recorded two-percent check, and its event ends Up.
	checks := traceRows(traceKindProbeLoss)
	require.Len(t, checks, 1)
	c := checks[0]
	require.EqualValues(t, 1200, c[40], "lost since the packet was sent")
	require.EqualValues(t, 20000, c[41], "flight when it was sent")
	require.EqualValues(t, bbrUp, c[5])
	require.NotZero(t, c[17]&1, "probe sample")
	set := traceRows(traceKindProbeSet)
	require.Len(t, set, 1)
	require.Equal(t, c[3], set[0][3])
	require.EqualValues(t, 19183, set[0][8], "inflight_longterm right after the crossing: 18800 + 18800/49")
	require.NotZero(t, set[0][17]&2)
	require.Zero(t, set[0][17]&1, "probe sampling ends at the crossing")
	last := post[len(post)-1]
	require.Equal(t, c[3], last[3])
	require.EqualValues(t, bbrDown, last[5])
	require.NotZero(t, last[17]&2, "previous probe too high")
	require.EqualValues(t, 1, trace.probeEntries)

	// Rounds are recorded with increasing round counts and the round's loss.
	rounds := traceRows(traceKindRound)
	require.NotEmpty(t, rounds)
	for i := 1; i < len(rounds); i++ {
		require.Greater(t, rounds[i][4], rounds[i-1][4])
	}
	var lost int64
	for _, r := range rounds {
		lost += r[40]
	}
	require.LessOrEqual(t, lost, int64(1200))

	// Dumping happens once, at the first event at or after the end time.
	require.NoFileExists(t, out)
	n := trace.n
	trace.untilNS = time.Now().UnixNano()
	x.ack(100000, 9000, SendUnknown)
	require.True(t, trace.dumped)
	require.Equal(t, n, trace.n)
	x.ack(100000, 9000, SendUnknown)
	require.Equal(t, n, trace.n, "nothing is recorded after the dump")
	var dump struct {
		Columns  []string
		Rows     [][traceCols]int64
		Overflow int
		Events   int64
	}
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &dump))
	require.Len(t, dump.Columns, traceCols)
	require.Len(t, dump.Rows, n)
	require.Zero(t, dump.Overflow)
	require.Equal(t, trace.rows[0], dump.Rows[0])
}

func TestTraceDumpAtExit(t *testing.T) {
	out := traceEnable(t)
	x := newBBRProbeTrace()
	x.cruise()
	n := trace.n
	require.Positive(t, n)
	TraceDumpAtExit()
	require.True(t, trace.dumped)
	first, err := os.ReadFile(out)
	require.NoError(t, err)
	var dump struct {
		Rows [][traceCols]int64
	}
	require.NoError(t, json.Unmarshal(first, &dump))
	require.Len(t, dump.Rows, n)
	TraceDumpAtExit()
	x.ack(100000, 9000, SendUnknown)
	again, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, first, again, "written once")
	require.Equal(t, n, trace.n, "nothing is recorded after the dump")

	traceConfigure(nil, "", "")
	TraceDumpAtExit() // disabled: no file, no panic
}

func TestTraceLossRoundRows(t *testing.T) {
	traceEnable(t)
	x := newBBRProbeTrace()
	x.up(t)
	for range 3 {
		x.ack(100000, 9000, SendUnknown)
	}
	x.ack(20000, 20000, SendUnknown)
	x.lose(60000, SendUnknown) // exactly two percent: no long-term reduction
	x.ack(20000, 8000, SendUnknown)
	pre, post := traceRows(traceKindLossPre), traceRows(traceKindLossPost)
	require.NotEmpty(t, pre)
	require.Equal(t, len(pre), len(post))
	p, q := pre[len(pre)-1], post[len(post)-1]
	require.Equal(t, p[3], q[3])
	require.NotZero(t, p[17]&(1<<9), "loss pending at the close")
	require.EqualValues(t, -1, p[7], "short-term bandwidth unbounded before the first loss round")
	require.EqualValues(t, 70000, q[7], "max(latest rate, 0.7 x max bandwidth)")
	require.EqualValues(t, 1200, p[32], "the round's lost bytes")
}

func TestTraceOutsideChangeAndForeignSender(t *testing.T) {
	traceEnable(t)
	x := newBBRProbeTrace()
	x.cruise()
	n := trace.n
	x.b.inflightShort = 5000 // a change between events is recorded at the next one
	x.ack(100000, 9000, SendUnknown)
	outside := traceRows(traceKindOutsidePost)
	require.Len(t, outside, 1)
	require.NotZero(t, outside[0][40]&(1<<2))
	require.Greater(t, trace.n, n)

	y := newBBRProbeTrace()
	before := trace.n
	y.ack(100000, 9000, SendUnknown)
	require.Equal(t, before, trace.n, "a second sender is not traced")
	require.EqualValues(t, 1, trace.foreign)
}

func TestTraceOverflowIsCounted(t *testing.T) {
	traceEnable(t)
	x := newBBRProbeTrace()
	trace.n = traceCapacity - 1
	x.cruise()
	require.Equal(t, traceCapacity, trace.n)
	require.Positive(t, trace.overflow)
}

// TestTraceFixture writes the checker's fixture trace: the port itself driven
// through a synthetic path with probe cycles, crossing and non-crossing losses,
// a bandwidth dip that ages out and ProbeRTT every five seconds. Flight follows
// the window, so ProbeRTT can drain and exit.
func TestTraceFixture(t *testing.T) {
	out := os.Getenv("BBR_TRACE_FIXTURE")
	if out == "" {
		t.Skip("set BBR_TRACE_FIXTURE to write the checker's fixture trace")
	}
	traceConfigure([]string{"fixture", "-role", "send"}, out, strconv.FormatInt(time.Now().Add(time.Hour).UnixNano(), 10))
	t.Cleanup(func() { traceConfigure(nil, "", "") })
	x := newBBRProbeTrace()
	x.b.random = func(n int32) int32 { return n / 2 }
	x.cruise()
	for i := range 900 {
		flight := max(1200, min(protocol.ByteCount(9000+(i%5)*1500), x.b.GetCongestionWindow()-1200))
		switch {
		case i%7 == 3:
			x.lose(flight+1200, SendUnknown) // above 2% of its flight: crosses while probing
		case i%11 == 5:
			x.lose(100000, SendUnknown) // 1.2%: never crosses
		}
		rate := uint64(100000 + (i%9)*2000)
		if i%97 > 80 {
			rate = 70000
		}
		x.ack(rate, flight, SendUnknown)
	}
	traceDump()
	require.FileExists(t, out)
	require.Zero(t, trace.overflow)
}

// loseBurst registers n packets sent at flight and declares them lost in one
// event, so the n-th check counts all n losses since its packet was sent.
func (x *bbrProbeTrace) loseBurst(n int, flight protocol.ByteCount) {
	lost := make([]PacketInfo, 0, n)
	for range n {
		x.ordinal++
		p := PacketInfo{Ordinal: x.ordinal, PathGeneration: x.b.pathGeneration, SampleGeneration: x.b.sampleGeneration, Length: 1200, AckEliciting: true, RegistrationValid: true, Delivery: DeliverySnapshot{Delivered: x.delivered, Lost: x.lost, Valid: true, PriorInFlight: flight - 1200, PostInFlight: flight}}
		x.b.Sent(SendEvent{Packet: p, PriorInFlight: flight - 1200, PostInFlight: flight})
		lost = append(lost, p)
	}
	x.lost += uint64(n) * 1200
	x.now = x.now.Add(time.Millisecond)
	x.b.Feedback(FeedbackEvent{PathGeneration: x.b.pathGeneration, SampleGeneration: x.b.sampleGeneration, Time: x.now, Lost: lost, PriorInFlight: flight, PostInFlight: flight - protocol.ByteCount(n)*1200, Delivery: DeliverySample{Delivered: x.delivered, Lost: x.lost}})
}

// TestTraceFixtureLong writes a second fixture: a BDP above 63 packets and loss
// only while probing (so the short-term bounds stay unset and the wall-clock
// trigger ends Cruise), with multi-packet loss bursts.
func TestTraceFixtureLong(t *testing.T) {
	out := os.Getenv("BBR_TRACE_FIXTURE_LONG")
	if out == "" {
		t.Skip("set BBR_TRACE_FIXTURE_LONG to write the checker's second fixture trace")
	}
	traceConfigure([]string{"fixture", "-role", "send"}, out, strconv.FormatInt(time.Now().Add(time.Hour).UnixNano(), 10))
	t.Cleanup(func() { traceConfigure(nil, "", "") })
	x := newBBRProbeTrace()
	x.b.random = func(n int32) int32 { return n / 3 }
	for range 4 {
		x.ack(1000000, 200000, SendUnknown)
	}
	for i := range 900 {
		flight := max(1200, min(protocol.ByteCount(100000+(i%5)*15000), x.b.GetCongestionWindow()-1200))
		probing := x.b.phase == bbrUp || x.b.phase == bbrRefill
		switch {
		case probing && i%3 == 0:
			x.loseBurst(4, flight+1200) // 4.8 KB of about 100 KB: crosses at the second packet
		case probing && i%3 == 1:
			x.lose(flight+1200, SendUnknown) // one packet: about 1%, never crosses
		}
		rate := uint64(1000000 + (i%9)*20000)
		if i%131 > 110 {
			rate = 700000
		}
		x.ack(rate, flight, SendUnknown)
	}
	traceDump()
	require.FileExists(t, out)
	require.Zero(t, trace.overflow)
}

func TestTraceResetRow(t *testing.T) {
	traceEnable(t)
	x := newBBRProbeTrace()
	x.cruise()
	x.b.Reset(x.b.pathGeneration, x.b.sampleGeneration+1, x.delivered)
	rows := traceRows(traceKindReset)
	require.Len(t, rows, 1)
	require.EqualValues(t, x.b.sampleGeneration, rows[0][41])
}
