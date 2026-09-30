package quic

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// phaseSegment is the result of one phase-aware read.
type phaseSegment struct {
	phase uint64
	data  []byte
}

// streamBytes returns the stream bytes [start, end); byte i has value i.
func streamBytes(start, end protocol.ByteCount) []byte {
	b := make([]byte, 0, end-start)
	for i := start; i < end; i++ {
		b = append(b, byte(i))
	}
	return b
}

// phasedStream is a receive stream on a connection with a closed receive
// phase. Frames are delivered in the test goroutine, which stands in for the
// connection loop.
type phasedStream struct {
	str   *ReceiveStream
	phase *receivePhase
}

func newPhasedStream(t *testing.T) *phasedStream {
	t.Helper()
	p := newReceivePhase()
	sender := NewMockStreamSender(gomock.NewController(t))
	sender.EXPECT().onHasStreamControlFrame(gomock.Any(), gomock.Any()).AnyTimes()
	sender.EXPECT().onHasConnectionData().AnyTimes()
	sender.EXPECT().onStreamCompleted(gomock.Any()).AnyTimes()
	str := newReceiveStream(42, sender, newTestStreamFlowController(42))
	str.gateReceivePhase(p)
	return &phasedStream{str: str, phase: p}
}

func (s *phasedStream) frame(t *testing.T, start, end protocol.ByteCount, fin bool) {
	t.Helper()
	f := &wire.StreamFrame{StreamID: 42, Offset: start, Data: streamBytes(start, end), Fin: fin}
	require.NoError(t, s.str.handleStreamFrame(f, monotime.Now()))
}

// open performs the owner's transition as the connection loop does.
func (s *phasedStream) open(t *testing.T) {
	t.Helper()
	req, err := s.phase.request(context.Background())
	require.NoError(t, err)
	s.phase.apply(func() {})
	<-req.done
	require.True(t, req.opened)
}

// readSegments performs phase-aware reads until want bytes were read or a
// read fails, and returns that error.
func (s *phasedStream) readSegments(t *testing.T, want int) ([]phaseSegment, error) {
	t.Helper()
	var segs []phaseSegment
	var total int
	for total < want {
		b := make([]byte, want-total)
		stop := interruptAfter(time.Second, func() { _ = s.str.SetReadDeadline(time.Now()) })
		n, phase, err := s.str.ReadReceivePhaseV1(b)
		stop()
		if n > 0 {
			segs = append(segs, phaseSegment{phase: phase, data: b[:n]})
		}
		total += n
		if err != nil {
			return segs, err
		}
		require.NotZero(t, n)
	}
	return segs, nil
}

func (s *phasedStream) requireSegments(t *testing.T, want []phaseSegment) {
	t.Helper()
	var total int
	for _, seg := range want {
		total += len(seg.data)
	}
	segs, err := s.readSegments(t, total)
	require.NoError(t, err)
	require.Equal(t, want, segs)
}

// Provenance belongs to byte positions: a gap admitted after the phase opened
// is open-phase data even though it is read before earlier-admitted bytes.
func TestReceivePhaseStreamGapFilledAfterOpen(t *testing.T) {
	s := newPhasedStream(t)
	s.frame(t, 10, 20, false)
	s.open(t)
	s.frame(t, 0, 10, false)
	s.requireSegments(t, []phaseSegment{
		{openReceivePhaseNumber, streamBytes(0, 10)},
		{closedReceivePhaseNumber, streamBytes(10, 20)},
	})
}

// A retransmission admitted after opening replaces queued frames without
// changing the phase of the bytes they held, whether it extends one frame or
// spans several gaps; a duplicate adds nothing.
func TestReceivePhaseStreamOverlapProvenance(t *testing.T) {
	t.Run("extends a queued frame", func(t *testing.T) {
		s := newPhasedStream(t)
		s.frame(t, 0, 10, false)
		s.open(t)
		s.frame(t, 0, 20, false)
		s.requireSegments(t, []phaseSegment{
			{closedReceivePhaseNumber, streamBytes(0, 10)},
			{openReceivePhaseNumber, streamBytes(10, 20)},
		})
	})

	t.Run("spans gaps", func(t *testing.T) {
		s := newPhasedStream(t)
		s.frame(t, 5, 10, false)
		s.frame(t, 15, 20, false)
		s.open(t)
		s.frame(t, 0, 25, false)
		s.frame(t, 5, 10, false)
		s.requireSegments(t, []phaseSegment{
			{openReceivePhaseNumber, streamBytes(0, 5)},
			{closedReceivePhaseNumber, streamBytes(5, 10)},
			{openReceivePhaseNumber, streamBytes(10, 15)},
			{closedReceivePhaseNumber, streamBytes(15, 20)},
			{openReceivePhaseNumber, streamBytes(20, 25)},
		})
	})
}

