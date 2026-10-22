package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qtls"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestEmissionDirectProbe(t *testing.T) {
	for _, client := range []bool{false, true} {
		for _, writeError := range []bool{false, true} {
			name := "server"
			if client {
				name = "client"
			}
			if writeError {
				name += "/write-error"
			} else {
				name += "/success"
			}
			t.Run(name, func(t *testing.T) {
				tc := newEmissionTestConnection(t, false)
				c := tc.conn
				recorder := &events.Recorder{}
				c.qlogger = recorder
				addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
				info := packetInfo{addr: netip.MustParseAddr("127.0.0.2")}
				connID := protocol.ParseConnectionID([]byte{8, 7, 6, 5})
				challenge := &wire.PathChallengeFrame{Data: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
				frames := []ackhandler.Frame{{Frame: challenge}}
				observed := observeConstructionBuffer(t)
				writes := 0
				now := monotime.Now()
				check := func(b []byte) error {
					writes++
					require.EqualValues(t, 1, (*observed).refCount, "storage stays live during syscall")
					require.Len(t, b, 1200)
					hdrLen, pn, _, _, err := wire.ParseShortHeader(b, connID.Len())
					require.NoError(t, err)
					require.EqualValues(t, 0, pn)
					require.Equal(t, connID.Bytes(), b[1:1+connID.Len()])
					parser := wire.NewFrameParser(true, false, false)
					payload := b[hdrLen : len(b)-7]
					typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
					require.NoError(t, err)
					f, _, err := parser.ParseLessCommonFrame(typ, payload[n:], c.version)
					require.NoError(t, err)
					require.Equal(t, challenge, f)
					// Path probes are registered before I/O but don't start the idle clock.
					_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
					require.NoError(t, err)
					require.Zero(t, c.firstAckElicitingPacketAfterIdleSentTime)
					if writeError {
						return errors.New("direct socket failure")
					}
					return nil
				}
				if client {
					raw := NewMockRawConn(gomock.NewController(t))
					tr := &Transport{conn: raw}
					tr.initOnce.Do(func() {}) // Transport initialization is outside the write seam.
					raw.EXPECT().WritePacket(gomock.Any(), addr, []byte(nil), uint16(0), protocol.ECNUnsupported).DoAndReturn(func(b []byte, _ net.Addr, _ []byte, _ uint16, _ protocol.ECN) (int, error) { return len(b), check(b) })
					require.NoError(t, c.emission.clientProbe(connID, frames[0], tr, addr, now))
				} else {
					candidate := &path{bytesReceived: 400, probeInterval: 200 * time.Millisecond, validationDeadline: now.Add(5 * time.Second)}
					tc.sendConn.EXPECT().WriteTo(gomock.Any(), addr, info).DoAndReturn(func(b []byte, _ net.Addr, _ packetInfo) error { return check(b) })
					require.NoError(t, c.emission.serverProbe(connID, frames, candidate, addr, info, 42, now))
					require.EqualValues(t, 1200, candidate.bytesSent, "local errors must not refund the path budget")
					require.Equal(t, 1, candidate.numChallenges)
					require.True(t, candidate.challenges[0].padded)
				}
				require.Equal(t, 1, writes)
				require.Zero(t, (*observed).refCount)
				require.Empty(t, c.emission.queue.(*sendQueue).queue, "direct probes never enter the ordinary queue")
				sent := recorder.Events(qlog.PacketSent{})
				require.Len(t, sent, 1)
				if !client {
					require.Equal(t, qlog.DatagramPayloadChecksum(42), sent[0].(qlog.PacketSent).DatagramPayloadChecksum)
				}
			})
		}
	}
}

