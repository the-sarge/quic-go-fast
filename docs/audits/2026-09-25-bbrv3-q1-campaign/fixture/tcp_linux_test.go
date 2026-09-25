package main

import (
	"context"
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
	go func() { _, e := receiveTCP(ctx, bulkReceive, controlReceive, cfg); done <- e }()
	r, e := sendTCP(ctx, bulkSend, controlSend, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if r.Controller != "cubic" || r.Receiver.UsefulBytes == 0 || r.Receiver.Corrupt != 0 || r.Control.Replies == 0 {
		t.Fatalf("invalid receipt: %+v", r)
	}
}
