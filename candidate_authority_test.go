package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Consumers negotiate candidate authority with upstream QUIC types only.
type candidateGroupV1 interface {
	CandidateGroupV1() (join func(*Transport) error, selectWinner func(context.Context, *Conn) error, closeGroup func(context.Context) error)
}

type candidateGroupAPI struct {
	join         func(*Transport) error
	selectWinner func(context.Context, *Conn) error
	closeGroup   func(context.Context) error
}

func newCandidateGroup(t *testing.T, tr *Transport) candidateGroupAPI {
	t.Helper()
	api, ok := any(tr).(candidateGroupV1)
	require.True(t, ok, "transport must expose a standard-type candidate group capability")
	join, selectWinner, closeGroup := api.CandidateGroupV1()
	return candidateGroupAPI{join: join, selectWinner: selectWinner, closeGroup: closeGroup}
}

func admitAllNetwork(netip.AddrPort, netip.AddrPort, []byte) bool { return true }

// newCandidateTransport returns an admitted transport ready to join a group.
func newCandidateTransport(t *testing.T, conn net.PacketConn) *Transport {
	t.Helper()
	tr := &Transport{Conn: conn, ConnectionIDLength: 8}
	require.NoError(t, configureNetworkAdmission(t, tr, admitAllNetwork, admitAllNetwork, false))
	t.Cleanup(func() { tr.Close() })
	return tr
}

func candidateClientTLS() *tls.Config {
	return &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: testdata.GetTLSConfig().NextProtos}
}

// candidateListener is satisfied by Listener and EarlyListener.
type candidateListener interface {
	Addr() net.Addr
	Accept(context.Context) (*Conn, error)
}

// dialCandidatePair establishes one connection from an ordinary client transport.
func dialCandidatePair(t *testing.T, ctx context.Context, ln candidateListener) (client, server *Conn) {
	t.Helper()
	clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { clientTransport.Close() })
	client, err := clientTransport.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
	require.NoError(t, err)
	server, err = ln.Accept(ctx)
	require.NoError(t, err)
	return client, server
}

func requireRemoteCandidateClose(t *testing.T, conn *Conn) {
	t.Helper()
	select {
	case <-conn.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("loser peer did not receive a close")
	}
	appErr, ok := context.Cause(conn.Context()).(*ApplicationError)
	require.True(t, ok, "peer must observe a standard application close, got %v", context.Cause(conn.Context()))
	require.True(t, appErr.Remote)
}

// requireCandidateTerminated waits for the connection's teardown, which
// finishes after its close outcome is published.
func requireCandidateTerminated(t *testing.T, conn *Conn) {
	t.Helper()
	select {
	case <-conn.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("candidate connection did not terminate")
	}
}

func TestCandidateGroupMultibindingWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	trA := newCandidateTransport(t, newUDPConnLocalhost(t))
	trB := newCandidateTransport(t, newUDPConnLocalhost(t))
	group := newCandidateGroup(t, trA)
	require.NoError(t, group.join(trA))
	require.NoError(t, group.join(trB))
	lnA, err := trA.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	lnB, err := trB.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	winnerPeer, winner := dialCandidatePair(t, ctx, lnA)
	loserPeer, loser := dialCandidatePair(t, ctx, lnB)

	// A nil result means the loser close reached its socket before revocation.
	require.NoError(t, group.selectWinner(ctx, winner))
	requireRemoteCandidateClose(t, loserPeer)
	requireCandidateTerminated(t, loser)
	exchangeAdmissionStream(t, ctx, winnerPeer, winner)
	require.Error(t, group.selectWinner(ctx, winner), "selection is one group transition")
	require.NoError(t, group.closeGroup(ctx), "closing a selected group leaves the winner to its owner")
	exchangeAdmissionStream(t, ctx, winnerPeer, winner)

	// Terminal cleanup severs the tokens that outlive their connections.
	require.NoError(t, winner.CloseWithError(0, ""))
	for _, conn := range []*Conn{winner, loser} {
		trA.candidates.mu.Lock()
		retained := conn.authority.conn
		trA.candidates.mu.Unlock()
		require.Nil(t, retained)
	}
}

type candidateProbeConn struct {
	fakeBatchSendConn
	probes int
}

func (c *candidateProbeConn) WriteTo([]byte, net.Addr, packetInfo) error {
	c.probes++
	return nil
}

// Work queued while a candidate was live is checked again when it runs:
// ordinary, batched and synchronous probe submissions stop, and queued input
// is released before unpacking.
func TestCandidateQueuedWorkRevoked(t *testing.T) {
	t.Run("receive", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		// The strict unpacker fails the test if fenced input reaches it.
		tc := newServerTestConnection(t, mockCtrl, nil, false, connectionOptUnpacker(NewMockUnpacker(mockCtrl)))
		tc.conn.authority = &connAuthority{}
		p := getShortHeaderPacket(t, tc.remoteAddr, tc.srcConnID, 1, []byte("queued"))
		tc.conn.handlePacket(p)
		tc.conn.authority.state.Store(authorityClosing)
		processed, err := tc.conn.handlePackets()
		require.NoError(t, err)
		require.False(t, processed)
		require.True(t, tc.conn.receivedPackets.Empty())
	})
	for _, state := range []uint32{authorityLive, authorityClosing, authorityRevoked} {
		synctest.Test(t, func(t *testing.T) {
			conn := &candidateProbeConn{fakeBatchSendConn: fakeBatchSendConn{accept: func(_ int, bufs [][]byte) (int, error) { return len(bufs), nil }}}
			q := newSendQueue(conn, nil).(*sendQueue)
			authority := &connAuthority{}
			q.authority = authority
			var bufs []*packetBuffer
			for i, gso := range []uint16{0, 0, 0, 1200} {
				buf := getPacketWithContents([]byte{byte(i)})
				bufs = append(bufs, buf)
				q.Send(buf, gso, protocol.ECT0, sendMetadata{})
			}
			authority.state.Store(state)
			probe := getPacketWithContents([]byte("probe"))
			require.NoError(t, q.SendProbe(probe, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443}, packetInfo{}))
			probe.Release()
			startAndFinishQueue(t, q, nil)

			submitted := len(conn.writes) + conn.probes
			for _, batch := range conn.batches {
				submitted += len(batch)
			}
			if state == authorityLive {
				require.Equal(t, 5, submitted)
			} else {
				require.Zero(t, submitted, "fenced candidate work must not reach the socket")
			}
			for _, buf := range bufs {
				require.Zero(t, buf.refCount, "revoked work still releases its storage")
			}
		})
	}
}

