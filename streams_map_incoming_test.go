package quic

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStream struct {
	id protocol.StreamID

	closed                bool
	closeErr              error
	sendWindow            protocol.ByteCount
	supportsResetStreamAt bool
}

func (s *mockStream) closeForShutdown(err error) {
	s.closed = true
	s.closeErr = err
}

func (s *mockStream) updateSendWindow(limit protocol.ByteCount) {
	s.sendWindow = limit
}

func (s *mockStream) enableResetStreamAt() {
	s.supportsResetStreamAt = true
}

func TestStreamsMapIncomingSparseStreamAllocation(t *testing.T) {
	for _, perspective := range []protocol.Perspective{protocol.PerspectiveClient, protocol.PerspectiveServer} {
		for _, streamType := range []protocol.StreamType{protocol.StreamTypeBidi, protocol.StreamTypeUni} {
			t.Run(fmt.Sprintf("%s/%d", perspective, streamType), func(t *testing.T) {
				var created int
				m := newIncomingStreamsMap(
					streamType,
					func(id protocol.StreamID) *mockStream {
						created++
						// Stop an eager implementation before it exhausts memory.
						require.LessOrEqual(t, created, 1, "one sparse ID must only allocate one stream")
						return &mockStream{id: id}
					},
					uint64(protocol.MaxStreamCount),
					func(wire.Frame) {},
					perspective,
				)
				id := protocol.MaxStreamCount.StreamID(streamType, perspective.Opposite())
				str, err := m.GetOrOpenStream(id)
				require.NoError(t, err)
				require.Equal(t, id, str.id)
				require.Equal(t, 1, created)
				require.Len(t, m.streams, 1)
				again, err := m.GetOrOpenStream(id)
				require.NoError(t, err)
				require.Same(t, str, again)
				require.Equal(t, 1, created)
				m.CloseWithError(assert.AnError)
				require.True(t, str.closed)
				_, err = m.AcceptStream(context.Background())
				require.ErrorIs(t, err, assert.AnError)
				require.Equal(t, 1, created)
			})
		}
	}
}

func TestStreamsMapIncomingSparseStreamLifecycle(t *testing.T) {
	for _, perspective := range []protocol.Perspective{protocol.PerspectiveClient, protocol.PerspectiveServer} {
		for _, streamType := range []protocol.StreamType{protocol.StreamTypeBidi, protocol.StreamTypeUni} {
			t.Run(fmt.Sprintf("%s/%d", perspective, streamType), func(t *testing.T) {
				var created int
				var frames []wire.Frame
				m := newIncomingStreamsMap(
					streamType,
					func(id protocol.StreamID) *mockStream {
						created++
						return &mockStream{id: id}
					},
					6,
					func(f wire.Frame) { frames = append(frames, f) },
					perspective,
				)
				first := protocol.StreamNum(1).StreamID(streamType, perspective.Opposite())
				last, err := m.GetOrOpenStream(first + 20)
				require.NoError(t, err)
				middle, err := m.GetOrOpenStream(first + 8)
				require.NoError(t, err)
				require.Equal(t, 2, created)

				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				str, err := m.AcceptStream(ctx)
				require.NoError(t, err)
				require.Equal(t, first, str.id)
				require.Equal(t, 3, created)
				again, err := m.GetOrOpenStream(first)
				require.NoError(t, err)
				require.Same(t, str, again)

				// An implicit stream still consumes credit: deleting one accepted
				// stream must not release credit for the rest of the sparse gap.
				require.NoError(t, m.DeleteStream(first))
				require.Equal(t, []wire.Frame{&wire.MaxStreamsFrame{Type: streamType, MaxStreamNum: 7}}, frames)
				str, err = m.GetOrOpenStream(first)
				require.NoError(t, err)
				require.Nil(t, str)

				// A completed stream waiting for acceptance must not be recreated.
				require.NoError(t, m.DeleteStream(first+20))
				str, err = m.GetOrOpenStream(first + 20)
				require.NoError(t, err)
				require.Nil(t, str)
				require.Len(t, frames, 1)

				str, err = m.AcceptStream(ctx)
				require.NoError(t, err)
				require.Equal(t, first+4, str.id)
				str, err = m.AcceptStream(ctx)
				require.NoError(t, err)
				require.Same(t, middle, str)
				for _, id := range []protocol.StreamID{first + 12, first + 16} {
					str, err = m.AcceptStream(ctx)
					require.NoError(t, err)
					require.Equal(t, id, str.id)
				}
				str, err = m.AcceptStream(ctx)
				require.NoError(t, err)
				require.Same(t, last, str)
				require.Equal(t, 6, created)
				require.Len(t, frames, 2)
				require.Equal(t, &wire.MaxStreamsFrame{Type: streamType, MaxStreamNum: 8}, frames[1])
				require.ErrorIs(t, m.DeleteStream(first+20), &qerr.TransportError{ErrorCode: qerr.StreamStateError})

				// Unopened credit is not available to AcceptStream.
				cancel()
				_, err = m.AcceptStream(ctx)
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, 6, created)
				m.CloseWithError(assert.AnError)
				require.True(t, middle.closed)
				require.ErrorIs(t, middle.closeErr, assert.AnError)
				_, err = m.AcceptStream(context.Background())
				require.ErrorIs(t, err, assert.AnError)
			})
		}
	}
}

