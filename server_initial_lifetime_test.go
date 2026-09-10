package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlogwriter"

	"github.com/stretchr/testify/require"
)

type initialLifetimeIDGenerator func() (ConnectionID, error)

func (g initialLifetimeIDGenerator) GenerateConnectionID() (ConnectionID, error) { return g() }
func (initialLifetimeIDGenerator) ConnectionIDLen() int                          { return 8 }

func TestServerInitialLifetimeGenerationError(t *testing.T) {
	for _, alreadyCanceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "construction_cause", true: "preserve_cause"}[alreadyCanceled], func(t *testing.T) {
			s := newZeroRTTLifetimeServer(t)
			id := randConnID(8)
			pending := zeroRTTLifetimePacket(t, id)
			s.handlePacketImpl(pending)
			defer s.retireZeroRTTQueue(id)
			initial := getValidInitialPacket(t, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 42}, randConnID(5), id)
			generationErr, priorErr := errors.New("generation failed"), errors.New("application canceled")
			var original context.Context
			application, cancelApplication := context.WithCancelCause(context.Background())
			defer cancelApplication(nil)
			if alreadyCanceled {
				cancelApplication(priorErr)
			}
			s.connContext = func(ctx context.Context, _ *ClientInfo) (context.Context, error) {
				original = ctx
				return application, nil
			}
			s.connIDGenerator = initialLifetimeIDGenerator(func() (ConnectionID, error) { return ConnectionID{}, generationErr })
			tracerCreated := false
			s.config.Tracer = func(context.Context, bool, ConnectionID) qlogwriter.Trace { tracerCreated = true; return nil }
			err := s.handleInitialImpl(initial, &wire.Header{DestConnectionID: id, Version: protocol.Version1})
			require.ErrorIs(t, err, generationErr)
			require.False(t, tracerCreated, "generation failure must precede tracer allocation")
			require.ErrorIs(t, context.Cause(original), generationErr)
			if alreadyCanceled {
				require.ErrorIs(t, context.Cause(application), priorErr)
			}
			require.Zero(t, initial.buffer.refCount)
			require.Contains(t, s.zeroRTTQueues, id, "generation failure preserves existing expiry policy")
			require.Equal(t, 1, pending.buffer.refCount)
		})
	}
}

// Delegate to the real TLS setup, observing only its lifecycle boundary.
type initialLifetimeCrypto struct {
	cryptoStreamHandler
	started, closed bool
}

func (c *initialLifetimeCrypto) StartHandshake(ctx context.Context) error {
	c.started = true
	return c.cryptoStreamHandler.StartHandshake(ctx)
}
func (c *initialLifetimeCrypto) Close() error {
	c.closed = true
	if err := c.cryptoStreamHandler.Close(); err != nil {
		return err
	}
	return errors.New("cleanup error must not replace construction failure")
}

type initialLifetimeLog struct {
	bytes.Buffer
	ctx    context.Context
	closed bool
	cause  error
}

func (w *initialLifetimeLog) Close() error {
	w.closed = true
	w.cause = context.Cause(w.ctx)
	return errors.New("log cleanup error")
}

