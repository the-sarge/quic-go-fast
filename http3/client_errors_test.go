package http3

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/stretchr/testify/require"
)

func TestClientConnectionErrorBoundaries(t *testing.T) {
	for _, operation := range []string{"open request", "extended connect settings"} {
		for _, remote := range []bool{false, true} {
			name := "local"
			if remote {
				name = "remote"
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				client, server := newConnPair(t)
				cc := (&Transport{}).NewClientConn(client)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				select {
				case <-client.HandshakeComplete():
				case <-ctx.Done():
					t.Fatal("handshake did not complete")
				}
				closing := client
				if remote {
					closing = server
				}
				require.NoError(t, closing.CloseWithError(quic.ApplicationErrorCode(ErrCodeExcessiveLoad), "overloaded"))
				select {
				case <-client.Context().Done():
				case <-ctx.Done():
					t.Fatal("connection did not close")
				}
				var err error
				if operation == "open request" {
					_, err = cc.OpenRequestStream(ctx)
				} else {
					req, reqErr := http.NewRequestWithContext(ctx, http.MethodConnect, "https://example.com", nil)
					require.NoError(t, reqErr)
					req.Proto = "connect"
					_, err = cc.RoundTrip(req)
				}
				requireHTTP3Error(t, err, remote, "overloaded")
			})
		}
	}
}

func TestClientResponseErrorContext(t *testing.T) {
	client, server := newConnPair(t)
	cc := (&Transport{}).NewClientConn(client)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := cc.RoundTrip(req)
		result <- err
	}()
	defer func() {
		cancel()
		client.CloseWithError(0, "")
		<-done
	}()
	str, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	str.CancelWrite(quic.StreamErrorCode(ErrCodeExcessiveLoad))
	select {
	case err := <-result:
		requireHTTP3Error(t, err, true, "")
		require.ErrorContains(t, err, "http3: parsing frame failed")
	case <-ctx.Done():
		t.Fatal("request did not finish")
	}
}
