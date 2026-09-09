package quic

import (
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// The operation deliberately retains its buffer after interruption. Returning
// from a watchdog must wait for this late completion, not just signal it.
type lateTimeoutOperation struct {
	interrupted chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (o *lateTimeoutOperation) SetReadDeadline(time.Time) error {
	o.once.Do(func() { close(o.interrupted) })
	return nil
}
func (o *lateTimeoutOperation) SetWriteDeadline(d time.Time) error { return o.SetReadDeadline(d) }
func (o *lateTimeoutOperation) Read(b []byte) (int, error) {
	<-o.interrupted
	<-o.release
	b[0] = 'x'
	return 1, io.EOF
}
func (o *lateTimeoutOperation) Peek(b []byte) (int, error) { return o.Read(b) }
func (o *lateTimeoutOperation) Write(b []byte) (int, error) {
	<-o.interrupted
	<-o.release
	return len(b), nil
}

func TestTimeoutHelpersJoinLateCompletion(t *testing.T) {
	for _, kind := range []string{"read", "peek", "write"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				op := &lateTimeoutOperation{interrupted: make(chan struct{}), release: make(chan struct{})}
				buffer := []byte{'a'}
				done := make(chan struct{})
				var n int
				var err error
				go func() {
					defer close(done)
					switch kind {
					case "read":
						n, err = (&readerWithTimeout{Reader: op, Timeout: time.Second}).Read(buffer)
					case "peek":
						n, err = (&peekerWithTimeout{Peeker: op, Timeout: time.Second}).Peek(buffer)
					case "write":
						n, err = (&writerWithTimeout{Writer: op, Timeout: time.Second}).Write(buffer)
					}
				}()
				time.Sleep(2 * time.Second)
				synctest.Wait()
				select {
				case <-done:
					t.Error("helper returned while operation still owned the buffer")
				default:
				}
				select {
				case <-op.interrupted:
				default:
					t.Error("watchdog did not interrupt the blocked operation")
				}
				// Also release a broken helper's operation so this regression cannot hang.
				op.SetReadDeadline(time.Time{})
				close(op.release)
				<-done
				synctest.Wait()
				require.Equal(t, 1, n)
				if kind == "write" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, io.EOF)
					require.Equal(t, []byte("x"), buffer)
				}
				buffer[0] = 'z' // safe for the caller to reuse immediately after return
			})
		})
	}
}
