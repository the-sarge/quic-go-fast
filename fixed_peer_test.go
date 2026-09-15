package quic

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

type fixedPeerV1 interface{ ConfigureFixedPeerV1(*net.UDPAddr) error }

func configureFixedPeer(t *testing.T, tr *Transport, peer *net.UDPAddr) error {
	t.Helper()
	api, ok := any(tr).(fixedPeerV1)
	require.True(t, ok, "transport must expose the standard-type fixed-peer extension")
	return api.ConfigureFixedPeerV1(peer)
}

func TestFixedPeerConfiguration(t *testing.T) {
	peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
	t.Run("valid and duplicate", func(t *testing.T) {
		tr := &Transport{Conn: newUDPConnLocalhost(t)}
		require.NoError(t, configureFixedPeer(t, tr, peer))
		require.Error(t, configureFixedPeer(t, tr, peer))
	})
	for _, tc := range []struct {
		name string
		peer *net.UDPAddr
	}{
		{"nil", nil},
		{"missing IP", &net.UDPAddr{Port: 12345}},
		{"malformed IP", &net.UDPAddr{IP: net.IP{1, 2, 3}, Port: 12345}},
		{"unspecified", &net.UDPAddr{IP: net.IPv4zero, Port: 12345}},
		{"zero port", &net.UDPAddr{IP: peer.IP}},
		{"negative port", &net.UDPAddr{IP: peer.IP, Port: -1}},
		{"overflow port", &net.UDPAddr{IP: peer.IP, Port: 65536}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &Transport{Conn: newUDPConnLocalhost(t)}
			require.Error(t, configureFixedPeer(t, tr, tc.peer))
			require.NoError(t, configureFixedPeer(t, tr, peer), "invalid configuration must not claim the slot")
		})
	}
	t.Run("opaque wrapper", func(t *testing.T) {
		tr := &Transport{Conn: &struct{ net.PacketConn }{newUDPConnLocalhost(t)}}
		require.Error(t, configureFixedPeer(t, tr, peer))
	})
	t.Run("typed nil", func(t *testing.T) {
		tr := &Transport{Conn: (*net.UDPConn)(nil)}
		require.Error(t, configureFixedPeer(t, tr, peer))
	})
	for _, operation := range []string{"write", "close"} {
		t.Run("late after "+operation, func(t *testing.T) {
			tr := &Transport{Conn: newUDPConnLocalhost(t)}
			defer tr.Close()
			if operation == "write" {
				_, err := tr.WriteTo([]byte("x"), peer)
				require.NoError(t, err)
			} else {
				require.NoError(t, tr.Close())
			}
			require.Error(t, configureFixedPeer(t, tr, peer))
		})
	}
	t.Run("changed socket", func(t *testing.T) {
		tr := &Transport{Conn: newUDPConnLocalhost(t)}
		require.NoError(t, configureFixedPeer(t, tr, peer))
		tr.Conn = newUDPConnLocalhost(t)
		_, err := tr.WriteTo([]byte("x"), peer)
		require.Error(t, err)
		require.Error(t, tr.Close())
	})
}

func TestFixedPeerWriteToCopiesAddress(t *testing.T) {
	selected, foreign := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	peerCopy := *selected.LocalAddr().(*net.UDPAddr)
	peerCopy.IP = append(net.IP(nil), peerCopy.IP...)
	peer := &peerCopy
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureFixedPeer(t, tr, peer))
	defer tr.Close()
	peer.IP[0] = 192
	peer.Port = foreign.LocalAddr().(*net.UDPAddr).Port
	n, err := tr.WriteTo([]byte("blocked"), foreign.LocalAddr())
	require.Error(t, err)
	require.Zero(t, n)
	n, err = tr.WriteTo([]byte("selected"), selected.LocalAddr())
	require.NoError(t, err)
	require.Equal(t, 8, n)
	require.NoError(t, selected.SetReadDeadline(time.Now().Add(time.Second)))
	b := make([]byte, 64)
	n, _, err = selected.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, "selected", string(b[:n]))
}

type policyWriteSocket struct {
	rawConn
	writes int
}

func (s *policyWriteSocket) WritePacket(b []byte, _ net.Addr, _ []byte, _ uint16, _ protocol.ECN) (int, error) {
	s.writes++
	return len(b), nil
}

func TestFixedPeerAddressAndSegmentAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, selected, observed, zone, observedZone string
		allow                                        bool
	}{
		{name: "IPv4 mapped", selected: "127.0.0.1", observed: "::ffff:127.0.0.1", allow: true},
		{name: "foreign IP", selected: "127.0.0.1", observed: "127.0.0.2"},
		{name: "IPv6", selected: "::1", observed: "::1", allow: true},
		{name: "observed scope", selected: "fe80::1", observed: "fe80::1", observedZone: "en0", allow: true},
		{name: "same scope", selected: "fe80::1", observed: "fe80::1", zone: "en0", observedZone: "en0", allow: true},
		{name: "missing scope", selected: "fe80::1", observed: "fe80::1", zone: "en0"},
		{name: "conflicting scope", selected: "fe80::1", observed: "fe80::1", zone: "en0", observedZone: "en1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &Transport{Conn: newUDPConnLocalhost(t)}
			require.NoError(t, configureFixedPeer(t, tr, &net.UDPAddr{IP: net.ParseIP(tc.selected), Port: 12345, Zone: tc.zone}))
			socket := &policyWriteSocket{}
			tr.policyConn.rawConn = socket
			addr := &net.UDPAddr{IP: net.ParseIP(tc.observed), Port: 12345, Zone: tc.observedZone}
			n, err := tr.policyConn.WritePacket([]byte("aabb"), addr, nil, 2, protocol.ECNUnsupported)
			if tc.allow {
				require.NoError(t, err)
				require.Equal(t, 4, n)
				require.Equal(t, 1, socket.writes)
			} else {
				require.Error(t, err)
				require.Zero(t, n)
				require.Zero(t, socket.writes)
			}
		})
	}
}

func TestFixedPeerReceiveBeforeRouting(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprint("known CID=", known), func(t *testing.T) {
			selected, foreign := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
			tr := &Transport{Conn: newUDPConnLocalhost(t)}
			require.NoError(t, configureFixedPeer(t, tr, selected.LocalAddr().(*net.UDPAddr)))
			require.NoError(t, tr.init(false))
			defer tr.Close()
			var packets chan receivedPacket
			data := []byte("\x00selected")
			if known {
				cid := protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8})
				packets = make(chan receivedPacket, 2)
				require.True(t, (*packetHandlerMap)(tr).Add(cid, &mockPacketHandler{packets: packets}))
				data = getPacket(t, cid)
			} else {
				tr.readingNonQUICPackets.Store(true)
			}
			_, err := foreign.WriteTo(data, tr.Conn.LocalAddr())
			require.NoError(t, err)
			_, err = selected.WriteTo(data, tr.Conn.LocalAddr())
			require.NoError(t, err)
			if known {
				select {
				case p := <-packets:
					defer p.buffer.Release()
					require.Equal(t, selected.LocalAddr(), p.remoteAddr)
				case <-time.After(time.Second):
					t.Fatal("selected packet not delivered")
				}
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				b := make([]byte, 128)
				n, addr, err := tr.ReadNonQUICPacket(ctx, b)
				require.NoError(t, err)
				require.Equal(t, data, b[:n])
				require.Equal(t, selected.LocalAddr(), addr)
			}
		})
	}
}

func TestFixedPeerRejectsAlternatePaths(t *testing.T) {
	for _, fixedOrigin := range []bool{true, false} {
		t.Run(fmt.Sprint("fixed origin=", fixedOrigin), func(t *testing.T) {
			peer := newUDPConnLocalhost(t).LocalAddr().(*net.UDPAddr)
			origin := &Transport{Conn: newUDPConnLocalhost(t)}
			target := &Transport{Conn: newUDPConnLocalhost(t)}
			if fixedOrigin {
				require.NoError(t, configureFixedPeer(t, origin, peer))
			} else {
				require.NoError(t, configureFixedPeer(t, target, peer))
			}
			require.NoError(t, origin.init(false))
			defer origin.Close()
			defer target.Close()
			c := &Conn{conn: newSendConn(origin.conn, peer, packetInfo{}, utils.DefaultLogger), peerParams: &wire.TransportParameters{}}
			c.emission.conn = &c.conn
			c.pathManagerOutgoing.Store(newPathManagerOutgoing(nil, nil, func() {}))
			path, err := c.AddPath(target)
			require.ErrorContains(t, err, "fixed peer")
			require.Nil(t, path, "no handle can be probed or switched")
			require.Nil(t, target.conn, "rejected target must not start I/O")
		})
	}
}

