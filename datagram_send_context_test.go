package quic

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type datagramAdmissionContext struct {
	context.Context
	checks int
	woken  chan struct{}
	resume chan struct{}
}

func TestConnectionSendDatagramContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := newDatagramQueue(func() {}, utils.DefaultLogger)
		conn := &Conn{datagramQueue: queue, version: protocol.Version1, peerParams: &wire.TransportParameters{MaxDatagramFrameSize: wire.MaxDatagramSize}}
		conn.maxPayloadSizeEstimate.Store(1200)
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(assert.AnError)
		require.ErrorIs(t, conn.SendDatagramContext(ctx, make([]byte, 1<<16)), context.Canceled)
		conn.peerParams.MaxDatagramFrameSize = 0
		require.ErrorIs(t, conn.SendDatagramContext(ctx, nil), context.Canceled)
		require.EqualError(t, conn.SendDatagramContext(context.Background(), nil), "datagram support disabled")
		conn.peerParams.MaxDatagramFrameSize = wire.MaxDatagramSize
		var tooLarge *DatagramTooLargeError
		require.ErrorAs(t, conn.SendDatagramContext(context.Background(), make([]byte, 1201)), &tooLarge)
		require.EqualValues(t, 1200, tooLarge.MaxDatagramPayloadSize)
		require.Nil(t, queue.Peek())

		payload := []byte("owned")
		require.NoError(t, conn.SendDatagramContext(context.Background(), payload))
		clear(payload)
		require.Equal(t, []byte("owned"), queue.Peek().Data)
		queue.Pop()
		for range maxDatagramSendQueueLen {
			require.NoError(t, conn.SendDatagram([]byte("queued")))
		}
		deadline, stop := context.WithTimeout(context.Background(), time.Millisecond)
		defer stop()
		require.ErrorIs(t, conn.SendDatagramContext(deadline, []byte("expired")), context.DeadlineExceeded)
		require.Equal(t, maxDatagramSendQueueLen, queue.sendQueue.Len())
		queue.Pop()
		require.NoError(t, conn.SendDatagramContext(context.Background(), []byte("reused")))
		for range maxDatagramSendQueueLen - 1 {
			require.Equal(t, []byte("queued"), queue.Peek().Data)
			queue.Pop()
		}
		require.Equal(t, []byte("reused"), queue.Peek().Data)
		queue.Pop()
		require.Nil(t, queue.Peek())
		queue.CloseWithError(assert.AnError)
		require.ErrorIs(t, conn.SendDatagramContext(context.Background(), nil), assert.AnError)
		require.ErrorAs(t, conn.SendDatagramContext(context.Background(), make([]byte, 1201)), &tooLarge)
	})
}

func (ctx *datagramAdmissionContext) Err() error {
	ctx.checks++
	if ctx.checks == 2 {
		close(ctx.woken)
		<-ctx.resume
	}
	return ctx.Context.Err()
}

func TestDatagramQueueCanceledNotifiedWaiter(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "contextual survivor"
		if legacy {
			name = "legacy survivor"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				queue := newDatagramQueue(func() {}, utils.DefaultLogger)
				defer queue.CloseWithError(assert.AnError)
				for index := range maxDatagramSendQueueLen {
					require.NoError(t, queue.Add(&wire.DatagramFrame{Data: []byte{byte(index)}}))
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				barrier := &datagramAdmissionContext{Context: ctx, woken: make(chan struct{}), resume: make(chan struct{})}
				canceled := make(chan error, 1)
				go func() { canceled <- queue.AddContext(barrier, &wire.DatagramFrame{Data: []byte("canceled")}) }()
				synctest.Wait()
				survived := make(chan error, 1)
				go func() {
					frame := &wire.DatagramFrame{Data: []byte("survivor")}
					if legacy {
						survived <- queue.Add(frame)
					} else {
						survived <- queue.AddContext(context.Background(), frame)
					}
				}()
				synctest.Wait()
				queue.Pop()
				select {
				case <-barrier.woken:
				case <-time.After(time.Second):
					t.Fatal("first waiter did not consume the notification")
				}
				cancel()
				close(barrier.resume)
				synctest.Wait()
				require.ErrorIs(t, <-canceled, context.Canceled)
				select {
				case err := <-survived:
					require.NoError(t, err)
				default:
					t.Fatal("surviving waiter needs another Pop")
				}
				require.Equal(t, maxDatagramSendQueueLen, queue.sendQueue.Len())
				for index := 1; index < maxDatagramSendQueueLen; index++ {
					require.Equal(t, []byte{byte(index)}, queue.Peek().Data)
					queue.Pop()
				}
				require.Equal(t, []byte("survivor"), queue.Peek().Data)
				queue.Pop()
				require.Nil(t, queue.Peek())
			})
		})
	}
}

func TestDatagramQueueContextCancellationBeforeCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queue := newDatagramQueue(func() {}, utils.DefaultLogger)
		for range maxDatagramSendQueueLen {
			require.NoError(t, queue.Add(&wire.DatagramFrame{Data: []byte("queued")}))
		}
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		result := make(chan error, 1)
		go func() { result <- queue.AddContext(ctx, &wire.DatagramFrame{Data: []byte("canceled")}) }()
		synctest.Wait()
		cancel(assert.AnError)
		synctest.Wait()
		require.ErrorIs(t, <-result, context.Canceled)
		require.Equal(t, maxDatagramSendQueueLen, queue.sendQueue.Len())
		for range maxDatagramSendQueueLen {
			require.Equal(t, []byte("queued"), queue.Peek().Data)
			queue.Pop()
		}
		require.Nil(t, queue.Peek())
	})
}

func TestDatagramQueueAdmissionWinsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := newDatagramQueue(cancel, utils.DefaultLogger)
	require.NoError(t, queue.AddContext(ctx, &wire.DatagramFrame{Data: []byte("admitted")}))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, []byte("admitted"), queue.Peek().Data)
	queue.Pop()
	require.Nil(t, queue.Peek())
}

func TestDatagramQueueContextClose(t *testing.T) {
	t.Run("observed cancellation precedes closure and capacity", func(t *testing.T) {
		queue := newDatagramQueue(func() {}, utils.DefaultLogger)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		queue.CloseWithError(assert.AnError)
		require.ErrorIs(t, queue.AddContext(ctx, &wire.DatagramFrame{}), context.Canceled)
		require.Nil(t, queue.Peek())
	})
	t.Run("closure after notification prevents admission", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			queue := newDatagramQueue(func() {}, utils.DefaultLogger)
			for range maxDatagramSendQueueLen {
				require.NoError(t, queue.Add(&wire.DatagramFrame{Data: []byte("queued")}))
			}
			barrier := &datagramAdmissionContext{Context: context.Background(), woken: make(chan struct{}), resume: make(chan struct{})}
			result := make(chan error, 1)
			go func() { result <- queue.AddContext(barrier, &wire.DatagramFrame{Data: []byte("closed")}) }()
			synctest.Wait()
			queue.Pop()
			<-barrier.woken
			queue.CloseWithError(assert.AnError)
			close(barrier.resume)
			synctest.Wait()
			require.ErrorIs(t, <-result, assert.AnError)
			for range maxDatagramSendQueueLen - 1 {
				require.Equal(t, []byte("queued"), queue.Peek().Data)
				queue.Pop()
			}
			require.Nil(t, queue.Peek())
		})
	})
	t.Run("blocked closure and cancellation", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			queue := newDatagramQueue(func() {}, utils.DefaultLogger)
			for range maxDatagramSendQueueLen {
				require.NoError(t, queue.Add(&wire.DatagramFrame{}))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- queue.AddContext(ctx, &wire.DatagramFrame{Data: []byte("late")}) }()
			synctest.Wait()
			queue.CloseWithError(assert.AnError)
			cancel()
			synctest.Wait()
			require.Contains(t, []error{assert.AnError, context.Canceled}, <-result)
			require.Equal(t, maxDatagramSendQueueLen, queue.sendQueue.Len())
		})
	})
}
