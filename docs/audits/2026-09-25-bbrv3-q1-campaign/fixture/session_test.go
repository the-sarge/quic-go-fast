package main

import (
	"context"
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

func TestNativeFixtureRoundTrip(t *testing.T) {
	for _, workload := range []string{"stream", "datagram", "completion", "delayed-start"} {
		t.Run(workload, func(t *testing.T) {
			cert, key, err := createCertificate(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg := Run{ID: "characterization", Controller: "reno", Workload: workload, WarmupMS: 100, MeasureMS: 1100, PayloadBytes: 1200, StartUnixNS: time.Now().Add(500 * time.Millisecond).UnixNano()}
			if workload == "delayed-start" {
				cfg.Workload = "stream"
				cfg.StartUnixNS = time.Now().Add(11 * time.Second).UnixNano()
			}
			if workload == "completion" {
				cfg.Workload = "stream"
				cfg.WarmupMS = 0
				cfg.MeasureMS = 60000
				cfg.CompletionBytes = 256 << 10
			}
			tlsServer, err := loadTLS(cert, key, true)
			if err != nil {
				t.Fatal(err)
			}
			tlsClient, err := loadTLS(cert, key, false)
			if err != nil {
				t.Fatal(err)
			}
			server, closeServer, err := newTransport("127.0.0.1:0", true)
			if err != nil {
				t.Fatal(err)
			}
			defer closeServer()
			client, closeClient, err := newTransport("127.0.0.1:0", true)
			if err != nil {
				t.Fatal(err)
			}
			defer closeClient()
			ctx, cancel := context.WithTimeout(context.Background(), time.Until(cfg.start())+6*time.Second)
			defer cancel()
			ln, err := server.Listen(tlsServer, quicConfig(cfg, newTrace()))
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan error, 1)
			go func() {
				c, e := ln.Accept(ctx)
				if e == nil {
					_, e = receiveSession(ctx, c, cfg)
				}
				done <- e
			}()
			conn, err := client.Dial(ctx, ln.Addr(), tlsClient, quicConfig(cfg, newTrace()))
			if err != nil {
				t.Fatal(err)
			}
			result, err := sendSession(ctx, conn, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = <-done; err != nil {
				t.Fatal(err)
			}
			if result.Receiver.UsefulBytes == 0 || result.Receiver.Corrupt != 0 || (cfg.CompletionBytes == 0 && result.Control.Replies == 0) {
				t.Fatalf("invalid native result: %+v", result)
			}
			if cfg.CompletionBytes > 0 && (!result.CompletionVerified || result.Censored || result.CompletionNS <= 0 || result.Receiver.UsefulBytes != 256<<10) {
				t.Fatalf("completion: %+v", result)
			}
			if result.Controller != "reno" {
				t.Fatalf("controller: %s", result.Controller)
			}
			conn.CloseWithError(0, "done")
		})
	}
}

func TestControlPendingCadenceAndWarmupAccounting(t *testing.T) {
	for _, warmup := range []int{0, 1000} {
		t.Run(time.Duration(warmup).String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := net.Pipe()
				defer client.Close()
				defer server.Close()
				cfg := Run{StartUnixNS: time.Now().UnixNano(), WarmupMS: int64(warmup), MeasureMS: int64(2100 - warmup)}
				ctx, cancel := context.WithTimeout(context.Background(), 2100*time.Millisecond)
				defer cancel()
				done := make(chan Control, 1)
				go func() { done <- observeControl(ctx, client, cfg) }()
				var request [32]byte
				if _, err := io.ReadFull(server, request[:]); err != nil {
					t.Fatal(err)
				}
				// Deliberately never answer. A single pending request must suppress new offers.
				synctest.Wait()
				time.Sleep(2100 * time.Millisecond)
				c := <-done
				want := 1
				if warmup > 0 {
					want = 0
				}
				if c.Offered != want || c.Unresolved != want || c.Skipped != 2 || c.Replies != 0 {
					t.Fatalf("pending cadence: %+v", c)
				}
			})
		})
	}
}