// After selection, input for a revoked candidate or an unknown connection ID
// has no effect: no close retransmission, reset or allocation, and the winner
// keeps its shared socket.
func TestCandidateLateInputSuppressed(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tr := newCandidateTransport(t, newUDPConnLocalhost(t))
	tr.StatelessResetKey = &StatelessResetKey{1}
	var allocations atomic.Int64
	tr.ConnContext = func(ctx context.Context, _ *ClientInfo) (context.Context, error) {
		allocations.Add(1)
		return ctx, nil
	}
	group := newCandidateGroup(t, tr)
	require.NoError(t, group.join(tr))
	ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	winnerPeer, winner := dialCandidatePair(t, ctx, ln)
	loserPeer, loser := dialCandidatePair(t, ctx, ln)
	loserIDs := registeredConnIDs(tr, loser)
	require.NotEmpty(t, loserIDs)
	// A candidate that closed itself earlier retains closing entries too.
	earlierPeer, earlier := dialCandidatePair(t, ctx, ln)
	earlierIDs := registeredConnIDs(tr, earlier)
	require.NotEmpty(t, earlierIDs)
	require.NoError(t, earlier.CloseWithError(0, ""))
	requireRemoteCandidateClose(t, earlierPeer)
	require.NoError(t, group.selectWinner(ctx, winner))
	requireRemoteCandidateClose(t, loserPeer)
	require.EqualValues(t, 3, allocations.Load())

	late := newUDPConnLocalhost(t)
	// Retained closing entries, then an unknown connection ID.
	lateIDs := append(append(loserIDs, earlierIDs...), protocol.ParseConnectionID([]byte{9, 9, 9, 9, 9, 9, 9, 9}))
	for _, cid := range lateIDs {
		short := append([]byte{0x40}, cid.Bytes()...)
		short = append(short, make([]byte, 64)...)
		// Ordinary closing retransmits on the 1st, 2nd, 4th, 8th and 16th input;
		// earlier peer packets may already have advanced that count.
		for range 16 {
			_, err := late.WriteTo(short, tr.Conn.LocalAddr())
			require.NoError(t, err)
		}
	}
	// A separate socket keeps the dialer's receive loop from consuming responses.
	lateTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	dialCtx, dialCancel := context.WithTimeout(ctx, scaleDuration(50*time.Millisecond))
	_, err = lateTransport.Dial(dialCtx, ln.Addr(), candidateClientTLS(), nil)
	dialCancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, lateTransport.Close())

	// The winner's round trip follows the late input through the same receive loop.
	exchangeAdmissionStream(t, ctx, winnerPeer, winner)
	require.EqualValues(t, 3, allocations.Load(), "a selected group admits no new candidate")
	require.NoError(t, late.SetReadDeadline(time.Now().Add(scaleDuration(50*time.Millisecond))))
	n, _, err := late.ReadFrom(make([]byte, 1500))
	require.Error(t, err, "late input produced a %d-byte response", n)
	var ne net.Error
	require.ErrorAs(t, err, &ne)
	require.True(t, ne.Timeout())
}

// registeredConnIDs snapshots the routing entries owned by conn.
func registeredConnIDs(tr *Transport, conn *Conn) []protocol.ConnectionID {
	tr.mutex.Lock()
	defer tr.mutex.Unlock()
	var ids []protocol.ConnectionID
	for id, h := range tr.handlers {
		if w, ok := h.(*wrappedConn); (ok && w.Conn == conn) || h == packetHandler(conn) {
			ids = append(ids, id)
		}
	}
	return ids
}

// retainingRunner keeps closed connection-ID entries for a fixed period
// instead of the connection's 3*PTO, which is about 10ms on loopback.
type retainingRunner struct {
	connRunner
	retention time.Duration
}

func (r retainingRunner) ReplaceWithClosed(ids []protocol.ConnectionID, connClose []byte, _ time.Duration) {
	r.connRunner.ReplaceWithClosed(ids, connClose, r.retention)
}

// retainClosedEntries applies retention to server connections of listeners
// created afterwards, so a test inspects closed entries without racing their
// retirement.
func retainClosedEntries(t *testing.T, retention time.Duration) {
	t.Helper()
	orig := newConnection
	newConnection = func(
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
		return orig(ctx, ctxCancel, conn, retainingRunner{connRunner: runner, retention: retention}, origDestConnID, retrySrcConnID, clientDestConnID, destConnID, srcConnID, connIDGenerator, statelessResetter, conf, tlsConf, tokenGenerator, clientAddressValidated, rtt, qlogTrace, logger, v)
	}
	t.Cleanup(func() { newConnection = orig })
}

