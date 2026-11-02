package quic

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func newCreditTestOwners(t *testing.T) (*testConnection, *packetHandlerMap) {
	t.Helper()
	tc := newEmissionTestConnection(t, true)
	c := tc.conn
	c.connIDManager.ChangeInitialConnID(tc.destConnID)
	route := &packetHandlerMap{connIDLen: tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
	require.True(t, route.Add(tc.srcConnID, c))
	c.connIDGenerator.initialClientDestConnID = nil // the private fixture is past handshake confirmation
	c.connIDGenerator.generator = &protocol.DefaultConnectionIDGenerator{ConnLen: tc.srcConnID.Len()}
	c.connIDGenerator.connRunners = connRunners{route: connectionRunnerCallbacks(route, c)}
	c.connIDManager.addStatelessResetToken = func(token protocol.StatelessResetToken) { route.AddResetToken(token, c) }
	c.connIDManager.removeStatelessResetToken = route.RemoveResetToken
	t.Cleanup(func() {
		q := c.emission.queue.(*sendQueue)
		for len(q.queue) > 0 {
			(<-q.queue).release()
		}
	})
	return tc, route
}

func newCreditTestConnection(t *testing.T, cap int, peerMax protocol.PathID) (*testConnection, *packetHandlerMap, *wire.TransportParameters) {
	t.Helper()
	tc, route := newCreditTestOwners(t)
	peerCIDLimit := uint64(4)
	if cap == 1 {
		peerCIDLimit = 2
	}
	params, err := tc.conn.enableMultipathCredit(cap, peerMax, peerCIDLimit)
	require.NoError(t, err)
	return tc, route, params
}

func creditIngress(t *testing.T, tc *testConnection, now monotime.Time, frames ...wire.Frame) error {
	t.Helper()
	var data []byte
	for _, f := range frames {
		var err error
		data, err = f.Append(data, protocol.Version1)
		require.NoError(t, err)
	}
	_, _, _, err := tc.conn.handleFrames(data, tc.srcConnID, protocol.Encryption1RTT, nil, now)
	return err
}

func creditDatagramFrames(t *testing.T, tc *testConnection, e queueEntry) []wire.Frame {
	t.Helper()
	var frames []wire.Frame
	var hasAbandonment bool
	require.Zero(t, e.gsoSize)
	hdrLen, _, _, _, err := wire.ParseShortHeader(e.buf.Data, tc.conn.connIDManager.Get().Len())
	require.NoError(t, err)
	payload := e.buf.Data[hdrLen : len(e.buf.Data)-7]
	parser := wire.NewFrameParser(true, false, false)
	parser.EnableMultipath()
	for len(payload) > 0 {
		typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
		require.NoError(t, err)
		payload = payload[n:]
		if typ == 0 {
			continue
		}
		f, n, err := parser.ParseLessCommonFrame(typ, payload, protocol.Version1)
		require.NoError(t, err)
		payload = payload[n:]
		frames = append(frames, f)
		if _, ok := f.(*wire.PathAbandonFrame); ok {
			hasAbandonment = true
		}
	}
	require.Equal(t, hasAbandonment, e.metadata.hasTerminalReceipt(), "receipt metadata follows the actual typed packed abandonment")
	return frames
}

func packedCreditFrames(t *testing.T, tc *testConnection) []wire.Frame {
	t.Helper()
	require.NoError(t, tc.conn.triggerSending(monotime.Now()).err)
	q := tc.conn.emission.queue.(*sendQueue)
	var frames []wire.Frame
	for len(q.queue) > 0 {
		e := <-q.queue
		frames = append(frames, creditDatagramFrames(t, tc, e)...)
		e.release()
	}
	return frames
}

func TestMultipathCreditInitialWindow(t *testing.T) {
	tc, route, params := newCreditTestConnection(t, 2, 1)
	require.Equal(t, protocol.PathID(1), *params.InitialMaxPathID)
	require.EqualValues(t, 4, params.ActiveConnectionIDLimit)
	frames := packedCreditFrames(t, tc)
	counts := map[protocol.PathID]int{}
	for _, frame := range frames {
		if f, ok := frame.(*wire.PathNewConnectionIDFrame); ok {
			counts[f.PathID]++
		}
	}
	require.Equal(t, map[protocol.PathID]int{0: 1, 1: 2}, counts)
	require.Len(t, route.handlers, 4)
	require.NoError(t, creditIngress(t, tc, monotime.Now(), &wire.MaxPathIDFrame{MaximumPathID: protocol.MaxPathID}, &wire.PathsBlockedFrame{MaximumPathID: 1}))
	require.Empty(t, packedCreditFrames(t, tc), "larger peer grants cannot create replacement credit or another unused window")
	require.Len(t, route.handlers, 4)
	require.NoError(t, creditIngress(t, tc, monotime.Now(), &wire.PathCIDsBlockedFrame{PathID: 1, NextSequenceNumber: 2}))
	additional := packedCreditFrames(t, tc)
	require.Len(t, additional, 1)
	require.EqualValues(t, 2, additional[0].(*wire.PathNewConnectionIDFrame).SequenceNumber)
	require.NoError(t, creditIngress(t, tc, monotime.Now(), &wire.PathCIDsBlockedFrame{PathID: 1, NextSequenceNumber: 2}))
	require.Empty(t, packedCreditFrames(t, tc), "duplicate CID requests do not consume additional records")
	require.NoError(t, creditIngress(t, tc, monotime.Now(), &wire.PathRetireConnectionIDFrame{PathID: 1, SequenceNumber: 2}))
	replacement := packedCreditFrames(t, tc)
	require.Len(t, replacement, 1, "retirement preserves the expanded three-CID window")
	require.EqualValues(t, 3, replacement[0].(*wire.PathNewConnectionIDFrame).SequenceNumber)
	require.Len(t, tc.conn.connIDGenerator.pathCIDs[1].active, 3)
}

func TestMultipathCreditAsymmetricAndConsumed(t *testing.T) {
	tc, route, params := newCreditTestConnection(t, 2, 0)
	require.Equal(t, protocol.PathID(1), *params.InitialMaxPathID)
	require.Len(t, packedCreditFrames(t, tc), 1)
	require.Len(t, route.handlers, 2)
	require.NoError(t, creditIngress(t, tc, monotime.Now(), receivedCreditCID(1, 0, 0)), "our grant admits peer metadata before their independent grant")
	require.Empty(t, packedCreditFrames(t, tc))
	require.Len(t, route.handlers, 2, "peer metadata does not authorize local issuance")
	require.False(t, tc.conn.pathCredit.consume(1))
	require.NoError(t, creditIngress(t, tc, monotime.Now(), &wire.MaxPathIDFrame{MaximumPathID: protocol.MaxPathID}))
	require.Len(t, packedCreditFrames(t, tc), 2)
	require.True(t, tc.conn.pathCredit.consume(1))
	require.False(t, tc.conn.pathCredit.consume(1), "consumed IDs cannot be reused")
	require.False(t, tc.conn.pathCredit.consume(protocol.MaxPathID))
	require.NoError(t, creditIngress(t, tc, monotime.Now(), &wire.MaxPathIDFrame{MaximumPathID: 0}, &wire.PathsBlockedFrame{MaximumPathID: 1}))
	require.Empty(t, packedCreditFrames(t, tc), "charged slots, including pending retirement, do not replenish credit")
	require.Len(t, route.handlers, 4)

	abandoned, abandonedRoute, _ := newCreditTestConnection(t, 2, 0)
	packedCreditFrames(t, abandoned)
	require.NoError(t, creditIngress(t, abandoned, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}, &wire.MaxPathIDFrame{MaximumPathID: 1}))
	for _, frame := range packedCreditFrames(t, abandoned) {
		_, issued := frame.(*wire.PathNewConnectionIDFrame)
		require.False(t, issued, "a late peer grant cannot revive an abandoned ID")
	}
	require.Len(t, abandonedRoute.handlers, 2)
	require.False(t, abandoned.conn.pathCredit.consume(1))
}

