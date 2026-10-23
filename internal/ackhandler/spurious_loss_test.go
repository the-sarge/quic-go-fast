package ackhandler

import (
	"slices"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

type spuriousLossFixture struct {
	h       *sentPacketHandler
	start   monotime.Time
	packets packetTracker
	events  events.Recorder
}

func newSpuriousLossFixture(ecn bool) *spuriousLossFixture {
	f := &spuriousLossFixture{start: monotime.Time(time.Hour)}
	f.h = NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, ecn, nil, protocol.PerspectiveServer, &f.events, utils.DefaultLogger).(*sentPacketHandler)
	return f
}

func (f *spuriousLossFixture) send(at time.Duration, level protocol.EncryptionLevel) protocol.PacketNumber {
	pn := f.h.PopPacketNumber(level)
	mark := protocol.ECNNon
	if level == protocol.Encryption1RTT {
		mark = f.h.ECNMode(true)
	}
	f.h.SentPacket(f.start.Add(at), pn, protocol.InvalidPacketNumber, nil, []Frame{f.packets.NewPingFrame(pn)}, level, mark, 1200, false, false)
	return pn
}

func (f *spuriousLossFixture) ack(t *testing.T, at time.Duration, pns ...protocol.PacketNumber) {
	t.Helper()
	_, err := f.h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(slices.Clone(pns)...)}, protocol.Encryption1RTT, f.start.Add(at))
	require.NoError(t, err)
}

func (f *spuriousLossFixture) loseFirstTwo(t *testing.T) []protocol.PacketNumber {
	t.Helper()
	pns := make([]protocol.PacketNumber, 5)
	for i := range pns {
		pns[i] = f.send(time.Duration(i)*time.Millisecond, protocol.Encryption1RTT)
	}
	f.ack(t, 100*time.Millisecond, pns[4])
	require.Equal(t, pns[:2], f.packets.Lost)
	f.events.Clear()
	return pns
}

func TestSpuriousLossLateOnlyACK(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := f.loseFirstTwo(t)
	flight := f.h.bytesInFlight
	rtt := f.h.rttStats.SmoothedRTT()
	f.ack(t, 105*time.Millisecond, pns[0], pns[4])
	require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1)
	require.Equal(t, flight, f.h.bytesInFlight)
	require.Equal(t, rtt, f.h.rttStats.SmoothedRTT(), "retained receipt is not a new RTT observation")
	require.Equal(t, pns[:2], f.packets.Lost)
	require.Equal(t, []protocol.PacketNumber{pns[4]}, f.packets.Acked)

	f.events.Clear()
	f.ack(t, 106*time.Millisecond, pns[0], pns[4])
	require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
	require.Equal(t, flight, f.h.bytesInFlight)
}

func TestSpuriousLossReorderedACK(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained only", true: "retained and live"}[live], func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			pns := f.loseFirstTwo(t)
			flight := f.h.bytesInFlight
			rtt := f.h.rttStats.SmoothedRTT()
			receipt := []protocol.PacketNumber{pns[0]}
			wantAcked := []protocol.PacketNumber{pns[4]}
			var wantReordering uint64
			if live {
				receipt = append(receipt, pns[2])
				flight -= 1200
				wantAcked = append(wantAcked, pns[2])
				wantReordering = 2
			}
			f.ack(t, 105*time.Millisecond, receipt...)
			require.Equal(t, []qlogwriter.Event{qlog.SpuriousLoss{
				EncryptionLevel: protocol.Encryption1RTT, PacketNumber: pns[0],
				PacketReordering: wantReordering, TimeReordering: 105 * time.Millisecond,
			}}, f.events.Events(qlog.SpuriousLoss{}))
			require.Equal(t, flight, f.h.bytesInFlight)
			require.Equal(t, rtt, f.h.rttStats.SmoothedRTT(), "older sends must not update RTT")
			require.Equal(t, wantAcked, f.packets.Acked)
			require.Equal(t, pns[:2], f.packets.Lost)

			f.events.Clear()
			f.ack(t, 106*time.Millisecond, receipt...)
			require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
			// This receipt overlaps the consumed record but also covers another loss.
			f.ack(t, 107*time.Millisecond, pns[0], pns[1], pns[4])
			require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1)
			require.Equal(t, pns[1], f.events.Events(qlog.SpuriousLoss{})[0].(qlog.SpuriousLoss).PacketNumber)
			require.Equal(t, flight, f.h.bytesInFlight)
			require.Equal(t, wantAcked, f.packets.Acked)
		})
	}
}

