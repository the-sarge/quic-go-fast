package http3

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/stretchr/testify/require"
)

type countingStreamClearer struct{ calls atomic.Int32 }

func (c *countingStreamClearer) clearStream(quic.StreamID) { c.calls.Add(1) }

// Keep the real QUIC cancellation, but put an explicit barrier before the
// existing closeSend callback. This isolates admission from callback scheduling
// without adding a scheduling hook to production code.
func newStreamWithDelayedSendClose(t *testing.T, stream *quic.Stream, clearer streamClearer, send func([]byte) error) (*stateTrackingStream, func()) {
	t.Helper()
	str := &stateTrackingStream{
		Stream: stream, clearer: clearer, sendDatagram: send, hasData: make(chan struct{}, 1),
	}
	release := make(chan struct{})
	done := make(chan struct{})
	stop := context.AfterFunc(stream.Context(), func() {
		<-release
		str.closeSend(context.Cause(stream.Context()))
		close(done)
	})
	finish := sync.OnceFunc(func() {
		close(release)
		if !stop() {
			<-done
		}
	})
	t.Cleanup(finish)
	return str, finish
}

func TestStateTrackingStreamDatagramSendAfterClose(t *testing.T) {
	for _, closure := range []string{"underlying close", "underlying cancel", "STOP_SENDING", "local connection close", "remote connection close", "wrapper close", "wrapper cancel"} {
		t.Run(closure, func(t *testing.T) {
			clientConn, serverConn := newConnPair(t, withDatagrams())
			peer, err := serverConn.OpenStream()
			require.NoError(t, err)
			_, err = peer.Write([]byte{0})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), scaleDuration(time.Second))
			defer cancel()
			client, err := clientConn.AcceptStream(ctx)
			require.NoError(t, err)
			client.SetReadDeadline(time.Now().Add(scaleDuration(time.Second)))
			_, err = io.ReadFull(client, make([]byte, 1))
			require.NoError(t, err)

			var clearer countingStreamClearer
			var sends int
			str, finish := newStreamWithDelayedSendClose(t, client, &clearer, func([]byte) error {
				sends++
				return nil
			})
			public := &Stream{datagramStream: str}
			request := &RequestStream{str: public}
			require.NoError(t, public.SendDatagram([]byte("open")))
			require.NoError(t, request.SendDatagram([]byte("open")))

			var wantCause error
			var wantHTTPError *Error
			switch closure {
			case "underlying close":
				require.NoError(t, client.Close())
				wantCause = context.Canceled
			case "underlying cancel", "wrapper cancel":
				if closure == "underlying cancel" {
					client.CancelWrite(1337)
				} else {
					str.CancelWrite(1337)
				}
				wantCause = &quic.StreamError{StreamID: client.StreamID(), ErrorCode: 1337}
				wantHTTPError = &Error{ErrorCode: 1337}
			case "STOP_SENDING":
				peer.CancelRead(1337)
				wantCause = &quic.StreamError{StreamID: client.StreamID(), ErrorCode: 1337, Remote: true}
				wantHTTPError = &Error{ErrorCode: 1337, Remote: true}
			case "local connection close", "remote connection close":
				remote := closure == "remote connection close"
				conn := clientConn
				if remote {
					conn = serverConn
				}
				require.NoError(t, conn.CloseWithError(42, "connection closed"))
				wantCause = &quic.ApplicationError{ErrorCode: 42, ErrorMessage: "connection closed", Remote: remote}
				wantHTTPError = &Error{ErrorCode: 42, ErrorMessage: "connection closed", Remote: remote}
			case "wrapper close":
				require.NoError(t, str.Close())
				wantCause = context.Canceled
			}
			select {
			case <-client.Context().Done():
			case <-ctx.Done():
				t.Fatal("send stream did not close")
			}
			require.ErrorIs(t, context.Cause(client.Context()), wantCause)
			for _, send := range []func([]byte) error{public.SendDatagram, request.SendDatagram} {
				err := send([]byte("late"))
				if closure == "wrapper close" {
					require.EqualError(t, err, "write on closed stream")
					require.NotErrorIs(t, err, context.Canceled)
				} else if wantHTTPError != nil {
					var h3err *Error
					require.ErrorAs(t, err, &h3err)
					require.Equal(t, wantHTTPError, h3err)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
			}
			require.Equal(t, 2, sends, "closed sends must not reach the callback")
			require.Zero(t, clearer.calls.Load(), "receive direction has not finished")

			if closure != "local connection close" && closure != "remote connection close" {
				_, err := peer.Write([]byte("receive still open"))
				require.NoError(t, err)
				data := make([]byte, len("receive still open"))
				_, err = io.ReadFull(str, data)
				require.NoError(t, err)
				require.Equal(t, []byte("receive still open"), data)
			}
			// Cover both completion orders without crossing every closure case.
			if closure == "underlying close" {
				finish()
				require.Zero(t, clearer.calls.Load())
			}
			str.CancelRead(17)
			finish()
			str.CancelRead(18)
			require.EqualValues(t, 1, clearer.calls.Load())
		})
	}
}