func TestMultipathCreditZeroAndSparse(t *testing.T) {
	tc, route, params := newCreditTestConnection(t, 1, protocol.MaxPathID)
	require.Zero(t, *params.InitialMaxPathID)
	require.Len(t, packedCreditFrames(t, tc), 1)
	require.NoError(t, creditIngress(t, tc, monotime.Now(), &wire.PathCIDsBlockedFrame{PathID: 0, NextSequenceNumber: 2}))
	require.Empty(t, packedCreditFrames(t, tc), "saturated peer credit waits for retirement without a local-capacity error")
	require.Nil(t, tc.conn.closeErr.Load())
	err := creditIngress(t, tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: protocol.MaxPathID, ConnectionID: protocol.ParseConnectionID([]byte{1})})
	var transportError *qerr.TransportError
	require.ErrorAs(t, err, &transportError)
	require.Equal(t, qerr.ProtocolViolation, transportError.ErrorCode)
	require.Len(t, route.handlers, 2)
}

func TestMultipathCreditUnusedAbandonRetention(t *testing.T) {
	tc, route, _ := newCreditTestConnection(t, 2, 1)
	packedCreditFrames(t, tc)
	tc.conn.observedRTT().UpdateRTT(2*time.Second, 0)
	tc.conn.observedRTT().SetMaxAckDelay(500 * time.Millisecond)
	now := monotime.Now()
	require.NoError(t, creditIngress(t, tc, now, &wire.PathAbandonFrame{PathID: 1}))
	frames := packedCreditFrames(t, tc)
	require.Len(t, frames, 1)
	require.Equal(t, &wire.PathAbandonFrame{PathID: 1}, frames[0])
	require.Len(t, route.handlers, 4)
	delay := 3 * tc.conn.observedRTT().PTO(true)
	require.NoError(t, tc.conn.pathCredit.expire(now.Add(delay-1)))
	require.Len(t, route.handlers, 4)
	require.NoError(t, creditIngress(t, tc, now.Add(delay-1), &wire.PathAbandonFrame{PathID: 1}))
	require.Empty(t, packedCreditFrames(t, tc))
	require.NoError(t, tc.conn.pathCredit.expire(now.Add(delay)))
	require.Len(t, route.handlers, 2)
	require.NoError(t, creditIngress(t, tc, now.Add(delay+1), &wire.PathAbandonFrame{PathID: 1}, &wire.PathCIDsBlockedFrame{PathID: 1, NextSequenceNumber: 2}))
	require.Empty(t, packedCreditFrames(t, tc))
	require.False(t, tc.conn.pathCredit.consume(1))
	require.Len(t, route.handlers, 2)
}