// Bytes the stream already dequeued for reading keep the phase in which they
// were admitted.
func TestReceivePhaseStreamReadAhead(t *testing.T) {
	s := newPhasedStream(t)
	s.frame(t, 0, 10, false)
	s.requireSegments(t, []phaseSegment{{closedReceivePhaseNumber, streamBytes(0, 3)}})
	s.open(t)
	s.frame(t, 10, 20, true)
	segs, err := s.readSegments(t, 17)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, []phaseSegment{
		{closedReceivePhaseNumber, streamBytes(3, 10)},
		{openReceivePhaseNumber, streamBytes(10, 20)},
	}, segs)
}

// A FIN or a reliable reset ends the stream as usual; every reliable byte is
// read, with its phase, before the terminal result, which releases provenance.
func TestReceivePhaseStreamTerminal(t *testing.T) {
	t.Run("FIN", func(t *testing.T) {
		s := newPhasedStream(t)
		s.frame(t, 0, 10, true)
		s.open(t)
		s.frame(t, 0, 10, true) // retransmitted after opening
		require.NotNil(t, s.str.frameQueue.openGaps)
		segs, err := s.readSegments(t, 10)
		require.ErrorIs(t, err, io.EOF)
		require.Equal(t, []phaseSegment{{closedReceivePhaseNumber, streamBytes(0, 10)}}, segs)
		n, _, err := s.str.ReadReceivePhaseV1(make([]byte, 1))
		require.Zero(t, n)
		require.ErrorIs(t, err, io.EOF)
		require.Nil(t, s.str.frameQueue.openGaps, "terminal cleanup releases provenance")
	})

	t.Run("reliable reset", func(t *testing.T) {
		s := newPhasedStream(t)
		s.frame(t, 0, 10, false)
		s.open(t)
		s.frame(t, 10, 20, false)
		require.NoError(t, s.str.handleResetStreamFrame(&wire.ResetStreamFrame{
			StreamID: 42, ErrorCode: 7, FinalSize: 30, ReliableSize: 20,
		}, monotime.Now()))
		segs, err := s.readSegments(t, 30)
		require.Equal(t, &StreamError{StreamID: 42, ErrorCode: 7, Remote: true}, err)
		require.Equal(t, []phaseSegment{
			{closedReceivePhaseNumber, streamBytes(0, 10)},
			{openReceivePhaseNumber, streamBytes(10, 20)},
		}, segs)
		require.Nil(t, s.str.frameQueue.openGaps, "terminal cleanup releases provenance")
	})
}

// Provenance never exceeds the sorter's existing gap limit, which continues
// to reject further fragmentation.
func TestReceivePhaseStreamBoundedFragmentation(t *testing.T) {
	s := newPhasedStream(t)
	const maxGaps = protocol.MaxStreamFrameSorterGaps
	// Single bytes at every other offset leave exactly maxGaps gaps.
	for i := protocol.ByteCount(1); i < maxGaps; i++ {
		s.frame(t, 2*i, 2*i+1, false)
	}
	s.open(t)
	s.frame(t, 0, 2, false)
	require.LessOrEqual(t, len(s.str.frameQueue.openGaps), maxGaps)
	s.requireSegments(t, []phaseSegment{
		{openReceivePhaseNumber, streamBytes(0, 2)},
		{closedReceivePhaseNumber, streamBytes(2, 3)},
	})

	s.frame(t, 2*maxGaps+1, 2*maxGaps+2, false)
	err := s.str.handleStreamFrame(&wire.StreamFrame{StreamID: 42, Offset: 2*maxGaps + 3, Data: []byte{0}}, monotime.Now())
	require.EqualError(t, err, "too many gaps in received data")
}

// Consumers read stream provenance with standard types only.
type receivePhaseReaderV1 interface {
	ReadReceivePhaseV1([]byte) (int, uint64, error)
}

var (
	_ receivePhaseReaderV1 = &Stream{}
	_ receivePhaseReaderV1 = &ReceiveStream{}
)

// handoffReader models the native handoff boundary: setup consumes exactly
// its bounded record in either phase, and application bytes require the
// open phase.
type handoffReader struct {
	str interface {
		receivePhaseReaderV1
		SetReadDeadline(time.Time) error
	}
}

func (r handoffReader) read(t *testing.T, n int) []phaseSegment {
	t.Helper()
	require.NoError(t, r.str.SetReadDeadline(time.Now().Add(5*time.Second)))
	var segs []phaseSegment
	for read := 0; read < n; {
		b := make([]byte, n-read)
		m, phase, err := r.str.ReadReceivePhaseV1(b)
		if m > 0 {
			if len(segs) > 0 && segs[len(segs)-1].phase == phase {
				segs[len(segs)-1].data = append(segs[len(segs)-1].data, b[:m]...)
			} else {
				segs = append(segs, phaseSegment{phase: phase, data: b[:m]})
			}
		}
		read += m
		if read < n {
			require.NoError(t, err)
		}
	}
	return segs
}

