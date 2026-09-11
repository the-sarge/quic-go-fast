package testdata

import (
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCertificates(t *testing.T) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", GetTLSConfig())
	require.NoError(t, err)
	deadline := time.Now().Add(5 * time.Second)
	serverDone := make(chan error, 1)
	t.Cleanup(func() {
		require.NoError(t, ln.Close())
		require.NoError(t, <-serverDone)
	})

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		// Close the accepted socket before publishing worker completion.
		defer func() {
			conn.Close()
			serverDone <- err
		}()
		if err = conn.SetDeadline(deadline); err != nil {
			return
		}
		_, err = conn.Write([]byte("foobar"))
	}()

	conn, err := tls.DialWithDialer(&net.Dialer{Deadline: deadline}, "tcp", ln.Addr().String(), &tls.Config{
		RootCAs: GetRootCA(), ServerName: "localhost",
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, conn.SetDeadline(deadline))
	data, err := io.ReadAll(conn)
	require.NoError(t, err)
	require.Equal(t, "foobar", string(data))
}
