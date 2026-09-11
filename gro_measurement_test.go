//go:build linux && grobench

package quic

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
)

// Measurement harness for the G2 adoption protocol
// (docs/audits/2026-09-11-g2-gro-protocol.md). A verification aid for that
// finite investigation, excluded from ordinary builds and CI by the grobench
// build tag; it is not a maintained product deliverable.
//
// One invocation runs one bulk transfer for one protocol cell and prints a
// single JSON line of metrics. Cells are selected with GROBENCH_CELL:
//
//	engaged      GRO on (transport-owned socket), peer GSO on
//	unexercised  GRO on, peer GSO disabled (QUIC_GO_DISABLE_GSO)
//	disabled     GRO disabled via QUIC_GO_DISABLE_GRO, peer GSO on
//	unavailable  caller-supplied receive socket (GRO never probed), peer GSO on
//
// Usage: go test -c -tags grobench -o grobench.test .
//	GROBENCH_CELL=engaged ./grobench.test -test.run TestGROMeasurementCell -test.v

// countingBatchConn counts receive syscalls (one ReadBatch call is one
// recvmmsg) and the coalesced segments each read delivers, using the same
// UDP_GRO control-message parse as the receive path.
type countingBatchConn struct {
	bc       batchConn
	reads    atomic.Int64
	messages atomic.Int64
	// segmentsPerMessage is a histogram of GRO coalescing: index = coalesced
	// segments in one kernel message (capped at the last bucket). recvmmsg
	// batching (several messages per call) is deliberately not counted here;
	// it shows up in reads vs messages instead.
	segmentsPerMessage [64]atomic.Int64
}

func (c *countingBatchConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	n, err := c.bc.ReadBatch(ms, flags)
	if err != nil {
		return n, err
	}
	c.reads.Add(1)
	c.messages.Add(int64(n))
	for i := range n {
		segments := segmentsInMessage(&ms[i])
		if segments >= len(c.segmentsPerMessage) {
			segments = len(c.segmentsPerMessage) - 1
		}
		c.segmentsPerMessage[segments].Add(1)
	}
	return n, err
}

func segmentsInMessage(msg *ipv4.Message) int {
	if msg.N == 0 {
		return 1
	}
	groSize := 0
	data := msg.OOB[:msg.NN]
	for len(data) > 0 {
		hdr, body, remainder, err := unix.ParseOneSocketControlMessage(data)
		if err != nil {
			break
		}
		if size, ok := parseUDPGROSegmentSize(&hdr, body); ok {
			groSize = size
		}
		data = remainder
	}
	if groSize <= 0 {
		return 1
	}
	return (msg.N + groSize - 1) / groSize
}

// countingConn counts delivered datagrams: every successful ReadPacket
// return is exactly one UDP datagram.
type countingConn struct {
	*oobConn
	datagrams atomic.Int64
}

func (c *countingConn) ReadPacket() (receivedPacket, error) {
	p, err := c.oobConn.ReadPacket()
	if err == nil {
		c.datagrams.Add(1)
	}
	return p, err
}

func grobenchTLSConfigs(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"grobench"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	server = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"grobench"}}
	client = &tls.Config{RootCAs: roots, NextProtos: []string{"grobench"}, ServerName: "grobench"}
	return server, client
}

const grobenchRecordSize = 1071

