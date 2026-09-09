package http3

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/quic-go/quic-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTransportConditionalEviction(t *testing.T) {
	const hostname = "quic-go.net:443"
	expected := &roundTripperWithCount{}
	replacement := &roundTripperWithCount{}
	for _, tc := range []struct {
		name    string
		clients map[string]*roundTripperWithCount
		want    *roundTripperWithCount
	}{
		{name: "current", clients: map[string]*roundTripperWithCount{hostname: expected}},
		{name: "nil map"},
		{name: "missing", clients: map[string]*roundTripperWithCount{"other.net:443": replacement}},
		{name: "replacement", clients: map[string]*roundTripperWithCount{hostname: replacement}, want: replacement},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &Transport{clients: tc.clients}
			tr.removeClient(hostname, expected)
			require.Same(t, tc.want, tr.clients[hostname])
			if tc.name == "missing" {
				require.Same(t, replacement, tr.clients["other.net:443"])
			}
		})
	}
}

func TestTransportDelayedDialFailurePreservesReplacement(t *testing.T) {
	conn, _ := newConnPair(t)
	cl := NewMockClientConn(gomock.NewController(t))
	dialing := make(chan struct{})
	close(dialing)
	replacement := &roundTripperWithCount{conn: conn, clientConn: cl, dialing: dialing}
	tr := &Transport{}
	tr.Dial = func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) {
		// getClient holds the mutex until the old attempt has acquired its entry.
		// Publish its successor before allowing that attempt's dial to fail.
		tr.mutex.Lock()
		tr.clients["quic-go.net:443"] = replacement
		tr.mutex.Unlock()
		return nil, assert.AnError
	}
	req := httptest.NewRequest(http.MethodGet, "https://quic-go.net/old", nil)
	_, err := tr.RoundTrip(req)
	require.ErrorIs(t, err, assert.AnError)

	next := httptest.NewRequest(http.MethodGet, "https://quic-go.net/next", nil)
	cl.EXPECT().RoundTrip(next).Return(&http.Response{Request: next}, nil)
	rsp, err := tr.RoundTripOpt(next, RoundTripOpt{OnlyCachedConn: true})
	require.NoError(t, err)
	require.Same(t, next, rsp.Request)
}

func TestTransportDelayedRequestFailurePreservesReplacement(t *testing.T) {
	for _, retry := range []bool{false, true} {
		name := "terminal"
		if retry {
			name = "retryable"
		}
		t.Run(name, func(t *testing.T) {
			conn, _ := newConnPair(t)
			ctrl := gomock.NewController(t)
			oldClient, newClient := NewMockClientConn(ctrl), NewMockClientConn(ctrl)
			dialing := make(chan struct{})
			close(dialing)
			replacement := &roundTripperWithCount{conn: conn, clientConn: newClient, dialing: dialing}
			tr := &Transport{
				Dial: func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) {
					return conn, nil
				},
				newClientConn: func(*quic.Conn) clientConn { return oldClient },
			}
			req := httptest.NewRequest(http.MethodGet, "https://quic-go.net/old", nil)
			oldClient.EXPECT().RoundTrip(req).DoAndReturn(func(*http.Request) (*http.Response, error) {
				// The old request fails only after a successor occupies its hostname.
				tr.mutex.Lock()
				tr.clients["quic-go.net:443"] = replacement
				tr.mutex.Unlock()
				if retry {
					return nil, &errConnUnusable{assert.AnError}
				}
				return nil, assert.AnError
			})
			if retry {
				newClient.EXPECT().RoundTrip(req).Return(&http.Response{Request: req}, nil)
			}
			_, err := tr.RoundTrip(req)
			if retry {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, assert.AnError)
			}
			next := httptest.NewRequest(http.MethodGet, "https://quic-go.net/next", nil)
			newClient.EXPECT().RoundTrip(next).Return(&http.Response{Request: next}, nil)
			rsp, err := tr.RoundTripOpt(next, RoundTripOpt{OnlyCachedConn: true})
			require.NoError(t, err)
			require.Same(t, next, rsp.Request)
		})
	}
}