// candidateWriteConn injects a socket failure for one destination.
type candidateWriteConn struct {
	net.PacketConn
	fail func(net.Addr) error
}

func (c *candidateWriteConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if err := c.fail(addr); err != nil {
		return 0, err
	}
	return c.PacketConn.WriteTo(b, addr)
}

// A close that cannot reach the socket is reported distinctly, and neither
// failure touches the winner sharing that socket.
func TestCandidateCloseOutcomes(t *testing.T) {
	errInjected := errors.New("injected close write failure")
	for _, tc := range []struct {
		name   string
		noPath bool
	}{{"no usable path", true}, {"socket failure", false}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var loser atomic.Pointer[Conn]
			// Only the loser's close runs after its authority left the live state.
			closing := func(addr netip.AddrPort) bool {
				l := loser.Load()
				return l != nil && l.RemoteAddr().(*net.UDPAddr).AddrPort() == addr && !l.authority.permitsWork()
			}
			socket := &candidateWriteConn{PacketConn: newUDPConnLocalhost(t), fail: func(addr net.Addr) error {
				if !tc.noPath && closing(addr.(*net.UDPAddr).AddrPort()) {
					return errInjected
				}
				return nil
			}}
			tr := &Transport{Conn: socket, ConnectionIDLength: 8}
			t.Cleanup(func() { tr.Close() })
			send := func(remote, _ netip.AddrPort, _ []byte) bool { return !tc.noPath || !closing(remote) }
			require.NoError(t, configureNetworkAdmission(t, tr, admitAllNetwork, send, false))
			group := newCandidateGroup(t, tr)
			require.NoError(t, group.join(tr))
			ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
			require.NoError(t, err)
			winnerPeer, winner := dialCandidatePair(t, ctx, ln)
			_, loserConn := dialCandidatePair(t, ctx, ln)
			loser.Store(loserConn)
			exchangeAdmissionStream(t, ctx, winnerPeer, winner)

			err = group.selectWinner(ctx, winner)
			var outcome interface{ NoUsablePathV1() bool }
			require.ErrorAs(t, err, &outcome)
			require.Equal(t, tc.noPath, outcome.NoUsablePathV1())
			if !tc.noPath {
				require.ErrorIs(t, err, errInjected)
			}
			exchangeAdmissionStream(t, ctx, winnerPeer, winner)
		})
	}
}

// One client socket carries the winner, an established loser and a loser still
// in its handshake. Each loser gets the standard close for its phase, and the
// shared transport keeps serving the winner.
func TestCandidateSharedSocketAndHandshakeClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tr := newCandidateTransport(t, newUDPConnLocalhost(t))
	group := newCandidateGroup(t, tr)
	require.NoError(t, group.join(tr))
	listen := func() *Listener {
		server := &Transport{Conn: newUDPConnLocalhost(t)}
		t.Cleanup(func() { server.Close() })
		ln, err := server.Listen(testdata.GetTLSConfig(), nil)
		require.NoError(t, err)
		return ln
	}
	dial := func(ln *Listener) (client, server *Conn) {
		client, err := tr.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
		require.NoError(t, err)
		server, err = ln.Accept(ctx)
		require.NoError(t, err)
		return client, server
	}
	winner, winnerPeer := dial(listen())
	_, loserPeer := dial(listen())

	silent := newUDPConnLocalhost(t)
	dialErr := make(chan error, 1)
	go func() {
		_, err := tr.Dial(ctx, silent.LocalAddr(), candidateClientTLS(), nil)
		dialErr <- err
	}()
	b := make([]byte, 1500)
	require.NoError(t, silent.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err := silent.ReadFrom(b) // the handshake loser has sent its first Initial
	require.NoError(t, err)

	require.NoError(t, group.selectWinner(ctx, winner))
	requireRemoteCandidateClose(t, loserPeer)
	require.Error(t, <-dialErr)
	for {
		n, _, err := silent.ReadFrom(b)
		require.NoError(t, err, "the handshake loser's close must reach its socket")
		if initialConnectionClose(t, b[:n]) {
			break
		}
	}
	exchangeAdmissionStream(t, ctx, winner, winnerPeer)
}

// initialConnectionClose opens a client Initial and reports whether it closes
// the connection with the standard handshake-phase APPLICATION_ERROR.
func initialConnectionClose(t *testing.T, data []byte) bool {
	t.Helper()
	hdr, packet, _, err := wire.ParsePacket(data)
	require.NoError(t, err)
	require.Equal(t, protocol.PacketTypeInitial, hdr.Type)
	_, opener := handshake.NewInitialAEAD(hdr.DestConnectionID, protocol.PerspectiveServer, hdr.Version)
	b := append([]byte(nil), packet...)
	pnOffset := int(hdr.ParsedLen())
	masked := append([]byte(nil), b[pnOffset:pnOffset+4]...)
	opener.DecryptHeader(b[pnOffset+4:pnOffset+20], &b[0], b[pnOffset:pnOffset+4])
	pnLen := int(b[0]&0x3) + 1
	copy(b[pnOffset+pnLen:pnOffset+4], masked[pnLen:]) // payload bytes were never masked
	var pn protocol.PacketNumber
	for _, x := range b[pnOffset : pnOffset+pnLen] {
		pn = pn<<8 | protocol.PacketNumber(x)
	}
	payload, err := opener.Open(nil, b[pnOffset+pnLen:], pn, b[:pnOffset+pnLen])
	require.NoError(t, err)
	// The close is the first frame after padding; otherwise this is a
	// retransmitted handshake Initial.
	parser := wire.NewFrameParser(false, false, false)
	typ, l, err := parser.ParseType(payload, protocol.EncryptionInitial)
	if err != nil || typ != wire.FrameTypeConnectionClose {
		return false
	}
	frame, _, err := parser.ParseLessCommonFrame(typ, payload[l:], hdr.Version)
	require.NoError(t, err)
	return frame.(*wire.ConnectionCloseFrame).ErrorCode == uint64(qerr.ApplicationErrorErrorCode)
}

