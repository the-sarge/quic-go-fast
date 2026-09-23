package quic

import (
	"crypto/tls"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

// Socket-option failures cannot be induced portably on owned UDP descriptors.
// This fixture replaces only the native option boundary; registration, joining,
// revocation and Close execute through the real endpoint and transport APIs.
type managedDFTestSocket struct {
	values       [2]int
	denySecond   bool
	failRestore  bool
	ignoreEnable bool
}

func (*managedDFTestSocket) options() ([]managedDFOption, error) {
	return []managedDFOption{{name: 0, value: 1}, {name: 1, value: 1}}, nil
}
func (s *managedDFTestSocket) get(o managedDFOption) (int, error) { return s.values[o.name], nil }
func (s *managedDFTestSocket) set(o managedDFOption, v int) error {
	if s.denySecond && o.name == 1 {
		return errManagedDFUnavailable
	}
	if s.failRestore && v != 1 {
		return errors.New("restore failed")
	}
	if s.ignoreEnable && v == 1 {
		return nil
	}
	s.values[o.name] = v
	return nil
}

func managedDFTestEndpoint(t *testing.T, socket *managedDFTestSocket) (net.PacketConn, func() (net.PacketConn, error)) {
	t.Helper()
	endpoint, acquire := newTestManagedEndpoint(t)
	endpoint.(*managedPacketConn).endpoint.dfControl = func(fn func(managedDFSocket) error) error { return fn(socket) }
	return endpoint, acquire
}

func TestManagedDFOptionalFailureRestoresPartialSetup(t *testing.T) {
	socket := &managedDFTestSocket{values: [2]int{7, 9}, denySecond: true}
	endpoint, acquire := managedDFTestEndpoint(t, socket)
	lease, err := acquire()
	require.NoError(t, err)
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil), "known-unchanged denied option must permit fallback")
	require.Equal(t, [2]int{7, 9}, socket.values)
	conn := tr.wrapExternalPacketIO(&basicConn{PacketConn: lease})
	require.False(t, conn.capabilities().DF)
	require.NoError(t, lease.Close())
	next, err := acquire()
	require.NoError(t, err)
	require.NoError(t, next.Close())
	require.NoError(t, endpoint.Close())
}

func TestManagedDFFailedRestorationTerminatesEndpoint(t *testing.T) {
	for _, phase := range []string{"setup rollback", "release"} {
		t.Run(phase, func(t *testing.T) {
			socket := &managedDFTestSocket{values: [2]int{7, 9}}
			endpoint, acquire := managedDFTestEndpoint(t, socket)
			lease, err := acquire()
			require.NoError(t, err)
			tr := &Transport{Conn: lease}
			if phase == "setup rollback" {
				socket.denySecond = true
				socket.failRestore = true
			}
			err = tr.ConfigureManagedPacketIOV1(lease, lease, nil)
			if phase == "setup rollback" {
				require.ErrorContains(t, err, "restore failed")
			} else {
				require.NoError(t, err)
			}
			socket.failRestore = true
			var wg sync.WaitGroup
			closeErrors := make([]error, 3)
			for i := range closeErrors {
				wg.Go(func() { closeErrors[i] = lease.Close() })
			}
			wg.Wait()
			for _, err := range closeErrors {
				require.ErrorContains(t, err, "restore failed")
			}
			_, err = acquire()
			require.ErrorIs(t, err, net.ErrClosed)
			_, err = endpoint.WriteTo([]byte("ordinary"), endpoint.LocalAddr())
			require.ErrorIs(t, err, net.ErrClosed)
			require.ErrorContains(t, endpoint.Close(), "restore failed")
		})
	}
}

func TestManagedDFAmbiguousSetupCannotAdvertiseCapability(t *testing.T) {
	socket := &managedDFTestSocket{values: [2]int{7, 9}, ignoreEnable: true}
	_, acquire := managedDFTestEndpoint(t, socket)
	lease, err := acquire()
	require.NoError(t, err)
	tr := &Transport{Conn: lease}
	require.ErrorContains(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil), "did not retain")
	require.Equal(t, [2]int{7, 9}, socket.values)
	require.NoError(t, lease.Close())
}

func TestManagedDFAdmissionAndFailedInitialization(t *testing.T) {
	socket := &managedDFTestSocket{values: [2]int{7, 9}}
	_, acquire := managedDFTestEndpoint(t, socket)
	lease, err := acquire()
	require.NoError(t, err)
	outer := &faultySyscallConn{PacketConn: lease}
	tr := &Transport{Conn: outer}
	require.Error(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	require.Equal(t, [2]int{7, 9}, socket.values, "rejected identity cannot mutate native options")
	require.NoError(t, tr.ConfigureManagedPacketIOV1(outer, lease, nil))
	require.Equal(t, [2]int{1, 1}, socket.values)
	_, err = tr.Listen(&tls.Config{}, nil)
	require.Error(t, err)
	require.Error(t, tr.Close())
	require.Equal(t, [2]int{1, 1}, socket.values, "Transport.Close does not release the lease")
	require.NoError(t, lease.Close())
	require.Equal(t, [2]int{7, 9}, socket.values)
}

func TestManagedDFRestorationJoinsIO(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		native := newManagedBlockingSocket()
		endpoint, acquire, err := newManagedPacketEndpoint(native)
		require.NoError(t, err)
		defer endpoint.Close()
		socket := &managedDFTestSocket{values: [2]int{7, 9}}
		endpoint.(*managedPacketConn).endpoint.dfControl = func(fn func(managedDFSocket) error) error { return fn(socket) }
		lease, err := acquire()
		require.NoError(t, err)
		tr := &Transport{Conn: lease}
		require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
		raw := tr.wrapExternalPacketIO(&basicConn{PacketConn: lease})
		ioDone := make(chan error, 1)
		go func() { _, _, err := lease.ReadFrom(make([]byte, 1)); ioDone <- err }()
		<-native.entered
		closed := make(chan error, 1)
		go func() { closed <- lease.Close() }()
		<-native.interrupted
		synctest.Wait()
		require.Equal(t, [2]int{1, 1}, socket.values, "restoration waits for active I/O")
		require.False(t, raw.capabilities().DF, "revocation precedes joining")
		close(native.finish)
		require.ErrorIs(t, <-ioDone, os.ErrDeadlineExceeded)
		require.NoError(t, <-closed)
		require.Equal(t, [2]int{7, 9}, socket.values)
	})
}
