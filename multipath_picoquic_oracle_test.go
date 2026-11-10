//go:build picoquic

package quic

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

func TestPicoquicMatrixAwaitsTraceConsumption(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		trace := newMatrixTrace()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- trace.wait(ctx, trace.usefulTrafficReady) }()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("returned before the recorder consumed useful traffic: %v", err)
		default:
		}
		for _, id := range []uint64{0, 1} {
			frames := []qlog.Frame{{Frame: &qlog.StreamFrame{Length: 32}}, {Frame: &qlog.DatagramFrame{Length: 32}}}
			trace.RecordEvent(qlog.PacketSent{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: id}, Frames: frames})
			trace.RecordEvent(qlog.PacketReceived{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: id}, Frames: frames})
		}
		synctest.Wait()
		require.NoError(t, <-done)
	})
}

func TestPicoquicMatrixCIDReceipt(t *testing.T) {
	sent := func(pn protocol.PacketNumber, sequence, retire uint64) qlog.PacketSent {
		return qlog.PacketSent{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT, PacketNumber: pn}, Path: &qlog.PathIdentity{PathID: 0}, Frames: []qlog.Frame{{Frame: &qlog.PathNewConnectionIDFrame{PathID: 2, SequenceNumber: sequence, RetirePriorTo: retire}}}}
	}
	ack := func(pn protocol.PacketNumber) qlog.PacketReceived {
		return qlog.PacketReceived{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: 0}, Frames: []qlog.Frame{{Frame: &qlog.AckFrame{AckRanges: []qlog.AckRange{{Smallest: pn, Largest: pn}}}}}}
	}
	trace := func() *matrixTrace {
		m := newMatrixTrace()
		m.cidReceipt = &matrixCIDReceipt{}
		return m
	}
	for _, test := range []struct {
		name   string
		events []qlogwriter.Event
		wantPN protocol.PacketNumber
	}{
		{"sent then ACK", []qlogwriter.Event{sent(100, 1, 0), ack(100)}, 100},
		{"ACK recorder first", []qlogwriter.Event{ack(100), sent(100, 1, 0)}, 100},
		{"later transmission acknowledged", []qlogwriter.Event{sent(100, 1, 0), sent(101, 1, 0), ack(101)}, 101},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := trace()
			for _, event := range test.events {
				m.RecordEvent(event)
			}
			witness, ready, err := m.cidReceiptStatus()
			require.NoError(t, err)
			require.True(t, ready)
			require.Equal(t, test.wantPN, witness.PacketNumber)
			require.Equal(t, uint64(1), witness.Sequence)
		})
	}
	t.Run("unrelated evidence", func(t *testing.T) {
		wrongTarget := ack(100)
		wrongTarget.Frames[0].Frame.(*qlog.AckFrame).PathID = 1
		wrongTarget.Frames[0].Frame.(*qlog.AckFrame).HasPathID = true
		wrongCarrier := ack(100)
		wrongCarrier.Path = &qlog.PathIdentity{PathID: 1}
		handshake := ack(100)
		handshake.Header.PacketType = qlog.PacketTypeHandshake
		missingCarrier := ack(100)
		missingCarrier.Path = nil
		empty := ack(100)
		empty.Frames[0].Frame.(*qlog.AckFrame).AckRanges = nil
		gap := ack(99)
		gap.Frames[0].Frame.(*qlog.AckFrame).AckRanges = []qlog.AckRange{{Smallest: 101, Largest: 101}, {Smallest: 99, Largest: 99}}
		for _, event := range []qlogwriter.Event{wrongTarget, wrongCarrier, handshake, missingCarrier, empty, gap, qlog.PacketSent{Header: ack(100).Header, Path: ack(100).Path, Frames: ack(100).Frames}, qlog.PacketDropped{Header: ack(100).Header}} {
			m := trace()
			m.RecordEvent(sent(100, 1, 0))
			m.RecordEvent(event)
			_, ready, err := m.cidReceiptStatus()
			require.NoError(t, err)
			require.False(t, ready, "%T supplies no matching ACK", event)
		}
		for _, event := range []qlogwriter.Event{sent(100, 1, 0), ack(100)} {
			m := trace()
			m.RecordEvent(event)
			_, ready, err := m.cidReceiptStatus()
			require.NoError(t, err)
			require.False(t, ready, "both observations are required")
		}
		m := trace()
		other := sent(100, 1, 0)
		other.Frames[0].Frame.(*qlog.PathNewConnectionIDFrame).PathID = 1
		m.RecordEvent(other)
		m.RecordEvent(ack(100))
		_, ready, err := m.cidReceiptStatus()
		require.NoError(t, err)
		require.False(t, ready)
		for _, alter := range []func(*qlog.PacketSent){
			func(e *qlog.PacketSent) { e.Path = nil },
			func(e *qlog.PacketSent) { e.Path.PathID = 1 },
			func(e *qlog.PacketSent) { e.Header.PacketType = qlog.PacketTypeHandshake },
			func(e *qlog.PacketSent) { e.Header.PacketNumber = protocol.InvalidPacketNumber },
		} {
			m := trace()
			event := sent(100, 1, 0)
			alter(&event)
			m.RecordEvent(event)
			m.RecordEvent(ack(100))
			_, ready, err := m.cidReceiptStatus()
			require.NoError(t, err)
			require.False(t, ready)
		}
	})
	t.Run("copied ACK and explicit path zero", func(t *testing.T) {
		m := trace()
		event := ack(100)
		frame := event.Frames[0].Frame.(*qlog.AckFrame)
		frame.HasPathID = true
		m.RecordEvent(event)
		frame.AckRanges[0] = qlog.AckRange{Smallest: 200, Largest: 200}
		m.RecordEvent(sent(100, 1, 0))
		_, ready, err := m.cidReceiptStatus()
		require.NoError(t, err)
		require.True(t, ready, "producer range reuse cannot change copied evidence")
	})
	t.Run("copied transmission and superseded candidate", func(t *testing.T) {
		m := trace()
		event := sent(100, 1, 0)
		m.RecordEvent(event)
		event.Frames[0].Frame.(*qlog.PathNewConnectionIDFrame).SequenceNumber = 200
		m.RecordEvent(sent(101, 2, 2))
		m.RecordEvent(ack(100))
		_, ready, err := m.cidReceiptStatus()
		require.NoError(t, err)
		require.False(t, ready, "acknowledged old sequence is below the observed cutoff")
		m.RecordEvent(ack(101))
		witness, ready, err := m.cidReceiptStatus()
		require.NoError(t, err)
		require.True(t, ready)
		require.Equal(t, uint64(2), witness.Sequence)
	})
	t.Run("retirement invalidates earlier witness", func(t *testing.T) {
		m := trace()
		m.RecordEvent(sent(100, 1, 0))
		m.RecordEvent(ack(100))
		m.RecordEvent(sent(101, 2, 2))
		_, ready, err := m.cidReceiptStatus()
		require.ErrorContains(t, err, "retirement")
		require.False(t, ready)
	})
	t.Run("bounded receipt fails closed", func(t *testing.T) {
		for _, kind := range []string{"transmissions", "ACK ranges"} {
			m := trace()
			limit := 64
			if kind == "ACK ranges" {
				limit = 256
			}
			for i := 0; i <= limit; i++ {
				if kind == "transmissions" {
					m.RecordEvent(sent(protocol.PacketNumber(i), 1, 0))
				} else {
					m.RecordEvent(ack(protocol.PacketNumber(i)))
				}
			}
			m.RecordEvent(sent(100, 1, 0))
			m.RecordEvent(ack(100))
			_, ready, err := m.cidReceiptStatus()
			require.ErrorContains(t, err, kind)
			require.False(t, ready)
		}
	})
	t.Run("waiter consumes receipt or cancellation", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			m := trace()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := m.waitCIDReceipt(ctx); done <- err }()
			m.RecordEvent(ack(100))
			m.RecordEvent(sent(100, 1, 0))
			require.NoError(t, <-done)
			m = trace()
			cancel()
			_, err := m.waitCIDReceipt(ctx)
			require.ErrorIs(t, err, context.Canceled)
		})
	})
}

