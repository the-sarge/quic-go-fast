package quic

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"

	"github.com/stretchr/testify/require"
)

func TestConnectionDatagramPacketReuse(t *testing.T) {
	for _, traced := range []bool{false, true} {
		t.Run(fmt.Sprintf("qlog=%t", traced), func(t *testing.T) {
			tc := newServerTestConnection(t, nil, &Config{EnableDatagrams: true}, false)
			var logs [][]qlog.Frame
			var log func([]qlog.Frame)
			if traced {
				log = func(frames []qlog.Frame) { logs = append(logs, frames) }
			}
			want := [][]byte{{}, []byte("first"), bytes.Repeat([]byte{'a'}, 1071)}
			var packet []byte
			for i, payload := range want {
				var err error
				packet, err = (&wire.DatagramFrame{Data: payload, DataLenPresent: i < len(want)-1}).Append(packet, protocol.Version1)
				require.NoError(t, err)
			}
			_, _, _, err := tc.conn.handleFrames(packet, protocol.ConnectionID{}, protocol.Encryption1RTT, log, monotime.Now())
			require.NoError(t, err)
			clear(packet) // packet storage can be reused before the application drains
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // a missing queue entry fails immediately
			var received [][]byte
			for _, payload := range want {
				data, err := tc.conn.ReceiveDatagram(ctx)
				require.NoError(t, err)
				require.Equal(t, payload, data)
				received = append(received, data)
			}
			packet, err = (&wire.DatagramFrame{Data: bytes.Repeat([]byte{'b'}, 1071)}).Append(packet[:0], protocol.Version1)
			require.NoError(t, err)
			_, _, _, err = tc.conn.handleFrames(packet, protocol.ConnectionID{}, protocol.Encryption1RTT, log, monotime.Now())
			require.NoError(t, err)
			later, err := tc.conn.ReceiveDatagram(ctx)
			require.NoError(t, err)
			later[0] = 'c'
			clear(packet)
			for i, payload := range want {
				require.Equal(t, payload, received[i])
			}
			require.Equal(t, byte('c'), later[0])
			require.Equal(t, bytes.Repeat([]byte{'b'}, 1070), later[1:])
			if traced {
				require.Len(t, logs, 2)
				require.Len(t, logs[0], len(want))
				for i, frame := range logs[0] {
					require.Equal(t, &qlog.DatagramFrame{Length: int64(len(want[i]))}, frame.Frame)
				}
			}
		})
	}
}

func TestConnectionDatagramOverflowPacketReuse(t *testing.T) {
	tc := newServerTestConnection(t, nil, &Config{EnableDatagrams: true}, false)
	for i := range maxDatagramRcvQueueLen {
		tc.conn.datagramQueue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte{byte(i)}})
	}
	packet, err := (&wire.DatagramFrame{Data: bytes.Repeat([]byte{'x'}, 1071)}).Append(nil, protocol.Version1)
	require.NoError(t, err)
	_, _, _, err = tc.conn.handleFrames(packet, protocol.ConnectionID{}, protocol.Encryption1RTT, nil, monotime.Now())
	require.NoError(t, err)
	clear(packet)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := range maxDatagramRcvQueueLen {
		data, err := tc.conn.ReceiveDatagram(ctx)
		require.NoError(t, err)
		require.Equal(t, []byte{byte(i)}, data)
	}
	_, err = tc.conn.ReceiveDatagram(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func BenchmarkDatagramParseAndReceive(b *testing.B) {
	for _, overflow := range []bool{false, true} {
		b.Run(fmt.Sprintf("overflow=%t", overflow), func(b *testing.B) {
			logger := utils.DefaultLogger.WithPrefix("parse-admission")
			logger.SetLogLevel(utils.LogLevelNothing)
			queue := newDatagramQueue(func() {}, logger)
			parser := wire.NewFrameParser(true, true, true)
			frame := &wire.DatagramFrame{Data: make([]byte, 1071), DataLenPresent: true}
			packet, err := frame.Append(nil, protocol.Version1)
			require.NoError(b, err)
			if overflow {
				for range maxDatagramRcvQueueLen {
					queue.HandleDatagramFrame(frame)
				}
			}
			b.ReportAllocs()
			b.SetBytes(1071)
			for b.Loop() {
				f, _, err := parser.ParseDatagramFrame(wire.FrameTypeDatagramWithLength, packet[1:], protocol.Version1)
				if err != nil {
					b.Fatal(err)
				}
				queue.HandleDatagramFrame(f)
				if !overflow {
					if _, err := queue.Receive(context.Background()); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
