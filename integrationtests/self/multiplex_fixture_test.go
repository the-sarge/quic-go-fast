package self_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

const multiplexShutdown = "multiplex fixture shutdown"

// multiplexTest owns the servers, clients and nested stream writers of one scenario.
// Its caller defers close before fallible setup, so it runs before socket cleanups.
type multiplexTest struct {
	ctx        context.Context
	cancel     context.CancelFunc
	listeners  []*quic.Listener
	transports []*quic.Transport
	workers    sync.WaitGroup
	stopOnce   sync.Once
	done       chan struct{}
	failed     chan struct{}

	mu    sync.Mutex
	conns []*quic.Conn
	err   error
}

func newMultiplexTest() *multiplexTest {
	ctx, cancel := context.WithCancel(context.Background())
	return &multiplexTest{ctx: ctx, cancel: cancel, done: make(chan struct{}), failed: make(chan struct{})}
}

func (f *multiplexTest) ownConn(conn *quic.Conn) bool {
	f.mu.Lock()
	if f.ctx.Err() == nil {
		f.conns = append(f.conns, conn)
		f.mu.Unlock()
		return true
	}
	f.mu.Unlock()
	// Accept or Dial may succeed concurrently with shutdown's connection snapshot.
	conn.CloseWithError(0, multiplexShutdown)
	return false
}

func (f *multiplexTest) recordError(err error) {
	if err == nil {
		return
	}
	if f.ctx.Err() != nil {
		var appErr *quic.ApplicationError
		if errors.Is(err, context.Canceled) || errors.Is(err, quic.ErrServerClosed) || errors.Is(err, net.ErrClosed) ||
			(errors.As(err, &appErr) && appErr.ErrorCode == 0 && appErr.ErrorMessage == multiplexShutdown) {
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil {
		f.err = err
		close(f.failed)
	}
}

func (f *multiplexTest) failure() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *multiplexTest) serve(ln *quic.Listener, write func(*quic.SendStream) error) {
	f.listeners = append(f.listeners, ln)
	if write == nil {
		write = func(str *quic.SendStream) error {
			_, err := str.Write(PRData)
			return err
		}
	}
	f.workers.Go(func() {
		runMultiplexTestServer(f, ln, write)
	})
}

func runMultiplexTestServer(f *multiplexTest, ln *quic.Listener, write func(*quic.SendStream) error) {
	for {
		conn, err := ln.Accept(f.ctx)
		if err != nil {
			f.recordError(fmt.Errorf("multiplex server %s accepting connection: %w", ln.Addr(), err))
			return
		}
		if !f.ownConn(conn) {
			return
		}
		str, err := conn.OpenUniStream()
		if err != nil {
			f.recordError(fmt.Errorf("multiplex server %s opening stream: %w", ln.Addr(), err))
			return
		}
		// The accept worker remains counted until it can no longer admit writers.
		f.workers.Go(func() {
			err := write(str)
			f.recordError(wrapMultiplexWriteError(ln.Addr(), err))
			f.recordError(wrapMultiplexWriteError(ln.Addr(), str.Close()))
		})
	}
}

func wrapMultiplexWriteError(addr net.Addr, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("multiplex server %s writing stream: %w", addr, err)
}

func (f *multiplexTest) receive(tr *quic.Transport, addr net.Addr) <-chan error {
	result := make(chan error, 1)
	f.workers.Go(func() {
		result <- dialAndReceiveData(f, tr, addr)
	})
	return result
}

func (f *multiplexTest) wait(result <-chan error, timeout time.Duration) error {
	select {
	case <-f.failed:
		return f.failure()
	case err := <-result:
		if serverErr := f.failure(); serverErr != nil {
			return serverErr
		}
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for multiplex client after %s", timeout)
	}
}

func (f *multiplexTest) shutdown() error {
	f.stopOnce.Do(func() {
		go func() {
			f.mu.Lock()
			f.cancel()
			conns := append([]*quic.Conn(nil), f.conns...)
			f.mu.Unlock()
			for _, ln := range f.listeners {
				f.recordError(ln.Close())
			}
			for _, conn := range conns {
				f.recordError(conn.CloseWithError(0, multiplexShutdown))
			}
			for _, tr := range f.transports {
				f.recordError(tr.Close())
			}
			f.workers.Wait()
			close(f.done)
		}()
	})
	select {
	case <-f.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("timeout joining multiplex accept, writer and client workers")
	}
}

func (f *multiplexTest) close(t *testing.T) {
	t.Helper()
	if err := f.shutdown(); err != nil {
		t.Error(err)
	}
	if err := f.failure(); err != nil {
		t.Error(err)
	}
}