func TestPicoquicMatrixReplayDispositions(t *testing.T) {
	const heldSize = 77
	for _, test := range []struct {
		name  string
		event qlogwriter.Event
	}{
		{"decoded", qlog.PacketReceived{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: 1}, Raw: qlog.RawInfo{Length: heldSize}}},
		{"duplicate", qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize}, Trigger: qlog.PacketDropDuplicate}},
		{"unknown_connection_id", qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize}, Trigger: qlog.PacketDropUnknownConnectionID}},
		{"key_unavailable", qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize}, Trigger: qlog.PacketDropKeyUnavailable}},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := newMatrixTrace()
			before := trace.replayEvidence(heldSize)
			require.Empty(t, trace.replayDisposition(heldSize, before))
			trace.RecordEvent(test.event)
			require.Equal(t, test.name, trace.replayDisposition(heldSize, before))
			require.Empty(t, trace.replayDisposition(heldSize, trace.replayEvidence(heldSize)), "old evidence cannot supply a new replay")
		})
	}
	t.Run("unrelated evidence", func(t *testing.T) {
		trace := newMatrixTrace()
		before := trace.replayEvidence(heldSize)
		trace.RecordEvent(qlog.PacketReceived{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: 0}, Raw: qlog.RawInfo{Length: heldSize}})
		trace.RecordEvent(qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize + 1}, Trigger: qlog.PacketDropUnknownConnectionID})
		trace.RecordEvent(qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize}, Trigger: qlog.PacketDropPayloadDecryptError})
		require.Empty(t, trace.replayDisposition(heldSize, before))
	})
}

