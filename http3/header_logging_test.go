package http3

import (
	"bytes"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/quic-go/qpack"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestServerRequestHeaderLogging(t *testing.T) {
	for _, logging := range []bool{false, true} {
		name := "disabled"
		var recorder events.Recorder
		var logger qlogwriter.Recorder
		if logging {
			name = "enabled"
			logger = &recorder
		}
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "https://example.com/path?q=1", nil)
			req.Header = http.Header{"X-Test": {"one", "two"}, "User-Agent": {"header-test"}}
			testServerRequestHandling(t, func(w http.ResponseWriter, got *http.Request) {
				require.Equal(t, req.Header, got.Header)
				require.Equal(t, req.Method, got.Method)
				require.Equal(t, "example.com", got.Host)
				require.Equal(t, "/path?q=1", got.RequestURI)
				require.Equal(t, "HTTP/3.0", got.Proto)
			}, req, logger)
			parsed := recorder.Events(qlog.FrameParsed{})
			if !logging {
				require.Empty(t, parsed)
				return
			}
			require.Len(t, parsed, 1)
			require.ElementsMatch(t, []qlog.HeaderField{
				{Name: ":method", Value: "GET"},
				{Name: ":authority", Value: "example.com"},
				{Name: ":scheme", Value: "https"},
				{Name: ":path", Value: "/path?q=1"},
				{Name: "x-test", Value: "one"},
				{Name: "x-test", Value: "two"},
				{Name: "user-agent", Value: "header-test"},
			}, parsed[0].(qlog.FrameParsed).Frame.Frame.(qlog.HeadersFrame).HeaderFields)
		})
	}
}

func TestRequestStreamResponseHeaderLogging(t *testing.T) {
	fields := []qpack.HeaderField{
		{Name: ":status", Value: "201"},
		{Name: "content-length", Value: "0"},
		{Name: "x-test", Value: "one"},
		{Name: "x-test", Value: "two"},
	}
	var block bytes.Buffer
	encoder := qpack.NewEncoder(&block)
	for _, field := range fields {
		require.NoError(t, encoder.WriteField(field))
	}
	data := append((&headersFrame{Length: uint64(block.Len())}).Append(nil), block.Bytes()...)
	for _, logging := range []bool{false, true} {
		name := "disabled"
		var recorder events.Recorder
		var logger qlogwriter.Recorder
		if logging {
			name = "enabled"
			logger = &recorder
		}
		t.Run(name, func(t *testing.T) {
			qstr := NewMockDatagramStream(gomock.NewController(t))
			qstr.EXPECT().StreamID().Return(quic.StreamID(0)).AnyTimes()
			qstr.EXPECT().Write(gomock.Any()).DoAndReturn(io.Discard.Write)
			qstr.EXPECT().Read(gomock.Any()).DoAndReturn(bytes.NewReader(data).Read).AnyTimes()
			str := newRequestStream(newStream(qstr, nil, nil, nil, logger), newRequestWriter(), nil, qpack.NewDecoder(), true, math.MaxInt, &http.Response{})
			require.NoError(t, str.SendRequestHeader(httptest.NewRequest(http.MethodGet, "https://example.com", nil)))
			res, err := str.ReadResponse()
			require.NoError(t, err)
			require.Equal(t, http.StatusCreated, res.StatusCode)
			require.Equal(t, int64(0), res.ContentLength)
			require.Equal(t, "HTTP/3.0", res.Proto)
			require.Equal(t, http.Header{"Content-Length": {"0"}, "X-Test": {"one", "two"}}, res.Header)
			parsed := recorder.Events(qlog.FrameParsed{})
			if !logging {
				require.Empty(t, parsed)
				return
			}
			require.Len(t, parsed, 1)
			require.Equal(t, []qlog.HeaderField{
				{Name: ":status", Value: "201"},
				{Name: "content-length", Value: "0"},
				{Name: "x-test", Value: "one"},
				{Name: "x-test", Value: "two"},
			}, parsed[0].(qlog.FrameParsed).Frame.Frame.(qlog.HeadersFrame).HeaderFields)
		})
	}
}

// Isolate the tracing-only collection cost from transport and recorder allocations.
func TestHeaderCollectionAllocations(t *testing.T) {
	for _, request := range []bool{true, false} {
		name := "response"
		fields := []qpack.HeaderField{{Name: ":status", Value: "200"}}
		if request {
			name = "request"
			fields = []qpack.HeaderField{
				{Name: ":method", Value: "GET"},
				{Name: ":scheme", Value: "https"},
				{Name: ":authority", Value: "example.com"},
				{Name: ":path", Value: "/"},
			}
		}
		fields = append(fields, qpack.HeaderField{Name: "x-test", Value: "one"}, qpack.HeaderField{Name: "x-test", Value: "two"})
		t.Run(name, func(t *testing.T) {
			measure := func(collect bool) float64 {
				return testing.AllocsPerRun(100, func() {
					var collected []qpack.HeaderField
					var target *[]qpack.HeaderField
					if collect {
						// Match the old disabled-logging callers: an empty slice, passed by pointer.
						target = &collected
					}
					var err error
					if request {
						_, err = requestFromHeaders(decodeFromSlice(fields), math.MaxInt, target)
					} else {
						err = updateResponseFromHeaders(&http.Response{}, decodeFromSlice(fields), math.MaxInt, target)
					}
					if err != nil {
						t.Fatal(err)
					}
				})
			}
			without, with := measure(false), measure(true)
			t.Logf("without collection: %.0f allocs; legacy collection: %.0f allocs", without, with)
			require.Less(t, without, with)
		})
	}
}
