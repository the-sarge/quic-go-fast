//go:build picoquic && linux && amd64

package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

type matrixSession struct {
	t       *testing.T
	ctx     context.Context
	conn    *Conn
	peer    *smokePeer
	trace   *matrixTrace
	dir     string
	role    string
	remote  *net.UDPAddr
	primary *Transport
}

func matrixTransport(t *testing.T, lastIPByte byte, trace *matrixTrace) *Transport {
	t.Helper()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, lastIPByte)})
	require.NoError(t, err)
	tr := &Transport{Conn: socket, ConnectionIDLength: 8}
	if trace != nil {
		tr.Tracer = trace
	}
	require.NoError(t, tr.ConfigureMultipathIOV1(100*time.Millisecond))
	t.Cleanup(func() {
		require.NoError(t, tr.Close())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, tr.WaitMultipathIOStoppedV1(ctx))
		if err := socket.Close(); err != nil {
			require.ErrorIs(t, err, net.ErrClosed)
		}
	})
	return tr
}

func newMatrixSession(t *testing.T, role string, negotiated, cidReceipt bool) *matrixSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	root, binary := os.Getenv("PICOQUIC_LOG_DIR"), os.Getenv("PICOQUIC_BINARY")
	require.NotEmpty(t, root)
	require.NotEmpty(t, binary)
	dir, err := os.MkdirTemp(root, "matrix-"+role+"-") //nolint:usetesting // Peer evidence survives the test process.
	require.NoError(t, err)
	roots, leaf := writeSmokeCertificates(t, dir)
	trace := newMatrixTrace()
	if cidReceipt {
		trace.cidReceipt = &matrixCIDReceipt{}
	}
	s := &matrixSession{t: t, ctx: ctx, role: role, dir: dir, trace: trace, primary: matrixTransport(t, 1, trace)}
	conf := &Config{Versions: []Version{Version1}, EnableDatagrams: true, MaxIdleTimeout: 10 * time.Second, Tracer: s.trace.tracer}
	require.NoError(t, conf.ConfigureMultipathV1(true, 2, "primary-backup"))
	cert, key, ca := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"), filepath.Join(dir, "ca.pem")
	mp := "1"
	if !negotiated {
		mp = "0"
	}
	if role == "client" {
		reserved, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		require.NoError(t, err)
		s.remote = reserved.LocalAddr().(*net.UDPAddr)
		require.NoError(t, reserved.Close())
		s.peer = startSmokePeer(t, ctx, binary, dir, "server", strconv.Itoa(s.remote.Port), "0", cert, key, ca, mp)
		s.awaitReady()
		s.conn, err = s.primary.Dial(ctx, s.remote, &tls.Config{ServerName: "localhost", RootCAs: roots, NextProtos: []string{"qgf-r17"}}, conf)
		require.NoError(t, err)
	} else {
		listener, err := s.primary.Listen(&tls.Config{Certificates: []tls.Certificate{leaf}, NextProtos: []string{"qgf-r17"}}, conf)
		require.NoError(t, err)
		t.Cleanup(func() { listener.Close() })
		port := s.primary.Conn.LocalAddr().(*net.UDPAddr).Port
		s.peer = startSmokePeer(t, ctx, binary, dir, "client", "0", strconv.Itoa(port), cert, key, ca, mp)
		s.conn, err = listener.Accept(ctx)
		require.NoError(t, err)
	}
	t.Cleanup(func() { s.conn.CloseWithError(0, "matrix cleanup") })
	ok, err := s.conn.MultipathNegotiatedV1()
	require.NoError(t, err)
	require.Equal(t, negotiated, ok)
	return s
}

func (s *matrixSession) awaitReady() {
	s.t.Helper()
	select {
	case <-s.peer.ready:
	case <-s.peer.done:
		s.t.Fatal("pinned peer exited before readiness; see peer.log")
	case <-s.ctx.Done():
		s.t.Fatal(s.ctx.Err())
	}
}

