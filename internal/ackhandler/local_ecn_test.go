package ackhandler

import (
	"fmt"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func newLocalECNTestHandler(t *testing.T, bbr bool, capable ...*bool) (*sentPacketHandler, *congestionRecorder) {
	t.Helper()
	handler, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
	require.NoError(t, err)
	h := handler.(*sentPacketHandler)
	r := &congestionRecorder{}
	if bbr {
		b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
		h.congestionEvents.sink = &localRichRecorder{recorder: r, controller: b}
		EnableBBRECN(h, func(bool) (uint64, bool, bool) {
			if len(capable) > 0 {
				return h.congestionEvents.pathGeneration, true, *capable[0]
			}
			return h.congestionEvents.pathGeneration, true, true
		})
	}
	return h, r
}

func localECNPacket(t *testing.T, h *sentPacketHandler, now monotime.Time) (protocol.PacketNumber, LocalCompletionToken) {
	t.Helper()
	mark := h.ECNMode(true)
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	token, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, mark, 1200)
	require.NoError(t, err)
	return pn, token
}

func TestLocalECNOutcomes(t *testing.T) {
	t.Run("uncertain loss degrades without claiming transmission", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			for _, ackOnly := range []bool{false, true} {
				t.Run(fmt.Sprintf("BBR=%t/ACK-only=%t", bbr, ackOnly), func(t *testing.T) {
					h, r := newLocalECNTestHandler(t, bbr)
					now := monotime.Now()
					a, ta := localECNPacket(t, h, now)
					require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(time.Millisecond)))
					ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(a), 1, 0)
					frames := []Frame{{Frame: &wire.PingFrame{}}}
					if ackOnly {
						frames = nil
					}
					uncertain := h.PopPacketNumber(protocol.Encryption1RTT)
					token, err := h.RegisterLocalPacket(PacketContext{}, now.Add(3*time.Millisecond), uncertain, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, h.ECNMode(true), 1200)
					require.NoError(t, err)
					require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUncertain, now.Add(4*time.Millisecond)))
					before := len(r.feedback)
					var higher []protocol.PacketNumber
					for range 4 {
						pn, token := localECNPacket(t, h, now.Add(5*time.Millisecond))
						higher = append(higher, pn)
						require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(6*time.Millisecond)))
					}
					ackBBRECNAt(t, h, now.Add(7*time.Millisecond), ackRanges(higher...), 5, 0)
					require.Equal(t, protocol.ECNNon, h.ECNMode(true))
					if ackOnly {
						for _, event := range r.feedback[before:] {
							require.Empty(t, event.Lost, "ACK-only retirement supplies no congestion-loss credit")
						}
					}
					ackBBRECNAt(t, h, now.Add(8*time.Millisecond), ackRanges(append(higher, uncertain)...), 6, 0)
					require.Equal(t, protocol.ECNNon, h.ECNMode(true), "late evidence cannot resurrect failed validation")
				})
			}
		}
	})
	t.Run("uncertain PTO retirement supplies no network loss credit", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			h, r := newLocalECNTestHandler(t, bbr)
			now := monotime.Now()
			a, ta := localECNPacket(t, h, now)
			require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(time.Millisecond)))
			ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(a), 1, 0)
			pn, token := localECNPacket(t, h, now.Add(3*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUncertain, now.Add(4*time.Millisecond)))
			before := len(r.feedback)
			require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, now.Add(5*time.Millisecond)))
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			ackBBRECNAt(t, h, now.Add(6*time.Millisecond), ackRanges(pn), 2, 0)
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			for _, event := range r.feedback[before:] {
				require.Empty(t, event.Lost)
			}
		}
	})
	t.Run("testing loss waits for relevant reservations", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			h, _ := newLocalECNTestHandler(t, bbr)
			now := monotime.Now()
			var pns [numECNTestingPackets]protocol.PacketNumber
			var tokens [numECNTestingPackets]LocalCompletionToken
			for i := range tokens {
				pns[i], tokens[i] = localECNPacket(t, h, now)
			}
			require.NoError(t, h.CompleteLocalPacket(tokens[0], LocalPacketAccepted, now.Add(time.Millisecond)))
			var last protocol.PacketNumber
			for range 4 {
				pn, token := localECNPacket(t, h, now.Add(2*time.Millisecond))
				last = pn
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(3*time.Millisecond)))
			}
			ackBBRECNAt(t, h, now.Add(4*time.Millisecond), ackRanges(last), 0, 0)
			fresh, token := localECNPacket(t, h, now.Add(5*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(6*time.Millisecond)))
			ackBBRECNAt(t, h, now.Add(7*time.Millisecond), ackRanges(pns[1], fresh), 1, 0)
			require.Equal(t, protocol.ECT0, h.ECNMode(true), "qualified ECT receipt remains usable while other tests are pending")
			for _, token := range tokens[1:] {
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(8*time.Millisecond)))
			}
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
		}
	})

	for _, bbr := range []bool{false, true} {
		t.Run(map[bool]string{false: "Reno", true: "BBR"}[bbr], func(t *testing.T) {
			h, _ := newLocalECNTestHandler(t, bbr)
			now := monotime.Now()
			var pns [numECNTestingPackets]protocol.PacketNumber
			var tokens [numECNTestingPackets]LocalCompletionToken
			for i := range pns {
				pns[i], tokens[i] = localECNPacket(t, h, now)
			}
			require.Equal(t, protocol.ECNNon, h.ECNMode(true), "pending tests reserve the ten-packet flight")
			require.NoError(t, h.CompleteLocalPacket(tokens[2], LocalPacketUnsubmitted, now.Add(time.Millisecond)))
			require.Equal(t, protocol.ECT0, h.ECNMode(true), "local disposal returns testing reservation")
			replacement, rt := localECNPacket(t, h, now.Add(time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(rt, LocalPacketAccepted, now.Add(2*time.Millisecond)))
			require.NoError(t, h.CompleteLocalPacket(tokens[1], LocalPacketUncertain, now.Add(time.Millisecond)))
			for i := range tokens {
				if i == 1 || i == 2 {
					continue
				}
				require.NoError(t, h.CompleteLocalPacket(tokens[i], LocalPacketAccepted, now.Add(time.Millisecond)))
			}
			ackBBRECNAt(t, h, now.Add(3*time.Millisecond), ackRanges(pns[0]), 1, 0)
			require.Equal(t, protocol.ECT0, h.ECNMode(true), "qualified receipts suffice without treating uncertainty as transmission")
			ackBBRECNAt(t, h, now.Add(4*time.Millisecond), ackRanges(pns[1], replacement), 3, 0)
			require.Equal(t, protocol.ECT0, h.ECNMode(true), "authenticated receipt resolves possible transmission")
		})
	}
}