func TestGROMeasurementCell(t *testing.T) {
	cell := os.Getenv("GROBENCH_CELL")
	totalBytes := 64 << 20
	if v := os.Getenv("GROBENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad GROBENCH_BYTES: %v", err)
		}
		totalBytes = n
	}
	// whole records only
	totalBytes = (totalBytes + grobenchRecordSize - 1) / grobenchRecordSize * grobenchRecordSize
	ownsReceiveSocket := true
	switch cell {
	case "engaged":
	case "unexercised":
		t.Setenv("QUIC_GO_DISABLE_GSO", "1")
	case "disabled":
		t.Setenv("QUIC_GO_DISABLE_GRO", "1")
	case "unavailable":
		ownsReceiveSocket = false
	default:
		t.Fatalf("unknown GROBENCH_CELL %q (want engaged|unexercised|disabled|unavailable)", cell)
	}

	serverTLS, clientTLS := grobenchTLSConfigs(t)
	quicConf := &Config{
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
	}

	// server: transport-owned socket, GSO per cell
	serverUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	serverTr := &Transport{Conn: serverUDP, createdConn: true, isSingleUse: true}
	defer serverTr.Close()
	ln, err := serverTr.Listen(serverTLS, quicConf)
	if err != nil {
		t.Fatal(err)
	}

	// client (receiver): the counting wrappers sit at the exact seams — one
	// ReadBatch call is one recvmmsg, one ReadPacket return is one datagram —
	// and are installed before any traffic flows.
	clientUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	rc, err := newConn(clientUDP, true, ownsReceiveSocket)
	if err != nil {
		t.Fatal(err)
	}
	batchCounter := &countingBatchConn{bc: rc.batchConn}
	rc.batchConn = batchCounter
	counting := &countingConn{oobConn: rc}
	clientTr := &Transport{Conn: counting, createdConn: ownsReceiveSocket, isSingleUse: true}
	defer clientTr.Close()

	if got := rc.capabilities().GRO; got != (cell == "engaged" || cell == "unexercised") {
		t.Fatalf("cell %s: receive-socket GRO capability = %v", cell, got)
	}

	// transfer: server streams fixed-size records to the client
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- func() error {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return err
			}
			str, err := conn.OpenUniStream()
			if err != nil {
				return err
			}
			// A bulk sender outpaces the packetizer: records are generated
			// in 32-record batches so several packets are queued at once and
			// the peer's GSO path actually forms segment bursts.
			batch := make([]byte, 0, 32*grobenchRecordSize)
			record := make([]byte, grobenchRecordSize)
			for i := range record {
				record[i] = byte(i)
			}
			for sent := 0; sent < totalBytes; {
				batch = batch[:0]
				for len(batch) < cap(batch) && sent < totalBytes {
					batch = append(batch, record...)
					sent += len(record)
				}
				if _, err := str.Write(batch); err != nil {
					return err
				}
			}
			return str.Close()
		}()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, err := clientTr.Dial(ctx, serverUDP.LocalAddr(), clientTLS, quicConf)
	if err != nil {
		t.Fatal(err)
	}
	str, err := conn.AcceptUniStream(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	start := time.Now()
	received := 0
	buf := make([]byte, 64<<10)
	for {
		n, err := str.Read(buf)
		received += n
		if err != nil {
			break
		}
	}
	elapsed := time.Since(start)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	if received != totalBytes {
		t.Fatalf("received %d of %d bytes", received, totalBytes)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	conn.CloseWithError(0, "done")

	hist := map[string]int64{}
	var coalescedMessages, coalescedDatagrams int64
	for segments := range len(batchCounter.segmentsPerMessage) {
		if c := batchCounter.segmentsPerMessage[segments].Load(); c > 0 {
			hist[strconv.Itoa(segments)] = c
			if segments > 1 {
				coalescedMessages += c
				coalescedDatagrams += int64(segments) * c
			}
		}
	}
	reads, datagrams := batchCounter.reads.Load(), counting.datagrams.Load()
	out := map[string]any{
		"cell":                cell,
		"bytes":               received,
		"elapsed_ns":          elapsed.Nanoseconds(),
		"throughput_mbps":     float64(received) / elapsed.Seconds() / 1e6,
		"receive_syscalls":    reads,
		"datagrams_delivered": datagrams,
		"syscalls_per_datagram": func() float64 {
			if datagrams == 0 {
				return 0
			}
			return float64(reads) / float64(datagrams)
		}(),
		"messages":                  batchCounter.messages.Load(),
		"segments_per_message_hist": hist,
		"coalesced_messages":        coalescedMessages,
		"coalesced_datagrams":       coalescedDatagrams,
		"total_alloc_bytes":         memAfter.TotalAlloc - memBefore.TotalAlloc,
		"mallocs":                   memAfter.Mallocs - memBefore.Mallocs,
		"num_gc":                    memAfter.NumGC - memBefore.NumGC,
		"heap_inuse_bytes":          memAfter.HeapInuse,
		"vm_hwm_kib":                readVmHWMKiB(t),
		"gso_cap":                   rc.capabilities().GSO,
		"gro_cap":                   rc.capabilities().GRO,
		"max_conn_retained":         protocol.MaxConnRetainedCoalescedBytes,
	}
	line, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("GROBENCH %s\n", line)
}

func readVmHWMKiB(t *testing.T) int64 {
	t.Helper()
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return -1
	}
	for line := range strings.Lines(string(data)) {
		if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				if v, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
					return v
				}
			}
		}
	}
	return -1
}
