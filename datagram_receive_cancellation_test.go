package quic

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

func canceledDatagramReceiveContext(t *testing.T, kind string) (context.Context, error) {
	t.Helper()
	cause := errors.New("application stopped receiving")
	switch kind {
	case "canceled":
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, context.Canceled
	case "cancel cause":
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(cause)
		return ctx, cause
	case "deadline":
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		t.Cleanup(cancel)
		return ctx, context.DeadlineExceeded
	case "deadline cause":
		ctx, cancel := context.WithDeadlineCause(context.Background(), time.Now().Add(-time.Second), cause)
		t.Cleanup(cancel)
		return ctx, cause
	default:
		t.Fatalf("unknown context kind %q", kind)
		return nil, nil
	}
}

func TestDatagramReceiveCanceledEntry(t *testing.T) {
	for _, public := range []bool{false, true} {
		name := "queue"
		if public {
			name = "connection"
		}
		t.Run(name, func(t *testing.T) {
			for _, kind := range []string{"canceled", "cancel cause", "deadline", "deadline cause"} {
				t.Run(kind, func(t *testing.T) {
					for _, state := range []string{"empty queue", "queued payload", "queued empty payload"} {
						t.Run(state, func(t *testing.T) {
							queue := newDatagramQueue(func() {}, utils.DefaultLogger)
							var stored []byte
							if state != "empty queue" {
								payload := []byte{}
								if state == "queued payload" {
									payload = []byte("queued")
								}
								queue.HandleDatagramFrame(&wire.DatagramFrame{Data: payload})
								stored = queue.rcvQueue[0]
							}
							storage := queue.rcvQueue
							depth := len(storage)
							receive := queue.Receive
							if public {
								conn := &Conn{config: &Config{EnableDatagrams: true}, datagramQueue: queue}
								receive = conn.ReceiveDatagram
							}
							ctx, cause := canceledDatagramReceiveContext(t, kind)
							data, err := receive(ctx)
							require.Nil(t, data)
							require.ErrorIs(t, err, cause)
							require.Len(t, queue.rcvQueue, depth)
							require.Equal(t, depth, queue.receiveQueueLen())
							require.Zero(t, queue.receiveDrops())
							if depth == 0 {
								return
							}
							require.Equal(t, stored, storage[0])
							require.Equal(t, stored, queue.rcvQueue[0])
							if len(stored) > 0 {
								require.Same(t, &stored[0], &queue.rcvQueue[0][0])
							}
							live, cancel := context.WithTimeout(context.Background(), time.Second)
							defer cancel()
							data, err = receive(live)
							require.NoError(t, err)
							require.Equal(t, stored, data)
							if len(stored) > 0 {
								require.Same(t, &stored[0], &data[0])
							}
							require.Nil(t, storage[0])
							require.Empty(t, queue.rcvQueue)
							require.Zero(t, queue.receiveQueueLen())
							require.Zero(t, queue.receiveDrops())
						})
					}
				})
			}
		})
	}
}

func TestDatagramReceiveCanceledEntryBeforeClose(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "queued"}[queued], func(t *testing.T) {
			queue := newDatagramQueue(func() {}, utils.DefaultLogger)
			if queued {
				queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("first")})
				queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("second")})
			}
			storage := queue.rcvQueue
			terminal := errors.New("connection closed")
			queue.CloseWithError(terminal)
			conn := &Conn{config: &Config{EnableDatagrams: true}, datagramQueue: queue}
			ctx, cause := canceledDatagramReceiveContext(t, "cancel cause")
			data, err := conn.ReceiveDatagram(ctx)
			require.Nil(t, data)
			require.Same(t, cause, err)
			require.Len(t, queue.rcvQueue, len(storage))
			require.Equal(t, len(storage), queue.receiveQueueLen())
			require.Zero(t, queue.receiveDrops())
			live, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if queued {
				for i, want := range []string{"first", "second"} {
					require.Equal(t, []byte(want), storage[i])
					data, err := conn.ReceiveDatagram(live)
					require.NoError(t, err)
					require.Equal(t, []byte(want), data)
					require.Nil(t, storage[i])
					require.Equal(t, 1-i, queue.receiveQueueLen())
				}
			}
			data, err = conn.ReceiveDatagram(live)
			require.Nil(t, data)
			require.Same(t, terminal, err)
			require.Empty(t, queue.rcvQueue)
			require.Zero(t, queue.receiveQueueLen())
			require.Zero(t, queue.receiveDrops())
		})
	}
}

func TestDatagramReceiveCanceledEntryWhenDisabled(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("queued")})
	conn := &Conn{config: &Config{}, datagramQueue: queue}
	ctx, _ := canceledDatagramReceiveContext(t, "cancel cause")
	data, err := conn.ReceiveDatagram(ctx)
	require.Nil(t, data)
	require.EqualError(t, err, "datagram support disabled")
	require.Len(t, queue.rcvQueue, 1)
	require.Equal(t, []byte("queued"), queue.rcvQueue[0])
	require.Equal(t, 1, queue.receiveQueueLen())
	require.Zero(t, queue.receiveDrops())
}

func TestDatagramReceiveBlockedCancellationCause(t *testing.T) {
	for _, public := range []bool{false, true} {
		name := "queue"
		if public {
			name = "connection"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				queue := newDatagramQueue(func() {}, utils.DefaultLogger)
				receive := queue.Receive
				if public {
					conn := &Conn{config: &Config{EnableDatagrams: true}, datagramQueue: queue}
					receive = conn.ReceiveDatagram
				}
				ctx, cancel := context.WithCancelCause(context.Background())
				defer cancel(nil)
				type result struct {
					data []byte
					err  error
				}
				results := make(chan result, 1)
				go func() {
					data, err := receive(ctx)
					results <- result{data, err}
				}()
				synctest.Wait()
				select {
				case <-results:
					t.Fatal("receive returned before cancellation")
				default:
				}
				cause := errors.New("application stopped receiving")
				cancel(cause)
				synctest.Wait()
				res := <-results
				require.Nil(t, res.data)
				require.Same(t, cause, res.err)
				require.Empty(t, queue.rcvQueue)
				require.Zero(t, queue.receiveQueueLen())
				require.Zero(t, queue.receiveDrops())
			})
		})
	}
}
