package quic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

// This socket boundary deliberately has setters but no inspection authority.
// Successful setters must not be confused with verified native queue limits.
type managedBufferSocket struct {
	net.PacketConn
	readCalls, writeCalls int
	readErr, writeErr     error
}

func (c *managedBufferSocket) SetReadBuffer(size int) error {
	c.readCalls++
	if size != desiredBufferSize {
		panic("unexpected receive target")
	}
	return c.readErr
}

func (c *managedBufferSocket) SetWriteBuffer(size int) error {
	c.writeCalls++
	if size != desiredBufferSize {
		panic("unexpected send target")
	}
	return c.writeErr
}

func TestManagedEndpointBufferSetupOnce(t *testing.T) {
	if runManagedBufferFixtureProcess(t) {
		return
	}
	for _, failure := range []string{"none", "receive", "send", "both"} {
		t.Run(failure, func(t *testing.T) {
			socket := &managedBufferSocket{PacketConn: listenExternalUDP(t)}
			if failure == "receive" || failure == "both" {
				socket.readErr = errors.New("receive denied")
			}
			if failure == "send" || failure == "both" {
				socket.writeErr = errors.New("send denied")
			}
			endpoint, acquire, err := newManagedPacketEndpoint(socket)
			require.NoError(t, err)
			defer endpoint.Close()
			require.Equal(t, 1, socket.readCalls, "receive setup precedes publication")
			require.Equal(t, 1, socket.writeCalls, "send is attempted independently")
			peer := listenExternalUDP(t)
			_, err = endpoint.WriteTo([]byte("establishment"), peer.LocalAddr())
			require.NoError(t, err, "setup failure leaves the socket usable")
			for range 2 {
				lease, err := acquire()
				require.NoError(t, err)
				var recorder events.Recorder
				tr := &Transport{Conn: lease, Tracer: &recorder}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
				_, err = tr.WriteTo([]byte("QUIC initialization"), peer.LocalAddr())
				require.NoError(t, err)
				require.NoError(t, tr.Close())
				message := managedBufferEvent(t, &recorder)
				require.Contains(t, message, "receive_buffer_status=unknown")
				require.Contains(t, message, "send_buffer_status=unknown")
				for _, setupErr := range []error{socket.readErr, socket.writeErr} {
					if setupErr != nil {
						require.Contains(t, message, setupErr.Error())
					}
				}
				require.NoError(t, lease.Close())
			}
			require.Equal(t, 1, socket.readCalls)
			require.Equal(t, 1, socket.writeCalls)
		})
	}
}

func managedBufferEvent(t *testing.T, recorder *events.Recorder) string {
	t.Helper()
	var messages []string
	for _, event := range recorder.Events(qlog.DebugEvent{}) {
		e := event.(qlog.DebugEvent)
		if e.EventName == "managed_packet_io" {
			messages = append(messages, e.Message)
		}
	}
	require.Len(t, messages, 1)
	return messages[0]
}

func TestManagedEndpointNativeBuffers(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "darwin", "windows", "freebsd", "openbsd":
	default:
		t.Skip("native buffer inspection unavailable on this platform")
	}
	endpoint, acquire := newTestManagedEndpoint(t)
	socket := endpoint.(*managedPacketConn).endpoint.conn.(*net.UDPConn)
	raw, err := socket.SyscallConn()
	require.NoError(t, err)
	receive, err := inspectReadBuffer(raw)
	require.NoError(t, err)
	send, err := inspectWriteBuffer(raw)
	require.NoError(t, err)
	require.Positive(t, receive)
	require.Positive(t, send)
	t.Logf("native queue limits: receive=%d send=%d target=%d", receive, send, desiredBufferSize)
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	var recorder events.Recorder
	tr := &Transport{Conn: lease, Tracer: &recorder}
	peer := listenExternalUDP(t)
	_, err = tr.WriteTo([]byte("native ordinary datagram"), peer.LocalAddr())
	require.NoError(t, err)
	defer tr.Close()
	message := managedBufferEvent(t, &recorder)
	for direction, size := range map[string]int{"receive": receive, "send": send} {
		require.Contains(t, message, fmt.Sprintf("%s_buffer_bytes=%d", direction, size))
		status := "configured"
		if size < desiredBufferSize {
			status = "insufficient"
		}
		require.Contains(t, message, direction+"_buffer_status="+status)
	}
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	buf := make([]byte, 64)
	n, _, err := peer.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, "native ordinary datagram", string(buf[:n]))
	require.Equal(t, connCapabilities{}, tr.conn.capabilities())
}

