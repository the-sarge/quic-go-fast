package self_test

import (
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTimeoutReaderJoinsClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, writer := io.Pipe()
		defer reader.Close()
		defer writer.Close()
		late := make(chan error, 1)
		go func() {
			time.Sleep(2 * time.Second)
			_, err := writer.Write([]byte("late"))
			late <- err
		}()
		buf := make([]byte, 4)
		n, err := (&readerWithTimeout{Reader: reader, Timeout: time.Second}).Read(buf)
		require.Zero(t, n)
		require.Error(t, err)
		copy(buf, "mine")
		require.ErrorIs(t, <-late, io.ErrClosedPipe)
		require.Equal(t, "mine", string(buf))
	})
}
