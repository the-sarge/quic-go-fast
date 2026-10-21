package quic

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatagramQueuePeekAndPop(t *testing.T) {
	var queued []struct{}
	queue := newDatagramQueue(func() { queued = append(queued, struct{}{}) }, utils.DefaultLogger)
	require.Nil(t, queue.Peek())
	require.Empty(t, queued)
	require.NoError(t, queue.Add(&wire.DatagramFrame{Data: []byte("foo")}))
	require.Len(t, queued, 1)
	require.Equal(t, &wire.DatagramFrame{Data: []byte("foo")}, queue.Peek())
	// calling peek again returns the same datagram
	require.Equal(t, &wire.DatagramFrame{Data: []byte("foo")}, queue.Peek())
	queue.Pop()
	require.Nil(t, queue.Peek())
}

func TestDatagramQueueSendQueueLength(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := newDatagramQueue(func() {}, utils.DefaultLogger)

		for range maxDatagramSendQueueLen {
			require.NoError(t, queue.Add(&wire.DatagramFrame{Data: []byte{0}}))
		}
		errChan := make(chan error, 1)
		go func() { errChan <- queue.Add(&wire.DatagramFrame{Data: []byte("foobar")}) }()

		synctest.Wait()

		select {
		case <-errChan:
			t.Fatal("expected to not receive error")
		default:
		}

		// peeking doesn't remove the datagram from the queue...
		require.NotNil(t, queue.Peek())
		synctest.Wait()
		select {
		case <-errChan:
			t.Fatal("expected to not receive error")
		default:
		}

		// ...but popping does
		queue.Pop()
		synctest.Wait()
		select {
		case err := <-errChan:
			require.NoError(t, err)
		default:
			t.Fatal("timeout")
		}
		// pop all the remaining datagrams
		for range maxDatagramSendQueueLen - 1 {
			queue.Pop()
		}
		f := queue.Peek()
		require.NotNil(t, f)
		require.Equal(t, &wire.DatagramFrame{Data: []byte("foobar")}, f)
	})
}

func TestDatagramQueueReceive(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)

	// receive frames that were received earlier
	queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("foo")})
	queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("bar")})
	data, err := queue.Receive(context.Background())
	require.NoError(t, err)
	require.Equal(t, []byte("foo"), data)
	data, err = queue.Receive(context.Background())
	require.NoError(t, err)
	require.Equal(t, []byte("bar"), data)
}

func TestDatagramReceiveOverflowNoAllocation(t *testing.T) {
	logger := utils.DefaultLogger.WithPrefix("overflow")
	logger.SetLogLevel(utils.LogLevelNothing)
	queue := newDatagramQueue(func() {}, logger)
	frame := &wire.DatagramFrame{Data: make([]byte, 1071)}
	for range maxDatagramRcvQueueLen {
		queue.HandleDatagramFrame(frame)
	}
	require.Zero(t, testing.AllocsPerRun(100, func() {
		queue.HandleDatagramFrame(frame)
	}))
}

func TestDatagramReceiveOverflowFIFO(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	for i := range maxDatagramRcvQueueLen + 1 {
		queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte{byte(i)}})
	}
	// Queued data takes precedence over cancellation, and overflow drops new data.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := range maxDatagramRcvQueueLen {
		data, err := queue.Receive(ctx)
		require.NoError(t, err)
		require.Equal(t, []byte{byte(i)}, data)
	}
	_, err := queue.Receive(ctx)
	require.ErrorIs(t, err, context.Canceled)
	// Admission resumes after draining, including an empty datagram.
	queue.HandleDatagramFrame(&wire.DatagramFrame{})
	data, err := queue.Receive(ctx)
	require.NoError(t, err)
	require.Empty(t, data)
}

// The queue admits a fixed 1,024 payloads and counts each one it refuses.
func TestDatagramReceiveOverflowCount(t *testing.T) {
	const depth, refused = 1024, 37
	require.Equal(t, depth, maxDatagramRcvQueueLen)
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	for i := range depth + refused {
		queue.HandleDatagramFrame(&wire.DatagramFrame{Data: binary.BigEndian.AppendUint16(nil, uint16(i))})
	}
	require.EqualValues(t, refused, queue.receiveDrops())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // A missing entry fails immediately instead of blocking the test.
	for i := range depth {
		data, err := queue.Receive(ctx)
		require.NoError(t, err)
		require.Equal(t, binary.BigEndian.AppendUint16(nil, uint16(i)), data)
	}
	_, err := queue.Receive(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, refused, queue.receiveDrops())
}

// Payloads discarded when the receive phase opens are not full-queue discards.
func TestDatagramReceiveDropsExcludeInvalidation(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	for range maxDatagramRcvQueueLen + 2 {
		queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("x")})
	}
	require.EqualValues(t, 2, queue.receiveDrops())
	queue.invalidateReceived()
	require.EqualValues(t, 2, queue.receiveDrops())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := queue.Receive(ctx)
	require.ErrorIs(t, err, context.Canceled, "invalidation empties the queue")
	// The emptied queue admits a full queue again, and counts only what follows.
	for range maxDatagramRcvQueueLen + 1 {
		queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("y")})
	}
	require.EqualValues(t, 3, queue.receiveDrops())
}