func receivedCreditCID(id protocol.PathID, seq, prior uint64) *wire.PathNewConnectionIDFrame {
	return &wire.PathNewConnectionIDFrame{PathID: id, SequenceNumber: seq, RetirePriorTo: prior, ConnectionID: protocol.ParseConnectionID([]byte{0xa0, byte(id), byte(seq)}), StatelessResetToken: protocol.StatelessResetToken{byte(id), byte(seq + 1)}}
}

func TestMultipathCreditPeerActiveCIDLimit(t *testing.T) {
	tc, route, _ := newCreditTestConnection(t, 2, 1)
	now := monotime.Now()
	for seq := range uint64(4) {
		require.NoError(t, creditIngress(t, tc, now, receivedCreditCID(1, seq, 0)))
	}
	err := creditIngress(t, tc, now, receivedCreditCID(1, 4, 0))
	var transportError *qerr.TransportError
	require.ErrorAs(t, err, &transportError)
	require.Equal(t, qerr.ConnectionIDLimitError, transportError.ErrorCode)
	require.Len(t, tc.conn.connIDManager.pathCIDs[1].records, 4, "reject a limit violation before storing a fifth CID")
	require.Empty(t, route.resetTokens, "unused CIDs have no reset-detection authority")
}

func TestMultipathCreditReceivedCIDChurnDeclines(t *testing.T) {
	tc, route, _ := newCreditTestConnection(t, 2, 1)
	packedCreditFrames(t, tc)
	now := monotime.Now()
	for seq := range uint64(17) {
		require.NoError(t, creditIngress(t, tc, now, receivedCreditCID(1, seq, seq)))
	}
	require.Len(t, tc.conn.connIDManager.pathCIDs[1].records, 16, "retain every admitted token; refuse the new CID at the bounded store")
	for _, entry := range tc.conn.connIDManager.pathCIDs[1].records {
		require.True(t, entry.hasToken)
		require.False(t, tc.conn.connIDManager.IsActiveStatelessResetToken(entry.StatelessResetToken))
	}
	require.Empty(t, route.resetTokens, "stored unused and retired evidence cannot authorize reset detection")
	var abandonment *wire.PathAbandonFrame
	for _, f := range packedCreditFrames(t, tc) {
		if a, ok := f.(*wire.PathAbandonFrame); ok {
			abandonment = a
		}
	}
	require.Equal(t, &wire.PathAbandonFrame{PathID: 1, ErrorCode: 0x3e75}, abandonment)
	require.Len(t, route.handlers, 4)
	require.NoError(t, creditIngress(t, tc, now, &wire.PathAbandonFrame{PathID: 1}))
	require.Len(t, tc.conn.connIDManager.pathCIDs[1].records, 16)
	require.NoError(t, tc.conn.pathCredit.expire(now.Add(3*tc.conn.observedRTT().PTO(false))))
	require.Empty(t, route.resetTokens)
	require.Len(t, route.handlers, 2)
}

