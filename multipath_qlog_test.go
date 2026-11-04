//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/qlogwriter/jsontext"
	"github.com/stretchr/testify/require"
)

// qlogSink is a real trace's far end. A gated sink holds the writer in RecordEvent.
type qlogSink struct {
	mu     sync.Mutex
	events []qlogwriter.Event
	gate   chan struct{}
	closed chan struct{}
}

func newQlogSink() *qlogSink { return &qlogSink{closed: make(chan struct{})} }

func (s *qlogSink) block() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gate = make(chan struct{})
}

func (s *qlogSink) AddProducer() qlogwriter.Recorder { return s }
func (s *qlogSink) SupportsSchemas(string) bool      { return true }

func (s *qlogSink) RecordEvent(event qlogwriter.Event) {
	s.mu.Lock()
	gate := s.gate
	s.mu.Unlock()
	if gate != nil {
		<-gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *qlogSink) Close() error {
	close(s.closed)
	return nil
}

func (s *qlogSink) recorded() []qlogwriter.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]qlogwriter.Event(nil), s.events...)
}

func (s *qlogSink) tracer(context.Context, bool, ConnectionID) qlogwriter.Trace { return s }

func encodeEvent(t *testing.T, event qlogwriter.Event) map[string]any {
	t.Helper()
	var b bytes.Buffer
	enc := jsontext.NewEncoder(&b)
	require.NoError(t, event.Encode(enc, time.Time{}))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(b.Bytes(), &decoded))
	return decoded
}

// TestPathQlogDistinctIdentities keeps equal packet numbers apart across paths, for
// sends, receipts, losses and lifecycle.
func TestPathQlogDistinctIdentities(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		sink := newQlogSink()
		f := newReliableFixtureWith(t, protocol.PerspectiveClient, sink.tracer)
		defer func() {
			require.NoError(t, f.c.qlogger.Close())
			<-sink.closed
		}()
		require.True(t, f.receive(t, 1, admissionTupleA, 0, &wire.PingFrame{}))
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		synctest.Wait()

		sent := make(map[uint64]bool)
		var received, lifecycle []*qlog.PathIdentity
		for _, event := range sink.recorded() {
			switch e := event.(type) {
			case qlog.PacketSent:
				if e.Header.PacketType == qlog.PacketType1RTT {
					require.NotNil(t, e.Path)
					sent[e.Path.PathID] = true
					encodeEvent(t, e) // every path frame has an encoder
				}
			case qlog.PacketReceived:
				if e.Path != nil {
					received = append(received, e.Path)
				}
			case qlog.PathLifecycleUpdated:
				lifecycle = append(lifecycle, &e.Path)
				require.Equal(t, uint64(1), e.Path.PathID)
			}
		}
		require.True(t, sent[0] && sent[1], "both paths sent: %v", sent)
		require.Equal(t, uint64(1), received[len(received)-1].PathID)
		require.NotEmpty(t, lifecycle)
	})
}

// TestPathQlogLossAndMTUAttribution names both paths' losses and MTU updates.
func TestPathQlogLossAndMTUAttribution(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		sink := newQlogSink()
		f := newReliableFixtureWith(t, protocol.PerspectiveClient, sink.tracer)
		defer func() {
			require.NoError(t, f.c.qlogger.Close())
			<-sink.closed
		}()
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.socket.writes = nil
		f.write(t, 6000)
		f.admissionFixture.pass(t)
		f.pass(t)
		writes := f.parseWrites(t, 1)
		require.GreaterOrEqual(t, len(writes), 5)
		// The packets three behind the acknowledged one fall to the reordering threshold.
		f.ackPath(t, 1, 1, writes[len(writes)-1:])
		f.pass(t)

		recorder := f.c.qlogger
		recorder.RecordEvent(qlog.MTUUpdated{Value: 1300})
		pathMTURecorder{Recorder: recorder, member: f.c.pathCredit.members[1]}.RecordEvent(qlog.MTUUpdated{Value: 1400})
		synctest.Wait()

		lost := 0
		mtus := make(map[int]uint64)
		for _, event := range sink.recorded() {
			switch e := event.(type) {
			case qlog.PacketLost:
				require.NotNil(t, e.Path)
				require.Equal(t, uint64(1), e.Path.PathID)
				lost++
			case qlog.MTUUpdated:
				require.NotNil(t, e.Path)
				mtus[e.Value] = e.Path.PathID
			}
		}
		require.NotZero(t, lost)
		require.Equal(t, map[int]uint64{1300: 0, 1400: 1}, mtus)

		// A client's Handshake traffic can outlast activation, until it is confirmed.
		header := &wire.ExtendedHeader{Header: wire.Header{Type: protocol.PacketTypeHandshake, Version: protocol.Version1}, PacketNumber: 1 << 20}
		f.c.logLongHeaderPacket(&longHeaderPacket{context: ackhandler.PacketContext{TupleGeneration: 4}, header: header}, protocol.ECNNon, 0)
		ping := []byte{byte(wire.FrameTypePing)}
		require.NoError(t, f.c.handleUnpackedLongHeaderPacket(&unpackedPacket{hdr: header, encryptionLevel: protocol.Encryption1RTT, data: ping}, protocol.ECNNon, monotime.Now(), 0, 100))
		synctest.Wait()
		var sentLong, receivedLong *qlog.PathIdentity
		for _, event := range sink.recorded() {
			switch e := event.(type) {
			case qlog.PacketSent:
				if e.Header.PacketType == qlog.PacketTypeHandshake {
					sentLong = e.Path
				}
			case qlog.PacketReceived:
				if e.Header.PacketType == qlog.PacketTypeHandshake {
					receivedLong = e.Path
				}
			}
		}
		require.Equal(t, &qlog.PathIdentity{PathID: 0, TupleGeneration: 4}, sentLong)
		require.Equal(t, &qlog.PathIdentity{PathID: 0}, receivedLong)
	})
}

