package http3

import (
	"bytes"
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
	"go.uber.org/mock/gomock"
)

func TestHTTP3MalformedTrailerDisposition(t *testing.T) {
	for _, tc := range []struct {
		name        string
		consumer    string
		field       qpack.HeaderField
		wantErr     string
		closeUpload bool
	}{
		{
			name: "pseudo field on request", consumer: "request",
			field:   qpack.HeaderField{Name: ":status", Value: "200"},
			wantErr: "http3: received pseudo header in trailer: :status",
		},
		{
			name: "invalid name on response", consumer: "response",
			field:   qpack.HeaderField{Name: "@", Value: "value"},
			wantErr: `invalid header field name: "@"`,
		},
		{
			name: "invalid value with completed upload", consumer: "response", closeUpload: true,
			field:   qpack.HeaderField{Name: "foo", Value: "\n"},
			wantErr: `invalid header field value for foo: "\n"`,
		},
		{
			name: "prohibited field on public stream", consumer: "public",
			field:   qpack.HeaderField{Name: "content-length", Value: "42"},
			wantErr: `invalid trailer field name: "content-length"`,
		},
		{
			name: "valid trailers and FIN", consumer: "request",
			field: qpack.HeaderField{Name: "finished", Value: "yes"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var recorder events.Recorder
			client, server := newConnPair(t, withClientRecorder(&recorder), withServerRecorder(&recorder))
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			block := encodeTrailerBlock(t, tc.field)
			wire := append(getDataFrame([]byte("body")), (&headersFrame{Length: uint64(len(block))}).Append(nil)...)
			wire = append(wire, block...)
			type readResult struct {
				body     []byte
				trailers http.Header
				err      error
			}
			var got readResult
			var peer *quic.Stream
			if tc.consumer == "response" {
				cc := (&Transport{}).NewClientConn(client)
				str, err := cc.OpenRequestStream(ctx)
				require.NoError(t, err)
				require.NoError(t, str.SendRequestHeader(httptest.NewRequest(http.MethodPost, "https://example.com", nil)))
				if tc.closeUpload {
					require.NoError(t, str.Close())
				}
				peer, err = server.AcceptStream(ctx)
				require.NoError(t, err)
				if tc.closeUpload {
					// Observe the upload FIN before the later cancellation.
					_, err = io.ReadAll(peer)
					require.NoError(t, err)
				}
				_, err = peer.Write(append(encodeResponse(t, http.StatusOK), wire...))
				require.NoError(t, err)
				rsp, err := str.ReadResponse()
				require.NoError(t, err)
				got.body, got.err = io.ReadAll(rsp.Body)
				got.trailers = rsp.Trailer
				require.NoError(t, rsp.Body.Close())
			} else {
				result := make(chan readResult, 1)
				release := make(chan struct{})
				s := &Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var reader io.Reader = r.Body
					if tc.consumer == "public" {
						reader = w.(HTTPStreamer).HTTPStream()
					}
					body, err := io.ReadAll(reader)
					result <- readResult{body: body, trailers: r.Trailer, err: err}
					<-release
				})}
				hconn, err := s.NewRawServerConn(server)
				require.NoError(t, err)
				peer, err = client.OpenStream()
				require.NoError(t, err)
				_, err = peer.Write(append(encodeRequest(t, httptest.NewRequest(http.MethodPost, "https://example.com", nil)), wire...))
				require.NoError(t, err)
				if tc.wantErr == "" {
					require.NoError(t, peer.Close())
				}
				incoming, err := server.AcceptStream(ctx)
				require.NoError(t, err)
				handled := make(chan struct{})
				go func() {
					defer close(handled)
					hconn.HandleRequestStream(incoming)
				}()
				t.Cleanup(func() {
					close(release)
					receiveExchange(t, handled)
				})
				got = receiveExchange(t, result)
			}
			require.Equal(t, "body", string(got.body))
			if tc.wantErr == "" {
				require.NoError(t, got.err)
				require.Equal(t, "yes", got.trailers.Get("Finished"))
			} else {
				require.EqualError(t, got.err, tc.wantErr)
				require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
				_, err := io.ReadAll(peer)
				if tc.closeUpload {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, &quic.StreamError{StreamID: peer.StreamID(), ErrorCode: quic.StreamErrorCode(ErrCodeMessageError), Remote: true})
				}
				select {
				case <-peer.Context().Done():
				case <-ctx.Done():
					t.Fatal("peer did not receive STOP_SENDING")
				}
				require.ErrorIs(t, context.Cause(peer.Context()), &quic.StreamError{
					StreamID: peer.StreamID(), ErrorCode: quic.StreamErrorCode(ErrCodeMessageError), Remote: true,
				})
				require.Contains(t, recorder.Events(qlog.FrameParsed{}), qlog.FrameParsed{
					StreamID: peer.StreamID(),
					Raw:      qlog.RawInfo{PayloadLength: len(block)},
					Frame:    qlog.Frame{Frame: qlog.HeadersFrame{}},
				})
			}
			require.NoError(t, client.Context().Err())
			require.NoError(t, server.Context().Err())
			// A new independent stream on the same connection remains usable.
			sibling, err := client.OpenStream()
			require.NoError(t, err)
			_, err = sibling.Write([]byte("ok"))
			require.NoError(t, err)
			other, err := server.AcceptStream(ctx)
			require.NoError(t, err)
			require.NoError(t, other.SetReadDeadline(time.Now().Add(time.Second)))
			b := make([]byte, 2)
			_, err = io.ReadFull(other, b)
			require.NoError(t, err)
			require.Equal(t, "ok", string(b))
		})
	}
}

func TestHTTP3MalformedTrailerDispositionPreservesNonsemanticErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		block   []byte
		length  uint64
		limit   int
		wantErr error
	}{
		{name: "truncated read", block: []byte{0}, length: 2, limit: 1024, wantErr: io.ErrUnexpectedEOF},
		{
			// The decoded size refusal precedes semantic rejection of this pseudo-field.
			name: "decoded size refusal", block: encodeTrailerBlock(t, qpack.HeaderField{Name: ":status", Value: "200"}),
			limit: 8, wantErr: errHeaderTooLarge,
		},
		{name: "QPACK failure", block: []byte{0}, limit: 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			length := tc.length
			if length == 0 {
				length = uint64(len(tc.block))
			}
			wire := append(getDataFrame([]byte("body")), (&headersFrame{Length: length}).Append(nil)...)
			wire = append(wire, tc.block...)
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().Read(gomock.Any()).DoAndReturn(bytes.NewReader(wire).Read).AnyTimes()
			// No cancellation is expected at the transport boundary for these errors.
			str, trailers := newTrailerTestStream(t, qstr, tc.limit, nil)
			err := readBodyThenTrailer(t, str)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.ErrorAs(t, err, new(*qpackError))
			}
			require.NotErrorAs(t, err, new(*trailerSemanticError))
			require.Nil(t, *trailers)
		})
	}
}
