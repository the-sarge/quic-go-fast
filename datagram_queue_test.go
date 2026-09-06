package quic

import (
	"bytes"
	"context"
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
		// Leave most entries queued, then refill across the wrap and capacity boundary.
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