func TestMultipathCreditIssuedCIDChurnDeclines(t *testing.T) {
	tc, route, _ := newCreditTestConnection(t, 2, 1)
	packedCreditFrames(t, tc)
	now := monotime.Now()
	for seq := range uint64(15) {
		require.NoError(t, creditIngress(t, tc, now, &wire.PathRetireConnectionIDFrame{PathID: 1, SequenceNumber: seq}))
	}
	require.Len(t, route.handlers, 18, "two path-zero CIDs plus the bounded 16-record path-one store")
	var abandonment *wire.PathAbandonFrame
	for _, f := range packedCreditFrames(t, tc) {
		if a, ok := f.(*wire.PathAbandonFrame); ok {
			abandonment = a
		}
	}
	require.Equal(t, &wire.PathAbandonFrame{PathID: 1, ErrorCode: 0x3e75}, abandonment)
	require.NoError(t, tc.conn.pathCredit.expire(now.Add(3*tc.conn.observedRTT().PTO(false))))
	require.Len(t, route.handlers, 18, "unanswered local decline cannot discard routing evidence")
	require.NoError(t, creditIngress(t, tc, now, &wire.PathAbandonFrame{PathID: 1}))
	require.NoError(t, tc.conn.pathCredit.expire(now.Add(3*tc.conn.observedRTT().PTO(false))))
	require.Len(t, route.handlers, 2)
}

func TestMultipathCreditLastPathResourceExhaustion(t *testing.T) {
	tc, route, _ := newCreditTestConnection(t, 2, 1)
	now := monotime.Now()
	for seq := uint64(1); seq <= 16; seq++ {
		f := receivedCreditCID(0, seq, seq)
		err := creditIngress(t, tc, now, &wire.NewConnectionIDFrame{SequenceNumber: seq, RetirePriorTo: seq, ConnectionID: f.ConnectionID, StatelessResetToken: f.StatelessResetToken})
		if seq < 16 {
			require.NoError(t, err)
		} else {
			var transportError *qerr.TransportError
			require.ErrorAs(t, err, &transportError)
			require.Equal(t, qerr.InternalError, transportError.ErrorCode)
		}
	}
	require.Len(t, tc.conn.connIDManager.pathCIDs[0].records, 16, "retain admitted CID/token evidence when the last path exhausts local capacity")
	require.Empty(t, route.resetTokens)
}

