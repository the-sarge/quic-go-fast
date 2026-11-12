//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"errors"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestNativeOrdinaryAcceptedCompletion(t *testing.T) {
	for _, kind := range []string{"accepted", "partial", "error", "MTU", "coalesced"} {
		t.Run(kind, func(t *testing.T) {
			h, err := ackhandler.NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
			require.NoError(t, err)
			now := monotime.Time(time.Second)
			h.DropPackets(protocol.EncryptionInitial, now)
			h.DropPackets(protocol.EncryptionHandshake, now)
			packets := make([]ordinaryLocalPacket, 3)
			for i := range packets {
				pn := h.PopPacketNumber(protocol.Encryption1RTT)
				packets[i].token, err = h.RegisterLocalPacket(ackhandler.PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, []ackhandler.Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1200)
				require.NoError(t, err)
			}
			require.NoError(t, h.CompleteLocalPacket(packets[0].token, ackhandler.LocalPacketAccepted, now))
			packets = packets[1:]
			accepted, datagrams := 2, 2
			var resultErr error
			switch kind {
			case "partial":
				accepted = 1
			case "error":
				resultErr = errors.New("native error")
			case "MTU":
				packets[1].mtu = &mtuFinderAckHandler{}
			case "coalesced":
				accepted, datagrams = 1, 1
			}
			s := &nativeOrdinarySender{recovery: h}
			require.Equal(t, kind == "accepted", s.completeAcceptedPackets(packets, datagrams, accepted, resultErr, now.Add(time.Millisecond)))
			if kind != "accepted" {
				for _, p := range packets {
					require.NoError(t, h.CompleteLocalPacket(p.token, ackhandler.LocalPacketAccepted, now))
				}
			}
		})
	}
}

// The wrapper deliberately declines the concrete-handler span preflight while
// keeping the same real recovery owner and ordered scalar completion behavior.
type nativeOrdinarySuffixScalar struct {
	ackhandler.LocalSentPacketHandler
	tokens   []ackhandler.LocalCompletionToken
	outcomes []ackhandler.LocalPacketOutcome
}

func (s *nativeOrdinarySuffixScalar) CompleteLocalPacket(token ackhandler.LocalCompletionToken, outcome ackhandler.LocalPacketOutcome, at monotime.Time) error {
	s.tokens = append(s.tokens, token)
	s.outcomes = append(s.outcomes, outcome)
	return s.LocalSentPacketHandler.CompleteLocalPacket(token, outcome, at)
}

type nativeOrdinarySuffixState struct {
	alarm     time.Duration
	allowance protocol.ByteCount
	dropped   uint64
	lost      uint64
	writes    int
	err       string
}