func (s *matrixSession) exchange(op byte, id byte, payload []byte) []byte {
	s.t.Helper()
	stream, err := s.conn.OpenStreamSync(s.ctx)
	require.NoError(s.t, err)
	deadline, _ := s.ctx.Deadline()
	require.NoError(s.t, stream.SetDeadline(deadline))
	_, err = stream.Write(append([]byte{op, id}, payload...))
	require.NoError(s.t, err)
	require.NoError(s.t, stream.Close())
	got, err := io.ReadAll(stream)
	require.NoError(s.t, err)
	return got
}

func (s *matrixSession) command(op byte, id byte) {
	s.t.Helper()
	require.Equal(s.t, []byte("OK"), s.exchange(op, id, nil))
}

func (s *matrixSession) echo(id byte) {
	s.t.Helper()
	sentBefore, receivedBefore := s.trace.traffic(uint64(id))
	payload := bytes.Repeat([]byte("R17 fixed useful STREAM\n"), 1024)
	require.Equal(s.t, payload, s.exchange('E', id, payload))
	if id != 255 {
		// Await recorder-visible STREAM progress on the selected path. The
		// scenario separately checks its observed silence window.
		require.NoError(s.t, s.trace.wait(s.ctx, func() bool {
			sent, received := s.trace.traffic(uint64(id))
			return sent.Stream > sentBefore.Stream && received.Stream > receivedBefore.Stream
		}))
	}
}

func (s *matrixSession) wait(want func() bool) {
	s.t.Helper()
	for !want() {
		select {
		case <-s.conn.PathChangesV1():
		case <-s.trace.changed:
		case <-s.ctx.Done():
			s.t.Fatal(s.ctx.Err())
		}
	}
}

func (s *matrixSession) path(id uint32) *Path {
	s.t.Helper()
	var found *Path
	s.wait(func() bool {
		for _, path := range s.conn.PathsV1() {
			state, err := path.StateV1()
			require.NoError(s.t, err)
			if state.WireID == id && state.Validated && state.Lifecycle == "live" {
				found = path
				return true
			}
		}
		return false
	})
	return found
}

func (s *matrixSession) open(id uint32) *Path {
	s.t.Helper()
	if s.role == "client" {
		path, err := s.conn.OpenPathV1(s.ctx, matrixTransport(s.t, byte(id)+1, s.trace), s.remote)
		require.NoError(s.t, err)
		got, err := path.PathIDV1()
		require.NoError(s.t, err)
		require.Equal(s.t, id, got)
	} else {
		s.command('O', 0)
	}
	return s.path(id)
}

// Only the independent-server replacement example conditions CID receipt
// before delivering the new tuple. It makes no arbitrary-reordering claim.
func (s *matrixSession) openReplacementWithCIDReceipt() {
	s.t.Helper()
	transport := matrixTransport(s.t, 3, s.trace)
	local := transport.Conn.LocalAddr().(*net.UDPAddr)
	payload := append([]byte(nil), local.IP.To4()...)
	payload = binary.BigEndian.AppendUint16(payload, uint16(local.Port))
	require.Equal(s.t, []byte("OK"), s.exchange('G', 2, payload))
	ctx, cancel := context.WithCancelCause(s.ctx)
	type openResult struct {
		path *Path
		err  error
	}
	result := make(chan openResult, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		path, err := s.conn.OpenPathV1(ctx, transport, s.remote)
		if err != nil {
			cancel(err)
		}
		result <- openResult{path: path, err: err}
	}()
	var opened openResult
	joined := false
	defer func() {
		cancel(nil)
		if !joined {
			opened = <-result
		}
		<-stopped
		s.t.Logf("replacement V1 caller joined: %v", opened.err)
	}()
	for {
		_, ready, err := s.trace.cidReceiptStatus()
		require.NoError(s.t, err)
		if ready {
			break
		}
		select {
		case opened = <-result:
			joined = true
			require.NoError(s.t, opened.err)
		case <-s.trace.changed:
		case <-ctx.Done():
			require.NoError(s.t, context.Cause(ctx))
		}
	}
	require.NoError(s.t, context.Cause(ctx))
	witness, ready, err := s.trace.cidReceiptStatus()
	require.NoError(s.t, err)
	require.True(s.t, ready)
	s.t.Logf("CID2 processing receipt: path-zero 1-RTT packet %d sequence %d", witness.PacketNumber, witness.Sequence)
	response, err := s.releaseTupleGate(ctx)
	require.NoError(s.t, err)
	require.Equal(s.t, []byte("OK"), response)
	if !joined {
		opened = <-result
		joined = true
	}
	require.NoError(s.t, opened.err)
	id, err := opened.path.PathIDV1()
	require.NoError(s.t, err)
	require.Equal(s.t, uint32(2), id)
	s.path(2)
}

