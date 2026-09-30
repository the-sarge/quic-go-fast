package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/wire"

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

// dialCandidatePair establishes one connection from an ordinary client transport.
func dialCandidatePair(t *testing.T, ctx context.Context, ln *Listener) (client, server *Conn) {
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
	var retained []*connAuthority
	tr.mutex.Lock()
	for _, id := range earlierIDs {
		if h, ok := tr.handlers[id]; ok {
			retained = append(retained, handlerAuthority(h))
		}
	}
	tr.mutex.Unlock()
	require.NotEmpty(t, retained)
	for _, a := range retained {
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