func TestSpuriousLossPacketEligibility(t *testing.T) {
	for _, kind := range []string{"ordinary", "MTU probe", "path probe", "ACK only"} {
		t.Run(kind, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			pn := f.h.PopPacketNumber(protocol.Encryption1RTT)
			frames := []Frame{f.packets.NewPingFrame(pn)}
			if kind == "ACK only" {
				frames = nil
			}
			f.h.SentPacket(f.start, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, protocol.ECNNon, 1200, kind == "MTU probe", kind == "path probe")
			var pns []protocol.PacketNumber
			for i := range 4 {
				pns = append(pns, f.send(time.Duration(i+1)*time.Millisecond, protocol.Encryption1RTT))
			}
			f.ack(t, 100*time.Millisecond, pns[3])
			flight := f.h.bytesInFlight
			lost := append([]protocol.PacketNumber(nil), f.packets.Lost...)
			f.events.Clear()
			f.ack(t, 105*time.Millisecond, pn, pns[1], pns[3])
			if kind == "ordinary" {
				require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1)
			} else {
				require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
			}
			require.Equal(t, flight-1200, f.h.bytesInFlight)
			require.Equal(t, lost, f.packets.Lost, "late receipts do not repeat loss callbacks")
			require.Equal(t, []protocol.PacketNumber{pns[3], pns[1]}, f.packets.Acked)
			f.events.Clear()
			f.ack(t, 106*time.Millisecond, pn, pns[1], pns[3])
			require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
			require.Equal(t, flight-1200, f.h.bytesInFlight)
			var fresh []protocol.PacketNumber
			for range 5 {
				fresh = append(fresh, f.send(110*time.Millisecond, protocol.Encryption1RTT))
			}
			f.ack(t, 210*time.Millisecond, fresh[4])
			if kind == "ordinary" {
				require.NotContains(t, f.packets.Lost, fresh[0])
			} else {
				require.Contains(t, f.packets.Lost, fresh[0], "excluded receipts cannot train loss policy")
				require.Contains(t, f.packets.Lost, fresh[1])
			}
		})
	}
}

func TestSpuriousLossMigration(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := f.loseFirstTwo(t)
	f.h.MigratedPath(f.start.Add(110*time.Millisecond), 1200)
	fresh := f.send(120*time.Millisecond, protocol.Encryption1RTT)
	f.ack(t, 220*time.Millisecond, pns[0], fresh)
	require.Empty(t, f.events.Events(qlog.SpuriousLoss{}), "old-path losses cannot become new-path evidence")
	require.Zero(t, f.h.bytesInFlight)
	require.Equal(t, []protocol.PacketNumber{pns[4], fresh}, f.packets.Acked)
	require.Equal(t, pns[:4], f.packets.Lost)

	// History on the new path is still usable after the reset.
	var next []protocol.PacketNumber
	for range 5 {
		next = append(next, f.send(230*time.Millisecond, protocol.Encryption1RTT))
	}
	f.ack(t, 330*time.Millisecond, next[4])
	f.events.Clear()
	f.ack(t, 335*time.Millisecond, pns[1], next[0], next[4])
	require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1)
	require.Equal(t, next[0], f.events.Events(qlog.SpuriousLoss{})[0].(qlog.SpuriousLoss).PacketNumber)
}

func TestSpuriousLossACKAdmission(t *testing.T) {
	for _, kind := range []string{"unsent", "skipped"} {
		t.Run(kind, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			pns := f.loseFirstTwo(t)
			invalid := pns[4] + 1
			if kind == "skipped" {
				previous := pns[4]
				for {
					pn := f.send(10*time.Millisecond, protocol.Encryption1RTT)
					if pn > previous+1 {
						invalid = pn - 1
						break
					}
					require.Less(t, pn, protocol.PacketNumber(1e6))
					previous = pn
				}
			}
			flight, window := f.h.bytesInFlight, f.h.congestion.GetCongestionWindow()
			rtt := f.h.rttStats.SmoothedRTT()
			f.events.Clear()
			_, err := f.h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[0], pns[2], invalid)}, protocol.Encryption1RTT, f.start.Add(105*time.Millisecond))
			require.ErrorIs(t, err, &qerr.TransportError{ErrorCode: qerr.ProtocolViolation})
			require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
			require.Equal(t, flight, f.h.bytesInFlight)
			require.Equal(t, window, f.h.congestion.GetCongestionWindow())
			require.Equal(t, rtt, f.h.rttStats.SmoothedRTT())
			require.Equal(t, pns[:2], f.packets.Lost)
			require.Equal(t, []protocol.PacketNumber{pns[4]}, f.packets.Acked)

			f.ack(t, 106*time.Millisecond, pns[0], pns[2], pns[4])
			require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1, "rejected ranges must not consume retained receipts")
			require.Equal(t, flight-1200, f.h.bytesInFlight)
		})
	}
}

