package quic

import (
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCIDPathRoutingIngress(t *testing.T) {
	tr := &Transport{
		connIDLen: 4,
		handlers:  make(map[protocol.ConnectionID]packetHandlerEntry),
		logger:    utils.DefaultLogger,
	}
	packets := make(chan receivedPacket, 2)
	handler := &mockPacketHandler{packets: packets}
	m := (*packetHandlerMap)(tr)
	zero := protocol.ParseConnectionID([]byte{1, 2, 3, 4})
	other := protocol.ParseConnectionID([]byte{5, 6, 7, 8})
	require.True(t, m.AddPath(zero, 0, handler))
	require.True(t, m.AddPath(other, protocol.MaxPathID, handler))
	remote := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	for _, tc := range []struct {
		cid  protocol.ConnectionID
		path protocol.PathID
	}{{zero, 0}, {other, protocol.MaxPathID}} {
		buf := getPacketBuffer()
		buf.Data = append(append(buf.Data[:0], 0x40), tc.cid.Bytes()...)
		tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data, remoteAddr: remote})
		p := <-packets
		require.Equal(t, tc.path, p.pathID)
		require.Equal(t, tc.path, p.Clone().pathID)
		p.buffer.Release()
	}
	// An unissued CID from the same tuple must not reach the connection.
	buf := getPacketBuffer()
	buf.Data = append(buf.Data[:0], 0x40, 9, 9, 9, 9)
	tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data, remoteAddr: remote})
	require.Empty(t, packets)
}

func TestCIDPathRoutingLengthAndRemoval(t *testing.T) {
	tr := &Transport{connIDLen: 4, handlers: make(map[protocol.ConnectionID]packetHandlerEntry), logger: utils.DefaultLogger}
	m := (*packetHandlerMap)(tr)
	packets := make(chan receivedPacket, 1)
	h := &mockPacketHandler{packets: packets}
	cid := protocol.ParseConnectionID([]byte{1, 2, 3, 4})
	require.False(t, m.AddPath(protocol.ParseConnectionID([]byte{1, 2}), 1, h))
	require.True(t, m.AddPath(cid, 1, h))
	require.False(t, m.AddPath(cid, 2, h), "a duplicate CID cannot change its binding")
	p := receivedPacket{data: []byte{0x40, 1, 2, 3, 4}}
	require.True(t, m.deliverPermitted(cid, p))
	require.Equal(t, protocol.PathID(1), (<-packets).pathID)
	m.Remove(cid)
	require.False(t, m.deliverPermitted(cid, p))
	require.True(t, m.AddPath(cid, 2, h))
	require.True(t, m.deliverPermitted(cid, p))
	require.Equal(t, protocol.PathID(2), (<-packets).pathID)
}

func TestCIDPathUnpackerRejectsUnadmittedPath(t *testing.T) {
	for _, name := range []string{"1-RTT", "0-RTT"} {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			u := NewMockUnpacker(ctrl) // No long-header opener may be consulted.
			tc := newServerTestConnection(t, ctrl, nil, false, connectionOptUnpacker(u))
			var data []byte
			if name == "1-RTT" {
				// No admitted protection exists for the routed path.
				cs := mocks.NewMockCryptoSetup(ctrl)
				cs.EXPECT().Get1RTTOpener().Return(nil, handshake.ErrKeysDropped)
				tc.conn.unpacker = newPacketUnpacker(cs, tc.srcConnID.Len())
				data = append(append([]byte{0x40}, tc.srcConnID.Bytes()...), 0)
			} else {
				hdr := &wire.ExtendedHeader{Header: wire.Header{
					Type: protocol.PacketType0RTT, Version: protocol.Version1,
					DestConnectionID: tc.srcConnID, SrcConnectionID: tc.destConnID, Length: 2,
				}, PacketNumber: 7, PacketNumberLen: protocol.PacketNumberLen1}
				var err error
				data, err = hdr.Append(nil, protocol.Version1)
				require.NoError(t, err)
				data = append(data, 1) // PING
			}
			tr := &Transport{connIDLen: tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), logger: utils.DefaultLogger}
			require.True(t, (*packetHandlerMap)(tr).AddPath(tc.srcConnID, 1, tc.conn))
			p := lifetimePacket(t, tc, data)
			tr.handlePacket(p)
			processed, err := tc.conn.handlePackets()
			require.NoError(t, err, "unsupported application input is a nonfatal drop")
			require.False(t, processed)
			require.False(t, tc.conn.receivedPacketHandler.IsPotentiallyDuplicate(7, protocol.Encryption1RTT))
			require.Empty(t, tc.conn.undecryptablePackets)
			require.Zero(t, p.buffer.refCount)
		})
	}
}