// blockedCloseServer holds the loser's close write until release, so tests
// control whether it has been submitted.
type blockedCloseServer struct {
	group                  candidateGroupAPI
	winnerPeer, winner     *Conn
	loser                  *Conn
	blocked, release       chan struct{}
	cancelLoserConnContext context.CancelFunc
}

func newBlockedCloseServer(t *testing.T, ctx context.Context) *blockedCloseServer {
	t.Helper()
	f := &blockedCloseServer{blocked: make(chan struct{}), release: make(chan struct{})}
	var loser atomic.Pointer[Conn]
	var held atomic.Bool
	socket := &candidateWriteConn{PacketConn: newUDPConnLocalhost(t), fail: func(addr net.Addr) error {
		// Hold only the loser's close; permitted close-only retransmissions
		// that follow it pass through.
		if l := loser.Load(); l != nil && l.RemoteAddr().String() == addr.String() && !l.authority.permitsWork() && held.CompareAndSwap(false, true) {
			close(f.blocked)
			<-f.release
		}
		return nil
	}}
	tr := &Transport{Conn: socket, ConnectionIDLength: 8}
	t.Cleanup(func() { tr.Close() })
	// The application owns each accepted connection's parent context.
	var parents []context.CancelFunc
	tr.ConnContext = func(ctx context.Context, _ *ClientInfo) (context.Context, error) {
		ctx, cancel := context.WithCancel(ctx)
		parents = append(parents, cancel) // the server goroutine serializes calls
		return ctx, nil
	}
	require.NoError(t, configureNetworkAdmission(t, tr, admitAllNetwork, admitAllNetwork, false))
	f.group = newCandidateGroup(t, tr)
	require.NoError(t, f.group.join(tr))
	ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	f.winnerPeer, f.winner = dialCandidatePair(t, ctx, ln)
	_, f.loser = dialCandidatePair(t, ctx, ln)
	loser.Store(f.loser)
	f.cancelLoserConnContext = parents[1]
	return f
}

func TestCandidateCancelRacesSelection(t *testing.T) {
	// Cancelling selection while a loser's close is unsubmitted reports the
	// cancellation, revokes the loser and keeps the winner; a racing
	// closeGroup waits for the transition and then leaves the winner alone.
	t.Run("caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		f := newBlockedCloseServer(t, ctx)
		selectCtx, cancelSelect := context.WithCancel(ctx)
		selected := make(chan error, 1)
		go func() { selected <- f.group.selectWinner(selectCtx, f.winner) }()
		<-f.blocked
		closed := make(chan error, 1)
		go func() { closed <- f.group.closeGroup(ctx) }()
		cancelSelect()
		err := <-selected
		require.ErrorIs(t, err, context.Canceled)
		var outcome interface{ NoUsablePathV1() bool }
		require.ErrorAs(t, err, &outcome)
		require.False(t, outcome.NoUsablePathV1())
		require.Equal(t, authorityRevoked, f.loser.authority.state.Load())
		require.NoError(t, <-closed)
		close(f.release) // the running submission may complete
		exchangeAdmissionStream(t, ctx, f.winnerPeer, f.winner)
	})

	// An application cancelling a loser's parent context does not terminate
	// the connection, so selection still waits for its actual submission.
	t.Run("application connection context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		f := newBlockedCloseServer(t, ctx)
		f.cancelLoserConnContext()
		selected := make(chan error, 1)
		go func() { selected <- f.group.selectWinner(ctx, f.winner) }()
		select {
		case <-f.blocked:
		case err := <-selected:
			t.Fatalf("selection completed (%v) before the loser close was submitted", err)
		}
		close(f.release)
		require.NoError(t, <-selected)
		exchangeAdmissionStream(t, ctx, f.winnerPeer, f.winner)
	})

	// A published outcome is the truth even when the caller's context has
	// also ended; select must not choose cancellation at random.
	t.Run("published outcome precedes cancellation", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(t.Context())
		cancel()
		errIO := errors.New("injected")
		for range 32 {
			for _, published := range []error{nil, candidateCloseFailure(errIO)} {
				for _, ctx := range []context.Context{cancelled, t.Context()} {
					// The run path publishes before it terminates, so both are ready.
					a := &connAuthority{done: make(chan struct{})}
					a.state.Store(authorityClosing)
					a.finish(published)
					require.Equal(t, published, a.awaitClose(ctx))
				}
			}
		}
	})
}

// Ungrouped transports keep ordinary closing: the peer observes the close, a
// closed connection ID retransmits it, and unknown IDs still get a reset.
func TestCandidateOrdinaryProfileClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tr := &Transport{Conn: newUDPConnLocalhost(t), ConnectionIDLength: 8, StatelessResetKey: &StatelessResetKey{1}}
	t.Cleanup(func() { tr.Close() })
	ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	client, server := dialCandidatePair(t, ctx, ln)
	ids := registeredConnIDs(tr, server)
	require.NotEmpty(t, ids)
	require.NoError(t, server.CloseWithError(7, "done"))
	requireRemoteCandidateClose(t, client)

	late := newUDPConnLocalhost(t)
	b := make([]byte, 1500)
	for _, cid := range []protocol.ConnectionID{ids[0], protocol.ParseConnectionID([]byte{9, 9, 9, 9, 9, 9, 9, 9})} {
		short := append([]byte{0x40}, cid.Bytes()...)
		short = append(short, make([]byte, 64)...)
		_, err := late.WriteTo(short, tr.Conn.LocalAddr())
		require.NoError(t, err)
		require.NoError(t, late.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, _, err = late.ReadFrom(b)
		require.NoError(t, err, "ordinary closing must answer connection ID %s", cid)
	}
}

func TestCandidateGroupContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	unadmitted := &Transport{Conn: newUDPConnLocalhost(t)}
	defer unadmitted.Close()
	group := newCandidateGroup(t, unadmitted)
	require.Error(t, group.join(nil))
	require.Error(t, group.join(unadmitted), "candidates require network admission")
	tr := newCandidateTransport(t, newUDPConnLocalhost(t))
	require.NoError(t, group.join(tr))
	require.Error(t, group.join(tr))
	require.Error(t, newCandidateGroup(t, tr).join(tr), "a transport belongs to one group")
	initialized := newCandidateTransport(t, newUDPConnLocalhost(t))
	ln, err := initialized.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	require.Error(t, group.join(initialized), "join precedes initialization")
	_, outsider := dialCandidatePair(t, ctx, ln)
	require.Error(t, group.selectWinner(ctx, nil))
	require.Error(t, group.selectWinner(ctx, outsider), "an ungrouped connection cannot win")

	retainClosedEntries(t, time.Minute)
	ln, err = tr.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	peer, candidate := dialCandidatePair(t, ctx, ln)
	earlierPeer, earlier := dialCandidatePair(t, ctx, ln)
	earlierIDs := registeredConnIDs(tr, earlier)
	require.NoError(t, earlier.CloseWithError(0, ""))
	requireRemoteCandidateClose(t, earlierPeer)
	// Without a winner, closeGroup closes every candidate and admits no more.
	require.NoError(t, group.closeGroup(ctx))
	requireRemoteCandidateClose(t, peer)
	var retained []packetHandler
	tr.mutex.Lock()
	for _, id := range earlierIDs {
		if h, ok := tr.handlers[id]; ok {
			retained = append(retained, h)
		}
	}
	tr.mutex.Unlock()
	require.NotEmpty(t, retained)
	for _, h := range retained {
		require.IsType(t, &closedLocalConn{}, h)
		a := handlerAuthority(h)
		require.Same(t, earlier.authority, a, "a retained entry carries its connection's token")
		require.False(t, a.permitsClose(), "retained entries of an earlier close lose authority too")
	}
	requireCandidateTerminated(t, candidate)
	require.Error(t, group.selectWinner(ctx, candidate))
	late := newCandidateTransport(t, newUDPConnLocalhost(t))
	require.Error(t, group.join(late), "a transitioned group accepts no transport")
	_, err = tr.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
	require.ErrorIs(t, err, errCandidateFenced)
	require.NoError(t, group.closeGroup(ctx))
}

// A transition requested with an already-done context fails without effect.
// The slot and the context may both be ready when acquire selects, so the
// check must not be left to select's random choice.
func TestCandidatePreCancelledTransition(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tr := newCandidateTransport(t, newUDPConnLocalhost(t))
	group := newCandidateGroup(t, tr)
	require.NoError(t, group.join(tr))
	ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	peer, candidate := dialCandidatePair(t, ctx, ln)
	g := tr.candidates
	require.NotNil(t, g)

	cancelled, cancelNow := context.WithCancel(ctx)
	cancelNow()
	// Before the fix each call committed about half the time, so this many
	// attempts cannot pass by chance.
	const attempts = 256
	requireUntouched := func(err error) {
		t.Helper()
		require.ErrorIs(t, err, context.Canceled)
		g.mu.Lock()
		state := g.state
		g.mu.Unlock()
		require.Equal(t, candidateGroupSetup, state)
		require.False(t, g.isFenced())
		require.Equal(t, authorityLive, candidate.authority.state.Load())
		require.Empty(t, g.op, "the transition slot must be released")
	}
	for range attempts {
		requireUntouched(group.closeGroup(cancelled))
		requireUntouched(group.selectWinner(cancelled, candidate))
	}
	exchangeAdmissionStream(t, ctx, peer, candidate)

	// The released slot admits a live transition, after which a done context
	// still reports its cause instead of the transitioned-group result.
	require.NoError(t, group.selectWinner(ctx, candidate))
	require.True(t, g.isFenced())
	for range attempts {
		require.ErrorIs(t, group.closeGroup(cancelled), context.Canceled)
		require.ErrorIs(t, group.selectWinner(cancelled, candidate), context.Canceled)
	}
	require.NoError(t, group.closeGroup(ctx), "a live context keeps the no-op result")
	exchangeAdmissionStream(t, ctx, peer, candidate)
}

// countingConnIDGenerator counts server connection-ID generation.
type countingConnIDGenerator struct {
	protocol.DefaultConnectionIDGenerator
	calls *atomic.Int32
}

func (g *countingConnIDGenerator) GenerateConnectionID() (ConnectionID, error) {
	g.calls.Add(1)
	return g.DefaultConnectionIDGenerator.GenerateConnectionID()
}