func TestEmissionServerProbeExhaustedPathBudget(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	recorder := &events.Recorder{}
	c.qlogger = recorder
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4242}
	candidate := &path{bytesReceived: 100, bytesSent: 300, validationDeadline: monotime.Now().Add(5 * time.Second)}
	frames := []ackhandler.Frame{{Frame: &wire.PathChallengeFrame{Data: [8]byte{1}}}}
	observed := observeConstructionBuffer(t)
	pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	require.NoError(t, c.emission.serverProbe(c.connIDManager.Get(), frames, candidate, addr, packetInfo{}, 0, monotime.Now()))
	require.Zero(t, candidate.numChallenges, "unsent challenge remains eligible when more credit arrives")
	require.EqualValues(t, 300, candidate.bytesSent)
	nextPN, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	require.Equal(t, pn, nextPN, "a blocked probe must not consume a packet number")
	require.NotNil(t, *observed)
	require.Zero(t, (*observed).refCount)
	require.Empty(t, recorder.Events(qlog.PacketSent{}))
	require.Empty(t, c.emission.queue.(*sendQueue).queue)
}

func TestEmissionDirectProbeFlood(t *testing.T) {
	for _, client := range []bool{false, true} {
		t.Run(map[bool]string{false: "server", true: "client"}[client], func(t *testing.T) {
			tc := newEmissionTestConnection(t, false)
			c := tc.conn
			now := monotime.Now()
			addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
			frame := ackhandler.Frame{Frame: &wire.PathResponseFrame{}, Handler: emptyHandler{}}
			candidate := &path{validated: true}
			writes := 0
			var tr *Transport
			if client {
				raw := NewMockRawConn(gomock.NewController(t))
				tr = &Transport{conn: raw}
				tr.initOnce.Do(func() {})
				raw.EXPECT().WritePacket(gomock.Any(), addr, []byte(nil), uint16(0), protocol.ECNUnsupported).DoAndReturn(func(b []byte, _ net.Addr, _ []byte, _ uint16, _ protocol.ECN) (int, error) {
					writes++
					return len(b), nil
				}).AnyTimes()
			} else {
				tc.sendConn.EXPECT().WriteTo(gomock.Any(), addr, packetInfo{}).DoAndReturn(func([]byte, net.Addr, packetInfo) error {
					writes++
					return nil
				}).AnyTimes()
			}
			probe := func() {
				t.Helper()
				if client {
					require.NoError(t, c.emission.clientProbe(c.connIDManager.Get(), frame, tr, addr, now))
				} else {
					require.NoError(t, c.emission.serverProbe(c.connIDManager.Get(), []ackhandler.Frame{frame}, candidate, addr, packetInfo{}, 0, now))
				}
			}
			for range protocol.MaxTrackedSentPackets + 100 {
				probe()
			}
			require.Positive(t, writes)
			require.Less(t, writes, protocol.MaxTrackedSentPackets)
			require.False(t, c.sentPacketHandler.CanSendPathProbe())
			nextPN, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			before := writes
			probe()
			afterPN, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.Equal(t, nextPN, afterPN, "a refused probe must not consume packet numbers")
			require.Equal(t, before, writes)
			if !client {
				candidate.validationDeadline = now.Add(5 * time.Second)
				challenge := ackhandler.Frame{Frame: &wire.PathChallengeFrame{Data: [8]byte{1}}}
				require.NoError(t, c.emission.serverProbe(c.connIDManager.Get(), []ackhandler.Frame{challenge}, candidate, addr, packetInfo{}, 0, now))
				require.Zero(t, candidate.numChallenges, "a capacity-blocked challenge must remain eligible for retry")
				require.Zero(t, candidate.nextProbeTime)
				require.Equal(t, before, writes)
			}
			_, err := c.sentPacketHandler.OnLossDetectionTimeout(now.Add(time.Second))
			require.NoError(t, err)
			require.True(t, c.sentPacketHandler.CanSendPathProbe())
			now = now.Add(time.Second)
			probe()
			require.Equal(t, before+1, writes)
		})
	}
}