// Once delivered, a payload is no longer referenced by the queue's storage, so
// a drained queue pins no payload memory. The check reads the storage itself
// rather than waiting on the garbage collector.
func TestDatagramReceiveReleasesDelivered(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	const n = 5
	for i := range n {
		queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte{byte(i)}})
	}
	storage := queue.rcvQueue // shares the backing array the queue reslices
	require.Len(t, storage, n)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := range n {
		data, err := queue.Receive(ctx)
		require.NoError(t, err)
		require.Equal(t, []byte{byte(i)}, data)
		require.Nil(t, storage[i], "delivered slot %d still references its payload", i)
		for j := i + 1; j < n; j++ {
			require.Equal(t, []byte{byte(j)}, storage[j], "undelivered slot %d", j)
		}
	}
}

func TestDatagramReceivePayloadOwnership(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	input := bytes.Repeat([]byte{'a'}, 1071)
	frame := &wire.DatagramFrame{Data: input}
	queue.HandleDatagramFrame(frame)
	for i := range input {
		input[i] = 'b'
	}
	first, err := queue.Receive(context.Background())
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{'a'}, 1071), first)
	queue.HandleDatagramFrame(frame)
	second, err := queue.Receive(context.Background())
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{'b'}, 1071), second)
	second[0] = 'c'
	require.Equal(t, bytes.Repeat([]byte{'a'}, 1071), first)
	require.Equal(t, byte('b'), input[0])
}

func TestDatagramReceiveConcurrentProducerDrainer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := newDatagramQueue(func() {}, utils.DefaultLogger)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		go func() {
			for i := range maxDatagramRcvQueueLen {
				queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte{byte(i)}})
			}
		}()
		for i := range maxDatagramRcvQueueLen {
			data, err := queue.Receive(ctx)
			require.NoError(t, err)
			require.Equal(t, []byte{byte(i)}, data)
		}
	})
}

func TestDatagramReceiveRefillDrain(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // A missing entry fails immediately instead of blocking the test.
	for cycle := range 16 {
		for i := range maxDatagramRcvQueueLen {
			queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte{byte(cycle), byte(i)}})
		}
		// Leave most entries queued, then refill past the admission limit.
		const drained = 17
		for i := range drained {
			data, err := queue.Receive(ctx)
			require.NoError(t, err)
			require.Equal(t, []byte{byte(cycle), byte(i)}, data)
		}
		for i := range drained + 1 {
			queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte{byte(cycle), byte(maxDatagramRcvQueueLen + i)}})
		}
		for i := drained; i < maxDatagramRcvQueueLen+drained; i++ {
			data, err := queue.Receive(ctx)
			require.NoError(t, err)
			require.Equal(t, []byte{byte(cycle), byte(i)}, data)
		}
		_, err := queue.Receive(ctx)
		require.ErrorIs(t, err, context.Canceled)
	}
}

func TestDatagramQueueReceiveBlocking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := newDatagramQueue(func() {}, utils.DefaultLogger)

		// block until a new frame is received
		type result struct {
			data []byte
			err  error
		}
		resultChan := make(chan result, 1)
		go func() {
			data, err := queue.Receive(context.Background())
			resultChan <- result{data, err}
		}()

		synctest.Wait()

		select {
		case <-resultChan:
			t.Fatal("expected to not receive result")
		default:
		}
		queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("foobar")})
		synctest.Wait()
		select {
		case result := <-resultChan:
			require.NoError(t, result.err)
			require.Equal(t, []byte("foobar"), result.data)
		default:
			t.Fatal("should have received a datagram frame")
		}

		// unblock when the context is canceled
		ctx, cancel := context.WithCancel(context.Background())
		errChan := make(chan error, 1)
		go func() {
			_, err := queue.Receive(ctx)
			errChan <- err
		}()

		synctest.Wait()
		select {
		case <-errChan:
			t.Fatal("expected to not receive error")
		default:
		}

		cancel()
		synctest.Wait()

		select {
		case err := <-errChan:
			require.ErrorIs(t, err, context.Canceled)
		default:
			t.Fatal("should have received a context canceled error")
		}
	})
}

func TestDatagramQueueClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := newDatagramQueue(func() {}, utils.DefaultLogger)

		for range maxDatagramSendQueueLen {
			require.NoError(t, queue.Add(&wire.DatagramFrame{Data: []byte{0}}))
		}
		errChan1 := make(chan error, 1)
		go func() { errChan1 <- queue.Add(&wire.DatagramFrame{Data: []byte("foobar")}) }()
		errChan2 := make(chan error, 1)
		go func() {
			_, err := queue.Receive(context.Background())
			errChan2 <- err
		}()

		queue.CloseWithError(assert.AnError)
		synctest.Wait()

		select {
		case err := <-errChan1:
			require.ErrorIs(t, err, assert.AnError)
		default:
			t.Fatal("should have received an error")
		}

		select {
		case err := <-errChan2:
			require.ErrorIs(t, err, assert.AnError)
		default:
			t.Fatal("should have received an error")
		}
	})
}

// receiveQueueLen mirrors the number of received payloads awaiting Receive
// after every push, pop and clear, so handlePacket can read it without the
// queue's mutex.
func TestDatagramQueueReceiveLenMirrorsQueue(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	require.Zero(t, queue.receiveQueueLen())

	for i := range 3 {
		queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte{byte(i)}})
		require.Equal(t, i+1, queue.receiveQueueLen())
	}
	// A payload refused by a full queue is not mirrored.
	for range maxDatagramRcvQueueLen - 3 {
		queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("x")})
	}
	queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("overflow")})
	require.Equal(t, maxDatagramRcvQueueLen, queue.receiveQueueLen())

	_, err := queue.Receive(context.Background())
	require.NoError(t, err)
	require.Equal(t, maxDatagramRcvQueueLen-1, queue.receiveQueueLen())

	queue.invalidateReceived()
	require.Zero(t, queue.receiveQueueLen())
}
