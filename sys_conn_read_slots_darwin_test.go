//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"context"
	"io"
	"testing"
	"time"

	"golang.org/x/net/ipv4"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/stretchr/testify/require"
)

func TestReadSlotsDarwinOptIn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		declared int
		slots    int
	}{
		{"declares_eight", 8, 8},
		{"declares_the_maximum", 64, 64},
		{"declares_one", 1, 1},
		{"declares_zero", 0, 1},
		{"declares_negative", -3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slots, slotLen, deliveredCap := readThroughSlotDeclaringConn(t, &slotDeclaringConn{UDPConn: newUDPConnLocalhost(t), slots: tc.declared})
			require.Equal(t, tc.slots, slots)
			want := protocol.MaxPacketBufferSize
			if tc.slots > 1 {
				want = protocol.MaxPacketBufferSize + 1
			}
			require.Equal(t, want, slotLen)
			require.Equal(t, want, deliveredCap)
		})
	}
}

func TestReadSlotsDarwinDefaultIsOneSlot(t *testing.T) {
	t.Run("undeclared_batch_adapter", func(t *testing.T) {
		offered := 0
		wrapper := &ordinaryBatchWrapper{OOBCapablePacketConn: newUDPConnLocalhost(t), batchConn: lifetimeBatchReader(func(ms []ipv4.Message) (int, error) {
			offered = len(ms)
			require.Len(t, ms[0].Buffers[0], protocol.MaxPacketBufferSize)
			ms[0].N = copy(ms[0].Buffers[0], "one")
			return 1, nil
		})}
		reader, err := newConn(wrapper, true, false)
		require.NoError(t, err)
		t.Cleanup(reader.releaseReadBuffers)
		p, err := reader.ReadPacket()
		require.NoError(t, err)
		defer p.buffer.Release()
		require.Equal(t, 1, offered)
		require.Equal(t, protocol.MaxPacketBufferSize, cap(p.buffer.Data))
	})
	t.Run("native_socket", func(t *testing.T) {
		reader, err := newConn(newUDPConnLocalhost(t), true, false)
		require.NoError(t, err)
		require.Len(t, reader.buffers, 1)
		require.False(t, reader.readSlots)
	})
}

func TestReadSlotsDarwinDeclarationAboveMaximum(t *testing.T) {
	_, err := newConn(&slotDeclaringConn{UDPConn: newUDPConnLocalhost(t), slots: 65}, true, false)
	require.ErrorContains(t, err, "ReadBatchSlotsV1 returned 65")
}

func TestReadSlotsDarwinKillSwitch(t *testing.T) {
	for _, tc := range []struct {
		value string
		slots int
	}{
		{"true", 1},
		{"1", 1},
		{"false", 8},
		{"", 8},
	} {
		t.Run("value_"+tc.value, func(t *testing.T) {
			t.Setenv(recvmsgXDisableEnv, tc.value)
			slots, _, _ := readThroughSlotDeclaringConn(t, &slotDeclaringConn{UDPConn: newUDPConnLocalhost(t), slots: 8})
			require.Equal(t, tc.slots, slots)
		})
	}
}

// A connection's whole lifetime runs over an opted-in adapter: every datagram
// arrives in a read slot, and every release returns it to the slot tier.
func TestReadSlotsDarwinConnection(t *testing.T) {
	serverConn := &slotDeclaringConn{UDPConn: newUDPConnLocalhost(t), slots: 8}
	serverTransport := &Transport{Conn: serverConn}
	t.Cleanup(func() { serverTransport.Close() })
	clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { clientTransport.Close() })
	ln, err := serverTransport.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), scaleDuration(10*time.Second))
	defer cancel()
	client, err := clientTransport.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
	require.NoError(t, err)
	server, err := ln.Accept(ctx)
	require.NoError(t, err)

	message := make([]byte, 100*protocol.MaxPacketBufferSize)
	go func() {
		str, err := client.OpenStream()
		if err != nil {
			return
		}
		str.Write(message)
		str.Close()
	}()
	str, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	received, err := io.ReadAll(str)
	require.NoError(t, err)
	require.Equal(t, message, received)
	slots, slotLen := serverConn.lastOffer()
	require.Equal(t, 8, slots)
	require.Equal(t, protocol.MaxPacketBufferSize+1, slotLen)
	require.NoError(t, client.CloseWithError(0, ""))
	require.NoError(t, server.CloseWithError(0, ""))
}