func TestEmissionMTUProbe(t *testing.T) {
	for _, writeError := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "write-error"}[writeError], func(t *testing.T) {
			tc := newEmissionTestConnection(t, false)
			c := tc.conn
			now := monotime.Now()
			c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1200, 1400, nil)
			c.mtuDiscoverer.Start(now.Add(-time.Hour))
			observed := observeConstructionBuffer(t)
			result := c.triggerSending(now)
			require.NoError(t, result.err)
			require.True(t, result.progress)
			require.EqualValues(t, 1, (*observed).refCount)
			q := c.emission.queue.(*sendQueue)
			require.Len(t, q.queue, 1)
			cause := errors.New("MTU write failure")
			tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), protocol.ECNUnsupported).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
				require.Len(t, b, 1300)
				_, pn, _, _, err := wire.ParseShortHeader(b, c.connIDManager.Get().Len())
				require.NoError(t, err)
				if writeError {
					return cause
				}
				_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
				require.NoError(t, err)
				require.EqualValues(t, 1300, c.mtuDiscoverer.CurrentSize())
				return nil
			})
			close(q.closeCalled)
			err := q.Run()
			if writeError {
				require.ErrorIs(t, err, cause)
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, (*observed).refCount)
		})
	}
}

func TestEmissionMTUProbeFullQueue(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	now := monotime.Now()
	c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1200, 1400, nil)
	c.mtuDiscoverer.Start(now.Add(-time.Hour))
	q := c.emission.queue.(*sendQueue)
	for range sendQueueCapacity {
		q.Send(getPacketBuffer(), 0, protocol.ECNUnsupported, sendMetadata{})
	}
	result := c.triggerSending(now)
	require.Equal(t, emissionQueueFull, result.stop)
	require.Equal(t, blockModeHardBlocked, c.blocked)
	require.True(t, c.mtuDiscoverer.ShouldSendProbe(now), "full queue must not consume probe intent")
	for len(q.queue) > 0 {
		(<-q.queue).buf.Release()
	}
}

func TestEmissionPathReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		q := c.emission.queue.(*sendQueue)
		first, second := getPacketWithContents([]byte("active")), getPacketWithContents([]byte("pending"))
		q.Send(first, 0, protocol.ECNNon, sendMetadata{})
		q.Send(second, 0, protocol.ECNNon, sendMetadata{})
		release := make(chan struct{})
		tc.sendConn.EXPECT().Write([]byte("active"), uint16(0), protocol.ECNNon).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-release; return nil })
		tc.sendConn.EXPECT().Write([]byte("pending"), uint16(0), protocol.ECNNon).Return(nil)
		worker := make(chan error, 1)
		go func() { worker <- q.Run() }()
		synctest.Wait()
		next := NewMockSendConn(gomock.NewController(t))
		replaced := make(chan sender, 1)
		go func() { replaced <- c.emission.replacePath(next, &c.handshakeSendFeedback) }()
		synctest.Wait()
		require.Same(t, q, c.emission.queue, "old worker must finish before queue replacement")
		require.EqualValues(t, 1, first.refCount)
		require.EqualValues(t, 1, second.refCount)
		close(release)
		synctest.Wait()
		require.NoError(t, <-worker)
		newQueue := <-replaced
		require.Same(t, newQueue, c.emission.queue)
		require.Same(t, next, c.conn)
		require.Zero(t, first.refCount)
		require.Zero(t, second.refCount)
		// The replacement remains connection-started and borrows the same feedback owner.
		require.Same(t, &c.handshakeSendFeedback, newQueue.(*sendQueue).feedback)
		go func() { worker <- newQueue.Run() }()
		newQueue.Close()
		require.NoError(t, <-worker)
	})
}