func TestSpuriousLossHistoryBound(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := make([]protocol.PacketNumber, 80)
	for i := range pns {
		pns[i] = f.send(0, protocol.Encryption1RTT)
	}
	f.ack(t, 100*time.Millisecond, pns[79])
	require.Equal(t, pns[:77], f.packets.Lost)
	f.events.Clear()
	f.ack(t, 105*time.Millisecond, pns...)
	reports := f.events.Events(qlog.SpuriousLoss{})
	require.Len(t, reports, 64)
	for i, report := range reports {
		require.Equal(t, pns[13+i], report.(qlog.SpuriousLoss).PacketNumber)
	}
	require.Zero(t, f.h.bytesInFlight)
	require.Equal(t, []protocol.PacketNumber{pns[79], pns[77], pns[78]}, f.packets.Acked)
	require.Equal(t, pns[:77], f.packets.Lost)
	f.events.Clear()
	f.ack(t, 106*time.Millisecond, pns...)
	require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
}

func TestSpuriousLossHistoryExpiry(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := f.loseFirstTwo(t)
	f.ack(t, 105*time.Millisecond, pns[2:]...)
	lifetime := 3 * f.h.rttStats.PTO(false)
	// Cleanup is strict: a send exactly at the cutoff remains discoverable.
	f.ack(t, lifetime, pns[4])
	f.ack(t, lifetime, pns[0], pns[4])
	require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1)
	f.events.Clear()
	// A duplicate ACK with no live records still performs age cleanup.
	f.ack(t, lifetime+2*time.Millisecond, pns[4])
	f.ack(t, lifetime+3*time.Millisecond, pns[1], pns[4])
	require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
	require.Equal(t, pns[:2], f.packets.Lost)
	require.Equal(t, []protocol.PacketNumber{pns[4], pns[2], pns[3]}, f.packets.Acked)
}

func TestSpuriousLossLegacyECNEligibility(t *testing.T) {
	for _, kind := range []string{"late only", "reordered with live", "same largest with live"} {
		t.Run(kind, func(t *testing.T) {
			f := newSpuriousLossFixture(true)
			pns := make([]protocol.PacketNumber, 10)
			for i := range pns {
				pns[i] = f.send(time.Duration(i)*time.Millisecond, protocol.Encryption1RTT)
			}
			_, err := f.h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[9]), ECT0: 1}, protocol.Encryption1RTT, f.start.Add(100*time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, protocol.ECT0, f.h.ECNMode(true))
			f.events.Clear()
			receipt := []protocol.PacketNumber{pns[0]}
			if kind != "late only" {
				receipt = append(receipt, pns[7])
			}
			if kind != "reordered with live" {
				receipt = append(receipt, pns[9])
			}
			// These counts would fail ECN validation if either legacy gate were loosened.
			_, err = f.h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(receipt...), ECT0: 1000}, protocol.Encryption1RTT, f.start.Add(105*time.Millisecond))
			require.NoError(t, err)
			require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1)
			require.Equal(t, protocol.ECT0, f.h.ECNMode(true))
			fresh := f.send(110*time.Millisecond, protocol.Encryption1RTT)
			_, err = f.h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(fresh), ECT0: 2}, protocol.Encryption1RTT, f.start.Add(210*time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, protocol.ECT0, f.h.ECNMode(true), "fresh advancing ACKs still validate ECN")
		})
	}
}

func TestSpuriousLossSharedApplicationSpace(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := make([]protocol.PacketNumber, 5)
	for i := range pns {
		level := protocol.Encryption1RTT
		if i < 2 {
			level = protocol.Encryption0RTT
		}
		pns[i] = f.send(time.Duration(i)*time.Millisecond, level)
	}
	f.ack(t, 100*time.Millisecond, pns[4])
	require.Equal(t, pns[:2], f.packets.Lost)
	f.ack(t, 105*time.Millisecond, pns[0], pns[1], pns[4])
	reports := f.events.Events(qlog.SpuriousLoss{})
	require.Len(t, reports, 2)
	for i, report := range reports {
		e := report.(qlog.SpuriousLoss)
		require.Equal(t, pns[i], e.PacketNumber)
		require.Equal(t, protocol.Encryption1RTT, e.EncryptionLevel)
	}
	require.Equal(t, []protocol.PacketNumber{pns[4]}, f.packets.Acked)
	f.events.Clear()
	f.ack(t, 106*time.Millisecond, pns[0], pns[1])
	require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
}

