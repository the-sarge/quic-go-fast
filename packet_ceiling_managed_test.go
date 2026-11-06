package quic

import (
	"bytes"
	"net"
	"os"
	"testing"
	"time"

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
	tr.packetCeiling = 1400 // retained internal lease-freezing characterization
	read(lease, 1452)       // ordinary establishment reads keep the default until sealing
	require.NoError(t, tr.beginPacketIO())
	read(lease, 1400)
	require.NoError(t, tr.beginPacketIO(), "sealing is idempotent")
	read(lease, 1400)
	require.Error(t, tr.ConfigurePacketCeilingV1(8972))
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

func TestPacketCeilingManagedCloseAfterHandback(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "registered but never started", true: "initialized then closed"}[started], func(t *testing.T) {
			_, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			tr := &Transport{Conn: lease}
			defer tr.Close()
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
			tr.packetCeiling = 1400 // retained internal lease-freezing characterization
			if started {
				require.NoError(t, tr.init(false))
				require.NoError(t, tr.Close())
			}
			require.NoError(t, lease.Close())
			next, err := acquire()
			require.NoError(t, err)
			defer next.Close()
			nextTransport := &Transport{Conn: next}
			require.NoError(t, nextTransport.ConfigureManagedPacketIOV1(next, next, nil))
			require.NoError(t, next.SetReadDeadline(time.Now().Add(-time.Second)))
			require.NoError(t, tr.Close(), "cleanup does not depend on prior initialization or a live lease")
			require.NoError(t, tr.Close(), "repeated cleanup preserves its existing result")
			b := make([]byte, 32)
			_, _, err = lease.ReadFrom(b)
			require.ErrorIs(t, err, net.ErrClosed, "copying a historic lease does not admit stale reads")
			_, _, err = next.ReadFrom(b)
			require.ErrorIs(t, err, os.ErrDeadlineExceeded, "old cleanup cannot clear the new lease's deadline")
			require.NoError(t, next.SetReadDeadline(time.Now().Add(time.Second)))
			peer := newUDPConnLocalhost(t)
			_, err = peer.WriteTo([]byte("new lease"), next.LocalAddr())
			require.NoError(t, err)
			n, _, err := next.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, "new lease", string(b[:n]))
		})
	}
}

func TestJumboManagedLeaseLoopback(t *testing.T) {
	// Keep this whole-datagram gate independent of the Windows URO guard.
	// The URO tests separately exercise the lease-frozen configured limit.
	t.Setenv("QUIC_GO_DISABLE_GRO", "true")
	for _, configured := range []protocol.ByteCount{0, 8972} {
		socket := newUDPConnLocalhost(t)
		endpoint, acquire, err := newManagedPacketEndpoint(socket)
		require.NoError(t, err)
		defer endpoint.Close()
		lease, err := acquire()
		require.NoError(t, err)
		tr := &Transport{Conn: lease}
		require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
		if configured != 0 {
			require.NoError(t, tr.ConfigurePacketCeilingV1(int(configured)))
		}
		require.NoError(t, tr.beginPacketIO())
		require.NoError(t, lease.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
		peer := newUDPConnLocalhost(t)
		for _, size := range []int{8972, 8973} {
			payload := bytes.Repeat([]byte{'m'}, size)
			_, err = peer.WriteTo(payload, lease.LocalAddr())
			require.NoError(t, err)
			buffer := make([]byte, 10000)
			n, _, err := lease.ReadFrom(buffer)
			require.NoError(t, err)
			require.Equal(t, payload, buffer[:n])
		}
		require.NoError(t, lease.Close())
	}
}
