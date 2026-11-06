package quic

import (
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// This receiver stands at the native read boundary; the public endpoint/lease
// operations still own registration, serialization and handback.
type packetCeilingManagedReceiver struct{ seen protocol.ByteCount }

func (r *packetCeilingManagedReceiver) ReadPacket() (receivedPacket, error) {
	return r.readPacketWithPacketCeiling(0)
}

func (r *packetCeilingManagedReceiver) readPacketWithPacketCeiling(ceiling packetCeiling) (receivedPacket, error) {
	r.seen = ceiling.size()
	buffer := getPacketBuffer()
	buffer.Data = append(buffer.Data, "payload"...)
	return receivedPacket{data: buffer.Data, buffer: buffer}, nil
}

func (*packetCeilingManagedReceiver) releaseReadBuffers() {}

func TestPacketCeilingManagedLeaseReads(t *testing.T) {
	// Hide native socket setup so the scripted receiver is retained on every OS.
	socket := &nonOOBPacketConn{PacketConn: newUDPConnLocalhost(t)}
	endpoint, acquire, err := newManagedPacketEndpoint(socket)
	require.NoError(t, err)
	defer endpoint.Close()
	receiver := &packetCeilingManagedReceiver{}
	endpoint.(*managedPacketConn).endpoint.receiver = receiver
	read := func(conn net.PacketConn, want protocol.ByteCount) {
		t.Helper()
		b := make([]byte, 32)
		n, _, err := conn.ReadFrom(b)
		require.NoError(t, err)
		require.Equal(t, "payload", string(b[:n]))
		require.Equal(t, want, receiver.seen)
	}
	read(endpoint, 1452)
	lease, err := acquire()
	require.NoError(t, err)
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	require.NoError(t, tr.setPacketCeilingForTesting(1400), "registration does not freeze the ceiling")
	read(lease, 1452) // ordinary establishment reads keep the default until sealing
	require.NoError(t, tr.beginPacketIO())
	read(lease, 1400)
	require.NoError(t, tr.beginPacketIO(), "sealing is idempotent")
	read(lease, 1400)
	require.Error(t, tr.setPacketCeilingForTesting(1300))
	require.NoError(t, lease.Close())
	read(endpoint, 1452)
	next, err := acquire()
	require.NoError(t, err)
	defer next.Close()
	nextTransport := &Transport{Conn: next}
	require.NoError(t, nextTransport.ConfigureManagedPacketIOV1(next, next, nil))
	require.NoError(t, nextTransport.beginPacketIO())
	read(next, 1452)
	require.Same(t, receiver, endpoint.(*managedPacketConn).endpoint.receiver)
	require.NoError(t, tr.beginPacketIO(), "late sealing is idempotent and cannot configure the new lease")
	read(next, 1452)
}