func TestManagedEndpointDiagnosticProvenance(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "true") // exercise ordinary diagnostics on every platform
	for _, kind := range []string{"parent", "lease", "registered", "registered batch", "wrapper", "wrapper batch", "unknown wrapper", "external wrapper"} {
		t.Run(kind, func(t *testing.T) {
			endpoint, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			conn := lease
			if kind == "parent" {
				require.NoError(t, lease.Close())
				conn = endpoint
			}
			if strings.Contains(kind, "wrapper") {
				conn = &struct{ net.PacketConn }{lease}
			}
			var recorder events.Recorder
			tr := &Transport{Conn: conn, Tracer: &recorder}
			batch := strings.HasSuffix(kind, "batch")
			var callback func([][]byte, []byte, *net.UDPAddr) (int, error)
			if batch {
				callback = lease.(managedBatchWriterV1).WriteBatchV1
			}
			switch kind {
			case "registered", "registered batch", "wrapper", "wrapper batch":
				require.NoError(t, tr.ConfigureManagedPacketIOV1(conn, lease, callback))
			case "external wrapper":
				require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, false, nil))
			}
			_, err = tr.WriteTo([]byte("init"), listenExternalUDP(t).LocalAddr())
			require.NoError(t, err)
			defer tr.Close()
			cap := tr.conn.capabilities()
			wantECN := (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && (kind == "registered" || kind == "registered batch" || kind == "wrapper batch")
			require.Equal(t, wantECN, cap.ECN)
			require.False(t, cap.DF)
			require.False(t, cap.GSO)
			require.False(t, cap.GRO)
			if kind == "unknown wrapper" || kind == "external wrapper" {
				for _, event := range recorder.Events(qlog.DebugEvent{}) {
					require.NotEqual(t, "managed_packet_io", event.(qlog.DebugEvent).EventName)
				}
				return
			}
			message := managedBufferEvent(t, &recorder)
			require.Contains(t, message, "provenance=managed_endpoint")
			require.Contains(t, message, "receive_mode=ordinary")
			require.Contains(t, message, fmt.Sprintf("batch_callback_available=%t", batch))
			require.Contains(t, message, fmt.Sprintf("df=false ecn=%t segmentation=false coalescing=false", wantECN))
		})
	}
}

