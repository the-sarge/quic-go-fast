package self_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/qlogwriter"

	"github.com/stretchr/testify/require"
)

// natRebindingFixture owns only the local scenario and its stream writer.
// Resource cleanups are registered in acquisition order, so the writer joins
// before the proxy, sockets, listener and keylog are released.
type natRebindingFixture struct {
	ctx              context.Context
	conn, serverConn *quic.Conn
	trace            *packetCounter
	workerDone       chan struct{}
	workerErr        error // published by closing workerDone
}

func newNATRebindingFixture(t *testing.T) *natRebindingFixture {
	t.Helper()
	tr, tracer := newPacketTracer()
	tlsConf := getTLSConfig()
	f, err := os.Create("keylog.txt")
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	tlsConf.KeyLogWriter = f
	server, err := quic.Listen(
		newUDPConnLocalhost(t),
		tlsConf,
		getQuicConfig(&quic.Config{
			Tracer: func(ctx context.Context, isClient bool, connID quic.ConnectionID) qlogwriter.Trace { return tracer },
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { server.Close() })

	newPath := newUDPConnLocalhost(t)
	clientUDPConn := newUDPConnLocalhost(t)

	oldPathRTT := scaleDuration(10 * time.Millisecond)
	newPathRTT := scaleDuration(20 * time.Millisecond)
	proxy := quicproxy.Proxy{
		ServerAddr: server.Addr().(*net.UDPAddr),
		Conn:       newUDPConnLocalhost(t),
	}
	var mx sync.Mutex
	var switchedPath bool
	var dataTransferred int
	proxy.DelayPacket = func(dir quicproxy.Direction, _, _ net.Addr, b []byte) time.Duration {
		mx.Lock()
		defer mx.Unlock()

		if dir == quicproxy.DirectionOutgoing {
			dataTransferred += len(b)
			if dataTransferred > len(PRData)/3 {
				if !switchedPath {
					if err := proxy.SwitchConn(clientUDPConn.LocalAddr().(*net.UDPAddr), newPath); err != nil {
						panic(fmt.Sprintf("failed to switch connection: %s", err))
					}
					switchedPath = true
				}
			}
		}
		if switchedPath {
			return newPathRTT
		}
		return oldPathRTT
	}
	require.NoError(t, proxy.Start())
	t.Cleanup(func() { proxy.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	conn, err := quic.Dial(ctx, clientUDPConn, proxy.LocalAddr(), getTLSClientConfig(), getQuicConfig(nil))
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseWithError(0, "") })

	serverConn, err := server.Accept(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { serverConn.CloseWithError(0, "") })

	return &natRebindingFixture{ctx: ctx, conn: conn, serverConn: serverConn, trace: tr}
}

func (f *natRebindingFixture) startWriter(t *testing.T, open func() (*quic.SendStream, error), write func(*quic.SendStream) error) {
	t.Helper()
	if open == nil {
		open = f.serverConn.OpenUniStream
	}
	if write == nil {
		write = func(str *quic.SendStream) error {
			_, err := str.Write(PRData)
			return err
		}
	}
	f.workerDone = make(chan struct{})
	t.Cleanup(func() {
		f.stopIO()
		if err := f.join(); err != nil {
			t.Error(err)
		}
	})
	go func() {
		defer close(f.workerDone)
		f.workerErr = func() error {
			str, err := open()
			if err != nil {
				return fmt.Errorf("opening NAT stream: %w", err)
			}
			if err := write(str); err != nil {
				return fmt.Errorf("writing NAT stream: %w", err)
			}
			str.Close()
			return nil
		}()
		if f.workerErr != nil {
			// Interrupt local accept/read without depending on delivery of a peer close.
			// Retain the operation error before waking the owner.
			f.conn.CloseWithError(0, "NAT writer failed")
		}
	}()
}

func (f *natRebindingFixture) stopIO() {
	f.conn.CloseWithError(0, "NAT fixture shutdown")
	f.serverConn.CloseWithError(0, "NAT fixture shutdown")
}

func (f *natRebindingFixture) join() error {
	select {
	case <-f.workerDone:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("timeout joining NAT stream worker")
	}
}

func (f *natRebindingFixture) receive() ([]byte, error) {
	str, err := f.conn.AcceptUniStream(f.ctx)
	var data []byte
	if err == nil {
		str.SetReadDeadline(time.Now().Add(5 * time.Second))
		data, err = io.ReadAll(str)
		if err != nil {
			err = fmt.Errorf("reading NAT stream: %w", err)
		}
	} else {
		err = fmt.Errorf("accepting NAT stream: %w", err)
	}
	if err != nil {
		f.stopIO()
	}
	if joinErr := f.join(); joinErr != nil {
		return nil, joinErr
	}
	// Stopping I/O can make the writer fail too. Preserve the initiating receive
	// error as well as the worker's operation/error instead of masking either.
	return data, errors.Join(f.workerErr, err)
}