func TestPicoquicMatrixMTUFinalLossObservation(t *testing.T) {
	const rtt = 10 * time.Millisecond
	rttStats := utils.NewRTTStats()
	rttStats.SetInitialRTT(rtt)
	var recorder events.Recorder
	finder := newMTUDiscoverer(rttStats, 1200, 1440, &recorder)
	now := monotime.Now()
	finder.Start(now)
	now = now.Add(mtuProbeDelay * rtt)
	var lastProbe protocol.ByteCount
	for probe := 0; probe < 16 && finder.ShouldSendProbe(now); probe++ {
		ping, size := finder.GetPing(now)
		lastProbe = size
		if size <= 1280 {
			ping.Handler.OnAcked(ping.Frame)
		} else {
			ping.Handler.OnLost(ping.Frame)
		}
		now = now.Add(mtuProbeDelay * rtt)
	}
	updates := recorder.Events(qlog.MTUUpdated{})
	require.NotEmpty(t, updates)
	require.Equal(t, protocol.ByteCount(1295), lastProbe, "the canonical final probe is lost")
	require.Equal(t, qlog.MTUUpdated{Value: 1278, Done: false}, updates[len(updates)-1])
	size, complete := matrixMTUState(finder)
	require.Equal(t, uint32(1278), size)
	require.True(t, complete, "the owner completed discovery even though its last qlog update did not")
}

func TestPicoquicMatrixMTUProgress(t *testing.T) {
	t.Run("already complete", func(t *testing.T) {
		count, err := matrixMTUProgress(t.Context(), nil, func() bool { return true }, func() { t.Fatal("no progress is needed") })
		require.NoError(t, err)
		require.Zero(t, count)
	})
	t.Run("pending owner needs paced traffic without trace wake", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			start, previous := time.Now(), time.Now()
			calls := 0
			count, err := matrixMTUProgress(ctx, nil, func() bool { return calls == 3 }, func() {
				require.GreaterOrEqual(t, time.Since(previous), matrixMTUProgressInterval)
				previous = time.Now()
				calls++
			})
			require.NoError(t, err)
			require.Equal(t, 3, count)
			require.Equal(t, 3*matrixMTUProgressInterval, time.Since(start))
		})
	})
	t.Run("notifications cannot amplify traffic", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			changed := make(chan struct{}, 64)
			for range cap(changed) {
				changed <- struct{}{}
			}
			start := time.Now()
			calls := 0
			count, err := matrixMTUProgress(ctx, changed, func() bool { return calls > 0 }, func() {
				require.GreaterOrEqual(t, time.Since(start), matrixMTUProgressInterval)
				calls++
			})
			require.NoError(t, err)
			require.Equal(t, 1, count)
		})
	})
	t.Run("completion notification requires no pulse", func(t *testing.T) {
		changed := make(chan struct{}, 1)
		changed <- struct{}{}
		checks := 0
		count, err := matrixMTUProgress(t.Context(), changed, func() bool { checks++; return checks == 2 }, func() { t.Fatal("completed owner needs no traffic") })
		require.NoError(t, err)
		require.Zero(t, count)
	})
	t.Run("completed owner without notification needs no pulse", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			checks := 0
			count, err := matrixMTUProgress(ctx, nil, func() bool { checks++; return checks == 2 }, func() { t.Fatal("timer must recheck completion first") })
			require.NoError(t, err)
			require.Zero(t, count)
		})
	})
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		count, err := matrixMTUProgress(ctx, nil, func() bool { return false }, func() { t.Fatal("cancellation forbids progress") })
		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, count)
	})
	t.Run("deadline stops pending work", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			defer cancel()
			calls := 0
			count, err := matrixMTUProgress(ctx, nil, func() bool { return false }, func() { calls++ })
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, 2, count)
			require.Equal(t, count, calls)
		})
	})
	t.Run("finite exchange cap", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 31*time.Second)
			defer cancel()
			calls := 0
			count, err := matrixMTUProgress(ctx, nil, func() bool { return false }, func() { calls++ })
			require.ErrorContains(t, err, "300 exchanges")
			require.Equal(t, matrixMTUProgressLimit, count)
			require.Equal(t, count, calls)
		})
	})
}
