package http3

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlogwriter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestBodyContentLength(t *testing.T) {
	for _, response := range []bool{false, true} {
		name := "request"
		if response {
			name = "response"
		}
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name          string
				contentLength int64
				data          string
				wantErr       error
			}{
				{name: "short", contentLength: 6, data: "foo", wantErr: io.ErrUnexpectedEOF},
				{name: "empty short", contentLength: 6, wantErr: io.ErrUnexpectedEOF},
				{name: "exact", contentLength: 6, data: "foobar"},
				{name: "unknown", contentLength: -1, data: "foo"},
				{name: "empty unknown", contentLength: -1},
				{name: "zero", contentLength: 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					for _, eofWithData := range []bool{false, true} {
						name := "separate EOF"
						if eofWithData {
							name = "EOF with data"
						}
						t.Run(name, func(t *testing.T) {
							buf := bytes.NewBuffer(getDataFrame([]byte(tc.data)))
							str := NewMockDatagramStream(gomock.NewController(t))
							str.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
							str.EXPECT().Read(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
								n, err := buf.Read(b)
								if eofWithData && buf.Len() == 0 {
									err = io.EOF
								}
								return n, err
							}).AnyTimes()
							if tc.wantErr != nil {
								str.EXPECT().CancelRead(quic.StreamErrorCode(ErrCodeMessageError))
								str.EXPECT().CancelWrite(quic.StreamErrorCode(ErrCodeMessageError))
							}
							hstr := newStream(str, nil, nil, nil, nil)
							done := make(chan struct{})
							var body io.Reader = newRequestBody(hstr, tc.contentLength, context.Background(), nil, nil)
							if response {
								body = newResponseBody(hstr, tc.contentLength, done)
							}
							data, err := io.ReadAll(body)
							require.Equal(t, tc.data, string(data))
							if tc.wantErr == nil {
								require.NoError(t, err)
							} else {
								require.ErrorIs(t, err, tc.wantErr)
								// Keep the error stable without canceling the stream again.
								n, err := body.Read(make([]byte, 1))
								require.Zero(t, n)
								require.ErrorIs(t, err, tc.wantErr)
							}
							if response {
								select {
								case <-done:
								default:
									t.Fatal("response body did not finish the request")
								}
							}
						})
					}
				})
			}
		})
	}
}

func TestResponseBodyReading(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	var buf bytes.Buffer
	buf.Write(getDataFrame([]byte("foobar")))
	str := NewMockDatagramStream(mockCtrl)
	str.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
	str.EXPECT().Read(gomock.Any()).DoAndReturn(buf.Read).AnyTimes()
	reqDone := make(chan struct{})
	rb := newResponseBody(
		newStream(str, nil, nil, func(io.Reader, *headersFrame, qlogwriter.Recorder) error { return nil }, nil),
		-1,
		reqDone,
	)

	data, err := io.ReadAll(rb)
	require.NoError(t, err)
	require.Equal(t, []byte("foobar"), data)
}

func TestResponseBodyReadError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	str := NewMockDatagramStream(mockCtrl)
	str.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
	str.EXPECT().Read(gomock.Any()).Return(0, assert.AnError).Times(2)
	reqDone := make(chan struct{})
	rb := newResponseBody(
		newStream(str, nil, nil, func(io.Reader, *headersFrame, qlogwriter.Recorder) error { return nil }, nil),
		-1,
		reqDone,
	)

	_, err := rb.Read([]byte{0})
	require.ErrorIs(t, err, assert.AnError)
	// repeated calls to Read should return the same error
	_, err = rb.Read([]byte{0})
	require.ErrorIs(t, err, assert.AnError)
	select {
	case <-reqDone:
	default:
		t.Fatal("reqDone should be closed")
	}
}

func TestResponseBodyClose(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	str := NewMockDatagramStream(mockCtrl)
	str.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
	str.EXPECT().CancelRead(quic.StreamErrorCode(ErrCodeRequestCanceled)).Times(2)
	reqDone := make(chan struct{})
	rb := newResponseBody(
		newStream(str, nil, nil, func(io.Reader, *headersFrame, qlogwriter.Recorder) error { return nil }, nil),
		-1,
		reqDone,
	)
	require.NoError(t, rb.Close())
	select {
	case <-reqDone:
	default:
		t.Fatal("reqDone should be closed")
	}

	// multiple calls to Close should be a no-op
	require.NoError(t, rb.Close())
}

func TestResponseBodyConcurrentClose(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	str := NewMockDatagramStream(mockCtrl)
	str.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
	str.EXPECT().CancelRead(quic.StreamErrorCode(ErrCodeRequestCanceled)).MaxTimes(3)
	reqDone := make(chan struct{})
	rb := newResponseBody(
		newStream(str, nil, nil, func(io.Reader, *headersFrame, qlogwriter.Recorder) error { return nil }, nil),
		-1,
		reqDone,
	)

	for range 3 {
		go rb.Close()
	}
	select {
	case <-reqDone:
	case <-time.After(time.Second):
		t.Fatal("reqDone should be closed")
	}
}

func TestResponseBodyLengthLimiting(t *testing.T) {
	t.Run("along frame boundary", func(t *testing.T) {
		testResponseBodyLengthLimiting(t, true)
	})

	t.Run("in the middle of a frame", func(t *testing.T) {
		testResponseBodyLengthLimiting(t, false)
	})
}

func testResponseBodyLengthLimiting(t *testing.T, alongFrameBoundary bool) {
	var buf bytes.Buffer
	buf.Write(getDataFrame([]byte("foo")))
	buf.Write(getDataFrame([]byte("bar")))

	l := int64(4)
	if alongFrameBoundary {
		l = 3
	}
	mockCtrl := gomock.NewController(t)
	str := NewMockDatagramStream(mockCtrl)
	str.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
	str.EXPECT().CancelRead(quic.StreamErrorCode(ErrCodeMessageError))
	str.EXPECT().CancelWrite(quic.StreamErrorCode(ErrCodeMessageError))
	str.EXPECT().Read(gomock.Any()).DoAndReturn(buf.Read).AnyTimes()
	rb := newResponseBody(
		newStream(str, nil, nil, func(io.Reader, *headersFrame, qlogwriter.Recorder) error { return nil }, nil),
		l,
		make(chan struct{}),
	)
	data, err := io.ReadAll(rb)
	require.Equal(t, []byte("foobar")[:l], data)
	require.ErrorIs(t, err, errTooMuchData)
	// check that repeated calls to Read also return the right error
	n, err := rb.Read([]byte{0})
	require.Zero(t, n)
	require.ErrorIs(t, err, errTooMuchData)
}