// awaitAdmitted returns the server's stream once n bytes were admitted.
func awaitAdmitted(t *testing.T, ctx context.Context, conn *Conn, n int) *Stream {
	t.Helper()
	str, err := conn.AcceptStream(ctx)
	require.NoError(t, err)
	require.NoError(t, str.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = str.Peek(make([]byte, n))
	require.NoError(t, err)
	return str
}

// Setup bytes admitted before the server opens its phase are read as closed
// phase, and later application bytes as open phase, never in one read. Reads
// return flow-control credit, so more than a stream window arrives.
func TestReceivePhaseStreamBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, server := phasePair{serverClosed: true}.dial(t, ctx)
	clientStr, err := client.OpenStreamSync(ctx)
	require.NoError(t, err)
	setup := streamBytes(0, 100)
	_, err = clientStr.Write(setup)
	require.NoError(t, err)
	serverStr := awaitAdmitted(t, ctx, server, len(setup))

	_, err = openReceivePhase(t, ctx, server)
	require.NoError(t, err)
	application := make([]byte, 2*protocol.DefaultInitialMaxStreamData)
	for i := range application {
		application[i] = byte(i % 251)
	}
	written := goWorker(t, ctx, func(ctx context.Context) error {
		stop := context.AfterFunc(ctx, func() { clientStr.CancelWrite(0) })
		defer stop()
		if _, err := clientStr.Write(application); err != nil {
			return err
		}
		return clientStr.Close()
	})

	r := handoffReader{str: serverStr}
	require.Equal(t, []phaseSegment{{closedReceivePhaseNumber, setup}}, r.read(t, len(setup)))
	require.Equal(t, []phaseSegment{{openReceivePhaseNumber, application}}, r.read(t, len(application)))
	require.NoError(t, <-written)
	n, _, err := serverStr.ReadReceivePhaseV1(make([]byte, 1))
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
}

// Application bytes coalesced with a setup record before the phase opened
// are reported as closed phase after the record is consumed, so the handoff
// terminates the connection instead of dropping or accepting them.
func TestReceivePhaseStreamEarlyApplicationRange(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, server := phasePair{serverClosed: true}.dial(t, ctx)
	clientStr, err := client.OpenStreamSync(ctx)
	require.NoError(t, err)
	setup, early := streamBytes(0, 40), streamBytes(40, 100)
	_, err = clientStr.Write(append(append([]byte{}, setup...), early...))
	require.NoError(t, err)
	serverStr := awaitAdmitted(t, ctx, server, len(setup)+len(early))

	_, err = openReceivePhase(t, ctx, server)
	require.NoError(t, err)
	r := handoffReader{str: serverStr}
	require.Equal(t, []phaseSegment{{closedReceivePhaseNumber, setup}}, r.read(t, len(setup)))
	app := r.read(t, len(early))
	require.Equal(t, []phaseSegment{{closedReceivePhaseNumber, early}}, app)
	serverStr.CancelRead(9)
	require.NoError(t, server.CloseWithError(9, "early application data"))

	_, err = client.AcceptStream(ctx)
	var appErr *ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, ApplicationErrorCode(9), appErr.ErrorCode)
	require.True(t, appErr.Remote)
}

// Unconfigured and false-configured connections have no stream provenance;
// the phase-aware read fails without consuming, and Read is unchanged.
func TestReceivePhaseStreamDefault(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprintf("configured %t", configured), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			serverTransport := &Transport{Conn: newUDPConnLocalhost(t)}
			t.Cleanup(func() { serverTransport.Close() })
			if configured {
				require.NoError(t, configureReceivePhases(t, serverTransport, false))
			}
			ln, err := serverTransport.Listen(testdata.GetTLSConfig(), nil)
			require.NoError(t, err)
			client, err := DialAddr(ctx, ln.Addr().String(), candidateClientTLS(), nil)
			require.NoError(t, err)
			t.Cleanup(func() { client.CloseWithError(0, "") })
			server, err := ln.Accept(ctx)
			require.NoError(t, err)

			clientStr, err := client.OpenStreamSync(ctx)
			require.NoError(t, err)
			data := streamBytes(0, 100)
			_, err = clientStr.Write(data)
			require.NoError(t, err)
			require.NoError(t, clientStr.Close())
			serverStr := awaitAdmitted(t, ctx, server, len(data))

			n, _, err := serverStr.ReadReceivePhaseV1(make([]byte, len(data)))
			require.Zero(t, n)
			require.ErrorIs(t, err, errNoReceivePhase)
			got, err := io.ReadAll(serverStr)
			require.NoError(t, err)
			require.Equal(t, data, got)
		})
	}
}