// get0RTTPacket builds a 0-RTT long header packet for connID.
func get0RTTPacket(t *testing.T, raddr net.Addr, connID protocol.ConnectionID) receivedPacket {
	t.Helper()
	return getLongHeaderPacket(t,
		raddr,
		&wire.ExtendedHeader{
			Header: wire.Header{
				Type:             protocol.PacketType0RTT,
				SrcConnectionID:  protocol.ParseConnectionID([]byte{5, 4, 3, 2, 1}),
				DestConnectionID: connID,
				Length:           123,
				Version:          protocol.Version1,
			},
			PacketNumberLen: protocol.PacketNumberLen4,
		},
		make([]byte, 123),
	)
}

// slabBackedPacket moves p onto a coalesced slab with one view, so the
// caller observes the view's release through slab.released().
func slabBackedPacket(t *testing.T, p receivedPacket) (receivedPacket, *coalescedSlab) {
	t.Helper()
	slab := getCoalescedSlab()
	slab.buf.Data = append(slab.buf.Data[:0], p.data...)
	views := slab.split(len(p.data))
	require.Len(t, views, 1)
	p.buffer.Release()
	p.buffer = views[0]
	p.data = views[0].Data
	return p, slab
}

// retainedEntries returns the closed connection-ID entries left for ids.
func retainedEntries(t *testing.T, tr *Transport, ids []protocol.ConnectionID) []*closedLocalConn {
	t.Helper()
	tr.mutex.Lock()
	defer tr.mutex.Unlock()
	var entries []*closedLocalConn
	for _, id := range ids {
		if h, ok := tr.handlers[id]; ok {
			entry, ok := h.(*closedLocalConn)
			require.True(t, ok, "expected a closed-local entry, got %T", h)
			entries = append(entries, entry)
		}
	}
	require.NotEmpty(t, entries)
	return entries
}

