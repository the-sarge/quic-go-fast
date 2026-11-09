//go:build picoquic

package quic

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

func TestPicoquicMatrixAwaitsTraceConsumption(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		trace := newMatrixTrace()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- trace.wait(ctx, trace.usefulTrafficReady) }()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("returned before the recorder consumed useful traffic: %v", err)
		default:
		}
		for _, id := range []uint64{0, 1} {
			frames := []qlog.Frame{{Frame: &qlog.StreamFrame{Length: 32}}, {Frame: &qlog.DatagramFrame{Length: 32}}}
			trace.RecordEvent(qlog.PacketSent{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: id}, Frames: frames})
			trace.RecordEvent(qlog.PacketReceived{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: id}, Frames: frames})
		}
		synctest.Wait()
		require.NoError(t, <-done)
	})
}

func TestPicoquicMatrixReplayDispositions(t *testing.T) {
	const heldSize = 77
	for _, test := range []struct {
		name  string
		event qlogwriter.Event
	}{
		{"decoded", qlog.PacketReceived{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: 1}, Raw: qlog.RawInfo{Length: heldSize}}},
		{"duplicate", qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize}, Trigger: qlog.PacketDropDuplicate}},
		{"unknown_connection_id", qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize}, Trigger: qlog.PacketDropUnknownConnectionID}},
		{"key_unavailable", qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize}, Trigger: qlog.PacketDropKeyUnavailable}},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := newMatrixTrace()
			before := trace.replayEvidence(heldSize)
			require.Empty(t, trace.replayDisposition(heldSize, before))
			trace.RecordEvent(test.event)
			require.Equal(t, test.name, trace.replayDisposition(heldSize, before))
			require.Empty(t, trace.replayDisposition(heldSize, trace.replayEvidence(heldSize)), "old evidence cannot supply a new replay")
		})
	}
	t.Run("unrelated evidence", func(t *testing.T) {
		trace := newMatrixTrace()
		before := trace.replayEvidence(heldSize)
		trace.RecordEvent(qlog.PacketReceived{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: 0}, Raw: qlog.RawInfo{Length: heldSize}})
		trace.RecordEvent(qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize + 1}, Trigger: qlog.PacketDropUnknownConnectionID})
		trace.RecordEvent(qlog.PacketDropped{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Raw: qlog.RawInfo{Length: heldSize}, Trigger: qlog.PacketDropPayloadDecryptError})
		require.Empty(t, trace.replayDisposition(heldSize, before))
	})
}

func TestPicoquicMatrixMTUFinalLossObservation(t *testing.T) {
	const rtt = 10 * time.Millisecond
	rttStats := utils.NewRTTStats()
	rttStats.SetInitialRTT(rtt)
	var recorder events.Recorder
	finder := newMTUDiscoverer(rttStats, 1200, 1440, &recorder)
	now := monotime.Now()
	finder.Start(now)
	now = now.Add(mtuProbeDelay * rtt)
	var lastProbe protocol.ByteCount
	for probe := 0; probe < 16 && finder.ShouldSendProbe(now); probe++ {
		ping, size := finder.GetPing(now)
		lastProbe = size
		if size <= 1280 {
			ping.Handler.OnAcked(ping.Frame)
		} else {
			ping.Handler.OnLost(ping.Frame)
		}
		now = now.Add(mtuProbeDelay * rtt)
	}
	updates := recorder.Events(qlog.MTUUpdated{})
	require.NotEmpty(t, updates)
	require.Equal(t, protocol.ByteCount(1295), lastProbe, "the canonical final probe is lost")
	require.Equal(t, qlog.MTUUpdated{Value: 1278, Done: false}, updates[len(updates)-1])
	size, complete := matrixMTUState(finder)
	require.Equal(t, uint32(1278), size)
	require.True(t, complete, "the owner completed discovery even though its last qlog update did not")
}
