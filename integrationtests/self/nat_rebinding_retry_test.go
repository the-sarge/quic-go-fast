package self_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

// This fixture correlates FIFO 1-RTT trace events with unbatched socket writes.
// It drops the first actual PATH_RESPONSE datagram without parsing ciphertext
// or adding a production test hook. Other client traffic continues to arrive.
type natResponseFilter struct {
	mx        sync.Mutex
	decisions []bool
	coalesced map[qlog.DatagramPayloadChecksum]bool
	selected  bool
	dropped   bool
	token     [8]byte
	err       error
}

func (f *natResponseFilter) trace(context.Context, bool, quic.ConnectionID) qlogwriter.Trace {
	return &events.Trace{Recorder: f}
}

func (f *natResponseFilter) Close() error { return nil }

func (f *natResponseFilter) RecordEvent(event qlogwriter.Event) {
	sent, ok := event.(qlog.PacketSent)
	if !ok || sent.Header.PacketType != qlog.PacketType1RTT {
		return
	}
	f.mx.Lock()
	defer f.mx.Unlock()
	drop := false
	for _, frame := range sent.Frames {
		if response, ok := frame.Frame.(*qlog.PathResponseFrame); ok && !f.selected {
			f.selected = true
			f.token = response.Data
			drop = true
		}
	}
	if sent.DatagramPayloadChecksum != 0 {
		if f.coalesced == nil {
			f.coalesced = make(map[qlog.DatagramPayloadChecksum]bool)
		}
		f.coalesced[sent.DatagramPayloadChecksum] = drop
	} else {
		f.decisions = append(f.decisions, drop)
	}
}

type natFilteredPacketConn struct {
	net.PacketConn
	filter *natResponseFilter
}

func (c *natFilteredPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	f := c.filter
	f.mx.Lock()
	checksum := qlog.CalculateDatagramPayloadChecksum(b)
	drop, coalesced := f.coalesced[checksum]
	if coalesced {
		delete(f.coalesced, checksum)
	} else if len(b) > 0 && b[0]&0x80 == 0 {
		if len(f.decisions) == 0 {
			f.err = errors.New("untraced short-header socket write")
		} else {
			drop = f.decisions[0]
			f.decisions = f.decisions[1:]
		}
	}
	f.dropped = f.dropped || drop
	f.mx.Unlock()
	if drop {
		return len(b), nil
	}
	return c.PacketConn.WriteTo(b, addr)
}

func TestNATRebindingUnansweredChallengeRetry(t *testing.T) {
	filter := &natResponseFilter{}
	f := newNATRebindingFixtureWithResponseFilter(t, filter)
	oldAddr := f.serverConn.RemoteAddr()
	ctx, cancel := context.WithTimeout(t.Context(), scaleDuration(5*time.Second))
	defer cancel()
	f.ctx = ctx
	// Keep candidate traffic eligible after the withheld response, even if the
	// server's old destination stops delivering useful stream bytes.
	probeStream, err := f.conn.OpenUniStream()
	require.NoError(t, err)
	stop := make(chan struct{})
	probeDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(scaleDuration(20 * time.Millisecond))
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				probeDone <- nil
				return
			case <-ticker.C:
				if _, err := probeStream.Write([]byte{1}); err != nil {
					select {
					case <-stop:
						probeDone <- nil
					default:
						probeDone <- err
					}
					return
				}
			}
		}
	}()
	defer func() {
		close(stop)
		probeStream.CancelWrite(0)
		select {
		case err := <-probeDone:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("candidate writer outlived test cleanup")
		}
	}()
	f.startWriter(t, nil, nil)
	data, err := f.receive()
	require.NoError(t, err)
	require.Equal(t, PRData, data)
	require.NotEqual(t, oldAddr.String(), f.serverConn.RemoteAddr().String(), "server switched to the validated candidate")
	filter.mx.Lock()
	dropped, token, filterErr := filter.dropped, filter.token, filter.err
	filter.mx.Unlock()
	require.NoError(t, filterErr)
	require.True(t, dropped, "the first PATH_RESPONSE reached the deliberate socket drop")
	var challenges [][8]byte
	var challengeConnIDs []quic.ConnectionID
	for _, packet := range f.trace.getSentShortHeaderPackets() {
		for _, frame := range packet.frames {
			if challenge, ok := frame.Frame.(*qlog.PathChallengeFrame); ok {
				challenges = append(challenges, challenge.Data)
				challengeConnIDs = append(challengeConnIDs, packet.hdr.DestConnectionID)
			}
		}
	}
	require.GreaterOrEqual(t, len(challenges), 2, "a single challenge must not satisfy this test")
	require.Equal(t, token, challenges[0])
	require.Equal(t, challengeConnIDs[0], challengeConnIDs[1], "retry retains the candidate's connection ID")
	foundLaterResponse := false
	for _, packet := range f.trace.getRcvdShortHeaderPackets() {
		for _, frame := range packet.frames {
			if response, ok := frame.Frame.(*qlog.PathResponseFrame); ok {
				require.NotEqual(t, token, response.Data, "the withheld response was never received")
				require.Contains(t, challenges[1:], response.Data)
				foundLaterResponse = true
			}
		}
	}
	require.True(t, foundLaterResponse)
}