// TestPathQlogBlockedSinkDropsMetadata keeps recovery recording and connection
// shutdown moving, with no progress at the sink.
func TestPathQlogBlockedSinkDropsMetadata(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		sink := newQlogSink()
		f := newReliableFixtureWith(t, protocol.PerspectiveClient, sink.tracer)
		recorder := f.c.qlogger.(*connRecorder)
		synctest.Wait()
		sink.block()
		for range 2 * qlogBufferSize {
			f.c.qlogger.RecordEvent(qlog.PTOCountUpdated{})
		}
		f.write(t, 1000)
		f.pass(t) // recovery and the packer record through the same recorder
		buffer := recorder.buffered.Load()
		require.NotNil(t, buffer)
		require.NotZero(t, buffer.dropped.Load())

		f.c.sentFirstPacket = true
		f.closeLocally(t, "blocked")
		require.Len(t, f.closeWrites(t, 1), 1)
		require.NoError(t, f.c.qlogger.Close(), "close doesn't wait for the sink")
		select {
		case <-sink.closed:
			t.Fatal("the sink is still blocked")
		default:
		}
		close(sink.gate)
		synctest.Wait()
		<-sink.closed
	})
}

// TestConnRecorderOrdinaryIsSynchronous preserves ordinary recording and its JSON.
func TestConnRecorderOrdinaryIsSynchronous(t *testing.T) {
	sink := newQlogSink()
	recorder := newConnRecorder(sink)
	event := qlog.MTUUpdated{Value: 1200, Done: true}
	recorder.RecordEvent(event)
	require.Equal(t, []qlogwriter.Event{event}, sink.recorded(), "recorded before returning")
	require.Equal(t, map[string]any{"mtu": 1200.0, "done": true}, encodeEvent(t, event), "no path field")
	require.NoError(t, recorder.Close())
	<-sink.closed
	require.Nil(t, newConnRecorder(nil))
}

func TestPathQlogFrameEncoding(t *testing.T) {
	for _, frame := range []wire.Frame{
		&wire.PathAbandonFrame{PathID: 1},
		&wire.PathStatusFrame{PathID: 1, SequenceNumber: 2, Backup: true},
		&wire.PathStatusFrame{PathID: 1, SequenceNumber: 3},
		&wire.PathNewConnectionIDFrame{PathID: 1, ConnectionID: protocol.ParseConnectionID([]byte{1, 2, 3, 4})},
		&wire.PathRetireConnectionIDFrame{PathID: 1},
		&wire.MaxPathIDFrame{MaximumPathID: 2},
		&wire.PathsBlockedFrame{MaximumPathID: 2},
		&wire.PathCIDsBlockedFrame{PathID: 1, NextSequenceNumber: 4},
		&wire.AckFrame{HasPathID: true, PathID: 1, AckRanges: []wire.AckRange{{Smallest: 1, Largest: 1}}},
	} {
		encoded := encodeEvent(t, qlog.PacketSent{Frames: []qlog.Frame{toQlogFrame(frame)}})
		require.Len(t, encoded["frames"], 1)
	}
}
