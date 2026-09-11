package utils

import (
	"bufio"
	"errors"
	"io"
)

type bufferedWriteCloser struct {
	*bufio.Writer
	io.Closer
}

// NewBufferedWriteCloser creates an io.WriteCloser from a bufio.Writer and an io.Closer
func NewBufferedWriteCloser(writer *bufio.Writer, closer io.Closer) io.WriteCloser {
	return &bufferedWriteCloser{
		Writer: writer,
		Closer: closer,
	}
}

// Close flushes buffered data and always closes the sink, joining any errors.
func (h bufferedWriteCloser) Close() error {
	return errors.Join(h.Flush(), h.Closer.Close())
}