func TestStreamsMapIncomingSparseStreamConcurrentAccept(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const num = 8
		m := newIncomingStreamsMap(
			protocol.StreamTypeUni,
			func(id protocol.StreamID) *mockStream { return &mockStream{id: id} },
			num,
			func(wire.Frame) {},
			protocol.PerspectiveServer,
		)
		type result struct {
			stream *mockStream
			err    error
		}
		results := make(chan result, num)
		for range num {
			go func() {
				str, err := m.AcceptStream(context.Background())
				results <- result{stream: str, err: err}
			}()
		}
		synctest.Wait()
		require.Empty(t, results)
		_, err := m.GetOrOpenStream(protocol.StreamNum(num).StreamID(protocol.StreamTypeUni, protocol.PerspectiveClient))
		require.NoError(t, err)
		synctest.Wait()
		require.Len(t, results, num)
		seen := make(map[protocol.StreamID]bool)
		for range num {
			r := <-results
			require.NoError(t, r.err)
			require.False(t, seen[r.stream.id])
			seen[r.stream.id] = true
		}
	})
}

func TestStreamsMapIncomingGettingStreams(t *testing.T) {
	t.Run("client", func(t *testing.T) {
		testStreamsMapIncomingGettingStreams(t, protocol.PerspectiveClient, protocol.FirstIncomingUniStreamClient)
	})
	t.Run("server", func(t *testing.T) {
		testStreamsMapIncomingGettingStreams(t, protocol.PerspectiveServer, protocol.FirstIncomingUniStreamServer)
	})
}

func testStreamsMapIncomingGettingStreams(t *testing.T, perspective protocol.Perspective, firstStream protocol.StreamID) {
	var newStreamCounter int
	const maxNumStreams = 10
	m := newIncomingStreamsMap(
		protocol.StreamTypeUni,
		func(id protocol.StreamID) *mockStream {
			newStreamCounter++
			return &mockStream{id: id}
		},
		maxNumStreams,
		func(f wire.Frame) {},
		perspective,
	)

	// Lower streams are implicitly opened without allocating their state.
	str, err := m.GetOrOpenStream(firstStream + 4)
	require.NoError(t, err)
	require.NotNil(t, str)
	require.Equal(t, 1, newStreamCounter)
	require.Equal(t, firstStream+4, str.id)
	// accept one of the streams
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	str, err = m.AcceptStream(ctx)
	require.NoError(t, err)
	require.Equal(t, firstStream, str.id)
	require.Equal(t, 2, newStreamCounter)
	// open some more streams
	str, err = m.GetOrOpenStream(firstStream + 16)
	require.NoError(t, err)
	require.Equal(t, 3, newStreamCounter)
	require.Equal(t, firstStream+16, str.id)
	// and accept all of them
	for i := 1; i < 5; i++ {
		str, err := m.AcceptStream(ctx)
		require.NoError(t, err)
		require.Equal(t, firstStream+4*protocol.StreamID(i), str.id)
	}

	_, err = m.GetOrOpenStream(firstStream + 4*maxNumStreams - 4)
	require.NoError(t, err)
	_, err = m.GetOrOpenStream(firstStream + 4*maxNumStreams)
	require.ErrorIs(t, err, &qerr.TransportError{ErrorCode: qerr.StreamLimitError})
	require.ErrorContains(t, err, "peer tried to open stream")
	require.Equal(t, 6, newStreamCounter)
}

