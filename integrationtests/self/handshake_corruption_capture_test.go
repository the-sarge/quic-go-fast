package self_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/quic-go/quic-go/testutils/simnet"

	"github.com/stretchr/testify/require"
)

// The captured failures lost every server Initial carrying CRYPTO, while
// ACK-only Initials reached the client. Model that semantic distinction using
// owned qlog scalars, not historical TLS bytes or datagram ordinals.
type initialStarvation struct {
	mu                    sync.Mutex
	control               string
	pending               []initialStarvationPacket
	mappingError          string
	attempts              int
	damaged               int
	initialCryptoReceived int
	initialACKReceived    int
	decryptErrors         int
	pto                   uint32
	released              bool
	handshakeKeys         bool
}

type initialStarvationPacket struct {
	length              int
	kind                qlog.PacketType
	crypto, startsHello bool
	checksum            qlog.DatagramPayloadChecksum
}

func (s *initialStarvation) mutate(p simnet.Packet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checksum := qlog.CalculateDatagramPayloadChecksum(p.Data)
	offset := 0
	for offset < len(p.Data) {
		if len(s.pending) == 0 {
			s.mappingError = "missing server packet metadata"
			return
		}
		packet := s.pending[0]
		s.pending = s.pending[1:]
		end := offset + packet.length
		if packet.length <= 0 || end > len(p.Data) || (packet.checksum != 0 && packet.checksum != checksum) {
			s.mappingError = fmt.Sprintf("server metadata length=%d checksum=%d; remaining=%d checksum=%d", packet.length, packet.checksum, len(p.Data)-offset, checksum)
			return
		}
		if packet.kind == qlog.PacketTypeInitial && packet.crypto {
			header, raw, _, err := wire.ParsePacket(p.Data[offset:end])
			if err != nil || header.Type != protocol.PacketTypeInitial || len(raw) != packet.length {
				s.mappingError = "Initial CRYPTO metadata does not match parsed packet boundary"
				return
			}
			if packet.startsHello {
				s.attempts++
			}
			if s.control == "release retransmission" && packet.startsHello && s.attempts == 2 {
				s.released = true
			}
			if s.control != "undamaged" && !s.released {
				p.Data[end-1] ^= 1
				s.damaged++
			}
		}
		offset = end
	}
}

type initialStarvationRecorder struct {
	state  *initialStarvation
	client bool
	qlogwriter.Recorder
}

func (r *initialStarvationRecorder) RecordEvent(ev qlogwriter.Event) {
	r.Recorder.RecordEvent(ev)
	s := r.state
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e := ev.(type) {
	case qlog.PacketSent:
		if r.client {
			return
		}
		p := initialStarvationPacket{length: e.Raw.Length, kind: e.Header.PacketType, checksum: e.DatagramPayloadChecksum}
		if p.kind == qlog.PacketTypeInitial {
			for _, frame := range e.Frames {
				if crypto, ok := frame.Frame.(*qlog.CryptoFrame); ok && crypto.Length > 0 {
					p.crypto = true
					p.startsHello = p.startsHello || crypto.Offset == 0
				}
			}
		}
		s.pending = append(s.pending, p)
	case qlog.PacketReceived:
		if !r.client || e.Header.PacketType != qlog.PacketTypeInitial {
			return
		}
		for _, frame := range e.Frames {
			switch f := frame.Frame.(type) {
			case *qlog.CryptoFrame:
				if f.Length > 0 {
					s.initialCryptoReceived++
				}
			case *qlog.AckFrame:
				s.initialACKReceived++
			}
		}
	case qlog.PacketDropped:
		if r.client && e.Header.PacketType == qlog.PacketTypeInitial && e.Trigger == qlog.PacketDropPayloadDecryptError {
			s.decryptErrors++
		}
	case qlog.PTOCountUpdated:
		if !r.client {
			s.pto = max(s.pto, e.PTOCount)
		}
	case qlog.KeyUpdated:
		if r.client && (e.KeyType == qlog.KeyTypeClientHandshake || e.KeyType == qlog.KeyTypeServerHandshake) {
			s.handshakeKeys = true
		}
	}
}