// The server processes its receive queue after transport dispatch, so an
// Initial or 0-RTT packet queued before the fence must obey the same
// admission as dispatch: no callback, allocation or retention for an
// unknown connection ID, and the entry's token for a known one. Packets are handed
// to the server's processing step directly, the way its receive loop does.
func TestCandidateFencedServerQueue(t *testing.T) {
	t.Run("queued Initial reaches no callback", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		constructed := countServerConstructors(t)
		var connIDs, verify, connContext, config, tracer atomic.Int32
		tr := newCandidateTransport(t, newUDPConnLocalhost(t))
		tr.ConnectionIDGenerator = &countingConnIDGenerator{
			DefaultConnectionIDGenerator: protocol.DefaultConnectionIDGenerator{ConnLen: 8},
			calls:                        &connIDs,
		}
		tr.VerifySourceAddress = func(net.Addr) bool { verify.Add(1); return true }
		tr.ConnContext = func(ctx context.Context, _ *ClientInfo) (context.Context, error) {
			connContext.Add(1)
			return ctx, nil
		}
		group := newCandidateGroup(t, tr)
		require.NoError(t, group.join(tr))
		ln, err := tr.ListenEarly(testdata.GetTLSConfig(), &Config{
			GetConfigForClient: func(*ClientInfo) (*Config, error) { config.Add(1); return nil, nil },
			Tracer: func(context.Context, bool, ConnectionID) qlogwriter.Trace {
				tracer.Add(1)
				return nil
			},
		})
		require.NoError(t, err)
		s := ln.baseServer
		raw := newUDPConnLocalhost(t)
		connID := randConnID(8)
		zeroRTT := get0RTTPacket(t, raw.LocalAddr(), connID)
		require.True(t, s.handlePacketImpl(zeroRTT), "0-RTT is queued for the Initial")
		require.Contains(t, s.zeroRTTQueues, connID)

		require.NoError(t, group.closeGroup(ctx))
		// A slab-backed view records its release on the slab, so the test
		// never reads a pooled buffer that another goroutine may reuse.
		initial, slab := slabBackedPacket(t, getValidInitialPacket(t, raw.LocalAddr(), randConnID(5), connID))
		require.True(t, s.handlePacketImpl(initial), "the Initial path owns its buffer")
		require.True(t, slab.released(), "the Initial's buffer is released")
		require.NotContains(t, s.zeroRTTQueues, connID, "the 0-RTT queue is retired with the Initial")
		_, registered := s.tr.Get(connID)
		require.False(t, registered)
		require.Zero(t, constructed.Load(), "no connection is constructed")
		for name, n := range map[string]*atomic.Int32{
			"VerifySourceAddress": &verify, "GetConfigForClient": &config, "ConnContext": &connContext,
			"GenerateConnectionID": &connIDs, "Tracer": &tracer,
		} {
			require.Zero(t, n.Load(), "%s ran for an Initial queued before the fence", name)
		}
	})

	t.Run("queued 0-RTT is not retained", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		tr := newCandidateTransport(t, newUDPConnLocalhost(t))
		group := newCandidateGroup(t, tr)
		require.NoError(t, group.join(tr))
		ln, err := tr.ListenEarly(testdata.GetTLSConfig(), nil)
		require.NoError(t, err)
		s := ln.baseServer
		require.NoError(t, group.closeGroup(ctx))

		connID := randConnID(8)
		zeroRTT, slab := slabBackedPacket(t, get0RTTPacket(t, newUDPConnLocalhost(t).LocalAddr(), connID))
		require.False(t, s.handlePacketImpl(zeroRTT), "the caller releases a fenced 0-RTT packet")
		require.NotContains(t, s.zeroRTTQueues, connID, "no 0-RTT queue is created after the fence")
		zeroRTT.buffer.Release() // as the server's receive loop does
		require.True(t, slab.released(), "the server kept no hold on the fenced packet")
	})

	t.Run("queued 0-RTT retires an existing queue", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		tr := newCandidateTransport(t, newUDPConnLocalhost(t))
		group := newCandidateGroup(t, tr)
		require.NoError(t, group.join(tr))
		ln, err := tr.ListenEarly(testdata.GetTLSConfig(), nil)
		require.NoError(t, err)
		s := ln.baseServer
		raddr := newUDPConnLocalhost(t).LocalAddr()
		connID := randConnID(8)
		// An oversized view stays on its slab while queued, so the queue's
		// release is observable without reading a pooled buffer.
		oversized := get0RTTPacket(t, raddr, connID)
		oversized.data = append(oversized.data, make([]byte, protocol.MaxLargePacketBufferSize)...)
		queued, queuedSlab := slabBackedPacket(t, oversized)
		require.True(t, s.handlePacketImpl(queued), "0-RTT is queued before the fence")
		require.Contains(t, s.zeroRTTQueues, connID)
		require.False(t, queuedSlab.released(), "the queue holds the oversized view")

		require.NoError(t, group.closeGroup(ctx))
		zeroRTT, slab := slabBackedPacket(t, get0RTTPacket(t, raddr, connID))
		require.False(t, s.handlePacketImpl(zeroRTT), "the caller releases a fenced 0-RTT packet")
		require.NotContains(t, s.zeroRTTQueues, connID, "the existing 0-RTT queue is retired")
		require.True(t, queuedSlab.released(), "the retired queue released its packets")
		zeroRTT.buffer.Release() // as the server's receive loop does
		require.True(t, slab.released(), "the server kept no hold on the fenced packet")
	})

	t.Run("registered winner still receives", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		tr := newCandidateTransport(t, newUDPConnLocalhost(t))
		group := newCandidateGroup(t, tr)
		require.NoError(t, group.join(tr))
		var winnerEvents events.Recorder
		ln, err := tr.Listen(testdata.GetTLSConfig(), &Config{
			Tracer: func(context.Context, bool, ConnectionID) qlogwriter.Trace {
				return &events.Trace{Recorder: &winnerEvents}
			},
		})
		require.NoError(t, err)
		winnerPeer, winner := dialCandidatePair(t, ctx, ln)
		require.NoError(t, group.selectWinner(ctx, winner))
		// The round trip confirms the handshake, so Initial keys are dropped.
		exchangeAdmissionStream(t, ctx, winnerPeer, winner)
		winnerIDs := registeredConnIDs(tr, winner)
		require.NotEmpty(t, winnerIDs)
		winnerEvents.Clear()
		initial := getValidInitialPacket(t, winnerPeer.LocalAddr(), randConnID(5), winnerIDs[0])
		require.True(t, ln.baseServer.handlePacketImpl(initial))
		require.Eventually(t, func() bool {
			return len(winnerEvents.Events(qlog.PacketDropped{})) > 0
		}, 5*time.Second, 10*time.Millisecond, "the winner never saw the Initial")
	})

	t.Run("fenced loser entry receives nothing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		retainClosedEntries(t, time.Minute)
		tr := newCandidateTransport(t, newUDPConnLocalhost(t))
		group := newCandidateGroup(t, tr)
		require.NoError(t, group.join(tr))
		ln, err := tr.ListenEarly(testdata.GetTLSConfig(), nil)
		require.NoError(t, err)
		winnerPeer, winner := dialCandidatePair(t, ctx, ln)
		loserPeer, loser := dialCandidatePair(t, ctx, ln)
		// The early listener accepts before the handshake completes; the
		// loser's close must be a standard 1-RTT application close.
		for _, conn := range []*Conn{winner, loser} {
			select {
			case <-conn.HandshakeComplete():
			case <-ctx.Done():
				t.Fatal("handshake did not complete")
			}
		}
		loserIDs := registeredConnIDs(tr, loser)
		require.NotEmpty(t, loserIDs)
		require.NoError(t, group.selectWinner(ctx, winner))
		requireRemoteCandidateClose(t, loserPeer)
		requireCandidateTerminated(t, loser)
		entries := retainedEntries(t, tr, loserIDs)
		s := ln.baseServer
		for _, id := range loserIDs {
			require.True(t, s.handlePacketImpl(getValidInitialPacket(t, loserPeer.LocalAddr(), randConnID(5), id)))
			require.True(t, s.handlePacketImpl(get0RTTPacket(t, loserPeer.LocalAddr(), id)))
		}
		for _, entry := range entries {
			require.Zero(t, entry.counter.Load(), "a fenced loser's entry received server-queued input")
		}
		exchangeAdmissionStream(t, ctx, winnerPeer, winner)
	})
}

// candidateSubmissionConn is a socket that fails chosen submissions and runs a
// group transition after each one.
type candidateSubmissionConn struct {
	rawConn
	fail        func(n int, gsoSize uint16) error
	after       func(n int)
	submissions []string
}

func (*candidateSubmissionConn) LocalAddr() net.Addr { return &net.UDPAddr{} }

func (*candidateSubmissionConn) capabilities() connCapabilities {
	return connCapabilities{GSO: true}
}

