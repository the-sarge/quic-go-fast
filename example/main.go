package main

import (
	"crypto/md5"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	_ "net/http/pprof"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/http3/qlog"
	"github.com/quic-go/quic-go/internal/testdata"
)

type binds []string

func (b binds) String() string {
	return strings.Join(b, ",")
}

func (b *binds) Set(v string) error {
	*b = strings.Split(v, ",")
	return nil
}

const (
	maxGeneratedResponseSize = 1 << 30
	maxEchoBodySize          = 1 << 20
	maxUploadBodySize        = 16 << 20
	maxUploadPartSize        = 8 << 20
	maxUploadParts           = 16
	maxConcurrentRequests    = 8
	responseChunkSize        = 32 << 10
)

// See https://en.wikipedia.org/wiki/Lehmer_random_number_generator
func writePRData(w io.Writer, size int64) error {
	var buf [responseChunkSize]byte
	seed := uint64(1)
	for size > 0 {
		chunk := buf[:min(size, int64(len(buf)))]
		for i := range chunk {
			seed = seed * 48271 % 2147483647
			chunk[i] = byte(seed)
		}
		n, err := w.Write(chunk)
		if err != nil {
			return err
		}
		if n != len(chunk) {
			return io.ErrShortWrite
		}
		size -= int64(n)
	}
	return nil
}

func writeBodyError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		status = http.StatusRequestEntityTooLarge
	}
	http.Error(w, http.StatusText(status), status)
}

func echo(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxEchoBodySize {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	// Buffer only this small, bounded body so oversized streaming requests can
	// receive a 413 before any successful response is committed.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEchoBodySize))
	if err != nil {
		writeBodyError(w, err)
		return
	}
	w.Write(body)
}

func upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		io.WriteString(w, `<html><body><form action="/demo/upload" method="post" enctype="multipart/form-data">
				<p>Maximum upload: 8 MiB per part, 16 MiB total.</p>
				<input type="file" name="uploadfile"><br>
				<input type="submit">
			</form></body></html>`)
		return
	}
	if r.ContentLength > maxUploadBodySize {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBodySize)
	reader, err := r.MultipartReader()
	if err != nil {
		writeBodyError(w, err)
		return
	}
	var checksum []byte
	for parts := 0; ; parts++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeBodyError(w, err)
			return
		}
		if parts >= maxUploadParts {
			http.Error(w, "too many multipart parts", http.StatusRequestEntityTooLarge)
			return
		}
		hash := md5.New()
		dst := io.Discard
		isUpload := checksum == nil && part.FormName() == "uploadfile" && part.FileName() != ""
		if isUpload {
			dst = hash
		}
		n, err := io.Copy(dst, io.LimitReader(part, maxUploadPartSize+1))
		if err != nil {
			writeBodyError(w, err)
			return
		}
		if n > maxUploadPartSize {
			http.Error(w, "multipart part too large", http.StatusRequestEntityTooLarge)
			return
		}
		if isUpload {
			checksum = hash.Sum(nil)
		}
	}
	// Include any multipart epilogue in the total body budget before returning
	// success. MultipartReader can stop at the closing boundary before body EOF.
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		writeBodyError(w, err)
		return
	}
	if checksum == nil {
		http.Error(w, "missing uploadfile", http.StatusBadRequest)
		return
	}
	fmt.Fprintf(w, "%x", checksum)
}

func setupHandler(www string) http.Handler {
	mux := http.NewServeMux()

	if len(www) > 0 {
		mux.Handle("/", http.FileServer(http.Dir(www)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			num, err := strconv.ParseInt(strings.ReplaceAll(r.RequestURI, "/", ""), 10, 64)
			if err != nil || num <= 0 || num > maxGeneratedResponseSize {
				w.WriteHeader(400)
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(num, 10))
			if err := writePRData(w, num); err != nil {
				log.Printf("Error sending generated data: %v", err)
			}
		})
	}

	mux.HandleFunc("/demo/tile", func(w http.ResponseWriter, r *http.Request) {
		// Small 40x40 png
		w.Write([]byte{
			0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
			0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x28,
			0x01, 0x03, 0x00, 0x00, 0x00, 0xb6, 0x30, 0x2a, 0x2e, 0x00, 0x00, 0x00,
			0x03, 0x50, 0x4c, 0x54, 0x45, 0x5a, 0xc3, 0x5a, 0xad, 0x38, 0xaa, 0xdb,
			0x00, 0x00, 0x00, 0x0b, 0x49, 0x44, 0x41, 0x54, 0x78, 0x01, 0x63, 0x18,
			0x61, 0x00, 0x00, 0x00, 0xf0, 0x00, 0x01, 0xe2, 0xb8, 0x75, 0x22, 0x00,
			0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
		})
	})

	mux.HandleFunc("/demo/tiles", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<html><head><style>img{width:40px;height:40px;}</style></head><body>")
		for i := range 200 {
			fmt.Fprintf(w, `<img src="/demo/tile?cachebust=%d">`, i)
		}
		io.WriteString(w, "</body></html>")
	})

	mux.HandleFunc("/demo/echo", echo)

	// accept file uploads and return the MD5 of the uploaded file
	// Limit each part to 8 MiB and the entire multipart body to 16 MiB.
	mux.HandleFunc("/demo/upload", upload)

	// Bound aggregate work without queueing requests and their bodies.
	active := make(chan struct{}, maxConcurrentRequests)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			http.Error(w, "server busy", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func main() {
	// defer profile.Start().Stop()
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
	// runtime.SetBlockProfileRate(1)

	bs := binds{}
	flag.Var(&bs, "bind", "bind to")
	www := flag.String("www", "", "www data")
	tcp := flag.Bool("tcp", false, "also listen on TCP")
	key := flag.String("key", "", "TLS key (requires -cert option)")
	cert := flag.String("cert", "", "TLS certificate (requires -key option)")
	flag.Parse()

	if len(bs) == 0 {
		bs = binds{"localhost:6121"}
	}

	handler := setupHandler(*www)

	var wg sync.WaitGroup
	var certFile, keyFile string
	if *key != "" && *cert != "" {
		keyFile = *key
		certFile = *cert
	} else {
		certFile, keyFile = testdata.GetCertificatePaths()
	}
	for _, b := range bs {
		fmt.Println("listening on", b)
		bCap := b
		wg.Go(func() {
			var err error
			if *tcp {
				err = http3.ListenAndServeTLS(bCap, certFile, keyFile, handler)
			} else {
				server := http3.Server{
					Handler: handler,
					Addr:    bCap,
					QUICConfig: &quic.Config{
						Tracer: qlog.DefaultConnectionTracer,
					},
				}
				err = server.ListenAndServeTLS(certFile, keyFile)
			}
			if err != nil {
				fmt.Println(err)
			}
		})
	}
	wg.Wait()
}
