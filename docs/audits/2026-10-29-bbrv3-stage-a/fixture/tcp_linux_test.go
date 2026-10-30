package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestTCPCompetitorNativeAccounting(t *testing.T) {
	pair := func() (*net.TCPConn, *net.TCPConn) {
		t.Helper()
		ln, e := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if e != nil {
			t.Fatal(e)
		}
		defer ln.Close()
		client, e := net.DialTCP("tcp4", nil, ln.Addr().(*net.TCPAddr))
		if e != nil {
			t.Fatal(e)
		}
		server, e := ln.AcceptTCP()
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { client.Close(); server.Close() })
		return client, server
	}
	bulkSend, bulkReceive := pair()
	controlSend, controlReceive := pair()
	cfg := Run{ID: "native-tcp", Controller: "cubic", Workload: "stream", StartUnixNS: time.Now().Add(300 * time.Millisecond).UnixNano(), MeasureMS: 1100, PayloadBytes: 16384}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var received Result
	go func() { var e error; received, e = receiveTCP(ctx, bulkReceive, controlReceive, cfg); done <- e }()
	r, e := sendTCP(ctx, bulkSend, controlSend, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if r.Controller != "cubic" || received.Receiver.UsefulBytes == 0 || received.Receiver.Corrupt != 0 || r.Control.Replies == 0 {
		t.Fatalf("invalid receipt: %+v", r)
	}
	t.Run("receipt survives undrained bulk at duration boundary", func(t *testing.T) {
		bulkSend, bulkReceive := pair()
		controlSend, controlReceive := pair()
		cfg := Run{ID: "queued-tail", Controller: "cubic", Workload: "stream", StartUnixNS: time.Now().Add(50 * time.Millisecond).UnixNano(), MeasureMS: 100, PayloadBytes: 16384}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done := make(chan error, 1)
		var receipt Result
		go func() { var e error; receipt, e = receiveTCP(ctx, bulkReceive, controlReceive, cfg); done <- e }()
		if e := writeJSON(controlSend, cfg); e != nil {
			t.Fatal(e)
		}
		var ack [1]byte
		if _, e := io.ReadFull(controlSend, ack[:]); e != nil {
			t.Fatal(e)
		}
		if e := waitUntil(ctx, cfg.start()); e != nil {
			t.Fatal(e)
		}
		p := make([]byte, cfg.PayloadBytes)
		encodePayload(p, 0)
		if e := writeAll(bulkSend, p); e != nil {
			t.Fatal(e)
		}
		// No bulk FIN arrives. The receiver must retain its local measurement
		// when the bounded drain ends, without requiring a network receipt.
		controlSend.CloseWrite()
		if e := <-done; e != nil {
			t.Fatal(e)
		}
		if receipt.Receiver.UsefulBytes != uint64(cfg.PayloadBytes-headerBytes) || receipt.Receiver.Corrupt != 0 {
			t.Fatalf("invalid terminal receipt: %+v", receipt)
		}
	})
}
