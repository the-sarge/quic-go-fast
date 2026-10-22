package quic

import (
	"context"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

func TestDatagramReceivePartialRefillFIFO(t *testing.T) {
	queue := newDatagramQueue(func() {}, utils.DefaultLogger)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var next, want int
	enqueue := func(count int) {
		for range count {
			queue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte{byte(next >> 8), byte(next)}})
			next++
		}
	}
	dequeue := func(count int) {
		for range count {
			data, err := queue.Receive(ctx)
			require.NoError(t, err)
			require.Equal(t, []byte{byte(want >> 8), byte(want)}, data)
			want++
		}
	}
	enqueue(96)
	for range 16 {
		dequeue(32)
		enqueue(32)
	}
	dequeue(96)
	require.Empty(t, queue.rcvQueue)
	require.Zero(t, queue.receiveQueueLen())
	cancel()
	_, err := queue.Receive(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