func TestEmissionPathRebinding(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	raw := NewMockRawConn(gomock.NewController(t))
	oldAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1111}
	newAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}
	raw.EXPECT().LocalAddr().Return(oldAddr)
	conn := newSendConn(raw, oldAddr, packetInfo{}, c.logger)
	q := newSendQueue(conn, &c.handshakeSendFeedback).(*sendQueue)
	c.emission.queue = q
	c.conn = conn
	buf := getPacketWithContents([]byte("queued before rebinding"))
	q.Send(buf, 0, protocol.ECNNon, sendMetadata{})
	c.emission.rebindPath(newAddr, packetInfo{})
	require.Same(t, q, c.emission.queue)
	raw.EXPECT().WritePacket(buf.Data, newAddr, gomock.Any(), uint16(0), protocol.ECNNon).Return(len(buf.Data), nil)
	close(q.closeCalled)
	require.NoError(t, q.Run())
	require.Zero(t, buf.refCount)
}

// Public state must consult the replacement's live capability, not a value
// cached when the path was published. The raw socket injects a real GSO error.
func TestConnectionStatePathCapabilities(t *testing.T) {
	if !platformSupportsGSO {
		t.Skip("GSO is not supported on this platform")
	}
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	require.False(t, c.ConnectionState().GSO)
	oldQueue := c.emission.queue
	oldWorker := make(chan error, 1)
	go func() { oldWorker <- oldQueue.Run() }()

	next := newSendConn(&gsoFallbackRawConn{}, tc.remoteAddr, packetInfo{}, c.logger)
	queue := c.emission.replacePath(next, &c.handshakeSendFeedback)
	require.NoError(t, <-oldWorker)
	require.True(t, c.ConnectionState().GSO)
	require.Equal(t, &net.UDPAddr{}, c.LocalAddr())
	require.Equal(t, tc.remoteAddr, c.RemoteAddr())

	buf := getPacketWithContents([]byte("foobar"))
	queue.Send(buf, 4, protocol.ECNNon, sendMetadata{})
	worker := make(chan error, 1)
	go func() { worker <- queue.Run() }()
	queue.Close()
	require.NoError(t, <-worker)
	require.Zero(t, buf.refCount)
	require.False(t, c.ConnectionState().GSO, "later GSO fallback must remain visible on the active path")
}

func TestEmissionServerProbeResponsePriority(t *testing.T) {
	for _, expiresDuringPacking := range []bool{false, true} {
		t.Run(map[bool]string{false: "only response fits", true: "attempt expires during packing"}[expiresDuringPacking], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tc := newEmissionTestConnection(t, false)
				c := tc.conn
				now := monotime.Now()
				// Keep recovery's sequential-number invariant live before packing
				// an attempt that may be discarded at its deadline.
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("prior packet")}))
				require.True(t, c.triggerSending(now).progress)
				(<-c.emission.queue.(*sendQueue).queue).release()
				nextPN, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				observed := observeConstructionBuffer(t)
				var allocations []*packetBuffer
				candidate := &path{bytesReceived: 8, probeInterval: 200 * time.Millisecond, nextProbeTime: now, validationDeadline: now.Add(5 * time.Second)}
				if expiresDuringPacking {
					candidate.bytesReceived = 400
				}
				ctrl := gomock.NewController(t)
				sealing := NewMockSealingManager(ctrl)
				sealer := newMockShortHeaderSealer(ctrl)
				first := true
				sealing.EXPECT().Get1RTTSealer().DoAndReturn(func() (handshake.ShortHeaderSealer, error) {
					allocations = append(allocations, *observed)
					if first && expiresDuringPacking {
						time.Sleep(5 * time.Second)
					}
					first = false
					return sealer, nil
				}).AnyTimes()
				c.emission.packer.cryptoSetup = sealing
				challenge := &wire.PathChallengeFrame{Data: [8]byte{1}}
				response := &wire.PathResponseFrame{Data: [8]byte{2}}
				frames := []ackhandler.Frame{{Frame: challenge, Handler: emptyHandler{}}, {Frame: response, Handler: emptyHandler{}}}
				addr := &net.UDPAddr{Port: 4242}
				var output []wire.Frame
				tc.sendConn.EXPECT().WriteTo(gomock.Any(), addr, packetInfo{}).DoAndReturn(func(b []byte, _ net.Addr, _ packetInfo) error {
					var err error
					output, err = decodeSchedulingFrames(b, c.connIDManager.Get().Len())
					require.NoError(t, err)
					return nil
				})
				require.NoError(t, c.emission.serverProbe(c.connIDManager.Get(), frames, candidate, addr, packetInfo{}, 0, now))
				require.Equal(t, []wire.Frame{response}, output)
				require.Zero(t, candidate.numChallenges)
				require.Equal(t, now, candidate.nextProbeTime)
				afterPN, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				require.Equal(t, nextPN+1, afterPN, "only the transmitted response consumes a number")
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("subsequent packet")}))
				require.True(t, c.triggerSending(monotime.Now()).progress)
				(<-c.emission.queue.(*sendQueue).queue).release()
				for _, buffer := range allocations {
					require.Zero(t, buffer.refCount, "discarded and transmitted storage is released")
				}
			})
		})
	}
}