func TestMultipathCreditOrdinaryCIDAliasAndConflict(t *testing.T) {
	tc, route, _ := newCreditTestConnection(t, 2, 1)
	f := receivedCreditCID(0, 1, 1)
	now := monotime.Now()
	require.NoError(t, creditIngress(t, tc, now, &wire.NewConnectionIDFrame{SequenceNumber: 1, RetirePriorTo: 1, ConnectionID: f.ConnectionID, StatelessResetToken: f.StatelessResetToken}, f))
	require.Equal(t, f.ConnectionID, tc.conn.connIDManager.Get())
	require.Empty(t, route.resetTokens)
	require.False(t, tc.conn.connIDManager.IsActiveStatelessResetToken(f.StatelessResetToken), "selection alone does not establish use")
	tc.conn.connIDManager.SentPacket()
	require.True(t, tc.conn.connIDManager.IsActiveStatelessResetToken(f.StatelessResetToken))
	require.Len(t, route.resetTokens, 1)
	f.StatelessResetToken[15] = 1
	var transportError *qerr.TransportError
	require.ErrorAs(t, creditIngress(t, tc, now, f), &transportError)
	require.Equal(t, qerr.ProtocolViolation, transportError.ErrorCode)
	require.Len(t, route.resetTokens, 1)
	f.StatelessResetToken[15] = 0
	next := receivedCreditCID(0, 2, 2)
	require.NoError(t, creditIngress(t, tc, now, next))
	require.False(t, tc.conn.connIDManager.IsActiveStatelessResetToken(f.StatelessResetToken))
	require.Empty(t, route.resetTokens, "retirement disarms both reset-detection consumers")
	require.True(t, tc.conn.connIDManager.pathCIDs[0].records[1].hasToken, "retirement retains token evidence")
	tc.conn.connIDManager.SentPacket()
	require.True(t, tc.conn.connIDManager.IsActiveStatelessResetToken(next.StatelessResetToken))
	require.Contains(t, route.resetTokens, next.StatelessResetToken)
}

func declineCreditPath(t *testing.T, tc *testConnection, now monotime.Time) {
	t.Helper()
	for seq := range uint64(17) {
		require.NoError(t, creditIngress(t, tc, now, receivedCreditCID(1, seq, seq)))
	}
}

func TestMultipathCreditAcceptedReceiptStartsTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tc, _, _ := newCreditTestConnection(t, 2, 1)
		packedCreditFrames(t, tc)
		c := tc.conn
		c.observedRTT().UpdateRTT(10*time.Second, 0)
		c.observedRTT().SetMaxAckDelay(2 * time.Second)
		duration := 3 * c.observedRTT().PTO(true)
		declineCreditPath(t, tc, monotime.Now())
		require.NoError(t, c.triggerSending(monotime.Now()).err)
		require.NoError(t, c.pathCredit.expire(monotime.Now().Add(time.Hour)), "registration and enqueue do not establish acceptance")
		q := c.emission.queue.(*sendQueue)
		require.Len(t, q.queue, 1)
		entry := <-q.queue
		require.True(t, entry.metadata.hasTerminalReceipt())
		creditDatagramFrames(t, tc, entry)
		q.queue <- entry
		release := make(chan struct{})
		q.conn = &fakeBatchSendConn{writeErr: func(_ int, _ []byte) error { <-release; return nil }}
		q.authorized = nil
		select {
		case <-c.sendingScheduled:
		default:
		}
		done := make(chan error, 1)
		go func() { done <- q.Run() }()
		synctest.Wait()
		time.Sleep(20 * time.Second)
		require.NoError(t, c.pathCredit.expire(monotime.Now()))
		accepted := monotime.Now()
		close(release)
		synctest.Wait()
		q.Close()
		require.NoError(t, <-done)
		require.Len(t, c.sendingScheduled, 1, "the worker wakes the existing connection owner")
		require.NoError(t, c.pathCredit.expire(accepted))
		deadline := accepted.Add(duration)
		require.Equal(t, deadline, c.pathCredit.nextExpiry())
		c.observedRTT().UpdateRTT(time.Second, 0)
		c.observedRTT().SetMaxAckDelay(0)
		require.NoError(t, c.pathCredit.expire(deadline.Add(-1)))
		require.Equal(t, deadline, c.pathCredit.nextExpiry(), "later RTT changes cannot restart or shorten the chosen deadline")
		var transportError *qerr.TransportError
		require.ErrorAs(t, c.pathCredit.expire(deadline), &transportError)
		require.Equal(t, qerr.InternalError, transportError.ErrorCode)
	})
}