func TestLocalECNSnapshotSupersession(t *testing.T) {
	t.Run("retired-only CE precedes Reno loss undo", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			outcome LocalPacketOutcome
		}{
			{name: "immediate"},
			{name: "deferred accepted", outcome: LocalPacketAccepted},
			{name: "invalidated unsubmitted", outcome: LocalPacketUnsubmitted},
			{name: "terminal uncertain", outcome: LocalPacketUncertain},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h, _ := newLocalECNTestHandler(t, false)
				now := monotime.Now()
				anchor, token := localECNPacket(t, h, now)
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(anchor), 1, 0)
				var pns []protocol.PacketNumber
				for range 4 {
					pn, token := localECNPacket(t, h, now.Add(3*time.Millisecond))
					pns = append(pns, pn)
					require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(4*time.Millisecond)))
				}
				require.Equal(t, protocol.ByteCount(38400), h.congestion.GetCongestionWindow())
				ackBBRECNAt(t, h, now.Add(5*time.Millisecond), ackRanges(pns[3]), 2, 0)
				require.Equal(t, protocol.ByteCount(26880), h.congestion.GetCongestionWindow())
				last, token := localECNPacket(t, h, now.Add(6*time.Millisecond))
				pns = append(pns, last)
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(7*time.Millisecond)))
				for h.QueueProbePacketAt(protocol.Encryption1RTT, now.Add(8*time.Millisecond)) {
				}
				ce := uint64(1)
				var pending LocalCompletionToken
				if tc.outcome != localPacketPending {
					_, pending = localECNPacket(t, h, now.Add(9*time.Millisecond))
					ce = 2
				}
				ackBBRECNAt(t, h, now.Add(10*time.Millisecond), ackRanges(pns...), 5, ce)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				expected := protocol.ByteCount(18816)
				if tc.outcome != localPacketPending {
					require.Equal(t, protocol.ByteCount(26880), h.congestion.GetCongestionWindow(), "pending CE must resolve before undo can restore the window")
					require.NoError(t, h.CompleteLocalPacket(pending, tc.outcome, now.Add(11*time.Millisecond)))
					switch tc.outcome {
					case LocalPacketUnsubmitted:
						expected = 38400
						require.Equal(t, protocol.ECNNon, h.ECNMode(true), "invalid counters do not invent CE or suppress legitimate undo")
					case LocalPacketUncertain:
						require.Equal(t, protocol.ByteCount(26880), h.congestion.GetCongestionWindow())
						require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, now.Add(12*time.Millisecond)))
						expected = 38400
						require.Equal(t, protocol.ECNNon, h.ECNMode(true), "unavailable evidence ends the pending CE decision without invented loss")
					}
				}
				require.Equal(t, expected, h.congestion.GetCongestionWindow())
				ackBBRECNAt(t, h, now.Add(13*time.Millisecond), ackRanges(pns...), 5, ce)
				require.Equal(t, expected, h.congestion.GetCongestionWindow(), "duplicate receipts repeat neither CE nor undo")
			})
		}
	})
	t.Run("historical report limits survive later traffic", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			for _, supersede := range []bool{false, true} {
				h, _ := newLocalECNTestHandler(t, bbr)
				now := monotime.Now()
				a, _ := localECNPacket(t, h, now)
				_, tb := localECNPacket(t, h, now)
				_, tc := localECNPacket(t, h, now)
				ackBBRECNAt(t, h, now.Add(time.Millisecond), ackRanges(a), 3, 0)
				if supersede {
					d, td := localECNPacket(t, h, now.Add(2*time.Millisecond))
					require.NoError(t, h.CompleteLocalPacket(td, LocalPacketAccepted, now.Add(3*time.Millisecond)))
					ackBBRECNAt(t, h, now.Add(4*time.Millisecond), ackRanges(d), 3, 0)
				}
				require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketUnsubmitted, now.Add(5*time.Millisecond)))
				d, td := localECNPacket(t, h, now.Add(6*time.Millisecond))
				require.NoError(t, h.CompleteLocalPacket(td, LocalPacketAccepted, now.Add(7*time.Millisecond)))
				require.NoError(t, h.CompleteLocalPacket(tc, LocalPacketAccepted, now.Add(8*time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(9*time.Millisecond), ackRanges(d), 3, 0)
				require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			}
		}
	})
	t.Run("completion supplies ECN without ACK authority", func(t *testing.T) {
		h, r := newLocalECNTestHandler(t, true)
		now := monotime.Now()
		a, _ := localECNPacket(t, h, now)
		_, tb := localECNPacket(t, h, now)
		ackBBRECNAt(t, h, now.Add(time.Millisecond), ackRanges(a), 1, 1)
		before := len(r.feedback)
		require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(2*time.Millisecond)))
		var ce uint64
		for _, e := range r.feedback[before:] {
			require.False(t, e.HasAck)
			require.Zero(t, e.PersistentCongestion.EndOrdinal)
			ce += e.ECN.Delta.CE
		}
		require.Equal(t, uint64(1), ce)
	})

	t.Run("qualified CE progresses through a pipeline", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			h, r := newLocalECNTestHandler(t, bbr)
			now := monotime.Now()
			a, ta := localECNPacket(t, h, now)
			require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(time.Millisecond)))
			ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(a), 1, 0)
			b, tb := localECNPacket(t, h, now.Add(3*time.Millisecond))
			_, pending := localECNPacket(t, h, now.Add(3*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(4*time.Millisecond)))
			before := h.congestion.GetCongestionWindow()
			ackBBRECNAt(t, h, now.Add(5*time.Millisecond), ackRanges(b), 1, 1)
			if bbr {
				require.Equal(t, uint64(1), r.feedback[len(r.feedback)-1].ECN.Delta.CE)
			} else {
				require.Less(t, h.congestion.GetCongestionWindow(), before)
			}
			require.NoError(t, h.CompleteLocalPacket(pending, LocalPacketAccepted, now.Add(6*time.Millisecond)))
		}
	})

	for _, bbr := range []bool{false, true} {
		t.Run(map[bool]string{false: "Reno", true: "BBR"}[bbr], func(t *testing.T) {
			h, r := newLocalECNTestHandler(t, bbr)
			now := monotime.Now()
			a, ta := localECNPacket(t, h, now)
			b, tb := localECNPacket(t, h, now)
			_, tc := localECNPacket(t, h, now)
			ackBBRECNAt(t, h, now.Add(time.Millisecond), ackRanges(a), 1, 0)
			ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(b), 1, 1)
			require.NoError(t, h.CompleteLocalPacket(tc, LocalPacketUnsubmitted, now.Add(3*time.Millisecond)))
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			if bbr {
				var ce uint64
				for _, feedback := range r.feedback {
					ce += feedback.ECN.Delta.CE
				}
				require.Equal(t, uint64(1), ce)
			}
			require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(4*time.Millisecond)))
			require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(3*time.Millisecond)))
			d, td := localECNPacket(t, h, now.Add(5*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(td, LocalPacketAccepted, now.Add(6*time.Millisecond)))
			ackBBRECNAt(t, h, now.Add(7*time.Millisecond), ackRanges(d), 2, 1)
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			if bbr {
				require.Zero(t, r.feedback[len(r.feedback)-1].ECN.Delta.CE, "superseded snapshots cannot duplicate congestion")
			}
			for _, invalid := range []string{"regression", "missing", "too few", "excess"} {
				t.Run(invalid, func(t *testing.T) {
					h, _ := newLocalECNTestHandler(t, bbr)
					a, _ := localECNPacket(t, h, now)
					b, _ := localECNPacket(t, h, now)
					c, _ := localECNPacket(t, h, now)
					first := uint64(1)
					if invalid == "missing" {
						first = 0
					}
					if invalid == "excess" {
						first = 4
					}
					ackBBRECNAt(t, h, now.Add(time.Millisecond), ackRanges(a), first, 0)
					second := uint64(1)
					if invalid == "regression" {
						second = 0
					}
					if invalid == "missing" || invalid == "excess" {
						second = 2
					}
					ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(b), second, 0)
					ackBBRECNAt(t, h, now.Add(3*time.Millisecond), ackRanges(c), 3, 0)
					require.Equal(t, protocol.ECNNon, h.ECNMode(true), "a newer report cannot repair invalid authenticated evidence")
				})
			}
		})
	}
}