func testNativeOrdinaryAcceptedSuffix(t *testing.T, count int, kind string, forceScalar bool) nativeOrdinarySuffixState {
	t.Helper()
	tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
	recovery := sender.recovery
	var scalar *nativeOrdinarySuffixScalar
	if forceScalar {
		scalar = &nativeOrdinarySuffixScalar{LocalSentPacketHandler: recovery}
		sender.recovery = scalar
	}
	if kind == "partial" || kind == "error" {
		socket.result = func(data []byte) (int, error) {
			if kind == "error" || len(socket.writes) == 2 {
				return 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.EAGAIN}
			}
			return len(data), nil
		}
	}
	// Small real packets exercise the 64-entry boundary within the 20 KiB pool.
	const size = 300
	buffer := getLargePacketBuffer()
	require.True(t, sender.reserveBuffer(buffer.Cap()).available)
	pns := make([]protocol.PacketNumber, count)
	for i := range count {
		pn, pnLen := recovery.PeekPacketNumber(protocol.Encryption1RTT)
		pns[i] = pn
		payloadSize := size - int(wire.ShortHeaderLen(tc.conn.connIDManager.Get(), pnLen)) - 7 - 3
		require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte{0x41}, payloadSize)}))
		receipt, err := tc.conn.emission.appendPacket(buffer, size, false, protocol.ECNNon, monotime.Now())
		require.NoError(t, err)
		require.EqualValues(t, size, receipt.size)
	}
	segment := uint16(size)
	if kind == "coalesced shape" {
		// Exercise the consumer's packet-to-datagram mapping, independently of
		// the handshake producer that normally creates this geometry.
		segment = 0
	}
	sender.Send(buffer, segment, protocol.ECNNon, sendMetadata{})
	record := sender.pending
	require.Equal(t, count, record.count)
	require.Len(t, record.members, 1)
	tokens := make([]ackhandler.LocalCompletionToken, count)
	for i, packet := range record.packets[:record.count] {
		tokens[i] = packet.token
	}
	receipt := &pathAbandonReceipt{}
	record.members[0].metadata.terminalReceipts = []*pathAbandonReceipt{receipt}
	sender.finish()
	synctest.Wait()
	require.Zero(t, buffer.refCount, "native payload has returned before completion processing")
	sender.serviceParticipant(sender.participant)
	require.Equal(t, ordinaryTerminalProcessing, record.state)
	require.Len(t, record.terminal.members, 1)
	fact := record.terminal.members[0]
	require.Zero(t, receipt.acceptedAt())
	require.Zero(t, recovery.GetLossDetectionTimeout(), "every real registration is still pending")
	switch kind {
	case "consumed prefix":
		require.NoError(t, recovery.CompleteLocalPacket(tokens[0], ackhandler.LocalPacketAccepted, fact.returnedAt))
	case "invalid suffix":
		record.packets[1].token = ackhandler.LocalCompletionToken{}
	case "prefix MTU":
		record.packets[0].mtu = &mtuFinderAckHandler{}
	case "suffix MTU":
		record.packets[1].mtu = &mtuFinderAckHandler{}
	}
	err := sender.consume()
	if kind == "consumed prefix" || kind == "invalid suffix" {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
	}
	if fact.err == nil {
		require.Equal(t, fact.returnedAt, receipt.acceptedAt())
	} else {
		require.Zero(t, receipt.acceptedAt(), "partial or failed members do not publish successful terminal receipts")
	}
	if scalar != nil {
		wantTokens := append([]ackhandler.LocalCompletionToken(nil), tokens...)
		if kind == "invalid suffix" {
			wantTokens[1] = ackhandler.LocalCompletionToken{}
		}
		require.Equal(t, wantTokens, scalar.tokens, "the reference completes every packet in original order")
		for i, outcome := range scalar.outcomes {
			want := ackhandler.LocalPacketAccepted
			if kind == "partial" && i == 1 || kind == "error" && i == 0 {
				want = ackhandler.LocalPacketUncertain
			} else if kind == "partial" && i > 1 || kind == "error" && i > 0 {
				want = ackhandler.LocalPacketUnsubmitted
			}
			require.Equal(t, want, outcome)
		}
	}
	for i, token := range tokens {
		completionErr := recovery.CompleteLocalPacket(token, ackhandler.LocalPacketAccepted, fact.returnedAt)
		if kind == "invalid suffix" && i == 1 {
			require.NoError(t, completionErr, "the invalid identity did not consume the real token")
		} else {
			require.Error(t, completionErr, "each original identity was consumed exactly once")
		}
	}
	for i := range sender.records {
		require.Equal(t, ordinaryFree, sender.records[i].state)
	}
	require.Zero(t, nativeProbeRetained(sender.endpoint, sender.participant))
	_, allowance := recovery.(interface {
		SendAllowance(monotime.Time) (ackhandler.SendMode, protocol.ByteCount)
	}).SendAllowance(fact.returnedAt)
	state := nativeOrdinarySuffixState{allowance: allowance, dropped: sender.droppedDatagrams, lost: tc.conn.connStats.PacketsLost.Load(), writes: len(socket.writes)}
	if alarm := recovery.GetLossDetectionTimeout(); !alarm.IsZero() {
		state.alarm = alarm.Sub(fact.returnedAt)
	}
	if err != nil {
		state.err = err.Error()
	}
	if fact.err == nil {
		// ACK all actual packet numbers, excluding any skipped number. Protocol
		// retirement remains valid after either completion strategy.
		var ranges []wire.AckRange
		for i := len(pns) - 1; i >= 0; i-- {
			if len(ranges) != 0 && ranges[len(ranges)-1].Smallest == pns[i]+1 {
				ranges[len(ranges)-1].Smallest = pns[i]
			} else {
				ranges = append(ranges, wire.AckRange{Smallest: pns[i], Largest: pns[i]})
			}
		}
		_, ackErr := recovery.ReceivedAck(&wire.AckFrame{AckRanges: ranges}, protocol.Encryption1RTT, fact.returnedAt.Add(time.Millisecond))
		require.NoError(t, ackErr)
		require.Zero(t, recovery.GetLossDetectionTimeout())
	}
	return state
}

func TestNativeOrdinaryAcceptedSuffix(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		kind  string
	}{
		{name: "one", count: 1},
		{name: "two", count: 2},
		{name: "three", count: 3},
		{name: "limit", count: maxGSOSegments},
		{name: "oversize", count: maxGSOSegments + 1},
		{name: "partial", count: 3, kind: "partial"},
		{name: "error", count: 3, kind: "error"},
		{name: "coalesced shape", count: 3, kind: "coalesced shape"},
		{name: "consumed prefix", count: 3, kind: "consumed prefix"},
		{name: "invalid suffix", count: 3, kind: "invalid suffix"},
		{name: "prefix MTU", count: 3, kind: "prefix MTU"},
		{name: "suffix MTU", count: 3, kind: "suffix MTU"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var states [2]nativeOrdinarySuffixState
			for i, forceScalar := range []bool{false, true} {
				nativeOrdinaryTest(t, func(t *testing.T) {
					states[i] = testNativeOrdinaryAcceptedSuffix(t, tc.count, tc.kind, forceScalar)
				})
			}
			require.Equal(t, states[1], states[0], "concrete recovery and the ordered scalar reference have the same externally observable result")
		})
	}
}