func (c *candidateSubmissionConn) WritePacket(p []byte, _ net.Addr, _ []byte, gsoSize uint16, _ protocol.ECN) (int, error) {
	n := len(c.submissions)
	c.submissions = append(c.submissions, string(p))
	err := c.fail(n, gsoSize)
	c.after(n)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Queued work that makes several socket submissions is authorized before each
// one: a transition during a GSO fallback or a first-send permission retry
// stops the remainder, and the entry ends as a fenced discard.
func TestCandidateWorkSubmissionAuthority(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GSO fallback and the permission retry only exist on Linux")
	}
	gsoFails := func(_ int, gsoSize uint16) error {
		if gsoSize != 0 {
			return errGSO
		}
		return nil
	}
	for _, tc := range []struct {
		name       string
		gsoSize    uint16
		fail       func(int, uint16) error
		fenceAfter int // submission index; -1 keeps the group unfenced
		want       []string
	}{
		{"unfenced GSO fallback", 4, gsoFails, -1, []string{"foobar", "foob", "ar"}},
		{"GSO fallback fenced after its first segment", 4, gsoFails, 1, []string{"foobar", "foob"}},
		{"permission retry fenced", 0, func(n int, _ uint16) error {
			if n == 0 {
				return errNotPermitted
			}
			return nil
		}, 0, []string{"foobar"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				authority := &connAuthority{}
				raw := &candidateSubmissionConn{fail: tc.fail, after: func(n int) {
					if n == tc.fenceAfter {
						authority.state.Store(authorityClosing)
					}
				}}
				q := newSendQueue(newSendConn(raw, &net.UDPAddr{}, packetInfo{}, utils.DefaultLogger), nil).(*sendQueue)
				q.authority = authority
				buf := getPacketWithContents([]byte("foobar"))
				q.Send(buf, tc.gsoSize, protocol.ECNCE, sendMetadata{})
				startAndFinishQueue(t, q, nil) // a fenced entry is not a write error
				require.Equal(t, tc.want, raw.submissions)
				require.Zero(t, buf.refCount, "the fenced entry still releases its storage")
			})
		})
	}
}

// A loser's close is authorized with close-only authority before each
// submission, including the first-send permission retry: fencing keeps that
// authority, revocation during the first attempt stops the retry.
func TestCandidateCloseSubmissionAuthority(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the permission retry only exists on Linux")
	}
	for _, row := range []struct {
		name       string
		transition uint32
		submitted  int
		wantErr    error
	}{
		{"fenced loser retries", authorityClosing, 2, nil},
		{"revoked during the first attempt", authorityRevoked, 1, errSubmissionDenied},
	} {
		t.Run(row.name, func(t *testing.T) {
			tc := newEmissionTestConnection(t, false)
			c := tc.conn
			sealing := c.emission.packer.cryptoSetup.(*MockSealingManager)
			sealing.EXPECT().GetInitialSealer().Return(nil, handshake.ErrKeysDropped)
			sealing.EXPECT().GetHandshakeSealer().Return(nil, handshake.ErrKeysDropped)
			authority := &connAuthority{}
			authority.state.Store(authorityClosing) // a fenced loser
			c.emission.bindAuthority(authority)
			raw := &candidateSubmissionConn{
				fail: func(n int, _ uint16) error {
					if n == 0 {
						return errNotPermitted
					}
					return nil
				},
				after: func(n int) {
					if n == 0 {
						authority.state.Store(row.transition)
					}
				},
			}
			*c.emission.conn = newSendConn(raw, &net.UDPAddr{}, packetInfo{}, utils.DefaultLogger)
			_, err := c.emission.close(&qerr.ApplicationError{})
			if row.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, row.wantErr)
			}
			require.Len(t, raw.submissions, row.submitted)
		})
	}
}

// fencingConnIDGenerator runs a group transition while a response that uses
// it is being prepared.
type fencingConnIDGenerator struct {
	protocol.DefaultConnectionIDGenerator
	transition func()
}

func (g *fencingConnIDGenerator) GenerateConnectionID() (ConnectionID, error) {
	g.transition()
	return g.DefaultConnectionIDGenerator.GenerateConnectionID()
}

// A stateless response dequeued before its group was fenced is checked again
// immediately before its write: preparation may run, but nothing reaches the
// socket and the received packet's storage is still released.
func TestCandidateFencedResponseSubmission(t *testing.T) {
	newGroup := func() *candidateGroup {
		return &candidateGroup{op: make(chan struct{}, 1), candidates: make(map[*connAuthority]struct{})}
	}

	t.Run("Retry fenced during preparation", func(t *testing.T) {
		g := newGroup()
		var transitions int
		s := newServerAdmissionLifetimeFixture()
		c := newQueueLifetimeConn()
		s.conn = c
		s.tr = (*packetHandlerMap)(&Transport{candidates: g})
		s.connIDGenerator = &fencingConnIDGenerator{transition: func() {
			transitions++
			require.NoError(t, g.closeGroup(t.Context()))
		}}
		s.tokenGenerator = handshake.NewTokenGenerator(TokenGeneratorKey{})
		p := serverAdmissionLifetimePacket()
		s.sendRetry(rejectedPacket{receivedPacket: p, hdr: &wire.Header{
			Type: protocol.PacketTypeInitial, Version: protocol.Version1,
			SrcConnectionID:  protocol.ParseConnectionID([]byte{1}),
			DestConnectionID: protocol.ParseConnectionID([]byte{2}),
		}})
		require.Equal(t, 1, transitions, "the Retry was prepared")
		require.Zero(t, c.writes, "a Retry fenced during preparation must not be written")
		require.Zero(t, p.buffer.refCount)
	})

	t.Run("stateless reset fenced after dequeue", func(t *testing.T) {
		g := newGroup()
		c := newQueueLifetimeConn()
		tr := &Transport{
			conn:              c,
			connIDLen:         4,
			statelessResetter: newStatelessResetter(&StatelessResetKey{}),
			logger:            utils.DefaultLogger,
			candidates:        g,
		}
		require.NoError(t, g.closeGroup(t.Context()))
		p := serverAdmissionLifetimePacket()
		tr.sendStatelessReset(p)
		require.Zero(t, c.writes, "a fenced stateless reset must not be written")
		require.Zero(t, p.buffer.refCount)
	})
}