func (s *matrixSession) releaseTupleGate(ctx context.Context) ([]byte, error) {
	stream, err := s.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		stream.CancelRead(0)
		stream.CancelWrite(0)
		close(canceled)
	})
	defer func() {
		if !stop() {
			<-canceled
		}
	}()
	deadline, _ := ctx.Deadline()
	if err := stream.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err := stream.Write([]byte{'g', 2}); err != nil {
		return nil, err
	}
	if err := stream.Close(); err != nil {
		return nil, err
	}
	return io.ReadAll(stream)
}

func (s *matrixSession) prefer(id uint32) {
	s.t.Helper()
	require.NoError(s.t, s.path(id).SetLocalUseV1("active"))
	for _, path := range s.conn.PathsV1() {
		state, err := path.StateV1()
		require.NoError(s.t, err)
		if state.WireID != id && state.Lifecycle == "live" {
			require.NoError(s.t, path.SetLocalUseV1("backup"))
		}
	}
}

func (s *matrixSession) datagram(id byte) {
	s.t.Helper()
	payload := bytes.Repeat([]byte{id, 'R', '1', '7'}, 64)
	require.NoError(s.t, s.conn.SendDatagram(payload))
	got, err := s.conn.ReceiveDatagram(s.ctx)
	require.NoError(s.t, err)
	require.Equal(s.t, payload, got)
}

func (s *matrixSession) mtuComplete(id protocol.PathID) bool {
	s.t.Helper()
	var complete bool
	err := s.conn.runPathCommand(func(registry *pathCreditRegistry, _ monotime.Time) error {
		var finder *mtuFinder
		if id == 0 {
			finder = s.conn.mtuDiscoverer
		} else if member := registry.members[id]; member != nil {
			finder = member.route.mtu
		}
		_, complete = matrixMTUState(finder)
		return nil
	})
	require.NoError(s.t, err)
	return complete
}

func (s *matrixSession) finish() {
	s.t.Helper()
	require.NoError(s.t, s.conn.CloseWithError(0, "R17 case complete"))
	s.peer.wait(s.t)
	s.t.Logf("retained peer evidence: %s", s.dir)
	for _, id := range []uint64{0, 1, 2} {
		sent, received := s.trace.traffic(id)
		s.t.Logf("path %d sent %+v received %+v", id, sent, received)
	}
}

func TestPicoquicMultipathMatrix(t *testing.T) {
	activateMultipathProfile(t)
	cases := []struct {
		name string
		run  func(*matrixSession)
	}{
		{"useful-transfer", matrixUsefulTransfer},
		{"absent-negotiation", matrixAbsentNegotiation},
		{"path-zero-ack", matrixPathZeroACK},
		{"key-update-silent-path", matrixKeyUpdate},
		{"status-backup-failover", matrixStatusFailover},
		{"abandon-reopen-late-feedback", matrixAbandonReopen},
		{"same-id-migration-rebinding", matrixMigration},
		{"unequal-mtu-invalid-frame", matrixMTUInvalid},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for _, role := range []string{"client", "server"} {
				t.Run(role, func(t *testing.T) {
					s := newMatrixSession(t, role, test.name != "absent-negotiation", test.name == "abandon-reopen-late-feedback" && role == "client")
					test.run(s)
					s.finish()
				})
			}
		})
	}
}

