package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func newLocalECNTestHandler(t *testing.T, bbr bool) (*sentPacketHandler, *congestionRecorder) {
	t.Helper()
	handler, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
	require.NoError(t, err)
	h := handler.(*sentPacketHandler)
	r := &congestionRecorder{}
	if bbr {
		b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
		h.congestionEvents.sink = &localRichRecorder{recorder: r, controller: b}
		EnableBBRECN(h, func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, true, true })
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
			ackBBRECNAt(t, h, now.Add(3*time.Millisecond), ackRanges(replacement), 1, 0)
			require.Equal(t, protocol.ECNNon, h.ECNMode(true), "uncertain evidence cannot certify the path")
			ackBBRECNAt(t, h, now.Add(4*time.Millisecond), ackRanges(pns[1], replacement), 2, 0)
			require.Equal(t, protocol.ECT0, h.ECNMode(true), "authenticated receipt resolves possible transmission")
		})
	}
}

func TestLocalECNSnapshotSupersession(t *testing.T) {
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
			h, _ := newLocalECNTestHandler(t, bbr)
			var old LocalCompletionToken
			for i := 0; i <= maxECNMarkRanges; i++ {
				_, token := localECNPacket(t, h, now)
				if i == 0 {
					old = token
				} else {
					require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
				}
			}
			require.Equal(t, protocol.ECNNon, h.ECNMode(true), "unavailable bounded evidence cannot certify ECN")
			require.NoError(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now.Add(2*time.Millisecond)))
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			h.CloseLocal(now.Add(3 * time.Millisecond))
			fresh, _ := newLocalECNTestHandler(t, bbr)
			require.Equal(t, protocol.ECT0, fresh.ECNMode(true))
			if bbr {
				h, _ := newLocalECNTestHandler(t, true)
				oldPN, old := localECNPacket(t, h, now)
				h.MigratedPath(now.Add(time.Millisecond), 1200)
				require.Equal(t, protocol.ECNNon, h.ECNMode(true), "old unresolved transmissions hold the counter fence")
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
