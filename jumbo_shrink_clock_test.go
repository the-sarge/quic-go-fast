//go:build linux || darwin || windows

package quic

import (
	"bytes"
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// Virtual time keeps immediate emissions in one clock tick. Their registered
// packet numbers still distinguish the padded probe from its later small PING.
func TestJumboFarSideShrinkSameClockTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, s, socket, _ := newJumboRefusalPair(t, "reno", "ordinary")
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		armed := bytes.Repeat([]byte{'j'}, 8200)
		require.NoError(t, c.SendDatagram(armed))
		got, err := s.ReceiveDatagram(ctx)
		require.NoError(t, err)
		require.Equal(t, armed, got)
		socket.dropAtOrAbove.Store(1501)
		data := bytes.Repeat([]byte("intact through far-side shrink"), 8192)
		done := make(chan error, 1)
		t.Cleanup(func() { c.CloseWithError(0, ""); <-done })
		go func() {
			defer close(done)
			stream, err := c.OpenUniStreamSync(ctx)
			if err == nil {
				_, err = stream.Write(data)
				if err == nil {
					err = stream.Close()
				}
			}
			done <- err
		}()
		stream, err := s.AcceptUniStream(ctx)
		require.NoError(t, err)
		require.NoError(t, stream.SetReadDeadline(time.Now().Add(5*time.Second)))
		got, err = io.ReadAll(stream)
		require.NoError(t, err)
		require.Equal(t, data, got)
		require.NoError(t, <-done)
	})
}