func TestStreamsMapIncomingAcceptingStreams(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := newIncomingStreamsMap(
			protocol.StreamTypeUni,
			func(id protocol.StreamID) *mockStream { return &mockStream{id: id} },
			5,
			func(f wire.Frame) {},
			protocol.PerspectiveClient,
		)

		// AcceptStream should respect the context
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		errChan := make(chan error, 1)
		go func() {
			_, err := m.AcceptStream(ctx)
			errChan <- err
		}()

		synctest.Wait()

		select {
		case <-errChan:
			t.Fatal("AcceptStream should not return")
		default:
		}

		cancel()
		synctest.Wait()
		select {
		case err := <-errChan:
			require.Equal(t, context.Canceled, err)
		default:
			t.Fatal("timeout")
		}

		// AcceptStream should block if there are no streams available
		go func() {
			_, err := m.AcceptStream(context.Background())
			errChan <- err
		}()

		synctest.Wait()

		select {
		case <-errChan:
			t.Fatal("AcceptStream should block")
		default:
		}

		_, err := m.GetOrOpenStream(protocol.FirstIncomingUniStreamClient)
		require.NoError(t, err)

		synctest.Wait()

		select {
		case err := <-errChan:
			require.NoError(t, err)
		default:
			t.Fatal("timeout")
		}
	})
}

func TestStreamsMapIncomingDeletingStreams(t *testing.T) {
	t.Run("client", func(t *testing.T) {
		testStreamsMapIncomingDeletingStreams(t, protocol.PerspectiveClient, protocol.FirstIncomingUniStreamClient)
	})
	t.Run("server", func(t *testing.T) {
		testStreamsMapIncomingDeletingStreams(t, protocol.PerspectiveServer, protocol.FirstIncomingUniStreamServer)
	})
}

func testStreamsMapIncomingDeletingStreams(t *testing.T, perspective protocol.Perspective, firstStream protocol.StreamID) {
	var frameQueue []wire.Frame
	m := newIncomingStreamsMap(
		protocol.StreamTypeUni,
		func(id protocol.StreamID) *mockStream { return &mockStream{id: id} },
		5,
		func(f wire.Frame) { frameQueue = append(frameQueue, f) },
		perspective,
	)
	err := m.DeleteStream(firstStream + 1337*4)
	require.ErrorIs(t, err, &qerr.TransportError{ErrorCode: qerr.StreamStateError})
	require.ErrorContains(t, err, "tried to delete unknown incoming stream")

	s, err := m.GetOrOpenStream(firstStream + 4)
	require.NoError(t, err)
	require.NotNil(t, s)
	// delete the stream
	require.NoError(t, m.DeleteStream(firstStream+4))
	require.Empty(t, frameQueue)
	// it's not returned by GetOrOpenStream anymore
	s, err = m.GetOrOpenStream(firstStream + 4)
	require.NoError(t, err)
	require.Nil(t, s)

	// AcceptStream still returns this stream
	str, err := m.AcceptStream(context.Background())
	require.NoError(t, err)
	require.Equal(t, firstStream, str.id)
	require.Empty(t, frameQueue)

	str, err = m.AcceptStream(context.Background())
	require.NoError(t, err)
	require.Equal(t, firstStream+4, str.id)
	// now the stream is deleted and new stream credit is issued
	require.Len(t, frameQueue, 1)
	require.Equal(t, &wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: 6}, frameQueue[0])
	frameQueue = frameQueue[:0]

	require.NoError(t, m.DeleteStream(firstStream))
	require.Len(t, frameQueue, 1)
	require.Equal(t, &wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: 7}, frameQueue[0])
}

// There's a maximum number that can be encoded in a MAX_STREAMS frame.
// Since the stream limit is configurable by the user, we can't rely on this number
// being high enough that it will never be reached in practice.
func TestStreamsMapIncomingDeletingStreamsWithHighLimits(t *testing.T) {
	t.Run("client", func(t *testing.T) {
		testStreamsMapIncomingDeletingStreamsWithHighLimits(t, protocol.PerspectiveClient, protocol.FirstIncomingUniStreamClient)
	})
	t.Run("server", func(t *testing.T) {
		testStreamsMapIncomingDeletingStreamsWithHighLimits(t, protocol.PerspectiveServer, protocol.FirstIncomingUniStreamServer)
	})
}

