package http09

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/stretchr/testify/require"
)

func TestServerRequestValidation(t *testing.T) {
	var handlerCalls atomic.Int32
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handlerCalls.Add(1)
		_, _ = io.WriteString(w, r.Method+" "+r.URL.RequestURI()+" "+r.Proto)
	})
	addr := startServer(t, handler)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, err := quic.DialAddr(ctx, addr.String(), &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{NextProto},
	}, httpTestConfig(t, "client"))
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseWithError(0, "") })

	for _, tc := range []struct {
		name     string
		request  string
		response string
	}{
		{name: "empty"},
		{name: "G", request: "G"},
		{name: "GE", request: "GE"},
		{name: "GET", request: "GET"},
		{name: "missing path", request: "GET "},
		{name: "line ending only", request: "\r\n"},
		{name: "truncated line", request: "GET \r\n"},
		{name: "wrong method", request: "POST /"},
		{name: "relative path", request: "GET x"},
		{name: "malformed URL", request: "GET /%zz\r\n"},
		{name: "root", request: "GET /", response: "GET / HTTP/0.9"},
		{name: "path and query", request: "GET /hello?name=world\r\n", response: "GET /hello?name=world HTTP/0.9"},
		{name: "trailing spaces", request: "GET /hello   \r\n", response: "GET /hello HTTP/0.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			callsBefore := handlerCalls.Load()
			str, err := conn.OpenStreamSync(ctx)
			require.NoError(t, err)
			require.NoError(t, str.SetDeadline(time.Now().Add(5*time.Second)))
			_, err = io.WriteString(str, tc.request)
			require.NoError(t, err)
			require.NoError(t, str.Close())
			response, err := io.ReadAll(str)
			if tc.response == "" {
				var streamErr *quic.StreamError
				require.ErrorAs(t, err, &streamErr)
				require.Equal(t, quic.StreamErrorCode(42), streamErr.ErrorCode)
				require.True(t, streamErr.Remote)
				require.Empty(t, response)
				require.Equal(t, callsBefore, handlerCalls.Load(), "rejected requests must not invoke the handler")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.response, string(response))
			require.Equal(t, callsBefore+1, handlerCalls.Load())
		})
	}
}
