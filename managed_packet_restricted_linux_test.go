package quic

import (
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

func TestManagedReceiveRestrictedSocket(t *testing.T) {
	mode := os.Getenv("QUIC_GO_TEST_RESTRICTED_SOCKET")
	if mode == "" {
		executable, err := os.Executable()
		require.NoError(t, err)
		for _, mode := range []string{"ecn", "ecn-v4", "ecn-v6", "other-error", "packet-info", "ecn-and-packet-info"} {
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, executable, "-test.run=^TestManagedReceiveRestrictedSocket$", "-test.v")
				cmd.Env = append(os.Environ(), "QUIC_GO_TEST_RESTRICTED_SOCKET="+mode)
				output, err := cmd.CombinedOutput()
				t.Logf("restricted subprocess:\n%s", output)
				require.NoError(t, err)
				if strings.Contains(string(output), "--- SKIP: TestManagedReceiveRestrictedSocket") {
					t.Skip("restricted subprocess could not install the fixture; see its output")
				}
			})
		}
		return
	}
	denyManagedAncillarySetup(t, mode)
	t.Setenv("QUIC_GO_DISABLE_GRO", "true")
	// Wildcard binding also exercises the required packet-info boundary.
	network, localAddr := "udp4", &net.UDPAddr{IP: net.IPv4zero}
	if mode == "ecn-v4" || mode == "ecn-v6" {
		network, localAddr = "udp", &net.UDPAddr{IP: net.IPv6zero}
	}
	endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(network, localAddr)
	require.NoError(t, err)
	defer endpoint.Close()
	peer := listenExternalUDP(t)
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	exchangeRestrictedDatagram(t, lease, peer, "before registration")
	var recorder events.Recorder
	tr := &Transport{Conn: lease, Tracer: &recorder}
	err = tr.ConfigureManagedPacketIOV1(lease, lease, nil)
	t.Logf("registration: %v", err)
	if mode == "ecn-v4" || mode == "ecn-v6" {
		require.NoError(t, err)
		e := endpoint.(*managedPacketConn).endpoint
		require.False(t, e.managedECN)
		require.Nil(t, e.receiver, "a partial-family failure must not retain an ECN-only reader")
		require.Nil(t, e.managedNative, "a partial-family failure must not retain the native metadata route")
		require.Nil(t, e.managedPacketRawFactory(lease.(*managedPacketConn)))
		require.True(t, e.managedECNSetup.admittedIPv4)
		require.True(t, e.managedECNSetup.admittedIPv6)
		wantFamily := "ipv" + strings.TrimPrefix(mode, "ecn-v")
		require.Equal(t, wantFamily, e.managedECNSetup.failedFamily)
		exchangeRestrictedDatagram(t, lease, peer, "after partial-family fallback")
		require.NoError(t, tr.Close())
		require.Contains(t, managedBufferEvent(t, &recorder), "ecn=false")
		require.Contains(t, managedBufferEvent(t, &recorder), "ecn_failed_family=\""+wantFamily+"\"")
		return
	}
	if mode != "ecn" {
		want := "activating ECN failed for both IPv4 and IPv6"
		if mode == "packet-info" {
			want = "activating packet info failed for both IPv4 and IPv6"
		}
		require.EqualError(t, err, want)
		require.EqualError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil), want, "fatal setup must remain fatal on retry")
		exchangeRestrictedDatagram(t, lease, peer, "after rejected registration")
		require.NoError(t, lease.Close())
		next, acquireErr := acquire()
		require.NoError(t, acquireErr)
		nextTransport := &Transport{Conn: next}
		require.EqualError(t, nextTransport.ConfigureManagedPacketIOV1(next, next, nil), want, "fatal setup must remain fatal in the next generation")
		require.NoError(t, next.Close())
		return
	}
	require.NoError(t, err)
	exchangeRestrictedDatagram(t, lease, peer, "after registration")
	require.NoError(t, tr.Close())
	require.Contains(t, externalPacketIOEvent(t, &recorder), "receive_permitted=true receive_eligible=true receive_enabled=false receive_disabled_reason=ancillary_setup_denied")
	require.Contains(t, managedBufferEvent(t, &recorder), "receive_mode=ordinary")
	require.Contains(t, managedBufferEvent(t, &recorder), "coalescing=false")
	exchangeRestrictedDatagram(t, lease, peer, "after transport close")
	require.NoError(t, lease.Close())
	exchangeRestrictedDatagram(t, endpoint, peer, "parent after return")
	next, err := acquire()
	require.NoError(t, err)
	defer next.Close()
	exchangeRestrictedDatagram(t, next, peer, "next lease")
	nextTransport := &Transport{Conn: next}
	require.NoError(t, nextTransport.ConfigureManagedPacketIOV1(next, next, nil))
	require.NoError(t, nextTransport.Close())
	// The native decoder remains strict for callers outside managed endpoints.
	native := listenExternalUDP(t)
	_, err = newConn(native, false, false)
	require.EqualError(t, err, "activating ECN failed for both IPv4 and IPv6")
	require.NoError(t, endpoint.Close())
	err = (&Transport{Conn: next}).ConfigureManagedPacketIOV1(next, next, nil)
	require.ErrorIs(t, err, net.ErrClosed)
	t.Logf("closed endpoint registration: %v", err)
}