// Use the production 1-RTT AEAD rather than transparent protection: FIPS AES-GCM
// consumes nonce state even when its ciphertext is never handed to the socket.
func newProbeAESGCMProtection(t *testing.T) (handshake.ShortHeaderSealer, handshake.ShortHeaderOpener) {
	t.Helper()
	defer qtls.SetCipherSuite(tls.TLS_AES_128_GCM_SHA256)()
	client := handshake.NewCryptoSetupClient(protocol.ConnectionID{}, &wire.TransportParameters{ActiveConnectionIDLimit: 2}, &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"probe-expiry"}}, false, utils.NewRTTStats(), nil, utils.DefaultLogger, protocol.Version1)
	serverConfig := testdata.GetTLSConfig()
	serverConfig.NextProtos = []string{"probe-expiry"}
	server := handshake.NewCryptoSetupServer(protocol.ConnectionID{}, &net.UDPAddr{}, &net.UDPAddr{}, &wire.TransportParameters{ActiveConnectionIDLimit: 2}, serverConfig, false, utils.NewRTTStats(), nil, utils.DefaultLogger, protocol.Version1)
	t.Cleanup(func() {
		clientErr, serverErr := client.Close(), server.Close()
		require.NoError(t, clientErr)
		require.NoError(t, serverErr)
	})
	require.NoError(t, client.StartHandshake(context.Background()))
	require.NoError(t, server.StartHandshake(context.Background()))
	complete := make(map[handshake.CryptoSetup]bool)
	for round := 0; len(complete) < 2 && round < 8; round++ {
		for _, pair := range [][2]handshake.CryptoSetup{{client, server}, {server, client}} {
			for event := pair[0].NextEvent(); event.Kind != handshake.EventNoEvent; event = pair[0].NextEvent() {
				switch event.Kind {
				case handshake.EventWriteInitialData:
					require.NoError(t, pair[1].HandleMessage(event.Data, protocol.EncryptionInitial))
				case handshake.EventWriteHandshakeData:
					require.NoError(t, pair[1].HandleMessage(event.Data, protocol.EncryptionHandshake))
				case handshake.EventHandshakeComplete:
					complete[pair[0]] = true
				}
			}
		}
	}
	require.Len(t, complete, 2)
	require.Equal(t, tls.TLS_AES_128_GCM_SHA256, server.ConnectionState().CipherSuite)
	server.SetHandshakeConfirmed()
	client.SetHandshakeConfirmed()
	sealer, err := server.Get1RTTSealer()
	require.NoError(t, err)
	opener, err := client.Get1RTTOpener()
	require.NoError(t, err)
	return sealer, opener
}

type probeSealRecorder struct {
	handshake.ShortHeaderSealer
	numbers []protocol.PacketNumber
}

