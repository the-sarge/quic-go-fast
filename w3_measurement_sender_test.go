//go:build linux && w3bench

package quic

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

// Sender half of the W3 URO adoption harness, v2 cross-host mode
// (docs/audits/2026-09-11-w3-uro-protocol.md): runs on the Linux KVM host,
// listens on W3BENCH_LISTEN, and bulk-sends the fixed workload to the
// measured Windows guest over the virtual NIC. The receiver-side metrics
// live in w3_measurement_test.go; this side only asserts and reports its
// GSO capability, which the unexercised cell turns off.
func TestW3MeasurementServer(t *testing.T) {
	listen := os.Getenv("W3BENCH_LISTEN")
	if listen == "" {
		t.Skip("W3BENCH_LISTEN not set")
	}
	if os.Getenv("W3BENCH_CELL") == "unexercised" {
		t.Setenv("QUIC_GO_DISABLE_GSO", "1")
	}
	totalBytes := 512 << 20
	if v := os.Getenv("W3BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad W3BENCH_BYTES: %v", err)
		}
		totalBytes = n
	}
	totalBytes = (totalBytes + w3benchRecordSize - 1) / w3benchRecordSize * w3benchRecordSize

	laddr, err := net.ResolveUDPAddr("udp4", listen)
	if err != nil {
		t.Fatal(err)
	}
	udpConn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		t.Fatal(err)
	}
	if err := udpConn.SetReadBuffer(4 << 20); err != nil {
		t.Fatal(err)
	}
	if err := udpConn.SetWriteBuffer(4 << 20); err != nil {
		t.Fatal(err)
	}
	conn, err := newConn(udpConn, true, true)
	if err != nil {
		t.Fatal(err)
	}
	wantGSO := os.Getenv("W3BENCH_CELL") != "unexercised"
	if got := conn.capabilities().GSO; got != wantGSO {
		t.Fatalf("sender GSO capability = %v, want %v", got, wantGSO)
	}

	serverTLS, pin := w3benchServerTLS(t)
	// The orchestration reads this line and passes the pin to the receiver
	// as W3BENCH_PEER_CERTPIN before starting it.
	fmt.Printf("W3BENCH_CERTPIN %s\n", pin)
	quicConf := &Config{
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
	}

	tr := &Transport{Conn: conn, createdConn: true, isSingleUse: true}
	defer tr.Close()
	ln, err := tr.Listen(serverTLS, quicConf)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	qconn, err := ln.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	str, err := qconn.OpenUniStream()
	if err != nil {
		t.Fatal(err)
	}
	record := make([]byte, w3benchRecordSize)
	for i := range record {
		record[i] = byte(i)
	}
	// A bulk sender outpaces the packetizer: records are generated in
	// 32-record batches so several packets are queued at once and the GSO
	// path forms real segment bursts for URO to coalesce.
	batch := make([]byte, 0, 32*w3benchRecordSize)
	sent := 0
	for sent < totalBytes {
		batch = batch[:0]
		for len(batch) < cap(batch) && sent < totalBytes {
			batch = append(batch, record...)
			sent += len(record)
		}
		if _, err := str.Write(batch); err != nil {
			t.Fatal(err)
		}
	}
	if err := str.Close(); err != nil {
		t.Fatal(err)
	}
	// Wait for the peer to finish reading before tearing down.
	select {
	case <-qconn.Context().Done():
	case <-ctx.Done():
	}

	out, _ := json.Marshal(map[string]any{
		"role":    "server",
		"cell":    os.Getenv("W3BENCH_CELL"),
		"round":   os.Getenv("W3BENCH_ROUND"),
		"gso_cap": conn.capabilities().GSO,
		"bytes":   sent,
	})
	fmt.Printf("W3BENCH %s\n", out)
}
