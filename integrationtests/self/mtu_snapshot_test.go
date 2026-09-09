package self_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

// Pause the first successful probe's ACK between the MTU event and publication
// of the new DATAGRAM limit. The recorder owns only the scalar MTU value.
type pausedMTURecorder struct {
	events.Recorder
	once    sync.Once
	updated chan int
	release chan struct{}
}

func (r *pausedMTURecorder) RecordEvent(ev qlogwriter.Event) {
	r.Recorder.RecordEvent(ev)
	if mtu, ok := ev.(qlog.MTUUpdated); ok && mtu.Value > 1200 {
		r.once.Do(func() {
			r.updated <- mtu.Value
			<-r.release
		})
	}
}

func TestPathMTUDiscoverySnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	ln, err := quic.Listen(newUDPConnLocalhost(t), getTLSConfig(), getQuicConfig(&quic.Config{EnableDatagrams: true}))
	require.NoError(t, err)
	defer ln.Close()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := ln.Accept(ctx)
		if err != nil {
			return
		}
		str, err := conn.AcceptUniStream(ctx)
		if err == nil {
			io.Copy(io.Discard, str)
		}
	}()

	recorder := &pausedMTURecorder{updated: make(chan int, 1), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(recorder.release) }) }
	tr := &quic.Transport{Conn: newUDPConnLocalhost(t)}
	defer func() {
		unblock()
		tr.Close()
	}()
	conn, err := tr.Dial(ctx, ln.Addr(), getTLSClientConfig(), getQuicConfig(&quic.Config{
		InitialPacketSize: 1200,
		EnableDatagrams:   true,
		Tracer:            newTracer(recorder),
	}))
	require.NoError(t, err)
	var writerDone chan struct{}
	defer func() {
		// Release a paused ACK even if an assertion fails, before closing.
		unblock()
		conn.CloseWithError(0, "")
		if writerDone != nil {
			<-writerDone
		}
		<-serverDone
	}()
	str, err := conn.OpenUniStream()
	require.NoError(t, err)
	writerDone = make(chan struct{})
	go func() {
		defer close(writerDone)
		str.Write(PRDataLong)
		str.Close()
	}()

	var mtu int
	select {
	case mtu = <-recorder.updated:
	case <-ctx.Done():
		t.Fatal("no successful MTU probe")
	}
	var sizeErr *quic.DatagramTooLargeError
	require.ErrorAs(t, conn.SendDatagram(make([]byte, 2000)), &sizeErr)
	t.Logf("MTU event=%d, DATAGRAM limit during ACK=%d", mtu, sizeErr.MaxDatagramPayloadSize)
	// An MTU event alone does not mean SendDatagram has the new estimate yet.
	require.Less(t, int(sizeErr.MaxDatagramPayloadSize), mtu-40)
	unblock()
	require.NoError(t, conn.CloseWithError(0, ""))
	require.ErrorAs(t, conn.SendDatagram(make([]byte, 2000)), &sizeErr)
	mtus := recorder.Events(qlog.MTUUpdated{})
	finalMTU := mtus[len(mtus)-1].(qlog.MTUUpdated).Value
	t.Logf("after close: MTU=%d, DATAGRAM limit=%d", finalMTU, sizeErr.MaxDatagramPayloadSize)
	require.GreaterOrEqual(t, int(sizeErr.MaxDatagramPayloadSize), finalMTU-40)
}
