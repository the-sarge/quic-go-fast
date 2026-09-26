package main

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Delay only the real UDP boundary. QUIC framing, flow control, session
// termination and receiver accounting all remain the real implementations.
type completionLimitedConn struct {
	net.PacketConn
	start   time.Time
	mu      sync.Mutex
	delayed bool
}

func (c *completionLimitedConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !time.Now().Before(c.start) {
		if !c.delayed {
			c.delayed = true
			// Separate sender start from receiver admission, so a premature
			// FIN is observable before the receiver's censoring deadline.
			time.Sleep(3 * time.Second)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return c.PacketConn.WriteTo(p, addr)
}

func TestIncompleteCompletionIsCensored(t *testing.T) {
	cert, key, err := createCertificate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, err := loadTLS(cert, key, true)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := loadTLS(cert, key, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Run{ID: "censored-completion", Controller: "reno", Workload: "stream", StartUnixNS: time.Now().Add(time.Second).UnixNano(), MeasureMS: 60000, PayloadBytes: 16384, CompletionBytes: 16 << 20}
	server, closeServer, err := newTransport("127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeServer()
	client, closeClient, err := newTransport("127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	client.Conn = &completionLimitedConn{PacketConn: client.Conn, start: cfg.start()}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	listener, err := server.Listen(serverTLS, quicConfig(cfg, newTrace()))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, e := listener.Accept(ctx)
		if e == nil {
			_, e = receiveSession(ctx, conn, cfg)
		}
		done <- e
	}()
	conn, err := client.Dial(ctx, listener.Addr(), clientTLS, quicConfig(cfg, newTrace()))
	if err != nil {
		t.Fatal(err)
	}
	result, err := sendSession(ctx, conn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !result.Censored || result.CompletionVerified || len(result.Errors) != 0 || result.Receiver.Corrupt != 0 || result.Receiver.UsefulBytes == 0 || result.Receiver.UsefulBytes >= uint64(cfg.CompletionBytes) {
		t.Fatalf("incomplete transfer must be censored without an integrity error: %+v", result)
	}
}

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
				expected := 3
				if warmup > 0 {
					expected = 2
				}
				if c.ExpectedOpportunities != expected || len(c.Opportunities) != expected || c.MissedOpportunities != 0 {
					t.Fatalf("cadence receipt: %+v", c)
				}
				for i, opportunity := range c.Opportunities {
					wantDue := cfg.start().Add(time.Duration(i+warmup/1000) * time.Second).UnixNano()
					if opportunity[0] != wantDue || opportunity[1] != wantDue {
						t.Fatalf("opportunity %d = %v, want scheduled and observed %d", i, opportunity, wantDue)
					}
				}
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

func TestCompetitorPauseRetainsFullMeasurementWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := Run{StartUnixNS: time.Now().UnixNano(), WarmupMS: 60000, MeasureMS: 300000, BulkPauseStartMS: 240000, BulkPauseEndMS: 270000}
		originalEnd := cfg.end()
		time.Sleep(300 * time.Second)
		var result Result
		done := make(chan error, 1)
		go func() { done <- waitBulkDemand(context.Background(), cfg, &result) }()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("bulk resumed inside absence interval")
		default:
		}
		time.Sleep(30 * time.Second)
		if e := <-done; e != nil {
			t.Fatal(e)
		}
		if cfg.end() != originalEnd || len(result.BulkPauseObservedNS) != 2 || result.BulkPauseObservedNS[1]-result.BulkPauseObservedNS[0] != int64(30*time.Second) {
			t.Fatalf("pause changed timing: %+v", result)
		}
	})
}