// Warning/logging state belongs to the process. Run each policy case in its own
// test process so the race suite and other socket tests cannot consume its budget.
func TestManagedEndpointBufferWarnings(t *testing.T) {
	if mode := os.Getenv("QUIC_TEST_MANAGED_BUFFER_WARNING"); mode != "" {
		if mode == "consumed" {
			warnBufferSize(errors.New("prior buffer warning"))
		}
		var output bytes.Buffer
		previous := log.Writer()
		log.SetOutput(&output)
		defer log.SetOutput(previous)
		for range 2 {
			socket := &managedBufferSocket{PacketConn: listenExternalUDP(t), readErr: errors.New("receive denied"), writeErr: errors.New("send denied")}
			if mode == "closed receive" {
				socket.readErr = errors.New("use of closed network connection")
			}
			endpoint, acquire, err := newManagedPacketEndpoint(socket)
			require.NoError(t, err)
			defer endpoint.Close()
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			var recorder events.Recorder
			tr := &Transport{Conn: lease, Tracer: &recorder}
			_, err = tr.WriteTo([]byte("init"), listenExternalUDP(t).LocalAddr())
			require.NoError(t, err)
			require.NoError(t, tr.Close())
			message := managedBufferEvent(t, &recorder)
			if mode != "closed receive" {
				require.Contains(t, message, "receive denied")
			}
			require.Contains(t, message, "send denied")
			require.Contains(t, message, "receive_buffer_status=unknown")
			require.Contains(t, message, "send_buffer_status=unknown")
		}
		wantWarnings := 0
		if mode == "enabled" || mode == "closed receive" {
			wantWarnings = 1
		}
		require.Equal(t, wantWarnings, strings.Count(output.String(), "See https://github.com/quic-go/quic-go/wiki/UDP-Buffer-Sizes"))
		if wantWarnings != 0 {
			if mode != "closed receive" {
				require.Contains(t, output.String(), "managed receive buffer: receive denied")
			}
			require.Contains(t, output.String(), "managed send buffer: send denied")
		}
		require.Contains(t, output.String(), "managed_packet_io: provenance=managed_endpoint")
		require.NotContains(t, output.String(), "Not a *net.UDPConn?")
		require.NotContains(t, output.String(), "Disabling optimizations")
		return
	}
	for _, mode := range []string{"enabled", "disabled", "consumed", "closed receive"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestManagedEndpointBufferWarnings$", "-test.count=1")
			cmd.Env = append(os.Environ(), "QUIC_TEST_MANAGED_BUFFER_WARNING="+mode, "QUIC_GO_LOG_LEVEL=debug", "QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING="+strconv.FormatBool(mode == "disabled"))
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}

// The fixture represents wiremux's selected-peer policy, including the callback
// it explicitly authorizes. Native socket ownership never bypasses these methods.
type managedPeerFilter struct {
	net.PacketConn
	peer          *net.UDPAddr
	batch         func([][]byte, []byte, *net.UDPAddr) (int, error)
	rejectedReads chan struct{}
}

func (c *managedPeerFilter) WriteTo(b []byte, addr net.Addr) (int, error) {
	if addr.String() != c.peer.String() {
		return 0, errors.New("foreign peer")
	}
	return c.PacketConn.WriteTo(b, addr)
}

func (c *managedPeerFilter) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(b)
		if err != nil || addr.String() == c.peer.String() {
			return n, addr, err
		}
		c.rejectedReads <- struct{}{}
	}
}

func (c *managedPeerFilter) sendBatch(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
	if addr.String() != c.peer.String() {
		return 0, errors.New("foreign peer")
	}
	return c.batch(bufs, oob, addr)
}

func TestManagedEndpointPreservesPeerPolicy(t *testing.T) {
	_, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	peer, foreign := listenExternalUDP(t), listenExternalUDP(t)
	outer := &managedPeerFilter{rejectedReads: make(chan struct{}, 1), PacketConn: lease, peer: peer.LocalAddr().(*net.UDPAddr), batch: lease.(managedBatchWriterV1).WriteBatchV1}
	tr := &Transport{Conn: outer}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(outer, lease, outer.sendBatch))
	_, err = tr.WriteTo([]byte("ordinary"), peer.LocalAddr())
	require.NoError(t, err)
	defer tr.Close()
	_, err = tr.WriteTo([]byte("forbidden"), foreign.LocalAddr())
	require.ErrorContains(t, err, "foreign peer")
	for _, receiver := range []*net.UDPConn{peer, foreign} {
		sc := newSendConn(tr.conn, receiver.LocalAddr(), packetInfo{}, utils.DefaultLogger)
		require.True(t, sc.batchSendAvailable())
		n, err := sc.sendBatch([][]byte{[]byte("batch")}, protocol.ECNUnsupported)
		if receiver == foreign {
			require.Zero(t, n)
			require.ErrorContains(t, err, "foreign peer")
		} else {
			require.Equal(t, 1, n)
			require.NoError(t, err)
		}
	}
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []string{"ordinary", "batch"} {
		b := make([]byte, 32)
		n, _, err := peer.ReadFrom(b)
		require.NoError(t, err)
		require.Equal(t, want, string(b[:n]))
	}
	// Arm non-QUIC delivery before sending; it is opt-in at the first read.
	canceled, stop := context.WithCancel(context.Background())
	stop()
	_, _, err = tr.ReadNonQUICPacket(canceled, make([]byte, 32))
	require.ErrorIs(t, err, context.Canceled)
	// A non-QUIC datagram must traverse the same selected-peer receive policy.
	_, err = foreign.WriteTo([]byte("\x00foreign"), lease.LocalAddr())
	require.NoError(t, err)
	_, err = peer.WriteTo([]byte("\x00allowed"), lease.LocalAddr())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b := make([]byte, 32)
	n, addr, err := tr.ReadNonQUICPacket(ctx, b)
	require.NoError(t, err)
	require.Equal(t, "\x00allowed", string(b[:n]))
	require.Equal(t, peer.LocalAddr(), addr)
	select {
	case <-outer.rejectedReads:
	case <-ctx.Done():
		t.Fatal("foreign read did not reach the policy wrapper")
	}
}

