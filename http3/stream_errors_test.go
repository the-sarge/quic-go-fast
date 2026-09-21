package http3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/quic-go/qpack"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlogwriter"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func requireHTTP3Error(t *testing.T, err error, remote bool, message string) {
	t.Helper()
	want := &Error{ErrorCode: ErrCodeExcessiveLoad, Remote: remote, ErrorMessage: message}
	require.ErrorIs(t, err, want)
	var got *Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, want, got)
}

func TestStreamErrorBoundaries(t *testing.T) {
	for _, operation := range []string{"read frame", "read payload", "read trailer", "write frame", "write payload", "try write", "send datagram", "receive datagram"} {
		t.Run(operation, func(t *testing.T) {
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
			failure := &quic.ApplicationError{ErrorCode: quic.ApplicationErrorCode(ErrCodeExcessiveLoad), Remote: true, ErrorMessage: "overloaded"}
			str := newStream(qstr, nil, nil, func(r io.Reader, _ *headersFrame, _ qlogwriter.Recorder) error {
				_, err := io.ReadAll(r)
				return err
			}, nil)
			var err error
			switch operation {
			case "read frame":
				qstr.EXPECT().Read(gomock.Any()).Return(0, failure)
				_, err = str.Read(make([]byte, 4))
			case "read payload", "read trailer":
				prefix := (&dataFrame{Length: 4}).Append(nil)
				if operation == "read trailer" {
					prefix = (&headersFrame{Length: 4}).Append(nil)
				}
				buf := bytes.NewBuffer(prefix)
				qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
					if buf.Len() > 0 {
						return buf.Read(p)
					}
					p[0] = 'x'
					return 1, failure
				}).AnyTimes()
				n, readErr := str.Read(make([]byte, 4))
				err = readErr
				if operation == "read payload" {
					require.Equal(t, 1, n)
				}
			case "write frame":
				qstr.EXPECT().Write(gomock.Any()).Return(0, failure)
				n, writeErr := str.Write([]byte("body"))
				err = writeErr
				require.Zero(t, n)
			case "write payload":
				gomock.InOrder(
					qstr.EXPECT().Write([]byte{0, 4}).Return(2, nil),
					qstr.EXPECT().Write([]byte("body")).Return(1, failure),
				)
				n, writeErr := str.Write([]byte("body"))
				err = writeErr
				require.Equal(t, 1, n)
			case "try write":
				qstr.EXPECT().TryWriteAll(gomock.Any()).Return(failure)
				err = str.TryWriteAll([]byte("body"))
			case "send datagram":
				qstr.EXPECT().SendDatagram([]byte("datagram")).Return(failure)
				err = str.SendDatagram([]byte("datagram"))
			case "receive datagram":
				qstr.EXPECT().ReceiveDatagram(gomock.Any()).Return(nil, failure)
				_, err = str.ReceiveDatagram(context.Background())
			}
			requireHTTP3Error(t, err, true, "overloaded")
		})
	}
}

func TestStreamUnrelatedErrors(t *testing.T) {
	for _, failure := range []error{io.EOF, context.Canceled, context.DeadlineExceeded, os.ErrDeadlineExceeded, quic.ErrWouldBlock, errors.New("custom cause"), &quic.TransportError{ErrorCode: 42}} {
		t.Run(failure.Error(), func(t *testing.T) {
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
			str := newStream(qstr, nil, nil, nil, nil)
			qstr.EXPECT().Read(gomock.Any()).Return(0, failure)
			_, err := str.Read(make([]byte, 1))
			require.True(t, failure == err, "error identity changed: %v", err)
			qstr.EXPECT().Write(gomock.Any()).Return(0, failure)
			_, err = str.Write([]byte("body"))
			require.True(t, failure == err, "error identity changed: %v", err)
			qstr.EXPECT().TryWriteAll(gomock.Any()).Return(failure)
			require.True(t, failure == str.TryWriteAll([]byte("body")))
			qstr.EXPECT().SendDatagram(gomock.Any()).Return(failure)
			require.True(t, failure == str.SendDatagram(nil))
			qstr.EXPECT().ReceiveDatagram(gomock.Any()).Return(nil, failure)
			_, err = str.ReceiveDatagram(context.Background())
			require.True(t, failure == err, "error identity changed: %v", err)
		})
	}
}

func TestRequestStreamErrorBoundaries(t *testing.T) {
	for _, operation := range []string{"request header", "response frame", "response headers"} {
		t.Run(operation, func(t *testing.T) {
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
			str := newRequestStream(newStream(qstr, nil, nil, nil, nil), newRequestWriter(), nil, qpack.NewDecoder(), true, 1024, &http.Response{})
			failure := &quic.StreamError{ErrorCode: quic.StreamErrorCode(ErrCodeExcessiveLoad), Remote: false}
			req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
			if operation == "request header" {
				qstr.EXPECT().Write(gomock.Any()).Return(0, failure)
				requireHTTP3Error(t, str.SendRequestHeader(req), false, "")
				return
			}
			qstr.EXPECT().Write(gomock.Any()).DoAndReturn(func(p []byte) (int, error) { return len(p), nil })
			require.NoError(t, str.SendRequestHeader(req))
			var prefix []byte
			contextMessage := "http3: parsing frame failed"
			if operation == "response headers" {
				prefix = (&headersFrame{Length: 4}).Append(nil)
				contextMessage = "http3: failed to read response headers"
			}
			buf := bytes.NewBuffer(prefix)
			qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
				if buf.Len() > 0 {
					return buf.Read(p)
				}
				return 0, failure
			}).AnyTimes()
			qstr.EXPECT().CancelRead(gomock.Any())
			qstr.EXPECT().CancelWrite(gomock.Any())
			_, err := str.ReadResponse()
			requireHTTP3Error(t, err, false, "")
			require.ErrorContains(t, err, contextMessage)
		})
	}
}

func TestTrailerWriteErrors(t *testing.T) {
	for _, side := range []string{"request", "response"} {
		t.Run(side, func(t *testing.T) {
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
			qstr.EXPECT().Write(gomock.Any()).Return(0, &quic.StreamError{ErrorCode: quic.StreamErrorCode(ErrCodeExcessiveLoad), Remote: true})
			stream := newStream(qstr, nil, nil, nil, nil)
			var err error
			if side == "request" {
				str := newRequestStream(stream, newRequestWriter(), nil, qpack.NewDecoder(), true, 1024, &http.Response{})
				req := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
				req.Trailer = http.Header{"Checksum": {"value"}}
				err = str.sendRequestTrailer(req)
			} else {
				w := newResponseWriter(stream, nil, false, nil)
				w.Header().Set("Trailer:Checksum", "value")
				err = w.writeTrailers()
			}
			requireHTTP3Error(t, err, true, "")
		})
	}
}
