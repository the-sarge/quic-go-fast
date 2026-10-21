package http3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/qpack"
	"github.com/quic-go/quic-go"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newResponseHeaderTestStream(t *testing.T, qstr *MockDatagramStream) *RequestStream {
	t.Helper()
	qstr.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
	qstr.EXPECT().Write(gomock.Any()).DoAndReturn(func(p []byte) (int, error) { return len(p), nil }).AnyTimes()
	str := newRequestStream(newStream(qstr, nil, nil, nil, nil), newRequestWriter(), nil, qpack.NewDecoder(), true, defaultMaxResponseHeaderBytes, &http.Response{})
	require.NoError(t, str.SendRequestHeader(httptest.NewRequest(http.MethodGet, "https://example.com", nil)))
	return str
}

func TestResponseHeaderBufferingStalledResponses(t *testing.T) {
	for _, payloadBytes := range []int{0, 1024} {
		t.Run(fmt.Sprintf("payload_%d", payloadBytes), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const responses = 16
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				capacities := make(chan int, responses)
				results := make(chan error, responses)
				for range responses {
					buf := bytes.NewBuffer((&headersFrame{Length: defaultMaxResponseHeaderBytes}).Append(nil))
					buf.Write(make([]byte, payloadBytes))
					qstr := NewMockDatagramStream(gomock.NewController(t))
					qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
						if buf.Len() > 0 {
							return buf.Read(p)
						}
						// The backing buffer stays live while this read waits for the peer.
						capacities <- payloadBytes + cap(p)
						<-ctx.Done()
						return 0, ctx.Err()
					}).AnyTimes()
					qstr.EXPECT().CancelRead(quic.StreamErrorCode(ErrCodeRequestIncomplete))
					qstr.EXPECT().CancelWrite(quic.StreamErrorCode(ErrCodeRequestIncomplete))
					str := newResponseHeaderTestStream(t, qstr)
					go func() {
						_, err := str.ReadResponse()
						results <- err
					}()
				}
				synctest.Wait()
				require.Len(t, capacities, responses)
				var totalCapacity int
				for range responses {
					totalCapacity += <-capacities
				}
				t.Logf("%d stalled responses retain %d bytes of header buffers after receiving %d payload bytes each", responses, totalCapacity, payloadBytes)
				time.Sleep(time.Second)
				synctest.Wait()
				require.Len(t, results, responses)
				for range responses {
					require.ErrorIs(t, <-results, context.DeadlineExceeded)
				}
				// Bound reservation by received data plus small growth overhead, independently of the declared 10 MiB.
				require.LessOrEqual(t, totalCapacity, responses*(2*payloadBytes+4096))
			})
		})
	}
}

func TestResponseHeaderBufferingCompleteReadError(t *testing.T) {
	for _, readErr := range []error{io.EOF, context.DeadlineExceeded} {
		t.Run(readErr.Error(), func(t *testing.T) {
			buf := bytes.NewBuffer(encodeResponse(t, http.StatusOK))
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
				n, err := buf.Read(p)
				if buf.Len() == 0 {
					return n, readErr
				}
				return n, err
			}).AnyTimes()
			str := newResponseHeaderTestStream(t, qstr)
			rsp, err := str.ReadResponse()
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, rsp.StatusCode)
		})
	}
}

func TestResponseHeaderBufferingFrameBoundary(t *testing.T) {
	var block bytes.Buffer
	enc := qpack.NewEncoder(&block)
	require.NoError(t, enc.WriteField(qpack.HeaderField{Name: ":status", Value: "200"}))
	value := strings.Repeat("a", 8192)
	require.NoError(t, enc.WriteField(qpack.HeaderField{Name: "x-large", Value: value}))
	require.NoError(t, enc.Close())
	buf := bytes.NewBuffer((&headersFrame{Length: uint64(block.Len())}).Append(nil))
	buf.Write(block.Bytes())
	buf.Write(getDataFrame([]byte("body")))
	qstr := NewMockDatagramStream(gomock.NewController(t))
	qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
		return buf.Read(p[:min(len(p), 127)])
	}).AnyTimes()
	str := newResponseHeaderTestStream(t, qstr)
	rsp, err := str.ReadResponse()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	require.Equal(t, value, rsp.Header.Get("X-Large"))
	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, "body", string(body))
}

func TestResponseHeaderBufferingReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		readErr error
		wantErr error
	}{
		{name: "empty", readErr: io.EOF, wantErr: io.EOF},
		{name: "truncated", payload: []byte{0}, readErr: io.EOF, wantErr: io.ErrUnexpectedEOF},
		{name: "deadline", payload: []byte{0}, readErr: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.NewBuffer((&headersFrame{Length: 8}).Append(nil))
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
				if buf.Len() > 0 {
					return buf.Read(p)
				}
				return copy(p, tc.payload), tc.readErr
			}).AnyTimes()
			qstr.EXPECT().CancelRead(quic.StreamErrorCode(ErrCodeRequestIncomplete))
			qstr.EXPECT().CancelWrite(quic.StreamErrorCode(ErrCodeRequestIncomplete))
			str := newResponseHeaderTestStream(t, qstr)
			_, err := str.ReadResponse()
			require.ErrorIs(t, err, tc.wantErr)
			require.ErrorContains(t, err, "http3: failed to read response headers")
		})
	}
}
