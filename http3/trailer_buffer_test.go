package http3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/quic-go/qpack"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newTrailerTestStream mirrors the client response and server request trailer callbacks,
// which differ only in the maxHeaderBytes they pass to decodeTrailers.
func newTrailerTestStream(t *testing.T, qstr *MockDatagramStream, maxHeaderBytes int, recorder qlogwriter.Recorder) (*Stream, *http.Header) {
	t.Helper()
	qstr.EXPECT().StreamID().Return(quic.StreamID(42)).AnyTimes()
	trailers := new(http.Header)
	str := newStream(qstr, nil, nil, func(r io.Reader, hf *headersFrame, recorder qlogwriter.Recorder) error {
		hdr, err := decodeTrailers(r, hf, maxHeaderBytes, qpack.NewDecoder(), recorder, quic.StreamID(42))
		if err != nil {
			return err
		}
		*trailers = hdr
		return nil
	}, recorder)
	return str, trailers
}

// readBodyThenTrailer reads the single "body" DATA frame, then returns the error of the Read that parses the trailer.
func readBodyThenTrailer(t *testing.T, str *Stream) error {
	t.Helper()
	b := make([]byte, 16)
	n, err := str.Read(b)
	require.NoError(t, err)
	require.Equal(t, "body", string(b[:n]))
	_, err = str.Read(b)
	return err
}

func encodeTrailerBlock(t *testing.T, fields ...qpack.HeaderField) []byte {
	t.Helper()
	var block bytes.Buffer
	enc := qpack.NewEncoder(&block)
	for _, f := range fields {
		require.NoError(t, enc.WriteField(f))
	}
	require.NoError(t, enc.Close())
	return block.Bytes()
}

func TestTrailerBufferingStalledTrailers(t *testing.T) {
	for _, side := range []struct {
		name           string
		maxHeaderBytes int
	}{
		{name: "client", maxHeaderBytes: defaultMaxResponseHeaderBytes},
		{name: "server", maxHeaderBytes: http.DefaultMaxHeaderBytes},
	} {
		for _, payloadBytes := range []int{0, 1024} {
			t.Run(fmt.Sprintf("%s/payload_%d", side.name, payloadBytes), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					const streams = 16
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					capacities := make(chan int, streams)
					results := make(chan error, streams)
					for range streams {
						buf := bytes.NewBuffer(getDataFrame([]byte("body")))
						buf.Write((&headersFrame{Length: uint64(side.maxHeaderBytes)}).Append(nil))
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
						str, _ := newTrailerTestStream(t, qstr, side.maxHeaderBytes, nil)
						go func() {
							_, err := io.ReadAll(str)
							results <- err
						}()
					}
					synctest.Wait()
					require.Len(t, capacities, streams)
					var totalCapacity int
					for range streams {
						totalCapacity += <-capacities
					}
					t.Logf("%d stalled trailers retain %d bytes of trailer buffers after receiving %d payload bytes each", streams, totalCapacity, payloadBytes)
					time.Sleep(time.Second)
					synctest.Wait()
					require.Len(t, results, streams)
					for range streams {
						require.ErrorIs(t, <-results, context.DeadlineExceeded)
					}
					// Bound reservation by received data plus small growth overhead, independently of the declared length.
					require.LessOrEqual(t, totalCapacity, streams*(2*payloadBytes+4096))
				})
			})
		}
	}
}

func TestTrailerBufferingReadErrors(t *testing.T) {
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
			buf := bytes.NewBuffer(getDataFrame([]byte("body")))
			buf.Write((&headersFrame{Length: 8}).Append(nil))
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
				if buf.Len() > 0 {
					return buf.Read(p)
				}
				return copy(p, tc.payload), tc.readErr
			}).AnyTimes()
			str, trailers := newTrailerTestStream(t, qstr, http.DefaultMaxHeaderBytes, nil)
			err := readBodyThenTrailer(t, str)
			require.ErrorIs(t, err, tc.wantErr)
			require.Nil(t, *trailers)
		})
	}
}

func TestTrailerBufferingCompleteReadError(t *testing.T) {
	block := encodeTrailerBlock(t, qpack.HeaderField{Name: "finished", Value: "yes"})
	for _, readErr := range []error{io.EOF, context.DeadlineExceeded} {
		t.Run(readErr.Error(), func(t *testing.T) {
			buf := bytes.NewBuffer(getDataFrame([]byte("body")))
			buf.Write((&headersFrame{Length: uint64(len(block))}).Append(nil))
			buf.Write(block)
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
				n, err := buf.Read(p)
				if buf.Len() == 0 {
					return n, readErr
				}
				return n, err
			}).AnyTimes()
			str, trailers := newTrailerTestStream(t, qstr, http.DefaultMaxHeaderBytes, nil)
			require.NoError(t, readBodyThenTrailer(t, str))
			require.Equal(t, http.Header{"Finished": {"yes"}}, *trailers)
		})
	}
}

func TestTrailerBufferingFrameBoundary(t *testing.T) {
	value := strings.Repeat("a", 8192)
	block := encodeTrailerBlock(t, qpack.HeaderField{Name: "x-large", Value: value})
	tail := getDataFrame([]byte("tail"))
	buf := bytes.NewBuffer(getDataFrame([]byte("body")))
	buf.Write((&headersFrame{Length: uint64(len(block))}).Append(nil))
	buf.Write(block)
	buf.Write(tail)
	qstr := NewMockDatagramStream(gomock.NewController(t))
	qstr.EXPECT().Read(gomock.Any()).DoAndReturn(func(p []byte) (int, error) {
		return buf.Read(p[:min(len(p), 127)])
	}).AnyTimes()
	str, trailers := newTrailerTestStream(t, qstr, http.DefaultMaxHeaderBytes, nil)
	client, _ := newConnPair(t)
	str.conn = newRawConn(client, false, nil, nopControlStrHandler, nil, nil)
	require.NoError(t, readBodyThenTrailer(t, str))
	require.Equal(t, value, trailers.Get("X-Large"))
	// Nothing past the trailer frame was consumed.
	require.Equal(t, tail, buf.Bytes())
	_, err := str.Read(make([]byte, 16))
	require.EqualError(t, err, "DATA frame received after trailers")
}

func TestTrailerBufferingTooLarge(t *testing.T) {
	const maxHeaderBytes = 1337
	payload := make([]byte, 16)
	buf := bytes.NewBuffer(getDataFrame([]byte("body")))
	buf.Write((&headersFrame{Length: maxHeaderBytes + 1}).Append(nil))
	buf.Write(payload)
	qstr := NewMockDatagramStream(gomock.NewController(t))
	qstr.EXPECT().Read(gomock.Any()).DoAndReturn(buf.Read).AnyTimes()
	var recorder events.Recorder
	str, trailers := newTrailerTestStream(t, qstr, maxHeaderBytes, &recorder)
	err := readBodyThenTrailer(t, str)
	require.EqualError(t, err, "http3: HEADERS frame too large: 1338 bytes (max: 1337)")
	require.Nil(t, *trailers)
	// The size check runs before any payload is read.
	require.Equal(t, payload, buf.Bytes())
	require.Equal(t,
		[]qlogwriter.Event{
			qlog.FrameParsed{
				StreamID: 42,
				Raw:      qlog.RawInfo{PayloadLength: maxHeaderBytes + 1},
				Frame:    qlog.Frame{Frame: qlog.HeadersFrame{}},
			},
		},
		filterQlogEventsForFrame(recorder.Events(qlog.FrameParsed{}), qlog.HeadersFrame{}),
	)
}