func testServerInitialLifetimeCollision(t *testing.T) {
	for _, alreadyCanceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "construction_cause", true: "preserve_cause"}[alreadyCanceled], func(t *testing.T) {
			s := newZeroRTTLifetimeServer(t)
			s.tlsConf = &tls.Config{}
			s.statelessResetter = newStatelessResetter(nil)
			s.tokenGenerator = handshake.NewTokenGenerator(handshake.TokenProtectorKey{})
			s.tr.resetTokens = make(map[protocol.StatelessResetToken]packetHandler)
			id, generatedID := randConnID(8), randConnID(8)
			pending := zeroRTTLifetimePacket(t, id)
			initial := getValidInitialPacket(t, pending.remoteAddr, randConnID(5), id)
			s.handlePacketImpl(pending)
			winner := &wrappedConn{testHooks: &connTestHooks{}}
			token := s.statelessResetter.GetStatelessResetToken(generatedID)
			s.connIDGenerator = initialLifetimeIDGenerator(func() (ConnectionID, error) {
				// Appear after the initial lookup, before the real registration.
				s.tr.AddWithConnID(id, generatedID, winner)
				s.tr.AddResetToken(token, winner)
				return generatedID, nil
			})
			priorErr := errors.New("application canceled")
			application, cancelApplication := context.WithCancelCause(context.Background())
			defer cancelApplication(nil)
			if alreadyCanceled {
				cancelApplication(priorErr)
			}
			var original context.Context
			s.connContext = func(ctx context.Context, _ *ClientInfo) (context.Context, error) {
				original = ctx
				return application, nil
			}
			log := &initialLifetimeLog{}
			logDone := make(chan struct{})
			s.config.Tracer = func(ctx context.Context, _ bool, _ ConnectionID) qlogwriter.Trace {
				log.ctx = ctx
				trace := qlogwriter.NewFileSeq(log)
				go func() { trace.Run(); close(logDone) }()
				return trace
			}
			constructed := make(chan struct{})
			var loser *wrappedConn
			var crypto *initialLifetimeCrypto
			s.newConn = func(
				ctx context.Context,
				ctxCancel context.CancelCauseFunc,
				conn sendConn,
				runner connRunner,
				origDestConnID protocol.ConnectionID,
				retrySrcConnID *protocol.ConnectionID,
				clientDestConnID protocol.ConnectionID,
				destConnID protocol.ConnectionID,
				srcConnID protocol.ConnectionID,
				connIDGenerator ConnectionIDGenerator,
				statelessResetter *statelessResetter,
				conf *Config,
				tlsConf *tls.Config,
				tokenGenerator *handshake.TokenGenerator,
				clientAddressValidated bool,
				rtt time.Duration,
				qlogTrace qlogwriter.Trace,
				logger utils.Logger,
				v protocol.Version,
			) *wrappedConn {
				loser = newConnection(ctx, ctxCancel, conn, runner, origDestConnID, retrySrcConnID, clientDestConnID, destConnID, srcConnID, connIDGenerator, statelessResetter, conf, tlsConf, tokenGenerator, clientAddressValidated, rtt, qlogTrace, logger, v)
				crypto = &initialLifetimeCrypto{cryptoStreamHandler: loser.cryptoStreamHandler}
				loser.cryptoStreamHandler = crypto
				close(constructed)
				return loser
			}
			// The channel makes the pre-fix blocked close observable without
			// leaving a worker behind. This is a test watchdog, not a latency SLA.
			done := make(chan error, 1)
			go func() {
				done <- s.handleInitialImpl(initial, &wire.Header{DestConnectionID: id, Version: protocol.Version1})
			}()
			<-constructed
			var err error
			timedOut := false
			select {
			case err = <-done:
			case <-time.After(time.Second):
				timedOut = true
				loser.ctxCancel(errors.New("test watchdog"))
				err = <-done
			}
			// Restore resources on the red path before reporting failure.
			closed := crypto.closed
			if !closed {
				loser.closePacketAdmission()
				crypto.Close()
				loser.qlogger.Close()
			}
			<-logDone
			require.False(t, timedOut, "registration failure waited for an unstarted connection loop")
			require.NoError(t, err)
			require.True(t, closed, "unstarted TLS must be closed")
			require.False(t, crypto.started)
			require.Nil(t, loser.timer, "protocol run must not start")
			require.True(t, loser.receivedPacketsClosed)
			require.True(t, loser.receivedPackets.Empty())
			require.Zero(t, initial.buffer.refCount)
			require.Zero(t, pending.buffer.refCount)
			require.NotContains(t, s.zeroRTTQueues, id)
			var refusal *TransportError
			require.ErrorAs(t, context.Cause(original), &refusal)
			require.Equal(t, ConnectionRefused, refusal.ErrorCode)
			require.False(t, refusal.Remote)
			if alreadyCanceled {
				require.ErrorIs(t, context.Cause(loser.Context()), priorErr)
			} else {
				require.Same(t, refusal, context.Cause(loser.Context()))
			}
			require.True(t, log.closed)
			require.Same(t, context.Cause(loser.Context()), log.cause, "cancel before qlog close")
			for _, connID := range []ConnectionID{id, generatedID} {
				got, ok := s.tr.Get(connID)
				require.True(t, ok)
				require.Same(t, winner, got)
			}
			require.Same(t, winner, s.tr.resetTokens[token])
			// The shared socket remains owned by its caller.
			require.NoError(t, s.conn.SetReadDeadline(time.Time{}))
		})
	}
}