func TestSpuriousLossDisposal(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected 0-RTT", true: "Retry"}[retry], func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			initial := f.h.PopPacketNumber(protocol.EncryptionInitial)
			f.h.SentPacket(f.start, initial, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.EncryptionInitial, protocol.ECNNon, 1200, false, false)
			a := f.send(0, protocol.Encryption0RTT)
			b := f.send(time.Millisecond, protocol.Encryption0RTT)
			if retry {
				f.h.ResetForRetry(f.start.Add(100 * time.Millisecond))
				require.Equal(t, []protocol.PacketNumber{a, b}, f.packets.Lost)
			} else {
				f.h.DropPackets(protocol.Encryption0RTT, f.start.Add(100*time.Millisecond))
				f.h.DropPackets(protocol.EncryptionInitial, f.start.Add(100*time.Millisecond))
				require.Empty(t, f.packets.Lost)
			}
			lost := slices.Clone(f.packets.Lost)
			fresh := f.send(110*time.Millisecond, protocol.Encryption1RTT)
			f.ack(t, 210*time.Millisecond, a, b, fresh)
			require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
			require.Zero(t, f.h.bytesInFlight)
			require.Equal(t, lost, f.packets.Lost)
			require.Equal(t, []protocol.PacketNumber{fresh}, f.packets.Acked)
		})
	}
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake} {
		t.Run(level.String()+" discard", func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			var pns []protocol.PacketNumber
			for range 5 {
				pns = append(pns, f.send(0, level))
			}
			_, err := f.h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[4])}, level, f.start.Add(100*time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, pns[:2], f.packets.Lost)
			f.h.DropPackets(level, f.start.Add(110*time.Millisecond))
			fresh := f.send(120*time.Millisecond, protocol.Encryption1RTT)
			f.ack(t, 220*time.Millisecond, fresh)
			require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
			require.Zero(t, f.h.bytesInFlight)
			require.Equal(t, pns[:2], f.packets.Lost)
		})
	}
}

func TestSpuriousLossRichFeedback(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(map[bool]string{false: "late-only branch", true: "ordinary branch"}[live], func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			r := &congestionRecorder{}
			EnableDeliverySampling(f.h, r, func() protocol.ByteCount { return 0 })
			pns := f.loseFirstTwo(t)
			require.Len(t, r.feedback, 1)
			require.Len(t, r.feedback[0].Lost, 2)
			receipt := []protocol.PacketNumber{pns[0]}
			wantFlight := protocol.ByteCount(2400)
			wantDelivered := uint64(2400)
			if live {
				receipt = append(receipt, pns[2])
				wantFlight = 1200
				wantDelivered = 3600
			} else {
				receipt = append(receipt, pns[4])
			}
			f.ack(t, 105*time.Millisecond, receipt...)
			require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1)
			require.Len(t, r.feedback, 2)
			e := r.feedback[1]
			require.EqualValues(t, 2400, e.PriorInFlight)
			require.Equal(t, wantFlight, e.PostInFlight)
			require.Equal(t, wantDelivered, e.Delivery.Delivered)
			require.Equal(t, uint64(1), e.Acked[0].Ordinal)
			require.Equal(t, congestion.DeliveryLost, e.Acked[0].Retirement)
			require.Equal(t, live, e.RTTEligible)
			require.False(t, e.RTTUpdated)
			require.Zero(t, e.RawRTT)
			if live {
				require.Len(t, e.Acked, 2)
				require.Equal(t, uint64(3), e.Acked[1].Ordinal)
				require.Equal(t, congestion.DeliveryLive, e.Acked[1].Retirement)
			} else {
				require.Len(t, e.Acked, 1)
			}
			require.Equal(t, 1, f.h.DeliveryStats().Retained)
			f.events.Clear()
			f.ack(t, 106*time.Millisecond, receipt...)
			require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
			require.Len(t, r.feedback, 2, "neither history may redeliver a consumed receipt")
		})
	}
}

func TestSpuriousLossDoesNotRevisitRetiredDatagram(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pn := f.h.PopPacketNumber(protocol.Encryption1RTT)
	frames := []Frame{{Frame: &wire.DatagramFrame{Data: []byte("unreliable payload")}}}
	f.h.SentPacket(f.start, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
	var pns []protocol.PacketNumber
	for range 4 {
		pns = append(pns, f.send(time.Millisecond, protocol.Encryption1RTT))
	}
	f.ack(t, 100*time.Millisecond, pns[3])
	// The caller can reuse retired frame storage. Late receipts own metadata only.
	frames[0] = Frame{Frame: &wire.PingFrame{}, Handler: &customFrameHandler{
		onLost:  func(wire.Frame) { t.Fatal("late receipt retransmitted reused storage") },
		onAcked: func(wire.Frame) { t.Fatal("late receipt acknowledged reused storage") },
	}}
	flight := f.h.bytesInFlight
	f.ack(t, 105*time.Millisecond, pn, pns[3])
	require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 1)
	require.Equal(t, flight, f.h.bytesInFlight)
	f.events.Clear()
	f.ack(t, 106*time.Millisecond, pn, pns[3])
	require.Empty(t, f.events.Events(qlog.SpuriousLoss{}))
	require.Equal(t, flight, f.h.bytesInFlight)
}