func matrixUsefulTransfer(s *matrixSession) {
	s.echo(0)
	s.datagram(0)
	s.open(1)
	s.prefer(1)
	s.echo(1)
	s.datagram(1)
	require.NoError(s.t, s.trace.wait(s.ctx, s.trace.usefulTrafficReady))
	for _, id := range []uint64{0, 1} {
		sent, received := s.trace.traffic(id)
		require.NotZero(s.t, sent.Stream, fmt.Sprintf("path %d sent useful STREAM", id))
		require.NotZero(s.t, received.Stream, fmt.Sprintf("path %d received useful STREAM", id))
		require.NotZero(s.t, sent.Datagram, fmt.Sprintf("path %d sent useful DATAGRAM", id))
		require.NotZero(s.t, received.Datagram, fmt.Sprintf("path %d received useful DATAGRAM", id))
	}
}

func matrixAbsentNegotiation(s *matrixSession) {
	require.Empty(s.t, s.conn.PathsV1())
	_, err := s.conn.OpenPathV1(s.ctx, s.primary, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1})
	require.ErrorIs(s.t, err, ErrMultipathNotNegotiatedV1)
	s.echo(255) // ordinary peer: no path-affinity API is requested
	s.datagram(0)
}

func matrixPathZeroACK(s *matrixSession) {
	s.echo(0)
	s.command('Z', 0)
	s.wait(func() bool {
		s.trace.mu.Lock()
		defer s.trace.mu.Unlock()
		return s.trace.ackZero[0] > 0 && s.trace.ackZero[1] > 0
	})
	s.echo(0)
	s.trace.mu.Lock()
	s.t.Logf("received 1-RTT ACK(0)/PATH_ACK(0): %v", s.trace.ackZero)
	s.trace.mu.Unlock()
}

func matrixKeyUpdate(s *matrixSession) {
	s.echo(0)
	s.open(1)
	s.prefer(1)
	s.echo(1)
	s.prefer(0)
	s.command('B', 1)
	require.NoError(s.t, s.path(1).AdvertiseStatusV1("backup"))
	s.echo(0)
	sentBefore, receivedBefore := s.trace.traffic(1)
	// Successful K applies fresh keys before its same-stream OK is protected.
	// That response and E0/E1 progress witness rotation even with equal phase bits.
	s.command('K', 0)
	s.echo(0)
	sentAfter, receivedAfter := s.trace.traffic(1)
	require.Equal(s.t, sentBefore.Packets, sentAfter.Packets, "path one remained silent during the update")
	require.Equal(s.t, receivedBefore.Packets, receivedAfter.Packets, "the peer also kept path one silent")
	s.command('V', 1)
	require.NoError(s.t, s.path(1).AdvertiseStatusV1("available"))
	s.prefer(1)
	s.echo(1)
	s.t.Log("pinned key rotation: protected request response, path-zero progress and path-one resumption complete")
}

func matrixStatusFailover(s *matrixSession) {
	s.echo(0)
	s.open(1)
	backup := s.path(1)
	require.NoError(s.t, backup.SetLocalUseV1("backup"))
	require.NoError(s.t, backup.AdvertiseStatusV1("backup"))
	s.command('B', 1)
	s.wait(func() bool {
		state, err := backup.StateV1()
		require.NoError(s.t, err)
		return state.PeerPreference == "backup"
	})
	s.echo(0)
	before, _ := s.trace.traffic(1)
	require.Zero(s.t, before.Stream, "the backup was not used for the ordinary transfer")
	require.NoError(s.t, backup.AdvertiseStatusV1("available"))
	s.command('V', 1)
	s.wait(func() bool {
		state, err := backup.StateV1()
		require.NoError(s.t, err)
		return state.PeerPreference == "available"
	})
	require.NoError(s.t, s.path(0).AdvertiseStatusV1("backup"))
	zeroBefore, err := s.path(0).StateV1()
	require.NoError(s.t, err)
	backupBefore, err := backup.StateV1()
	require.NoError(s.t, err)
	require.True(s.t, zeroBefore.Primary, "path zero is still primary immediately before failure")
	require.False(s.t, backupBefore.Primary)
	require.Equal(s.t, "backup", backupBefore.LocalUse)
	if s.role == "client" {
		require.NoError(s.t, s.primary.Conn.Close(), "real local endpoint failure")
	} else {
		s.command('F', 0) // opaque packet loss on the independent client's original path
	}
	s.echo(1)
	s.datagram(1)
	state, err := backup.StateV1()
	require.NoError(s.t, err)
	require.True(s.t, state.Primary)
	require.NoError(s.t, s.conn.Context().Err())
}