func TestHandshakeCapturedCorruption(t *testing.T) {
	for _, control := range []string{"Initial starvation", "release retransmission", "undamaged"} {
		t.Run(control, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				state := &initialStarvation{control: control}
				router := &callbackRouter{Router: &simnet.PerfectRouter{}}
				clientSocket, serverSocket, closeLink := newSimnetLinkWithRouter(t, scaleDuration(5*time.Millisecond), router)
				defer closeLink(t)
				router.OnSendPacket = func(p simnet.Packet) {
					if p.From.String() == serverSocket.LocalAddr().String() {
						state.mutate(p)
					}
				}
				d := newHandshakeDiagnostics(t, fmt.Sprintf("captured #46 control=%s version=%s timeout=%s", control, version, scaleDuration(time.Second)))
				d.label = "corruption"
				tracer := func(ctx context.Context, client bool, id quic.ConnectionID) qlogwriter.Trace {
					return &events.Trace{Recorder: &initialStarvationRecorder{state: state, client: client, Recorder: d.tracer(ctx, client, id).AddProducer()}}
				}
				conf := getQuicConfig(&quic.Config{DisablePathMTUDiscovery: true, Tracer: tracer})
				serverTr := &quic.Transport{Conn: serverSocket}
				defer serverTr.Close()
				clientTr := &quic.Transport{Conn: clientSocket}
				defer clientTr.Close()
				listener, err := serverTr.Listen(getTLSConfig(), conf)
				require.NoError(t, err)
				defer listener.Close()
				ctx, cancel := context.WithTimeout(t.Context(), scaleDuration(time.Second))
				defer cancel()
				d.phase(true, "Dial")
				conn, err := clientTr.Dial(ctx, serverSocket.LocalAddr(), getTLSClientConfig(), conf)
				d.phase(true, fmt.Sprintf("Dial returned: %v", err))
				if control == "Initial starvation" {
					require.ErrorIs(t, err, context.DeadlineExceeded)
					require.Nil(t, conn)
				} else {
					require.NoError(t, err)
					defer conn.CloseWithError(0, "")
					server, err := listener.Accept(ctx)
					require.NoError(t, err)
					defer server.CloseWithError(0, "")
					stream, err := conn.OpenStreamSync(ctx)
					require.NoError(t, err)
					deadline, _ := ctx.Deadline()
					require.NoError(t, stream.SetDeadline(deadline))
					payload := GeneratePRData(4096)
					_, err = stream.Write(payload)
					require.NoError(t, err)
					require.NoError(t, stream.Close())
					received, err := server.AcceptStream(ctx)
					require.NoError(t, err)
					require.NoError(t, received.SetDeadline(deadline))
					data, err := io.ReadAll(received)
					require.NoError(t, err)
					require.Equal(t, payload, data)
					_, err = received.Write(data)
					require.NoError(t, err)
					require.NoError(t, received.Close())
					echoed, err := io.ReadAll(stream)
					require.NoError(t, err)
					require.Equal(t, payload, echoed)
				}
				synctest.Wait()
				state.mu.Lock()
				defer state.mu.Unlock()
				require.Empty(t, state.mappingError)
				t.Logf("Initial attempts=%d damaged=%d received_CRYPTO=%d received_ACK=%d authentication_drops=%d PTO=%d released=%t handshake_keys=%t", state.attempts, state.damaged, state.initialCryptoReceived, state.initialACKReceived, state.decryptErrors, state.pto, state.released, state.handshakeKeys)
				if control == "Initial starvation" {
					require.GreaterOrEqual(t, state.attempts, 2)
					require.GreaterOrEqual(t, state.damaged, 2)
					require.Equal(t, state.damaged, state.decryptErrors)
					require.Zero(t, state.initialCryptoReceived)
					require.Positive(t, state.initialACKReceived)
					require.Positive(t, state.pto)
					require.False(t, state.handshakeKeys)
				} else {
					require.Positive(t, state.initialCryptoReceived)
					require.True(t, state.handshakeKeys)
					if control == "release retransmission" {
						require.True(t, state.released)
						require.Positive(t, state.damaged)
						require.Positive(t, state.decryptErrors)
						require.Positive(t, state.pto)
					} else {
						require.Zero(t, state.damaged)
						require.Zero(t, state.decryptErrors)
					}
				}
			})
		})
	}
}
