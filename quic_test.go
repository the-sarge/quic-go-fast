package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

// in the tests for the stream deadlines we set a deadline
// and wait to make an assertion when Read / Write was unblocked
// on the CIs, the timing is a lot less precise, so scale every duration by this factor
func scaleDuration(t time.Duration) time.Duration {
	scaleFactor := 1
	if f, err := strconv.Atoi(os.Getenv("TIMESCALE_FACTOR")); err == nil { // parsing "" errors, so this works fine if the env is not set
		scaleFactor = f
	}
	if scaleFactor == 0 {
		panic("TIMESCALE_FACTOR is 0")
	}
	return time.Duration(scaleFactor) * t
}

func newUDPConnLocalhost(t testing.TB) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func getPacket(t *testing.T, connID protocol.ConnectionID) []byte {
	return getPacketWithPacketType(t, connID, protocol.PacketTypeHandshake, 2)
}

func getPacketWithPacketType(t *testing.T, connID protocol.ConnectionID, typ protocol.PacketType, length protocol.ByteCount) []byte {
	t.Helper()
	b, err := (&wire.ExtendedHeader{
		Header: wire.Header{
			Type:             typ,
			DestConnectionID: connID,
			Length:           length,
			Version:          protocol.Version1,
		},
		PacketNumberLen: protocol.PacketNumberLen2,
	}).Append(nil, protocol.Version1)
	require.NoError(t, err)
	return append(b, bytes.Repeat([]byte{42}, int(length)-2)...)
}

func areConnsRunning() bool {
	var b bytes.Buffer
	pprof.Lookup("goroutine").WriteTo(&b, 1)
	return strings.Contains(b.String(), "quic-go.(*Conn).run")
}

func TestAreConnsRunning(t *testing.T) {
	require.False(t, areConnsRunning())
	server := newUDPConnLocalhost(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := DialAddr(ctx, server.LocalAddr().String(), &tls.Config{InsecureSkipVerify: true}, nil)
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(scaleDuration(time.Second)):
			t.Error("dial worker did not exit")
		}
		require.Eventually(t, func() bool { return !areConnsRunning() }, scaleDuration(time.Second), time.Millisecond)
	})

	// Receiving an Initial proves the real connection loop has started. The
	// silent peer keeps it alive until cleanup cancels the dial.
	require.NoError(t, server.SetReadDeadline(time.Now().Add(scaleDuration(time.Second))))
	_, _, err := server.ReadFrom(make([]byte, 1500))
	require.NoError(t, err)
	require.True(t, areConnsRunning(), "live Conn.run must be visible to the suite leak guard")
}

func areTransportsRunning() bool {
	var b bytes.Buffer
	pprof.Lookup("goroutine").WriteTo(&b, 1)
	return strings.Contains(b.String(), "quic-go.(*Transport).listen")
}

func TestMain(m *testing.M) {
	status := m.Run()
	if status != 0 {
		os.Exit(status)
	}
	if areConnsRunning() {
		fmt.Println("stray connection goroutines found")
		os.Exit(1)
	}
	if areTransportsRunning() {
		fmt.Println("stray transport goroutines found")
		os.Exit(1)
	}
	os.Exit(status)
}
