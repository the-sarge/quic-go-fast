package http3

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quic-go/qpack"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func failTransportResponse(t *testing.T, tr *Transport, server *quic.Conn) *roundTripperWithCount {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		_, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com/invalid", nil))
		result <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	str, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	require.NoError(t, str.SetReadDeadline(time.Now().Add(time.Second)))
	decodeHeader(t, str)
	entry := exchangeEntry(t, tr)
	var headers bytes.Buffer
	enc := qpack.NewEncoder(&headers)
	require.NoError(t, enc.WriteField(qpack.HeaderField{Name: ":method", Value: "GET"}))
	require.NoError(t, enc.Close())
	frame := (&headersFrame{Length: uint64(headers.Len())}).Append(nil)
	_, err = str.Write(append(frame, headers.Bytes()...))
	require.NoError(t, err)
	require.ErrorContains(t, receiveExchange(t, result), "invalid response pseudo header")
	tr.mutex.Lock()
	defer tr.mutex.Unlock()
	require.NotContains(t, tr.clients, "example.com:443")
	return entry
}

func TestTransportMalformedResponseClosesEvictedConnection(t *testing.T) {
	tr, client, server := exchangeTransport(t)
	entry := failTransportResponse(t, tr, server)
	require.Eventually(t, func() bool { return entry.useCount.Load() == 0 }, time.Second, time.Millisecond)
	receiveExchange(t, client.Context().Done())
}

func TestTransportRetiredConnectionLifetime(t *testing.T) {
	for _, finish := range []string{"response-close", "response-eof", "request-cancel", "transport-close"} {
		t.Run(finish, func(t *testing.T) {
			tr, client, server := exchangeTransport(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "https://example.com/active", nil).WithContext(ctx)
			rsp, str := exchangeResponse(t, tr, server, req, encodeResponse(t, http.StatusOK))
			entry := exchangeEntry(t, tr)
			failTransportResponse(t, tr, server)
			require.Eventually(t, func() bool { return entry.useCount.Load() == 1 }, time.Second, time.Millisecond)
			require.NoError(t, client.Context().Err(), "another exchange still uses the retired connection")
			tr.CloseIdleConnections()
			require.NoError(t, client.Context().Err())
			switch finish {
			case "response-close":
				require.NoError(t, rsp.Body.Close())
			case "response-eof":
				require.NoError(t, str.Close())
				_, err := io.ReadAll(rsp.Body)
				require.NoError(t, err)
			case "request-cancel":
				cancel()
			case "transport-close":
				require.NoError(t, tr.Close())
			}
			receiveExchange(t, client.Context().Done())
			receiveExchange(t, rsp.Body.(*exchangeBody).lifetime.stopped)
			require.Zero(t, entry.useCount.Load())
		})
	}
}

func TestTransportRetiredConnectionWaitsForUpload(t *testing.T) {
	tr, client, server := exchangeTransport(t)
	input := newExchangeInput()
	t.Cleanup(func() { input.Close() })
	req := httptest.NewRequest(http.MethodPost, "https://example.com/upload", input)
	rsp, str := exchangeResponse(t, tr, server, req, encodeResponse(t, http.StatusOK))
	receiveExchange(t, input.entered)
	entry := exchangeEntry(t, tr)
	failTransportResponse(t, tr, server)
	require.NoError(t, rsp.Body.Close())
	require.Eventually(t, func() bool { return entry.useCount.Load() == 1 }, time.Second, time.Millisecond)
	tr.CloseIdleConnections()
	require.NoError(t, client.Context().Err(), "the unfinished upload still uses the retired connection")
	require.Zero(t, input.closes.Load())
	close(input.finished)
	receiveExchange(t, rsp.Body.(*exchangeBody).lifetime.stopped)
	receiveExchange(t, client.Context().Done())
	require.Zero(t, entry.useCount.Load())
	require.EqualValues(t, 1, input.closes.Load())
	str.CancelRead(0)
}

func TestTransportRetiredConnectionPreservesReplacement(t *testing.T) {
	oldClient, oldServer := newConnPair(t)
	newClient, newServer := newConnPair(t)
	connections := make(chan *quic.Conn, 2)
	connections <- oldClient
	connections <- newClient
	tr := &Transport{Dial: func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) {
		return <-connections, nil
	}}
	t.Cleanup(func() { tr.Close() })
	req := httptest.NewRequest(http.MethodGet, "https://example.com/active", nil)
	oldResponse, _ := exchangeResponse(t, tr, oldServer, req, encodeResponse(t, http.StatusOK))
	failTransportResponse(t, tr, oldServer)
	newResponse, _ := exchangeResponse(t, tr, newServer, req.Clone(req.Context()), encodeResponse(t, http.StatusOK))
	replacement := exchangeEntry(t, tr)
	require.Same(t, newClient, replacement.conn)
	require.NoError(t, oldResponse.Body.Close())
	receiveExchange(t, oldResponse.Body.(*exchangeBody).lifetime.stopped)
	receiveExchange(t, oldClient.Context().Done())
	require.NoError(t, newClient.Context().Err())
	require.Same(t, replacement, exchangeEntry(t, tr))
	require.NoError(t, newResponse.Body.Close())
	receiveExchange(t, newResponse.Body.(*exchangeBody).lifetime.stopped)
	tr.CloseIdleConnections()
	receiveExchange(t, newClient.Context().Done())
}