func matrixAbandonReopen(s *matrixSession) {
	s.echo(0)
	old := s.open(1)
	s.prefer(1)
	s.command('H', 1)
	s.echo(1) // one encrypted peer packet is held; recovery completes the stream
	held := s.exchange('Q', 1, nil)
	require.Len(s.t, held, 2)
	heldSize := int(binary.BigEndian.Uint16(held))
	require.Positive(s.t, heldSize, "the peer retained one opaque datagram")
	s.prefer(0)
	require.NoError(s.t, old.AbandonV1(0))
	if s.role == "client" {
		s.openReplacementWithCIDReceipt()
	} else {
		s.open(2)
	}
	s.prefer(2)
	s.echo(2)
	before := s.trace.replayEvidence(heldSize)
	acksBefore := s.trace.ackCount(1)
	s.command('R', 1)
	s.wait(func() bool {
		return s.trace.replayDisposition(heldSize, before) != "" && s.trace.ackCount(1) > acksBefore
	})
	s.t.Logf("replayed %d-byte packet disposition: %s; retired PATH_ACK(1) observed", heldSize, s.trace.replayDisposition(heldSize, before))
	s.echo(2)
	state, err := old.StateV1()
	require.NoError(s.t, err)
	require.Contains(s.t, []string{"retiring", "retained", "closed"}, state.Lifecycle)
}

func matrixMigration(s *matrixSession) {
	s.echo(0)
	path := s.open(1)
	s.prefer(1)
	s.echo(1)
	before, err := path.StateV1()
	require.NoError(s.t, err)
	sentBefore, _ := s.trace.traffic(1)
	if s.role == "client" {
		target := matrixTransport(s.t, 3, s.trace)
		require.NoError(s.t, path.MigrateV1(s.ctx, target))
	} else {
		s.command('N', 1)
	}
	s.echo(1)
	s.wait(func() bool {
		after, err := path.StateV1()
		require.NoError(s.t, err)
		return after.TupleGeneration > before.TupleGeneration
	})
	after, err := path.StateV1()
	require.NoError(s.t, err)
	require.Equal(s.t, before.WireID, after.WireID)
	require.Len(s.t, s.conn.PathsV1(), 2, "same-ID replacement creates no third protocol path")
	sentAfter, _ := s.trace.traffic(1)
	require.Greater(s.t, sentAfter.LastPN, sentBefore.LastPN, "the path packet-number space continues")
}

func matrixMTUInvalid(s *matrixSession) {
	s.echo(0)
	s.command('M', 1)
	s.open(1)
	s.prefer(1)
	s.echo(1)
	s.datagram(1)
	// The selected path owns the discovery timer; an idle backup's timer is
	// suspended. Finish each path's discovery while it is selected.
	s.wait(func() bool { return s.mtuComplete(1) })
	s.prefer(0)
	s.echo(0)
	s.wait(func() bool { return s.mtuComplete(0) })
	zero, err := s.path(0).StateV1()
	require.NoError(s.t, err)
	one, err := s.path(1).StateV1()
	require.NoError(s.t, err)
	require.Greater(s.t, zero.PMTU, uint32(1280))
	require.LessOrEqual(s.t, one.PMTU, uint32(1280))
	s.t.Logf("qualified unequal UDP payload limits: %d / %d", zero.PMTU, one.PMTU)
	stream, err := s.conn.OpenStreamSync(s.ctx)
	require.NoError(s.t, err)
	deadline, _ := s.ctx.Deadline()
	require.NoError(s.t, stream.SetDeadline(deadline))
	_, err = stream.Write([]byte{'I', 0})
	require.NoError(s.t, err)
	require.NoError(s.t, stream.Close())
	select {
	case <-s.conn.Context().Done():
	case <-s.ctx.Done():
		s.t.Fatal(s.ctx.Err())
	}
	var transportError *TransportError
	require.True(s.t, errors.As(context.Cause(s.conn.Context()), &transportError))
	require.Equal(s.t, TransportErrorCode(0xa), transportError.ErrorCode)
}

