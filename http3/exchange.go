package http3

import (
	"context"
	"io"
	"sync"

	"github.com/quic-go/quic-go"
)

// requestLifetime owns one attempt, including setup, the delivered (possibly
// decoded) response, and the uploader. Only this owner releases pooled usage.
type requestLifetime struct {
	input        *requestInput
	release      func()
	stream       *RequestStream
	done         chan struct{}
	stopped      chan struct{}
	mutex        sync.Mutex
	responseDone bool
	uploadDone   bool
	observing    bool
}

// requestInput closes caller-owned input once even when cancellation overlaps
// uploader cleanup. A stream-opening retry leaves this wrapper unused and gives
// the untouched original input to the next attempt.
type requestInput struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (b *requestInput) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

func newRequestLifetime(body io.ReadCloser) *requestLifetime {
	l := &requestLifetime{done: make(chan struct{}), stopped: make(chan struct{})}
	if body != nil {
		l.input = &requestInput{ReadCloser: body}
	}
	return l
}

func (l *requestLifetime) closeInput() {
	if l.input != nil {
		l.input.Close()
	}
}

// observe starts only after stream creation. Setup still owns the upload half,
// so cancellation cannot release usage before setup hands it to the uploader.
func (l *requestLifetime) observe(ctx, connCtx context.Context, str *RequestStream) {
	l.stream = str
	l.observing = true
	go func() {
		select {
		case <-ctx.Done():
			l.abort()
		case <-connCtx.Done():
			l.abort()
		case <-l.done:
		}
		<-l.done
		l.finalize()
	}()
}

func (l *requestLifetime) finalize() {
	if l.release != nil {
		l.release()
	}
	close(l.stopped)
}

// end reports a half finishing. It never waits: the cancellation observer must
// be able to report response completion while the uploader is still exiting.
func (l *requestLifetime) end(upload bool) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.responseDone && l.uploadDone {
		return true
	}
	if upload {
		l.uploadDone = true
	} else {
		l.responseDone = true
	}
	if !l.responseDone || !l.uploadDone {
		return false
	}
	close(l.done)
	if !l.observing {
		l.finalize()
	}
	return true
}

func (l *requestLifetime) uploadFinished() {
	if l.end(true) {
		<-l.stopped
	}
}

func (l *requestLifetime) responseFinished() {
	// Decoder errors can precede raw EOF. Always terminate receive-side cleanup,
	// without changing the body's error or aborting an ongoing request upload.
	if l.stream != nil {
		l.stream.CancelRead(quic.StreamErrorCode(ErrCodeRequestCanceled))
	}
	if l.end(false) {
		<-l.stopped
	}
}

func (l *requestLifetime) abort() {
	if l.stream != nil {
		l.stream.CancelRead(quic.StreamErrorCode(ErrCodeRequestCanceled))
		l.stream.CancelWrite(quic.StreamErrorCode(ErrCodeRequestCanceled))
	}
	l.closeInput()
	l.end(false)
}

type exchangeBody struct {
	io.ReadCloser
	lifetime *requestLifetime
}

func (b *exchangeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.lifetime.responseFinished()
	}
	return n, err
}

func (b *exchangeBody) Close() error {
	err := b.ReadCloser.Close()
	b.lifetime.responseFinished()
	return err
}
