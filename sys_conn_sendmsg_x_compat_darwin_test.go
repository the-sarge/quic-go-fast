//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"net"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// This is native EMSGSIZE evidence only. It does not establish the kernel's
// suppression of other errnos after partial progress.
func TestSendmsgXNativeMessageSizeProgress(t *testing.T) {
	major, err := getMacOSVersion()
	require.NoError(t, err)
	if _, ok := sendmsgXQualifiedKernel(major, runtime.GOARCH); !ok {
		t.Skipf("unqualified Darwin kernel %d/%s", major, runtime.GOARCH)
	}
	resetSendmsgXForTesting(t)
	sendmsgXEnsureQualified()
	require.True(t, sendmsgXAvailable())
	for _, prefix := range []bool{false, true} {
		name := "oversized_first"
		if prefix {
			name = "accepted_prefix"
		}
		t.Run(name, func(t *testing.T) {
			sender, err := newSelfCheckSocket(unix.AF_INET)
			require.NoError(t, err)
			defer unix.Close(sender)
			receiver, port, err := newSelfCheckReceiver(unix.AF_INET)
			require.NoError(t, err)
			defer unix.Close(receiver)
			dest := newSendmsgXDest(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}, unix.AF_INET)
			payloads := [][]byte{make([]byte, 65535), []byte("unsent suffix")}
			if prefix {
				payloads = append([][]byte{[]byte("accepted prefix")}, payloads...)
			}
			accepted, errno := sendmsgXBatchTo(sender, payloads, dest.name(), dest.namelen,
				appendIPv4ECNMsg(nil, protocol.ECT0), make([]msghdrX, len(payloads)), make([]syscall.Iovec, len(payloads)))
			t.Logf("offered=%d accepted=%d errno=%d", len(payloads), accepted, errno)
			if prefix {
				require.Zero(t, errno)
				require.Equal(t, 1, accepted)
				require.NoError(t, verifySelfCheckDelivery(receiver, [][]byte{[]byte("accepted prefix")}, unix.IPPROTO_IP, msgTypeIPTOS))
			} else {
				require.Equal(t, syscall.EMSGSIZE, errno)
				require.Equal(t, -1, accepted)
			}
			// The synchronous call is finished; neither rejected entry nor
			// suffix may have been queued at the loopback peer.
			_, _, _, _, err = unix.Recvmsg(receiver, make([]byte, 65536), nil, unix.MSG_DONTWAIT)
			require.ErrorIs(t, err, unix.EAGAIN)
		})
	}
}

func TestSendmsgXManagedLeaseReacquisition(t *testing.T) {
	major, err := getMacOSVersion()
	require.NoError(t, err)
	if _, ok := sendmsgXQualifiedKernel(major, runtime.GOARCH); !ok {
		t.Skipf("unqualified Darwin kernel %d/%s", major, runtime.GOARCH)
	}
	resetSendmsgXForTesting(t)
	sendmsgXEnsureQualified()
	require.True(t, sendmsgXAvailable())
	endpoint, acquire := newTestManagedEndpoint(t)
	peer := listenExternalUDP(t)
	var stale func([][]byte, []byte, *net.UDPAddr) (int, error)
	for _, generation := range []string{"first", "reacquired"} {
		lease, err := acquire()
		require.NoError(t, err)
		t.Cleanup(func() { lease.Close() })
		writer := lease.(managedBatchWriterV1).WriteBatchV1
		tr := &Transport{Conn: lease}
		require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, writer))
		payloads := [][]byte{[]byte(generation + " one"), []byte(generation + " two")}
		addr := peer.LocalAddr().(*net.UDPAddr)
		before, _, _ := sendmsgXCountersSnapshot()
		if stale != nil {
			n, err := stale(payloads, nil, addr)
			require.ErrorIs(t, err, net.ErrClosed)
			require.Zero(t, n)
			after, _, _ := sendmsgXCountersSnapshot()
			require.Equal(t, before, after)
		}
		n, err := writer(payloads, nil, addr)
		require.NoError(t, err)
		require.Equal(t, 2, n)
		after, _, _ := sendmsgXCountersSnapshot()
		require.Greater(t, after, before)
		require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
		for _, want := range payloads {
			buf := make([]byte, 64)
			n, from, err := peer.ReadFromUDP(buf)
			require.NoError(t, err)
			require.Equal(t, want, buf[:n])
			require.Equal(t, endpoint.LocalAddr(), from)
		}
		stale = writer
		require.NoError(t, lease.Close())
	}
	require.NoError(t, endpoint.Close())
	_, err = acquire()
	require.ErrorIs(t, err, net.ErrClosed)
}