type managedUninspectableSocket struct{ *net.UDPConn }

func (c *managedUninspectableSocket) SyscallConn() (syscall.RawConn, error) {
	return nil, errors.New("inspection denied")
}

func TestManagedEndpointInspectionFailure(t *testing.T) {
	if runManagedBufferFixtureProcess(t) {
		return
	}
	socket := &managedUninspectableSocket{UDPConn: listenExternalUDP(t)}
	endpoint, acquire, err := newManagedPacketEndpoint(socket)
	require.NoError(t, err)
	defer endpoint.Close()
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	var recorder events.Recorder
	tr := &Transport{Conn: lease, Tracer: &recorder}
	_, err = tr.WriteTo([]byte("usable after inspection failure"), listenExternalUDP(t).LocalAddr())
	require.NoError(t, err)
	defer tr.Close()
	message := managedBufferEvent(t, &recorder)
	require.Contains(t, message, "receive_buffer_status=unknown")
	require.Contains(t, message, "send_buffer_status=unknown")
	require.Equal(t, 2, strings.Count(message, "inspection denied"))
}

func TestManagedEndpointDiagnosticInspectionDoesNotWarn(t *testing.T) {
	mode := os.Getenv("QUIC_TEST_MANAGED_INSPECTION")
	if mode == "" {
		for _, kind := range []string{"unavailable", "denied"} {
			t.Run(kind, func(t *testing.T) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestManagedEndpointDiagnosticInspectionDoesNotWarn$", "-test.count=1")
				cmd.Env = append(os.Environ(), "QUIC_TEST_MANAGED_INSPECTION="+kind, "QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING=false", "QUIC_GO_LOG_LEVEL=debug")
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
			})
		}
		return
	}
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	var socket net.PacketConn = &managedBufferSocket{PacketConn: listenExternalUDP(t)}
	reason := "socket buffer inspection unavailable"
	if mode == "denied" {
		socket = &managedUninspectableSocket{UDPConn: listenExternalUDP(t)}
		reason = "inspection denied"
	}
	endpoint, acquire, err := newManagedPacketEndpoint(socket)
	require.NoError(t, err)
	defer endpoint.Close()
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	var recorder events.Recorder
	tr := &Transport{Conn: lease, Tracer: &recorder}
	_, err = tr.WriteTo([]byte("usable"), listenExternalUDP(t).LocalAddr())
	require.NoError(t, err)
	require.NoError(t, tr.Close())
	message := managedBufferEvent(t, &recorder)
	require.Contains(t, message, "receive_buffer_status=unknown")
	require.Contains(t, message, "send_buffer_status=unknown")
	require.Contains(t, message, reason)
	require.Contains(t, output.String(), reason)
	require.NotContains(t, output.String(), "UDP-Buffer-Sizes")
	warnBufferSize(errors.New("later genuine sizing failure"))
	require.Contains(t, output.String(), "later genuine sizing failure")
	require.Equal(t, 1, strings.Count(output.String(), "UDP-Buffer-Sizes"))
}

// Intentional setup failures run separately from tests that may inspect the
// process warning budget. The child executes this test once and owns its logger.
func runManagedBufferFixtureProcess(t *testing.T) bool {
	t.Helper()
	if os.Getenv("QUIC_TEST_MANAGED_BUFFER_FIXTURE") == t.Name() {
		return false
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
	cmd.Env = append(os.Environ(), "QUIC_TEST_MANAGED_BUFFER_FIXTURE="+t.Name())
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return true
}
