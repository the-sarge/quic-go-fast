package qtls

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/stretchr/testify/require"
)

type cipherFixtureResult struct {
	cipher uint16
	err    error
}

// cipherFixture owns the cipher override until its TLS worker has stopped.
// Only the accept worker and teardown share the accepted-connection slot.
type cipherFixture struct {
	listener net.Listener
	client   *tls.Conn
	done     chan struct{}
	result   cipherFixtureResult
	mutex    sync.Mutex
	accepted *tls.Conn
	closing  bool
}

func newCipherFixture(t *testing.T, cs uint16) *cipherFixture {
	t.Helper()
	f := &cipherFixture{done: make(chan struct{})}
	reset := SetCipherSuite(cs)
	t.Cleanup(func() {
		if f.listener == nil {
			reset()
			return
		}
		f.mutex.Lock()
		f.closing = true
		accepted := f.accepted
		f.mutex.Unlock()
		f.listener.Close()
		if f.client != nil {
			f.client.NetConn().Close()
		}
		if accepted != nil {
			accepted.NetConn().Close()
		}
		select {
		case <-f.done:
			reset()
		case <-time.After(time.Second):
			// Restoring shared state while the worker still uses TLS would be unsafe.
			t.Error("timeout joining cipher fixture worker; cipher override not restored")
		}
	})
	var err error
	f.listener, err = tls.Listen("tcp4", "localhost:0", testdata.GetTLSConfig())
	require.NoError(t, err)
	go f.serve()
	return f
}

func (f *cipherFixture) serve() {
	defer close(f.done)
	conn, err := f.listener.Accept()
	if err != nil {
		f.result.err = err
		return
	}
	accepted := conn.(*tls.Conn)
	defer accepted.Close()
	f.mutex.Lock()
	f.accepted = accepted
	closing := f.closing
	f.mutex.Unlock()
	if closing {
		return
	}
	_, f.result.err = accepted.Read(make([]byte, 10))
	f.result.cipher = accepted.ConnectionState().CipherSuite
}

func (f *cipherFixture) dial(ctx context.Context) (*tls.Conn, error) {
	// Acquire the TCP socket separately so failed TLS handshakes also have an owner.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp4", f.listener.Addr().String())
	if err != nil {
		return nil, err
	}
	f.client = tls.Client(raw, &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost"})
	return f.client, f.client.HandshakeContext(ctx)
}

func (f *cipherFixture) wait(t *testing.T) cipherFixtureResult {
	t.Helper()
	select {
	case <-f.done:
		return f.result
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for cipher fixture worker")
		return cipherFixtureResult{}
	}
}