func TestCIDPathZeroLengthFallback(t *testing.T) {
	tr := &Transport{handlers: make(map[protocol.ConnectionID]packetHandlerEntry), logger: utils.DefaultLogger}
	packets := make(chan receivedPacket, 1)
	m := (*packetHandlerMap)(tr)
	require.False(t, m.AddPath(protocol.ConnectionID{}, 1, &mockPacketHandler{packets: packets}))
	require.True(t, m.Add(protocol.ConnectionID{}, &mockPacketHandler{packets: packets}))
	buf := getPacketBuffer()
	buf.Data = append(buf.Data[:0], 0x40, 0)
	tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data})
	p := <-packets
	require.Zero(t, p.pathID)
	p.buffer.Release()
}

func TestCIDPathIssuedBindingLifetime(t *testing.T) {
	tr := &Transport{connIDLen: 4, handlers: make(map[protocol.ConnectionID]packetHandlerEntry), logger: utils.DefaultLogger}
	m := (*packetHandlerMap)(tr)
	packets := make(chan receivedPacket, 1)
	handler := &mockPacketHandler{packets: packets}
	initial := protocol.ParseConnectionID([]byte{1, 2, 3, 4})
	require.True(t, m.Add(initial, handler))
	var frame *wire.NewConnectionIDFrame
	g := newConnIDGenerator(m, initial, nil, newStatelessResetter(&StatelessResetKey{1}), connRunnerCallbacks{
		AddConnectionID:    func(cid protocol.ConnectionID) { require.True(t, m.Add(cid, handler)) },
		RemoveConnectionID: m.Remove,
		ReplaceWithClosed:  m.ReplaceWithClosed,
	}, func(f wire.Frame) { frame = f.(*wire.NewConnectionIDFrame) }, &protocol.DefaultConnectionIDGenerator{ConnLen: 4})
	require.NoError(t, g.SetMaxActiveConnIDs(2))
	issued := frame.ConnectionID
	buf := getPacketBuffer()
	buf.Data = append(append(buf.Data[:0], 0x40), issued.Bytes()...)
	tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data})
	p := <-packets
	require.Zero(t, p.pathID)
	p.buffer.Release()
	expiry := monotime.Now().Add(time.Second)
	require.NoError(t, g.Retire(1, initial, expiry))
	_, ok := m.Get(issued)
	require.True(t, ok, "retiring CID keeps routing until its retention expires")
	g.RemoveRetiredConnIDs(expiry)
	_, ok = m.Get(issued)
	require.False(t, ok)
	g.RemoveAll()
	_, ok = m.Get(initial)
	require.False(t, ok)
	_, ok = m.Get(frame.ConnectionID)
	require.False(t, ok)
}

func TestCIDPathClosedHandlerLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &Transport{connIDLen: 4, handlers: make(map[protocol.ConnectionID]packetHandlerEntry), logger: utils.DefaultLogger}
		m := (*packetHandlerMap)(tr)
		cid := protocol.ParseConnectionID([]byte{1, 2, 3, 4})
		require.True(t, m.AddPath(cid, 1, &mockPacketHandler{}))
		m.ReplaceWithClosed([]protocol.ConnectionID{cid}, nil, time.Second)
		buf := getPacketBuffer()
		buf.Data = append(append(buf.Data[:0], 0x40), cid.Bytes()...)
		tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data})
		_, ok := m.Get(cid)
		require.True(t, ok, "closed handler absorbs delayed input")
		time.Sleep(2 * time.Second)
		synctest.Wait()
		_, ok = m.Get(cid)
		require.False(t, ok)
		packets := make(chan receivedPacket, 1)
		require.True(t, m.AddPath(cid, 2, &mockPacketHandler{packets: packets}))
		require.True(t, m.deliverPermitted(cid, receivedPacket{}))
		require.Equal(t, protocol.PathID(2), (<-packets).pathID)
	})
}