func (s *probeSealRecorder) Seal(dst, src []byte, pn protocol.PacketNumber, ad []byte) []byte {
	s.numbers = append(s.numbers, pn)
	return s.ShortHeaderSealer.Seal(dst, src, pn, ad)
}

func TestEmissionServerProbeExpiryWithAESGCM(t *testing.T) {
	for _, peerResponse := range []bool{true, false} {
		t.Run(map[bool]string{true: "reply to peer", false: "challenge only"}[peerResponse], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tc := newEmissionTestConnection(t, false)
				c := tc.conn
				protection, opener := newProbeAESGCMProtection(t)
				sealer := &probeSealRecorder{ShortHeaderSealer: protection}
				observed := observeConstructionBuffer(t)
				var allocations []*packetBuffer
				expireNext := false
				sealing := NewMockSealingManager(gomock.NewController(t))
				sealing.EXPECT().Get1RTTSealer().DoAndReturn(func() (handshake.ShortHeaderSealer, error) {
					allocations = append(allocations, *observed)
					if expireNext {
						expireNext = false
						time.Sleep(5 * time.Second)
					}
					return sealer, nil
				}).AnyTimes()
				c.emission.packer.cryptoSetup = sealing
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("prior packet")}))
				require.True(t, c.triggerSending(monotime.Now()).progress)
				(<-c.emission.queue.(*sendQueue).queue).release()
				now := monotime.Now()
				candidate := &path{bytesReceived: 400, probeInterval: 200 * time.Millisecond, nextProbeTime: now, validationDeadline: now.Add(5 * time.Second)}
				challenge := &wire.PathChallengeFrame{Data: [8]byte{1}}
				response := &wire.PathResponseFrame{Data: [8]byte{2}}
				frames := []ackhandler.Frame{{Frame: challenge, Handler: emptyHandler{}}}
				if peerResponse {
					frames = append(frames, ackhandler.Frame{Frame: response, Handler: emptyHandler{}})
				}
				addr := &net.UDPAddr{Port: 4242}
				if peerResponse {
					tc.sendConn.EXPECT().WriteTo(gomock.Any(), addr, packetInfo{}).DoAndReturn(func(b []byte, _ net.Addr, _ packetInfo) error {
						unpacker := &packetUnpacker{shortHdrConnIDLen: c.connIDManager.Get().Len()}
						_, _, _, plaintext, err := unpacker.unpackShortHeaderPacket(opener, monotime.Now(), bytes.Clone(b))
						require.NoError(t, err)
						parser := wire.NewFrameParser(true, false, false)
						typ, n, err := parser.ParseType(plaintext, protocol.Encryption1RTT)
						require.NoError(t, err)
						frame, m, err := parser.ParseLessCommonFrame(typ, plaintext[n:], c.version)
						require.NoError(t, err)
						require.Equal(t, response, frame)
						require.Equal(t, len(plaintext), n+m, "only the peer response is emitted")
						return nil
					})
				}
				expireNext = true
				require.NoError(t, c.emission.serverProbe(c.connIDManager.Get(), frames, candidate, addr, packetInfo{}, 0, now))
				require.Zero(t, candidate.numChallenges)
				require.Equal(t, now, candidate.nextProbeTime)
				expectedNumbers := []protocol.PacketNumber{0, 1}
				if peerResponse {
					require.EqualValues(t, 1200, candidate.bytesSent)
					expectedNumbers = append(expectedNumbers, 2)
				} else {
					require.Zero(t, candidate.bytesSent)
				}
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("subsequent packet")}))
				require.True(t, c.triggerSending(monotime.Now()).progress)
				(<-c.emission.queue.(*sendQueue).queue).release()
				require.Equal(t, expectedNumbers, sealer.numbers, "only committed packets consume AEAD nonce state")
				for _, buffer := range allocations {
					require.Zero(t, buffer.refCount)
				}
			})
		})
	}
}