func testStreamsMapIncomingDeletingStreamsWithHighLimits(t *testing.T, pers protocol.Perspective, firstStream protocol.StreamID) {
	var frameQueue []wire.Frame
	m := newIncomingStreamsMap(
		protocol.StreamTypeUni,
		func(id protocol.StreamID) *mockStream { return &mockStream{id: id} },
		uint64(protocol.MaxStreamCount-2),
		func(f wire.Frame) { frameQueue = append(frameQueue, f) },
		pers,
	)

	// open a bunch of streams
	_, err := m.GetOrOpenStream(firstStream + 16)
	require.NoError(t, err)
	// accept all streams
	for range 5 {
		_, err := m.AcceptStream(context.Background())
		require.NoError(t, err)
	}
	require.Empty(t, frameQueue)
	require.NoError(t, m.DeleteStream(firstStream+12))
	require.Len(t, frameQueue, 1)
	require.Equal(t,
		&wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: protocol.MaxStreamCount - 1},
		frameQueue[0],
	)
	require.NoError(t, m.DeleteStream(firstStream+8))
	require.Len(t, frameQueue, 2)
	require.Equal(t,
		&wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: protocol.MaxStreamCount},
		frameQueue[1],
	)
	// at this point, we can't increase the stream limit any further, so no more MAX_STREAMS frames will be sent
	require.NoError(t, m.DeleteStream(firstStream+4))
	require.NoError(t, m.DeleteStream(firstStream))
	require.Len(t, frameQueue, 2)
}

func TestStreamsMapIncomingClosing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := newIncomingStreamsMap(
			protocol.StreamTypeUni,
			func(id protocol.StreamID) *mockStream { return &mockStream{id: id} },
			5,
			func(f wire.Frame) {},
			protocol.PerspectiveServer,
		)

		var streams []*mockStream
		_, err := m.GetOrOpenStream(protocol.FirstIncomingUniStreamServer + 8)
		require.NoError(t, err)
		for range 3 {
			str, err := m.AcceptStream(context.Background())
			require.NoError(t, err)
			streams = append(streams, str)
		}

		errChan := make(chan error, 1)
		go func() {
			_, err := m.AcceptStream(context.Background())
			errChan <- err
		}()

		m.CloseWithError(assert.AnError)
		synctest.Wait()

		// accepted streams should be closed
		for _, str := range streams {
			require.True(t, str.closed)
			require.ErrorIs(t, str.closeErr, assert.AnError)
		}
		// AcceptStream should return the error
		select {
		case err := <-errChan:
			require.ErrorIs(t, err, assert.AnError)
		default:
			t.Fatal("timeout")
		}
	})
}

func TestStreamsMapIncomingRandomized(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const num = 1000

		streamType := []protocol.StreamType{protocol.StreamTypeUni, protocol.StreamTypeBidi}[rand.IntN(2)]
		firstStream := protocol.FirstIncomingUniStreamServer
		if streamType == protocol.StreamTypeBidi {
			firstStream = protocol.FirstIncomingBidiStreamServer
		}

		m := newIncomingStreamsMap(
			streamType,
			func(id protocol.StreamID) *mockStream { return &mockStream{id: id} },
			num,
			func(f wire.Frame) {},
			protocol.PerspectiveServer,
		)

		ids := make([]protocol.StreamID, num)
		for i := range num {
			ids[i] = firstStream + 4*protocol.StreamID(i)
		}
		rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })

		errChan1 := make(chan error, 1)
		go func() {
			for range num {
				if _, err := m.AcceptStream(context.Background()); err != nil {
					errChan1 <- err
					return
				}
			}
			close(errChan1)
		}()

		errChan2 := make(chan error, 1)
		go func() {
			for i := range num {
				if _, err := m.GetOrOpenStream(ids[i]); err != nil {
					errChan2 <- err
					return
				}
			}
			close(errChan2)
		}()

		synctest.Wait()

		select {
		case err := <-errChan1:
			require.NoError(t, err)
		default:
			t.Fatal("should have accepted all streams")
		}
		select {
		case err := <-errChan2:
			require.NoError(t, err)
		default:
			t.Fatal("should have opened all streams")
		}
	})
}
