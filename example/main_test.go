package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedResponse(t *testing.T) {
	handler := setupHandler("")
	for _, path := range []string{"/0", "/-1", "/not-a-size", "/" + strconv.Itoa(maxGeneratedResponseSize+1)} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/32785", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "32785", response.Header().Get("Content-Length"))
	require.Equal(t, 32785, response.Body.Len())
	// Golden digest of the original deterministic sequence across a chunk boundary.
	require.Equal(t, "01af897c0e1b3c7f2802a1aa4cc085c87cc4d3bf277c2805fb6383951baae4cd", fmt.Sprintf("%x", sha256.Sum256(response.Body.Bytes())))
}

type failingResponseWriter struct {
	*httptest.ResponseRecorder
	err       error
	writeSize int
	writes    int
}

func (w *failingResponseWriter) Write(p []byte) (int, error) {
	w.writeSize = len(p)
	w.writes++
	return 0, w.err
}

func TestGeneratedResponseStopsAfterWriteFailure(t *testing.T) {
	for _, writeErr := range []error{io.ErrClosedPipe, nil} {
		t.Run(fmt.Sprint(writeErr), func(t *testing.T) {
			w := &failingResponseWriter{ResponseRecorder: httptest.NewRecorder(), err: writeErr}
			// A maximum-size URL must reach the writer after generating one small
			// chunk and stop immediately on either a failed or short write.
			setupHandler("").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/1073741824", nil))
			require.Equal(t, 1, w.writes)
			require.Equal(t, responseChunkSize, w.writeSize)
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type countingReader struct {
	io.Reader
	bytesRead int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("body read failed")
}

func TestEchoBodyLimits(t *testing.T) {
	handler := setupHandler("")
	for _, tc := range []struct {
		name   string
		size   int64
		known  bool
		status int
	}{
		{name: "empty", known: true, status: http.StatusOK},
		{name: "at limit", size: maxEchoBodySize, status: http.StatusOK},
		{name: "known oversized body", size: maxEchoBodySize + 1, known: true, status: http.StatusRequestEntityTooLarge},
		{name: "streamed oversized body", size: maxEchoBodySize * 100, status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &countingReader{Reader: io.LimitReader(zeroReader{}, tc.size)}
			request := httptest.NewRequest(http.MethodPost, "/demo/echo", body)
			if tc.known {
				request.ContentLength = tc.size
			} else {
				request.ContentLength = -1
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
			if tc.status == http.StatusOK {
				require.EqualValues(t, tc.size, response.Body.Len())
			} else if tc.known {
				require.Zero(t, body.bytesRead, "reject known oversized bodies before reading")
			} else {
				require.EqualValues(t, maxEchoBodySize+1, body.bytesRead)
			}
		})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/demo/echo", strings.NewReader("echo this")))
	require.Equal(t, "echo this", response.Body.String())

	response = httptest.NewRecorder()
	body := io.MultiReader(strings.NewReader("partial body"), errorReader{})
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/demo/echo", body))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.NotContains(t, response.Body.String(), "partial body")
}

// Construct multipart bodies from separate readers to exercise large wire sizes
// without buffering the body in the test process.
func newUploadRequest(t *testing.T, parts ...io.Reader) *http.Request {
	t.Helper()
	var headers bytes.Buffer
	writer := multipart.NewWriter(&headers)
	readers := make([]io.Reader, 0, 2*len(parts)+1)
	for _, body := range parts {
		_, err := writer.CreateFormFile("uploadfile", "test.bin")
		require.NoError(t, err)
		readers = append(readers, bytes.NewReader(bytes.Clone(headers.Bytes())), body)
		headers.Reset()
	}
	require.NoError(t, writer.Close())
	readers = append(readers, bytes.NewReader(bytes.Clone(headers.Bytes())))
	request := httptest.NewRequest(http.MethodPost, "/demo/upload", io.MultiReader(readers...))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.ContentLength = -1
	return request
}

func TestUploadChecksum(t *testing.T) {
	handler := setupHandler("")
	for _, tc := range []struct {
		name     string
		body     string
		checksum string
	}{
		{name: "nonempty", body: "abc", checksum: "900150983cd24fb0d6963f7d28e17f72"},
		{name: "empty", checksum: "d41d8cd98f00b204e9800998ecf8427e"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, newUploadRequest(t, strings.NewReader(tc.body)))
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, tc.checksum, response.Body.String())
		})
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/demo/upload", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `<input type="file" name="uploadfile">`)
}

func TestUploadLimits(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)
	handler := setupHandler("")
	for _, tc := range []struct {
		name      string
		partSizes []int64
		known     bool
		epilogue  int64
		status    int
		maxRead   int64
	}{
		{name: "at part limit", partSizes: []int64{maxUploadPartSize}, status: http.StatusOK, maxRead: maxUploadBodySize},
		{name: "oversized file", partSizes: []int64{maxUploadPartSize * 100}, status: http.StatusRequestEntityTooLarge, maxRead: maxUploadPartSize + 8192},
		{name: "oversized total", partSizes: []int64{maxUploadPartSize, maxUploadPartSize}, status: http.StatusRequestEntityTooLarge, maxRead: maxUploadBodySize + 1},
		{name: "oversized epilogue", partSizes: []int64{1}, epilogue: maxUploadBodySize, status: http.StatusRequestEntityTooLarge, maxRead: maxUploadBodySize + 1},
		{name: "known oversized body", partSizes: []int64{1}, known: true, status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := make([]io.Reader, len(tc.partSizes))
			for i, size := range tc.partSizes {
				parts[i] = io.LimitReader(zeroReader{}, size)
			}
			request := newUploadRequest(t, parts...)
			body := &countingReader{Reader: io.MultiReader(request.Body, io.LimitReader(zeroReader{}, tc.epilogue))}
			request.Body = io.NopCloser(body)
			if tc.known {
				request.ContentLength = maxUploadBodySize + 1
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
			require.LessOrEqual(t, body.bytesRead, tc.maxRead)
			entries, err := os.ReadDir(tempDir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestUploadPartCount(t *testing.T) {
	handler := setupHandler("")
	for _, parts := range []int{maxUploadParts, maxUploadParts + 1} {
		t.Run(strconv.Itoa(parts), func(t *testing.T) {
			readers := make([]io.Reader, parts)
			for i := range readers {
				readers[i] = strings.NewReader("abc")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, newUploadRequest(t, readers...))
			if parts == maxUploadParts {
				require.Equal(t, http.StatusOK, response.Code)
				require.Equal(t, "900150983cd24fb0d6963f7d28e17f72", response.Body.String())
			} else {
				require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
			}
		})
	}
}

func TestUploadRejectsMalformedBody(t *testing.T) {
	handler := setupHandler("")
	for _, tc := range []struct {
		name        string
		contentType string
		body        io.Reader
	}{
		{name: "not multipart", contentType: "text/plain", body: strings.NewReader("abc")},
		{name: "missing file", contentType: "multipart/form-data; boundary=b", body: strings.NewReader("--b--\r\n")},
		{name: "truncated file", contentType: "multipart/form-data; boundary=b", body: strings.NewReader("--b\r\nContent-Disposition: form-data; name=\"uploadfile\"; filename=\"file\"\r\n\r\nabc")},
		{name: "incomplete boundary", contentType: "multipart/form-data; boundary=b", body: strings.NewReader("--b\r\nContent-Disposition: form-data; name=\"uploadfile\"; filename=\"file\"\r\n\r\nabc\r\n--b")},
		{name: "read error", contentType: "multipart/form-data; boundary=b", body: errorReader{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/demo/upload", tc.body)
			request.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
}

type blockedReader struct {
	entered chan<- struct{}
	release <-chan struct{}
}

func (r blockedReader) Read([]byte) (int, error) {
	r.entered <- struct{}{}
	<-r.release
	return 0, io.EOF
}

func TestHandlerConcurrencyLimit(t *testing.T) {
	handler := setupHandler("")
	entered := make(chan struct{}, maxConcurrentRequests)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	statuses := make(chan int, maxConcurrentRequests)
	var wg sync.WaitGroup
	defer func() {
		unblock()
		wg.Wait()
	}()
	for range maxConcurrentRequests {
		wg.Go(func() {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/demo/echo", blockedReader{entered: entered, release: release})
			handler.ServeHTTP(response, request)
			statuses <- response.Code
		})
	}
	for range maxConcurrentRequests {
		<-entered
	}
	for _, path := range []string{"/1024", "/demo/echo", "/demo/upload"} {
		response := httptest.NewRecorder()
		body := &countingReader{Reader: zeroReader{}}
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, body))
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		require.Zero(t, body.bytesRead)
	}
	unblock()
	wg.Wait()
	for range maxConcurrentRequests {
		require.Equal(t, http.StatusOK, <-statuses)
	}
	// Both successful and rejected requests must release their admission slot.
	for range maxConcurrentRequests + 1 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/0", nil))
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/1", nil))
	require.Equal(t, http.StatusOK, response.Code)
}