func TestManagedReceiveClosedSocket(t *testing.T) {
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	// Inject native closure without marking the endpoint closed, so registration
	// reaches descriptor control instead of failing the earlier lease guard.
	require.NoError(t, endpoint.(*managedPacketConn).endpoint.conn.Close())
	tr := &Transport{Conn: lease}
	require.ErrorIs(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil), net.ErrClosed)
	require.ErrorIs(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil), net.ErrClosed, "closed-socket setup must remain fatal on retry")
	_, _, err = lease.ReadFrom(make([]byte, 1))
	require.ErrorIs(t, err, net.ErrClosed)
	_, err = lease.WriteTo([]byte("closed"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})
	require.ErrorIs(t, err, net.ErrClosed)
}

func exchangeRestrictedDatagram(t *testing.T, conn net.PacketConn, peer *net.UDPConn, payload string) {
	t.Helper()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, peer.SetDeadline(time.Now().Add(5*time.Second)))
	n, err := conn.WriteTo([]byte(payload), peer.LocalAddr())
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	b := make([]byte, 128)
	n, addr, err := peer.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, payload, string(b[:n]))
	_, err = peer.WriteTo(b[:n], addr)
	require.NoError(t, err)
	n, addr, err = conn.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, payload, string(b[:n]))
	wantAddr := peer.LocalAddr().(*net.UDPAddr)
	gotAddr := addr.(*net.UDPAddr)
	require.Equal(t, wantAddr.Port, gotAddr.Port)
	require.Equal(t, wantAddr.Zone, gotAddr.Zone)
	require.True(t, wantAddr.IP.Equal(gotAddr.IP))
	t.Logf("ordinary ReadFrom/WriteTo succeeded: %s", payload)
}

// The filter lives only in a disposable subprocess. TSYNC includes every Go
// runtime thread; a thread-local filter would not reliably restrict socket I/O.
func denyManagedAncillarySetup(t *testing.T, mode string) {
	t.Helper()
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" {
		t.Skip("restricted-socket fixture supports Linux arm64 and amd64")
	}
	ecn4, ecn6 := uint32(unix.IP_RECVTOS), uint32(unix.IPV6_RECVTCLASS)
	pi4, pi6 := ^uint32(0), ^uint32(0)
	deniedErrno := uint32(unix.EPERM)
	switch mode {
	case "ecn":
	case "ecn-v4":
		ecn6 = ^uint32(0)
	case "ecn-v6":
		ecn4 = ^uint32(0)
	case "other-error":
		deniedErrno = uint32(unix.EINVAL)
	case "packet-info":
		ecn4, ecn6 = ^uint32(0), ^uint32(0)
		pi4, pi6 = unix.IP_PKTINFO, unix.IPV6_RECVPKTINFO
	case "ecn-and-packet-info":
		pi4, pi6 = unix.IP_PKTINFO, unix.IPV6_RECVPKTINFO
	default:
		t.Fatalf("unknown restriction: %s", mode)
	}
	// Native little-endian seccomp_data: nr at 0, args[1] at 24,
	// args[2] at 32. The fixture executes only this binary's native ABI.
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_SETSOCKOPT, Jf: 12},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 24},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.IPPROTO_IP, Jf: 4},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 32},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: ecn4, Jt: 7},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: pi4, Jt: 6},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.IPPROTO_IPV6, Jf: 5},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 32},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: ecn6, Jt: 2},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: pi6, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | deniedErrno},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Skipf("seccomp unavailable: %v", err)
	}
	result, _, errno := unix.RawSyscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filter)
	if errno != 0 || result != 0 {
		t.Skipf("seccomp TSYNC unavailable: result=%d errno=%v", result, errno)
	}
}