func TestFixedPeerStatelessTraffic(t *testing.T) {
	for _, kind := range []string{"retry", "version negotiation", "reset", "terminal"} {
		t.Run(kind, func(t *testing.T) {
			selected, foreign := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
			tr := &Transport{Conn: newUDPConnLocalhost(t), ConnectionIDLength: 8, StatelessResetKey: &StatelessResetKey{1}}
			require.NoError(t, configureFixedPeer(t, tr, selected.LocalAddr().(*net.UDPAddr)))
			tr.VerifySourceAddress = func(net.Addr) bool { return true }
			_, err := tr.Listen(&tls.Config{}, nil)
			require.NoError(t, err)
			defer tr.Close()
			cid := protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8})
			var data []byte
			switch kind {
			case "retry", "version negotiation":
				version := protocol.Version1
				if kind == "version negotiation" {
					version = 0x42
				}
				p := getLongHeaderPacket(t, selected.LocalAddr(), &wire.ExtendedHeader{Header: wire.Header{Type: protocol.PacketTypeInitial, SrcConnectionID: cid, DestConnectionID: cid, Version: version}, PacketNumberLen: protocol.PacketNumberLen4}, make([]byte, protocol.MinInitialPacketSize))
				defer p.buffer.Release()
				data = p.data
			default:
				data = append([]byte{0x40}, cid.Bytes()...)
				data = append(data, make([]byte, 64)...)
				if kind == "terminal" {
					m := (*packetHandlerMap)(tr)
					require.True(t, m.Add(cid, &mockPacketHandler{}))
					m.ReplaceWithClosed([]protocol.ConnectionID{cid}, []byte("terminal"), time.Second)
				}
			}
			_, err = foreign.WriteTo(data, tr.Conn.LocalAddr())
			require.NoError(t, err)
			_, err = selected.WriteTo(data, tr.Conn.LocalAddr())
			require.NoError(t, err)
			require.NoError(t, selected.SetReadDeadline(time.Now().Add(time.Second)))
			b := make([]byte, 1500)
			n, _, err := selected.ReadFrom(b)
			require.NoError(t, err)
			switch kind {
			case "version negotiation":
				require.True(t, wire.IsVersionNegotiationPacket(b[:n]))
			case "retry":
				hdr, _, _, err := wire.ParsePacket(b[:n])
				require.NoError(t, err)
				require.Equal(t, protocol.PacketTypeRetry, hdr.Type)
			case "reset":
				require.Equal(t, protocol.MinStatelessResetSize, n)
			case "terminal":
				require.Equal(t, "terminal", string(b[:n]))
			}
			require.NoError(t, foreign.SetReadDeadline(time.Now().Add(scaleDuration(20*time.Millisecond))))
			_, _, err = foreign.ReadFrom(b)
			require.Error(t, err)
			var ne net.Error
			require.ErrorAs(t, err, &ne)
			require.True(t, ne.Timeout())
		})
	}
}

func TestFixedPeerRegisteredBatchAndRebinding(t *testing.T) {
	selected, foreign := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	calls := 0
	writer, err := tr.UDPBatchWriterV1(tr.Conn.(*net.UDPConn))
	require.NoError(t, err)
	require.NoError(t, tr.ConfigureExternalPacketIOV1(tr.Conn, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		calls++
		return writer(bufs, oob, addr)
	}))
	require.NoError(t, configureFixedPeer(t, tr, selected.LocalAddr().(*net.UDPAddr)))
	require.NoError(t, tr.init(false))
	defer tr.Close()
	sc := newSendConn(tr.conn, selected.LocalAddr(), packetInfo{}, utils.DefaultLogger)
	n, err := sc.sendBatch([][]byte{[]byte("one"), []byte("two")}, protocol.ECNUnsupported)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, 1, calls)
	sc.ChangeRemoteAddr(foreign.LocalAddr(), packetInfo{})
	n, err = sc.sendBatch([][]byte{[]byte("blocked")}, protocol.ECNUnsupported)
	require.Error(t, err)
	require.Zero(t, n)
	require.Equal(t, 1, calls)
	require.Error(t, sc.Write([]byte("blocked"), 0, protocol.ECNUnsupported))
	require.Error(t, sc.WriteTo([]byte("probe"), foreign.LocalAddr(), packetInfo{}))
	require.NoError(t, selected.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []string{"one", "two"} {
		b := make([]byte, 64)
		n, _, err := selected.ReadFrom(b)
		require.NoError(t, err)
		require.Equal(t, want, string(b[:n]))
	}
}

func TestFixedPeerClosePreservesBorrowedSocket(t *testing.T) {
	selected, foreign, socket := newUDPConnLocalhost(t), newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	tr := &Transport{Conn: socket}
	require.NoError(t, configureFixedPeer(t, tr, selected.LocalAddr().(*net.UDPAddr)))
	require.NoError(t, tr.init(false))
	_, err := foreign.WriteTo([]byte("discard"), socket.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, tr.Close())
	_, err = socket.WriteTo([]byte("caller reuse"), foreign.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, foreign.SetReadDeadline(time.Now().Add(time.Second)))
	b := make([]byte, 64)
	n, _, err := foreign.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, "caller reuse", string(b[:n]))
}