func TestLocalECNLifecycleAndExhaustion(t *testing.T) {
	t.Run("settled flight retains ECN capacity", func(t *testing.T) {
		for _, outcome := range []LocalPacketOutcome{LocalPacketAccepted, LocalPacketUnsubmitted} {
			for _, bbr := range []bool{false, true} {
				t.Run(fmt.Sprintf("BBR=%t/outcome=%d", bbr, outcome), func(t *testing.T) {
					h, events := newLocalECNTestHandler(t, bbr)
					now := monotime.Now()
					anchor, token := localECNPacket(t, h, now)
					require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
					ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(anchor), 1, 0)
					var pns []protocol.PacketNumber
					for range maxECNMarkRanges + 2 {
						pn, token := localECNPacket(t, h, now.Add(3*time.Millisecond))
						pns = append(pns, pn)
						require.NoError(t, h.CompleteLocalPacket(token, outcome, now.Add(4*time.Millisecond)))
					}
					require.Equal(t, protocol.ECT0, h.ECNMode(true), "equivalent settled facts retain semantic range capacity")
					counts := uint64(len(pns))
					if outcome == LocalPacketUnsubmitted {
						pn, token := localECNPacket(t, h, now.Add(5*time.Millisecond))
						require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(6*time.Millisecond)))
						pns, counts = []protocol.PacketNumber{pn}, 1
					}
					ackBBRECNAt(t, h, now.Add(7*time.Millisecond), ackRanges(pns...), counts, 1)
					require.Equal(t, protocol.ECT0, h.ECNMode(true))
					if bbr {
						require.Equal(t, uint64(1), events.feedback[len(events.feedback)-1].ECN.Delta.CE)
					}
				})
			}
		}
	})
	t.Run("migration and uncertain outcome converge in either order", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			for _, retirementFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("BBR=%t/retirement-first=%t", bbr, retirementFirst), func(t *testing.T) {
					h, r := newLocalECNTestHandler(t, bbr)
					now := monotime.Now()
					a, ta := localECNPacket(t, h, now)
					require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(time.Millisecond)))
					ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(a), 1, 0)
					pn, token := localECNPacket(t, h, now.Add(3*time.Millisecond))
					before := len(r.feedback)
					if !retirementFirst {
						require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUncertain, now.Add(4*time.Millisecond)))
					}
					h.MigratedPath(now.Add(5*time.Millisecond), 1200)
					if retirementFirst {
						require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUncertain, now.Add(6*time.Millisecond)))
					}
					require.Equal(t, protocol.ECNNon, h.ECNMode(true))
					ackBBRECNAt(t, h, now.Add(7*time.Millisecond), ackRanges(pn), 2, 0)
					require.Equal(t, protocol.ECNNon, h.ECNMode(true), "late proof cannot reopen unavailable evidence")
					for _, event := range r.feedback[before:] {
						require.Empty(t, event.Lost)
					}
				})
			}
		}
	})
	t.Run("pending retirement waits for a known native outcome", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			for _, outcome := range []LocalPacketOutcome{LocalPacketAccepted, LocalPacketUnsubmitted} {
				h, _ := newLocalECNTestHandler(t, bbr)
				now := monotime.Now()
				a, ta := localECNPacket(t, h, now)
				require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(a), 1, 0)
				pn, token := localECNPacket(t, h, now.Add(3*time.Millisecond))
				h.MigratedPath(now.Add(4*time.Millisecond), 1200)
				require.NoError(t, h.CompleteLocalPacket(token, outcome, now.Add(5*time.Millisecond)))
				counts := uint64(1)
				if outcome == LocalPacketAccepted {
					counts++
					ackBBRECNAt(t, h, now.Add(6*time.Millisecond), ackRanges(pn), counts, 0)
				}
				require.Equal(t, protocol.ECT0, h.ECNMode(true), "pending retirement alone does not fail validation")
				fresh, accepted := localECNPacket(t, h, now.Add(7*time.Millisecond))
				require.NoError(t, h.CompleteLocalPacket(accepted, LocalPacketAccepted, now.Add(8*time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(9*time.Millisecond), ackRanges(fresh), counts+1, 0)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
			}
		}
	})

	t.Run("pending mark outlives capable feedback compaction", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			for _, outcome := range []LocalPacketOutcome{LocalPacketAccepted, LocalPacketUnsubmitted} {
				h, r := newLocalECNTestHandler(t, bbr)
				now := monotime.Now()
				a, ta := localECNPacket(t, h, now)
				require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(a), 1, 0)
				_, pending := localECNPacket(t, h, now.Add(3*time.Millisecond))
				c, tc := localECNPacket(t, h, now.Add(3*time.Millisecond))
				require.NoError(t, h.CompleteLocalPacket(tc, LocalPacketAccepted, now.Add(4*time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(5*time.Millisecond), ackRanges(c), 2, 0)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				require.NoError(t, h.CompleteLocalPacket(pending, outcome, now.Add(6*time.Millisecond)))
				require.Equal(t, protocol.ECT0, h.ECNMode(true), "authoritative compaction is not missing evidence")
				d, td := localECNPacket(t, h, now.Add(7*time.Millisecond))
				require.NoError(t, h.CompleteLocalPacket(td, LocalPacketAccepted, now.Add(8*time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(9*time.Millisecond), ackRanges(d), 2, 1)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				if bbr {
					require.Equal(t, uint64(1), r.feedback[len(r.feedback)-1].ECN.Delta.CE)
				}
			}
		}
	})

	t.Run("discharged prefixes keep ECN usable", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			for _, hole := range []string{"unsubmitted", "loss", "0-RTT drop", "ACK-only", "migration"} {
				t.Run(fmt.Sprintf("%t/%s", bbr, hole), func(t *testing.T) {
					h, _ := newLocalECNTestHandler(t, bbr)
					now := monotime.Now()
					var counts uint64
					if hole == "0-RTT drop" {
						_, token := registerLocalTestPacket(t, h, now, protocol.Encryption0RTT, []Frame{{Frame: &wire.PingFrame{}}})
						h.DropPackets(protocol.Encryption0RTT, now.Add(time.Millisecond))
						require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
					} else {
						var token LocalCompletionToken
						if hole == "ACK-only" {
							pn := h.PopPacketNumber(protocol.Encryption1RTT)
							var err error
							token, err = h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, h.ECNMode(true), 1200)
							require.NoError(t, err)
						} else {
							_, token = localECNPacket(t, h, now)
						}
						outcome := LocalPacketAccepted
						if hole == "unsubmitted" {
							outcome = LocalPacketUnsubmitted
						}
						require.NoError(t, h.CompleteLocalPacket(token, outcome, now.Add(time.Millisecond)))
						if hole == "migration" && bbr {
							h.MigratedPath(now.Add(time.Millisecond), 1200)
							pn, token := localECNPacket(t, h, now.Add(time.Millisecond))
							require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
							counts = 1
							ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(pn), counts, 0)
						}
					}
					for i := range maxECNMarkRanges + 2 {
						at := now.Add(time.Duration(i+3) * time.Millisecond)
						pn, token := localECNPacket(t, h, at)
						require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, at))
						counts++
						ackBBRECNAt(t, h, at.Add(time.Millisecond), ackRanges(pn), counts, 0)
						require.Equal(t, protocol.ECT0, h.ECNMode(true), "resolved holes cannot force evidence exhaustion")
					}
				})
			}
		}
	})

	for _, bbr := range []bool{false, true} {
		t.Run(map[bool]string{false: "Reno", true: "BBR"}[bbr], func(t *testing.T) {
			now := monotime.Now()
			for _, lifecycle := range []string{"Retry", "0-RTT drop", "close"} {
				t.Run(lifecycle, func(t *testing.T) {
					h, _ := newLocalECNTestHandler(t, bbr)
					_, old := registerLocalTestPacket(t, h, now, protocol.Encryption0RTT, []Frame{{Frame: &wire.PingFrame{}}})
					switch lifecycle {
					case "Retry":
						h.ResetForRetry(now.Add(time.Millisecond))
					case "0-RTT drop":
						h.DropPackets(protocol.Encryption0RTT, now.Add(time.Millisecond))
					case "close":
						h.CloseLocal(now.Add(time.Millisecond))
					}
					require.NoError(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now.Add(2*time.Millisecond)))
					if lifecycle == "close" {
						require.Equal(t, protocol.ECNNon, h.ECNMode(true))
						return
					}
					pn, token := localECNPacket(t, h, now.Add(3*time.Millisecond))
					require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(4*time.Millisecond)))
					ackBBRECNAt(t, h, now.Add(5*time.Millisecond), ackRanges(pn), 1, 0)
					require.Equal(t, protocol.ECT0, h.ECNMode(true), "fresh evidence remains usable after disposal")
				})
			}
			capable := true
			h, _ := newLocalECNTestHandler(t, bbr, &capable)
			anchor, accepted := localECNPacket(t, h, now)
			require.NoError(t, h.CompleteLocalPacket(accepted, LocalPacketAccepted, now.Add(time.Millisecond)))
			ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(anchor), 1, 0)
			var old LocalCompletionToken
			for i := 0; i <= maxECNMarkRanges; i++ {
				_, token := localECNPacket(t, h, now)
				if i == 0 {
					old = token
				}
			}
			require.Equal(t, protocol.ECNNon, h.ECNMode(true), "unresolved identities exhaust bounded evidence explicitly")
			require.NoError(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now.Add(2*time.Millisecond)))
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			if bbr {
				capable = false
				require.Equal(t, protocol.ECNUnsupported, h.ECNMode(true))
				capable = true
				require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			}
			h.CloseLocal(now.Add(3 * time.Millisecond))
			fresh, _ := newLocalECNTestHandler(t, bbr)
			require.Equal(t, protocol.ECT0, fresh.ECNMode(true))
			if bbr {
				capable := true
				h, _ := newLocalECNTestHandler(t, true, &capable)
				oldPN, old := localECNPacket(t, h, now)
				h.MigratedPath(now.Add(time.Millisecond), 1200)
				require.Equal(t, protocol.ECNNon, h.ECNMode(true), "old unresolved transmissions hold the counter fence")
				capable = false
				require.Equal(t, protocol.ECNUnsupported, h.ECNMode(true))
				capable = true
				require.NoError(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now.Add(2*time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(3*time.Millisecond), ackRanges(oldPN), 1, 0)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				pn, token := localECNPacket(t, h, now.Add(4*time.Millisecond))
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(5*time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(6*time.Millisecond), ackRanges(pn), 2, 0)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
			}
		})
	}
}