func TestMultipathCreditDuplicateAndReciprocalReceipt(t *testing.T) {
	tc, route, _ := newCreditTestConnection(t, 2, 1)
	packedCreditFrames(t, tc)
	declineCreditPath(t, tc, monotime.Now())
	require.NoError(t, tc.conn.triggerSending(monotime.Now()).err)
	q := tc.conn.emission.queue.(*sendQueue)
	require.Len(t, q.queue, 1)
	entry := <-q.queue
	receipt := entry.metadata.terminalReceipts[0]
	require.NotNil(t, receipt)
	q.queue <- entry
	q.conn, q.authorized = &fakeBatchSendConn{}, nil
	close(q.closeCalled)
	require.NoError(t, q.Run())
	accepted := receipt.acceptedAt()
	require.NotZero(t, accepted)
	require.NoError(t, tc.conn.pathCredit.expire(accepted))
	receipt.publish(accepted.Add(time.Hour))
	require.Equal(t, accepted.Add(30*time.Second), tc.conn.pathCredit.nextExpiry())
	delay := 3 * tc.conn.observedRTT().PTO(true)
	received := accepted.Add(30*time.Second - delay/2)
	require.NoError(t, creditIngress(t, tc, received, &wire.PathAbandonFrame{PathID: 1}))
	require.NoError(t, tc.conn.pathCredit.expire(accepted.Add(30*time.Second)))
	require.Len(t, route.handlers, 4)
	expiry := received.Add(delay)
	require.NoError(t, tc.conn.pathCredit.expire(expiry))
	require.Len(t, route.handlers, 2)
	receipt.publish(expiry.Add(time.Hour))
	require.NoError(t, tc.conn.pathCredit.expire(expiry.Add(time.Hour)))
	require.Zero(t, tc.conn.pathCredit.nextExpiry())
}

func TestMultipathCreditCloseCleanup(t *testing.T) {
	tc, route, _ := newCreditTestConnection(t, 2, 1)
	active := receivedCreditCID(0, 1, 1)
	require.NoError(t, creditIngress(t, tc, monotime.Now(), active))
	tc.conn.connIDManager.SentPacket()
	require.Contains(t, route.resetTokens, active.StatelessResetToken)
	require.NoError(t, creditIngress(t, tc, monotime.Now(), receivedCreditCID(1, 0, 0)))
	declineCreditPath(t, tc, monotime.Now())
	require.NoError(t, tc.conn.triggerSending(monotime.Now()).err)
	q := tc.conn.emission.queue.(*sendQueue)
	entry := <-q.queue
	receipt := entry.metadata.terminalReceipts[0]
	require.NotNil(t, receipt)
	entry.release()
	var closingIDs []protocol.ConnectionID
	callbacks := tc.conn.connIDGenerator.connRunners[route]
	callbacks.ReplaceWithClosed = func(ids []protocol.ConnectionID, _ []byte, _ time.Duration) { closingIDs = ids }
	tc.conn.connIDGenerator.connRunners[route] = callbacks
	tc.conn.connIDGenerator.ReplaceWithClosed(nil, time.Second)
	var routedIDs []protocol.ConnectionID
	for cid := range route.handlers {
		routedIDs = append(routedIDs, cid)
	}
	require.ElementsMatch(t, routedIDs, closingIDs, "graceful close inherits every active and retained routing obligation")
	tc.conn.pathCredit.close()
	tc.conn.connIDGenerator.RemoveAll()
	tc.conn.connIDManager.Close()
	receipt.publish(monotime.Now())
	require.Zero(t, receipt.acceptedAt(), "a disposed terminal cannot acquire late acceptance")
	require.Empty(t, route.handlers)
	require.Empty(t, route.resetTokens)
	require.NoError(t, tc.conn.pathCredit.expire(monotime.Now().Add(time.Hour)))
	require.Zero(t, tc.conn.pathCredit.nextExpiry())
}

