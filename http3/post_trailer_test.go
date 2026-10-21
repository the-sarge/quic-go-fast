package http3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3/qlog"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/quic-go/qpack"
	"github.com/stretchr/testify/require"
)

func TestHTTP3PostTrailerFrameDisposition(t *testing.T) {
	block := encodeTrailerBlock(t, qpack.HeaderField{Name: "finished", Value: "yes"})
	headers := (&headersFrame{Length: uint64(len(block))}).Append(nil)
	headers = append(headers, block...)
	for _, tc := range []struct {
		name      string
		consumer  string
		tail      []byte
		wantErr   string
		wantFrame any
	}{
		{
			name: "DATA on request body", consumer: "request", tail: getDataFrame([]byte("tail")),
			wantErr: "DATA frame received after trailers", wantFrame: qlog.DataFrame{},
		},
		{
			name: "HEADERS on response body", consumer: "response", tail: headers,
			wantErr: "additional HEADERS frame received after trailers", wantFrame: qlog.HeadersFrame{},
		},
		{
			name: "DATA on public stream", consumer: "public", tail: getDataFrame([]byte("tail")),
			wantErr: "DATA frame received after trailers", wantFrame: qlog.DataFrame{},
		},
		{name: "valid trailers and FIN", consumer: "request"},
		{
			name: "unknown extension and FIN", consumer: "response", tail: []byte{0x21, 3, 'e', 'x', 't'},
			wantFrame: qlog.UnknownFrame{Type: 0x21},
		},
		{
			name: "parser first close", consumer: "request", tail: []byte{0x2, 0},
			wantErr: "http3: reserved frame type: 2", wantFrame: qlog.ReservedFrame{Type: 0x2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var recorder events.Recorder
			client, server := newConnPair(t, withClientRecorder(&recorder), withServerRecorder(&recorder))
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			sibling, err := client.OpenStream()
			require.NoError(t, err)
			_, err = sibling.Write([]byte("s"))
			require.NoError(t, err)
			otherSibling, err := server.AcceptStream(ctx)
			require.NoError(t, err)
			_, err = io.ReadFull(otherSibling, make([]byte, 1))
			require.NoError(t, err)

			type readResult struct {
				body     []byte
				trailers http.Header
				err      error
			}
			wire := append(getDataFrame([]byte("body")), headers...)
			wire = append(wire, tc.tail...)
			var got readResult
			receiver, peer, peerSibling := server, client, sibling
			var streamID quic.StreamID
			if tc.consumer == "response" {
				receiver, peer, peerSibling = client, server, otherSibling
				cc := (&Transport{}).NewClientConn(client)
				str, err := cc.OpenRequestStream(ctx)
				require.NoError(t, err)
				require.NoError(t, str.SendRequestHeader(httptest.NewRequest(http.MethodGet, "https://example.com", nil)))
				require.NoError(t, str.Close())
				incoming, err := server.AcceptStream(ctx)
				require.NoError(t, err)
				streamID = incoming.StreamID()
				_, err = incoming.Write(append(encodeResponse(t, http.StatusOK), wire...))
				require.NoError(t, err)
				require.NoError(t, incoming.Close())
				rsp, err := str.ReadResponse()
				require.NoError(t, err)
				got.body, got.err = io.ReadAll(rsp.Body)
				got.trailers = rsp.Trailer
				require.NoError(t, rsp.Body.Close())
			} else {
				result := make(chan readResult, 1)
				s := &Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var reader io.Reader = r.Body
					if tc.consumer == "public" {
						str := w.(HTTPStreamer).HTTPStream()
						str.Close()
						reader = str
					}
					body, err := io.ReadAll(reader)
					result <- readResult{body: body, trailers: r.Trailer, err: err}
				})}
				hconn, err := s.NewRawServerConn(server)
				require.NoError(t, err)
				str, err := client.OpenStream()
				require.NoError(t, err)
				streamID = str.StreamID()
				req := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
				_, err = str.Write(append(encodeRequest(t, req), wire...))
				require.NoError(t, err)
				require.NoError(t, str.Close())
				incoming, err := server.AcceptStream(ctx)
				require.NoError(t, err)
				handled := make(chan struct{})
				go func() {
					defer close(handled)
					hconn.HandleRequestStream(incoming)
				}()
				got = receiveExchange(t, result)
				receiveExchange(t, handled)
			}
			require.Equal(t, "body", string(got.body))
			require.Equal(t, "yes", got.trailers.Get("Finished"))
			if tc.wantErr == "" {
				require.NoError(t, got.err)
				require.NoError(t, client.Context().Err())
				require.NoError(t, server.Context().Err())
				_, err = otherSibling.Write([]byte("ok"))
				require.NoError(t, err)
				require.NoError(t, sibling.SetReadDeadline(time.Now().Add(time.Second)))
				b := make([]byte, 2)
				_, err = io.ReadFull(sibling, b)
				require.NoError(t, err)
				require.Equal(t, "ok", string(b))
			} else {
				require.EqualError(t, got.err, tc.wantErr)
				if tc.name == "parser first close" {
					// A later caller's classification must not replace the parser's close.
					require.NoError(t, receiver.CloseWithError(quic.ApplicationErrorCode(ErrCodeGeneralProtocolError), "later close"))
				}
				select {
				case <-peer.Context().Done():
				case <-ctx.Done():
					t.Fatal("peer did not receive connection closure")
				}
				wantClose := &quic.ApplicationError{Remote: true, ErrorCode: quic.ApplicationErrorCode(ErrCodeFrameUnexpected)}
				require.ErrorIs(t, context.Cause(peer.Context()), wantClose)
				require.ErrorIs(t, context.Cause(receiver.Context()), &quic.ApplicationError{
					ErrorCode: quic.ApplicationErrorCode(ErrCodeFrameUnexpected),
				})
				_, err = peerSibling.Read(make([]byte, 1))
				require.ErrorIs(t, err, wantClose)
			}
			if tc.wantFrame != nil {
				raw := qlog.RawInfo{Length: len(tc.tail), PayloadLength: 4}
				switch tc.wantFrame.(type) {
				case qlog.HeadersFrame:
					raw = qlog.RawInfo{PayloadLength: len(block)}
				case qlog.UnknownFrame:
					raw = qlog.RawInfo{Length: 2, PayloadLength: 3}
				case qlog.ReservedFrame:
					raw = qlog.RawInfo{Length: 2}
				}
				require.Contains(t, recorder.Events(qlog.FrameParsed{}), qlog.FrameParsed{
					StreamID: streamID,
					Raw:      raw,
					Frame:    qlog.Frame{Frame: tc.wantFrame},
				})
			}
		})
	}
}
