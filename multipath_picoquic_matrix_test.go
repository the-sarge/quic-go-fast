//go:build picoquic && linux && amd64

package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
)

type matrixPathTraffic struct {
	Stream, Datagram, Packets uint64
	LastPN                    int64
}

// The existing qlog event owner supplies decoded identities. This recorder
// observes those events; it does not parse or infer QUIC from packet bytes.
type matrixTrace struct {
	mu             sync.Mutex
	sent, received map[uint64]matrixPathTraffic
	ackZero        [2]uint64 // ordinary ACK, explicit PATH_ACK(0), received in 1-RTT
	keyUpdates     uint64
	acks           map[uint64]uint64
	mtuDone        map[uint64]bool
	changed        chan struct{}
}

func newMatrixTrace() *matrixTrace {
	return &matrixTrace{sent: make(map[uint64]matrixPathTraffic), received: make(map[uint64]matrixPathTraffic), acks: make(map[uint64]uint64), mtuDone: make(map[uint64]bool), changed: make(chan struct{}, 1)}
}

func (m *matrixTrace) AddProducer() qlogwriter.Recorder { return m }
func (m *matrixTrace) SupportsSchemas(string) bool      { return true }
func (m *matrixTrace) Close() error                     { return nil }
func (m *matrixTrace) tracer(context.Context, bool, ConnectionID) qlogwriter.Trace {
	return m
}

func (m *matrixTrace) RecordEvent(event qlogwriter.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var path *qlog.PathIdentity
	var header qlog.PacketHeader
	var frames []qlog.Frame
	target := m.sent
	switch e := event.(type) {
	case qlog.PacketSent:
		path, header, frames = e.Path, e.Header, e.Frames
	case qlog.PacketReceived:
		path, header, frames, target = e.Path, e.Header, e.Frames, m.received
		if header.PacketType == qlog.PacketType1RTT {
			for _, frame := range frames {
				if ack, ok := frame.Frame.(*qlog.AckFrame); ok {
					m.acks[uint64(ack.PathID)]++
					if ack.PathID != 0 {
						continue
					}
					if ack.HasPathID {
						m.ackZero[1]++
					} else {
						m.ackZero[0]++
					}
				}
			}
		}
	case qlog.KeyUpdated:
		if e.KeyPhase > 0 {
			m.keyUpdates++
		}
	case qlog.MTUUpdated:
		if e.Path != nil && e.Done {
			m.mtuDone[e.Path.PathID] = true
		}
	}
	if path != nil && header.PacketType == qlog.PacketType1RTT {
		traffic := target[path.PathID]
		traffic.Packets++
		traffic.LastPN = int64(header.PacketNumber)
		for _, frame := range frames {
			switch f := frame.Frame.(type) {
			case *qlog.StreamFrame:
				traffic.Stream += uint64(f.Length)
			case *qlog.DatagramFrame:
				traffic.Datagram += uint64(f.Length)
			}
		}
		target[path.PathID] = traffic
	}
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

func (m *matrixTrace) traffic(id uint64) (matrixPathTraffic, matrixPathTraffic) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sent[id], m.received[id]
}

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

func matrixTransport(t *testing.T, lastIPByte byte) *Transport {
	t.Helper()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, lastIPByte)})
	require.NoError(t, err)
	tr := &Transport{Conn: socket, ConnectionIDLength: 8}
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

func newMatrixSession(t *testing.T, role string, negotiated bool) *matrixSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	root, binary := os.Getenv("PICOQUIC_LOG_DIR"), os.Getenv("PICOQUIC_BINARY")
	require.NotEmpty(t, root)
	require.NotEmpty(t, binary)
	dir, err := os.MkdirTemp(root, "matrix-"+role+"-") //nolint:usetesting // Peer evidence survives the test process.
	require.NoError(t, err)
	roots, leaf := writeSmokeCertificates(t, dir)
	s := &matrixSession{t: t, ctx: ctx, role: role, dir: dir, trace: newMatrixTrace(), primary: matrixTransport(t, 1)}
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
	payload := bytes.Repeat([]byte("R17 fixed useful STREAM\n"), 1024)
	require.Equal(s.t, payload, s.exchange('E', id, payload))
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
		path, err := s.conn.OpenPathV1(s.ctx, matrixTransport(s.t, byte(id)+1), s.remote)
		require.NoError(s.t, err)
		got, err := path.PathIDV1()
		require.NoError(s.t, err)
		require.Equal(s.t, id, got)
	} else {
		s.command('O', 0)
	}
	return s.path(id)
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
					s := newMatrixSession(t, role, test.name != "absent-negotiation")
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
	s.trace.mu.Lock()
	keysBefore := s.trace.keyUpdates
	s.trace.mu.Unlock()
	s.command('K', 0)
	s.echo(0)
	s.wait(func() bool {
		s.trace.mu.Lock()
		defer s.trace.mu.Unlock()
		return s.trace.keyUpdates > keysBefore
	})
	sentAfter, receivedAfter := s.trace.traffic(1)
	require.Equal(s.t, sentBefore.Packets, sentAfter.Packets, "path one remained silent during the update")
	require.Equal(s.t, receivedBefore.Packets, receivedAfter.Packets, "the peer also kept path one silent")
	s.command('V', 1)
	require.NoError(s.t, s.path(1).AdvertiseStatusV1("available"))
	s.prefer(1)
	s.echo(1)
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
	require.NoError(s.t, s.path(0).AdvertiseStatusV1("backup"))
	s.command('B', 0)
	s.prefer(1)
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
	s.prefer(0)
	require.NoError(s.t, old.AbandonV1(0))
	s.open(2)
	s.prefer(2)
	s.echo(2)
	_, before := s.trace.traffic(1)
	s.command('R', 1)
	s.wait(func() bool { _, after := s.trace.traffic(1); return after.Packets > before.Packets })
	s.echo(2)
	state, err := old.StateV1()
	require.NoError(s.t, err)
	require.Contains(s.t, []string{"retiring", "retained", "closed"}, state.Lifecycle)
	log, err := os.ReadFile(filepath.Join(s.dir, "peer.log"))
	require.NoError(s.t, err)
	require.Contains(s.t, string(log), "held path-one packet")
	require.Contains(s.t, string(log), "replayed path-one packet")
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
		target := matrixTransport(s.t, 3)
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
	s.wait(func() bool {
		s.trace.mu.Lock()
		defer s.trace.mu.Unlock()
		return s.trace.mtuDone[1]
	})
	s.prefer(0)
	s.echo(0)
	s.wait(func() bool {
		s.trace.mu.Lock()
		defer s.trace.mu.Unlock()
		return s.trace.mtuDone[0]
	})
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
	clientTr, serverTr := matrixTransport(t, 1), matrixTransport(t, 1)
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