func TestMultipathCreditDefaultDisabledAndFresh(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	err := creditIngress(t, tc, monotime.Now(), &wire.PathsBlockedFrame{MaximumPathID: 0})
	var transportError *qerr.TransportError
	require.ErrorAs(t, err, &transportError)
	require.Equal(t, qerr.FrameEncodingError, transportError.ErrorCode)
	params, err := tc.conn.enableMultipathCredit(0, 1, 4)
	require.Error(t, err)
	require.Nil(t, params)
	require.Nil(t, tc.conn.pathCredit)
	tc.conn.srcConnIDLen = 0
	params, err = tc.conn.enableMultipathCredit(2, 1, 4)
	require.Error(t, err)
	require.Nil(t, params)
	require.Nil(t, tc.conn.pathCredit)
	fresh, route, params := newCreditTestConnection(t, 1, 0)
	require.Zero(t, *params.InitialMaxPathID)
	require.Len(t, packedCreditFrames(t, fresh), 1)
	_, ok := fresh.conn.connIDManager.GetConnIDForPath(1)
	require.False(t, ok, "private multipath credit cannot allocate a legacy migration CID")
	require.Error(t, fresh.conn.connIDGenerator.AddConnRunner(&packetHandlerMap{}, connRunnerCallbacks{}))
	require.NoError(t, fresh.conn.connIDGenerator.SetMaxActiveConnIDs(4))
	require.Len(t, route.handlers, 2, "legacy limit calls cannot expand the fixed initial window")

	failed, partialRoute := newCreditTestOwners(t)
	callbacks := failed.conn.connIDGenerator.connRunners[partialRoute]
	register := callbacks.AddPathConnectionID
	callbacks.AddPathConnectionID = func(cid protocol.ConnectionID, path protocol.PathID) bool {
		return path == 0 && register(cid, path)
	}
	failed.conn.connIDGenerator.connRunners[partialRoute] = callbacks
	params, err = failed.conn.enableMultipathCredit(2, 1, 4)
	require.Error(t, err)
	require.Nil(t, params)
	require.True(t, failed.conn.pathCredit.closed)
	require.NotNil(t, failed.conn.closeErr.Load(), "partial publication fences activation through the normal close owner")
	require.Len(t, partialRoute.handlers, 2, "published routes survive until close cleanup")
	failed.conn.connIDGenerator.RemoveAll()
	failed.conn.connIDManager.Close()
	require.Empty(t, partialRoute.handlers)

	rotated, rotatedRoute := newCreditTestOwners(t)
	h := rotated.conn.connIDManager
	initialToken := protocol.StatelessResetToken{10}
	h.SentPacket() // confirmed handshake already used this CID
	h.SetStatelessResetToken(initialToken)
	h.activeSequenceNumber, h.highestRetired = 10, 10
	h.handshakeComplete, h.packetsPerConnectionID = true, protocol.PacketsPerConnectionID
	for seq := uint64(11); seq <= 13; seq++ {
		f := receivedCreditCID(0, seq, 0)
		require.NoError(t, h.Add(&wire.NewConnectionIDFrame{SequenceNumber: seq, ConnectionID: f.ConnectionID, StatelessResetToken: f.StatelessResetToken}))
	}
	_, err = rotated.conn.enableMultipathCredit(2, 1, 4)
	require.NoError(t, err)
	require.True(t, h.IsActiveStatelessResetToken(initialToken))
	require.Contains(t, rotatedRoute.resetTokens, initialToken, "activation preserves an already-used active CID's detection authority")
	stale := receivedCreditCID(0, 9, 0)
	require.NoError(t, creditIngress(t, rotated, monotime.Now(), &wire.NewConnectionIDFrame{SequenceNumber: 9, ConnectionID: stale.ConnectionID, StatelessResetToken: stale.StatelessResetToken}))
	require.Len(t, h.pathCIDs[0].records, 4, "activation preserves the legacy retirement watermark")
	require.NotContains(t, h.pathCIDs[0].records, uint64(9))
	require.NotContains(t, rotatedRoute.resetTokens, stale.StatelessResetToken)
	require.Contains(t, packedCreditFrames(t, rotated), &wire.PathRetireConnectionIDFrame{SequenceNumber: 9})

	for _, selectBeforeActivation := range []bool{false, true} {
		unused, unusedRoute := newCreditTestOwners(t)
		manager := unused.conn.connIDManager
		original := manager.activeConnectionID
		if selectBeforeActivation {
			unused.conn.noteEmissionRegistration() // use of the old identity cannot transfer through rotation
		}
		manager.SetStatelessResetToken(initialToken)
		manager.SetHandshakeComplete()
		next := receivedCreditCID(0, 1, 0)
		require.NoError(t, manager.Add(&wire.NewConnectionIDFrame{SequenceNumber: 1, ConnectionID: next.ConnectionID, StatelessResetToken: next.StatelessResetToken}))
		current, token := original, initialToken
		if selectBeforeActivation {
			current, token = next.ConnectionID, next.StatelessResetToken
			require.Equal(t, current, manager.Get())
			require.Contains(t, unusedRoute.resetTokens, token, "ordinary selection preserves legacy registration")
		}
		_, err = unused.conn.enableMultipathCredit(2, 1, 4)
		require.NoError(t, err)
		require.Equal(t, current, manager.Get(), "activation does not rotate the current CID")
		require.Empty(t, unusedRoute.resetTokens, "activation disarms a selected but unused CID")
		require.False(t, manager.IsActiveStatelessResetToken(initialToken))
		require.False(t, manager.IsActiveStatelessResetToken(next.StatelessResetToken))
		unused.conn.noteEmissionRegistration()
		require.True(t, manager.IsActiveStatelessResetToken(token))
		require.Contains(t, unusedRoute.resetTokens, token)
		require.Len(t, unusedRoute.resetTokens, 1, "use arms only the current CID")
		manager.Close()
		require.Empty(t, unusedRoute.resetTokens)
		require.False(t, manager.IsActiveStatelessResetToken(token))
		_, err = unused.conn.enableMultipathCredit(2, 1, 4)
		require.Error(t, err)
	}

	for _, replaceIdentity := range []bool{false, true} {
		replaced, replacedRoute := newCreditTestOwners(t)
		manager := replaced.conn.connIDManager
		replaced.conn.noteEmissionRegistration()
		manager.SetStatelessResetToken(initialToken)
		current := manager.Get()
		if replaceIdentity {
			current = protocol.ParseConnectionID([]byte{9, 8, 7, 6, 5, 4, 3, 2})
		}
		manager.ChangeInitialConnID(current)
		_, err = replaced.conn.enableMultipathCredit(2, 1, 4)
		require.NoError(t, err)
		require.Equal(t, !replaceIdentity, manager.IsActiveStatelessResetToken(initialToken), "use belongs to the actual CID identity")
		require.Equal(t, !replaceIdentity, replacedRoute.resetTokens[initialToken] != nil)
		replaced.conn.noteEmissionRegistration()
		require.True(t, manager.IsActiveStatelessResetToken(initialToken))
		require.Contains(t, replacedRoute.resetTokens, initialToken)
		manager.Close()
		require.Empty(t, replacedRoute.resetTokens)
	}

	closed, _ := newCreditTestOwners(t)
	closed.conn.connIDManager.Close()
	_, err = closed.conn.enableMultipathCredit(2, 1, 4)
	require.Error(t, err, "a closed CID owner cannot activate private state")
}
