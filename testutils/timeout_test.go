package testutils

import (
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunWithTimeoutPreservesResultAndStopsWatchdog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		interrupted := false
		n, err := RunWithTimeout(time.Second, func() { interrupted = true }, func() (int, error) { return 3, io.EOF })
		require.Equal(t, 3, n)
		require.ErrorIs(t, err, io.EOF)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.False(t, interrupted)
	})
}

func TestRunWithTimeoutJoinsInterrupt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		interrupted := make(chan struct{})
		release := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			RunWithTimeout(time.Second, func() {
				close(interrupted)
				<-release
			}, func() (int, error) {
				<-interrupted
				return 0, io.EOF
			})
		}()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Error("returned before interrupt finished")
		default:
		}
		close(release)
		<-done
	})
}
