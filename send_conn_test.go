package quic

import (
	"errors"
	"net"
	"net/netip"
	"runtime"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Only if appendUDPSegmentSizeMsg actually appends a message (and isn't only a stub implementation),
// GSO is actually supported on this platform.
var platformSupportsGSO = len(appendUDPSegmentSizeMsg([]byte{}, 1337)) > 0

func TestSendConnLocalAndRemoteAddress(t *testing.T) {
	remoteAddr := &net.UDPAddr{IP: net.IPv4(192, 168, 100, 200), Port: 1337}
	rawConn := NewMockRawConn(gomock.NewController(t))
	rawConn.EXPECT().LocalAddr().Return(&net.UDPAddr{IP: net.IPv4(10, 11, 12, 13), Port: 14}).Times(2)
	c := newSendConn(
		rawConn,
		remoteAddr,
		packetInfo{addr: netip.AddrFrom4([4]byte{127, 0, 0, 42})},
		utils.DefaultLogger,
	)
	require.Equal(t, "127.0.0.42:14", c.LocalAddr().String())
	require.Equal(t, remoteAddr, c.RemoteAddr())

	// the local raw conn's local address is only used if we don't an address from the packet info
	c = newSendConn(rawConn, remoteAddr, packetInfo{}, utils.DefaultLogger)
	require.Equal(t, "10.11.12.13:14", c.LocalAddr().String())
}

func TestSendConnOOB(t *testing.T) {
	remoteAddr := &net.UDPAddr{IP: net.IPv4(192, 168, 100, 200), Port: 1337}
	rawConn := NewMockRawConn(gomock.NewController(t))
	rawConn.EXPECT().LocalAddr()
	rawConn.EXPECT().capabilities().AnyTimes()
	pi := packetInfo{addr: netip.IPv6Loopback()}
	rawConn.EXPECT().WritePacket([]byte("foobar"), remoteAddr, pi.OOB(), uint16(0), protocol.ECT1)
	require.NotEmpty(t, pi.OOB())
	c := newSendConn(rawConn, remoteAddr, pi, utils.DefaultLogger)
	require.NoError(t, c.Write([]byte("foobar"), 0, protocol.ECT1))
}

func TestSendConnDetectGSOFailure(t *testing.T) {
	if !platformSupportsGSO {
		t.Skip("GSO is not supported on this platform")
	}

	remoteAddr := &net.UDPAddr{IP: net.IPv4(192, 168, 100, 200), Port: 1337}
	rawConn := NewMockRawConn(gomock.NewController(t))
	rawConn.EXPECT().LocalAddr()
	rawConn.EXPECT().capabilities().Return(connCapabilities{GSO: true}).MinTimes(1)
	c := newSendConn(rawConn, remoteAddr, packetInfo{}, utils.DefaultLogger)
	gomock.InOrder(
		rawConn.EXPECT().WritePacket([]byte("foobar"), remoteAddr, gomock.Any(), uint16(4), protocol.ECNCE).Return(0, errGSO),
		rawConn.EXPECT().WritePacket([]byte("foob"), remoteAddr, gomock.Any(), uint16(0), protocol.ECNCE).Return(4, nil),
		rawConn.EXPECT().WritePacket([]byte("ar"), remoteAddr, gomock.Any(), uint16(0), protocol.ECNCE).Return(2, nil),
	)
	require.NoError(t, c.Write([]byte("foobar"), 4, protocol.ECNCE))
	require.False(t, c.capabilities().GSO)
}

// Avoid mock-controller synchronization between socket writes and capability reads:
// the test must exercise the synchronization provided by sconn itself.
type gsoFallbackRawConn struct {
	rawConn
}

func (*gsoFallbackRawConn) LocalAddr() net.Addr { return &net.UDPAddr{} }

func (*gsoFallbackRawConn) capabilities() connCapabilities {
	return connCapabilities{GSO: true, ECN: true, DF: true}
}

func (*gsoFallbackRawConn) WritePacket(p []byte, _ net.Addr, _ []byte, gsoSize uint16, _ protocol.ECN) (int, error) {
	if gsoSize != 0 {
		return 0, errGSO
	}
	return len(p), nil
}

func TestSendConnGSOFallbackConcurrentCapabilities(t *testing.T) {
	if !platformSupportsGSO {
		t.Skip("GSO is not supported on this platform")
	}

	c := newSendConn(&gsoFallbackRawConn{}, &net.UDPAddr{}, packetInfo{}, utils.DefaultLogger)
	require.Equal(t, connCapabilities{GSO: true, ECN: true, DF: true}, c.capabilities())
	start := make(chan struct{})
	readDone := make(chan connCapabilities, 1)
	go func() {
		<-start
		var caps connCapabilities
		for range 1000 {
			caps = c.capabilities()
		}
		readDone <- caps
	}()
	close(start)
	err := c.Write([]byte("foobar"), 4, protocol.ECNCE)
	caps := <-readDone
	require.NoError(t, err)
	require.True(t, caps.ECN)
	require.True(t, caps.DF)
	require.Equal(t, connCapabilities{GSO: false, ECN: true, DF: true}, c.capabilities())
}

var errSubmissionCap = errors.New("submission cap reached")

// gsoErrorOnceRawConn fails its first submission with errGSO and records every
// submission. Past a cap it fails with errSubmissionCap, so a write that keeps
// resubmitting fails the test instead of hanging it.
type gsoErrorOnceRawConn struct {
	rawConn
	lengths  []int
	gsoSizes []uint16
}

func (*gsoErrorOnceRawConn) LocalAddr() net.Addr { return &net.UDPAddr{} }

func (*gsoErrorOnceRawConn) capabilities() connCapabilities { return connCapabilities{GSO: true} }

func (c *gsoErrorOnceRawConn) WritePacket(p []byte, _ net.Addr, _ []byte, gsoSize uint16, _ protocol.ECN) (int, error) {
	c.lengths = append(c.lengths, len(p))
	c.gsoSizes = append(c.gsoSizes, gsoSize)
	switch {
	case len(c.lengths) == 1:
		return 0, errGSO
	case len(c.lengths) > 10:
		return 0, errSubmissionCap
	}
	return len(p), nil
}

// A write without a segment size was not segmented, so its error is not a GSO
// failure: it is returned after one submission and GSO stays enabled.
func TestSendConnGSOErrorWithoutSegmentSize(t *testing.T) {
	if !platformSupportsGSO {
		t.Skip("GSO is not supported on this platform")
	}

	rawConn := &gsoErrorOnceRawConn{}
	c := newSendConn(rawConn, &net.UDPAddr{}, packetInfo{}, utils.DefaultLogger)
	err := c.Write([]byte("foobar"), 0, protocol.ECNCE)
	require.ErrorIs(t, err, errGSO)
	require.True(t, isGSOError(err))
	require.Equal(t, []int{6}, rawConn.lengths)
	require.True(t, c.capabilities().GSO)
}

// A segmented write falls back even when its payload fits in one segment.
func TestSendConnGSOFailureSingleSegment(t *testing.T) {
	if !platformSupportsGSO {
		t.Skip("GSO is not supported on this platform")
	}

	rawConn := &gsoErrorOnceRawConn{}
	c := newSendConn(rawConn, &net.UDPAddr{}, packetInfo{}, utils.DefaultLogger)
	require.NoError(t, c.Write([]byte("foobar"), 6, protocol.ECNCE))
	require.Equal(t, []int{6, 6}, rawConn.lengths)
	require.Equal(t, []uint16{6, 0}, rawConn.gsoSizes)
	require.False(t, c.capabilities().GSO)
}

func TestSendConnSendmsgFailures(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux exhibits this bug, we don't need to work around it on other platforms")
	}

	remoteAddr := &net.UDPAddr{IP: net.IPv4(192, 168, 100, 200), Port: 1337}

	t.Run("first call to sendmsg fails", func(t *testing.T) {
		rawConn := NewMockRawConn(gomock.NewController(t))
		rawConn.EXPECT().LocalAddr()
		rawConn.EXPECT().capabilities().AnyTimes()
		c := newSendConn(rawConn, remoteAddr, packetInfo{}, utils.DefaultLogger)
		gomock.InOrder(
			rawConn.EXPECT().WritePacket([]byte("foobar"), remoteAddr, gomock.Any(), gomock.Any(), protocol.ECNCE).Return(0, errNotPermitted),
			rawConn.EXPECT().WritePacket([]byte("foobar"), remoteAddr, gomock.Any(), uint16(0), protocol.ECNCE).Return(6, nil),
		)
		require.NoError(t, c.Write([]byte("foobar"), 0, protocol.ECNCE))
	})

	t.Run("later call to sendmsg fails", func(t *testing.T) {
		rawConn := NewMockRawConn(gomock.NewController(t))
		rawConn.EXPECT().LocalAddr()
		rawConn.EXPECT().capabilities().AnyTimes()
		c := newSendConn(rawConn, remoteAddr, packetInfo{}, utils.DefaultLogger)
		rawConn.EXPECT().WritePacket([]byte("foobar"), remoteAddr, gomock.Any(), gomock.Any(), protocol.ECNCE).Return(0, errNotPermitted).Times(2)
		require.Error(t, c.Write([]byte("foobar"), 0, protocol.ECNCE))
	})
}

func TestSendConnRemoteAddrChange(t *testing.T) {
	ln1 := newUDPConnLocalhost(t)
	ln2 := newUDPConnLocalhost(t)

	c := newSendConn(
		&basicConn{PacketConn: newUDPConnLocalhost(t)},
		ln1.LocalAddr(),
		packetInfo{},
		utils.DefaultLogger,
	)

	require.NoError(t, c.Write([]byte("foobar"), 0, protocol.ECNUnsupported))
	ln1.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 1024)
	n, err := ln1.Read(b)
	require.NoError(t, err)
	require.Equal(t, "foobar", string(b[:n]))

	require.NoError(t, c.WriteTo([]byte("foobaz"), ln2.LocalAddr(), packetInfo{}))
	ln2.SetReadDeadline(time.Now().Add(time.Second))
	b = make([]byte, 1024)
	n, err = ln2.Read(b)
	require.NoError(t, err)
	require.Equal(t, "foobaz", string(b[:n]))

	c.ChangeRemoteAddr(ln2.LocalAddr(), packetInfo{})
	require.NoError(t, c.Write([]byte("lorem ipsum"), 0, protocol.ECNUnsupported))
	ln2.SetReadDeadline(time.Now().Add(time.Second))
	b = make([]byte, 1024)
	n, err = ln2.Read(b)
	require.NoError(t, err)
	require.Equal(t, "lorem ipsum", string(b[:n]))
}
