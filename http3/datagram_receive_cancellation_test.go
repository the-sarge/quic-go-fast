package http3

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"

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

func datagramReceiveEntry(t *testing.T, str *stateTrackingStream, entry string) func(context.Context) ([]byte, error) {
	t.Helper()
	public := &Stream{datagramStream: str}
	switch entry {
	case "owner":
		return str.ReceiveDatagram
	case "stream":
		return public.ReceiveDatagram
	case "request stream":
		return (&RequestStream{str: public}).ReceiveDatagram
	default:
		t.Fatalf("unknown receive entry %q", entry)
		return nil
	}
}

func TestHTTP3DatagramReceiveCanceledEntry(t *testing.T) {
	for _, entry := range []string{"owner", "stream", "request stream"} {
		t.Run(entry, func(t *testing.T) {
			for _, kind := range []string{"canceled", "cancel cause", "deadline", "deadline cause"} {
				t.Run(kind, func(t *testing.T) {
					for _, state := range []string{"empty queue", "queued payload", "queued empty payload"} {
						t.Run(state, func(t *testing.T) {
							str := &stateTrackingStream{hasData: make(chan struct{}, 1)}
							var stored []byte
							if state != "empty queue" {
								stored = []byte{}
								if state == "queued payload" {
									stored = []byte("queued")
								}
								str.enqueueDatagram(stored)
							}
							storage := str.queue
							depth := len(storage)
							receive := datagramReceiveEntry(t, str, entry)
							ctx, cause := canceledDatagramReceiveContext(t, kind)
							data, err := receive(ctx)
							require.Nil(t, data)
							require.ErrorIs(t, err, cause)
							require.Len(t, str.queue, depth)
							if depth == 0 {
								return
							}
							require.Equal(t, stored, storage[0])
							require.Equal(t, stored, str.queue[0])
							if len(stored) > 0 {
								require.Same(t, &stored[0], &str.queue[0][0])
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
							require.Empty(t, str.queue)
						})
					}
				})
			}
		})
	}
}

func TestHTTP3DatagramReceiveCanceledEntryBeforeClose(t *testing.T) {
	for _, entry := range []string{"owner", "stream", "request stream"} {
		t.Run(entry, func(t *testing.T) {
			for _, state := range []string{"empty", "queued"} {
				t.Run(state, func(t *testing.T) {
					str := &stateTrackingStream{hasData: make(chan struct{}, 1)}
					if state == "queued" {
						str.enqueueDatagram([]byte("first"))
						str.enqueueDatagram([]byte("second"))
					}
					storage := str.queue
					str.closeReceive(io.EOF)
					str.enqueueDatagram([]byte("late"))
					receive := datagramReceiveEntry(t, str, entry)
					ctx, cause := canceledDatagramReceiveContext(t, "cancel cause")
					data, err := receive(ctx)
					require.Nil(t, data)
					require.Same(t, cause, err)
					require.Len(t, str.queue, len(storage))
					live, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					if state == "queued" {
						for i, want := range []string{"first", "second"} {
							require.Equal(t, []byte(want), storage[i])
							data, err := receive(live)
							require.NoError(t, err)
							require.Equal(t, []byte(want), data)
							require.Nil(t, storage[i])
							require.Len(t, str.queue, 1-i)
						}
					}
					data, err = receive(live)
					require.Nil(t, data)
					require.ErrorIs(t, err, io.EOF)
					require.Empty(t, str.queue)
				})
			}
		})
	}
}

func TestHTTP3DatagramReceiveBlockedCancellationCause(t *testing.T) {
	for _, entry := range []string{"owner", "stream", "request stream"} {
		t.Run(entry, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				str := &stateTrackingStream{hasData: make(chan struct{}, 1)}
				receive := datagramReceiveEntry(t, str, entry)
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
				require.Empty(t, str.queue)
			})
		})
	}
}

func TestHTTP3DatagramReceiveErrorTranslation(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  error
		want *Error
	}{
		{"stream error", &quic.StreamError{StreamID: 4, ErrorCode: 42, Remote: true}, &Error{ErrorCode: 42, Remote: true}},
		{"application error", &quic.ApplicationError{ErrorCode: 43, ErrorMessage: "closed", Remote: true}, &Error{ErrorCode: 43, ErrorMessage: "closed", Remote: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, entry := range []string{"owner", "stream", "request stream"} {
				t.Run(entry, func(t *testing.T) {
					for _, source := range []string{"context cause", "terminal error"} {
						t.Run(source, func(t *testing.T) {
							str := &stateTrackingStream{hasData: make(chan struct{}, 1)}
							var ctx context.Context
							if source == "context cause" {
								canceled, cancel := context.WithCancelCause(context.Background())
								cancel(tc.raw)
								ctx = canceled
								str.enqueueDatagram([]byte("queued"))
							} else {
								live, cancel := context.WithTimeout(context.Background(), time.Second)
								defer cancel()
								ctx = live
								str.closeReceive(tc.raw)
							}
							data, err := datagramReceiveEntry(t, str, entry)(ctx)
							require.Nil(t, data)
							if entry == "owner" {
								require.Same(t, tc.raw, err)
							} else {
								var translated *Error
								require.ErrorAs(t, err, &translated)
								require.Equal(t, tc.want, translated)
							}
							if source == "context cause" {
								require.Len(t, str.queue, 1)
								require.Equal(t, []byte("queued"), str.queue[0])
							}
						})
					}
				})
			}
		})
	}
}
