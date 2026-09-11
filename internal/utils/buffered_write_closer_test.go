package utils

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func TestBufferedWriteCloserFlushBeforeClosing(t *testing.T) {
	buf := &bytes.Buffer{}

	w := bufio.NewWriter(buf)
	wc := NewBufferedWriteCloser(w, &nopCloser{})
	_, err := wc.Write([]byte("foobar"))
	require.NoError(t, err)
	require.Zero(t, buf.Len())
	require.NoError(t, wc.Close())
	require.Equal(t, "foobar", buf.String())
}

func TestBufferedWriteCloserClosesSinkAfterFlushError(t *testing.T) {
	flushErr := errors.New("flush failed")
	closeErr := errors.New("close failed")
	for _, sinkCloseErr := range []error{nil, closeErr} {
		t.Run(fmt.Sprint(sinkCloseErr), func(t *testing.T) {
			sink := &failingBufferedSink{writeErr: flushErr, closeErr: sinkCloseErr}
			wc := NewBufferedWriteCloser(bufio.NewWriter(sink), sink)
			_, err := wc.Write([]byte("tail event"))
			require.NoError(t, err)
			err = wc.Close()
			require.ErrorIs(t, err, flushErr)
			require.Equal(t, 1, sink.closes)
			if sinkCloseErr != nil {
				require.ErrorIs(t, err, sinkCloseErr)
			}
		})
	}
}

type failingBufferedSink struct {
	writeErr error
	closeErr error
	closes   int
}

func (s *failingBufferedSink) Write([]byte) (int, error) { return 0, s.writeErr }
func (s *failingBufferedSink) Close() error              { s.closes++; return s.closeErr }