// These literal draft-21 vectors cover the pin's documented zero oracle gap.
// They are normative parser evidence, not an independent-peer matrix instance.
func TestMultipathNegotiatedZeroVectors(t *testing.T) {
	for _, perspective := range []protocol.Perspective{protocol.PerspectiveClient, protocol.PerspectiveServer} {
		for _, test := range []struct {
			name    string
			encoded []byte
			present bool
			maximum protocol.PathID
		}{
			{"absent", nil, false, 0},
			{"zero", []byte{0x3e, 1, 0}, true, 0},
			{"one", []byte{0x3e, 1, 1}, true, 1},
		} {
			t.Run(fmt.Sprintf("%s/%s", perspective, test.name), func(t *testing.T) {
				// initial_source_connection_id is mandatory in both roles;
				// original_destination_connection_id is also mandatory for a server.
				parameters := []byte{0x0f, 1, 1}
				if perspective == protocol.PerspectiveServer {
					parameters = append(parameters, 0, 1, 2)
				}
				parameters = append(parameters, test.encoded...)
				var decoded wire.TransportParameters
				require.NoError(t, decoded.Unmarshal(parameters, perspective))
				if !test.present {
					require.Nil(t, decoded.InitialMaxPathID)
				} else {
					require.NotNil(t, decoded.InitialMaxPathID)
					require.Equal(t, test.maximum, *decoded.InitialMaxPathID)
				}
			})
		}
	}
	for _, vector := range [][]byte{{0x02, 0, 0, 0, 0}, {0x3e, 0, 0, 0, 0, 0}} {
		parser := wire.NewFrameParser(false, false, false)
		parser.EnableMultipath()
		ack, consumed, err := parser.ParseAckFrame(wire.FrameType(vector[0]), vector[1:], protocol.Encryption1RTT, protocol.Version1)
		require.NoError(t, err)
		require.Equal(t, len(vector)-1, consumed)
		require.Equal(t, protocol.PathID(0), ack.PathID)
		require.Equal(t, vector[0] == 0x3e, ack.HasPathID)
		require.Equal(t, protocol.PacketNumber(0), ack.LargestAcked())
	}
}

// This controlled self-to-self exchange exercises the real V1 constructors with
// initial_max_path_id present and zero. It makes no independent-interop claim.
func TestMultipathNegotiatedZeroControlledPeer(t *testing.T) {
	activateMultipathProfile(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	clientTr, serverTr := matrixTransport(t, 1, nil), matrixTransport(t, 1, nil)
	conf := &Config{Versions: []Version{Version1}}
	require.NoError(t, conf.ConfigureMultipathV1(true, 1, "primary-backup"))
	listener, err := serverTr.Listen(nativeServerTLS(), conf)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	client, err := clientTr.Dial(ctx, serverTr.Conn.LocalAddr(), nativeClientTLS(), conf)
	require.NoError(t, err)
	t.Cleanup(func() { client.CloseWithError(0, "") })
	server, err := listener.Accept(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { server.CloseWithError(0, "") })
	for _, conn := range []*Conn{client, server} {
		negotiated, err := conn.MultipathNegotiatedV1()
		require.NoError(t, err)
		require.True(t, negotiated)
		paths := conn.PathsV1()
		require.Len(t, paths, 1)
		id, err := paths[0].PathIDV1()
		require.NoError(t, err)
		require.Zero(t, id)
	}
	stream, err := client.OpenStreamSync(ctx)
	require.NoError(t, err)
	deadline, _ := ctx.Deadline()
	require.NoError(t, stream.SetDeadline(deadline))
	_, err = stream.Write([]byte("negotiated zero"))
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	incoming, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	require.NoError(t, incoming.SetDeadline(deadline))
	payload, err := io.ReadAll(incoming)
	require.NoError(t, err)
	require.Equal(t, []byte("negotiated zero"), payload)
	_, err = incoming.Write([]byte("zero is present"))
	require.NoError(t, err)
	require.NoError(t, incoming.Close())
	payload, err = io.ReadAll(stream)
	require.NoError(t, err)
	require.Equal(t, []byte("zero is present"), payload)
}
