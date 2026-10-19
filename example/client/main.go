package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/http3/qlog"
	"github.com/quic-go/quic-go/internal/testdata"
)

func main() {
	quiet := flag.Bool("q", false, "don't print the data")
	keyLogFile := flag.String("keylog", "", "key log file")
	insecure := flag.Bool("insecure", false, "skip certificate verification")
	timeout := flag.Duration("timeout", 30*time.Second, "maximum time per request, including reading the response body (must be positive)")
	maxResponseSize := flag.Int64("max-response-size", 10<<20, "maximum response body size in bytes (must be positive)")
	flag.Parse()
	urls := flag.Args()
	if *timeout <= 0 || *maxResponseSize <= 0 {
		log.Fatal("timeout and max-response-size must be positive")
	}

	var keyLog io.Writer
	if len(*keyLogFile) > 0 {
		f, err := os.Create(*keyLogFile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		keyLog = f
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		log.Fatal(err)
	}
	testdata.AddRootCA(pool)

	roundTripper := &http3.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:            pool,
			InsecureSkipVerify: *insecure,
			KeyLogWriter:       keyLog,
		},
		QUICConfig: &quic.Config{
			Tracer: qlog.DefaultConnectionTracer,
		},
	}
	defer roundTripper.Close()
	hclient := &http.Client{
		Transport: roundTripper,
		Timeout:   *timeout,
	}

	var output io.Writer = os.Stdout
	if *quiet {
		output = io.Discard
	}
	var wg sync.WaitGroup
	for _, addr := range urls {
		log.Printf("GET %s", addr)
		wg.Go(func() {
			n, err := fetchResponse(hclient, addr, output, *maxResponseSize)
			if err != nil {
				log.Fatalf("GET %s: %v", addr, err)
			}
			log.Printf("Response Body for %s: %d bytes", addr, n)
		})
	}
	wg.Wait()
}

func fetchResponse(client *http.Client, addr string, output io.Writer, maxResponseSize int64) (int64, error) {
	rsp, err := client.Get(addr)
	if err != nil {
		return 0, err
	}
	defer rsp.Body.Close()
	log.Printf("Got response for %s: %#v", addr, rsp)

	n, err := io.Copy(output, io.LimitReader(rsp.Body, maxResponseSize))
	if err != nil || n < maxResponseSize {
		return n, err
	}
	// Probe for overflow without writing beyond the limit or overflowing an int64.
	if _, err := io.CopyN(io.Discard, rsp.Body, 1); err != io.EOF {
		if err != nil {
			return n, err
		}
		return n, fmt.Errorf("response body exceeds %d bytes", maxResponseSize)
	}
	return n, nil
}
