//go:build windows && w3bench

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
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Measurement harness for the W3 URO adoption protocol
// (docs/audits/2026-09-11-w3-uro-protocol.md). A verification aid for that
// finite investigation, excluded from ordinary builds and CI by the w3bench
// build tag; it is not a maintained product deliverable.
//
// One invocation runs one transfer for one protocol cell and prints a single
// JSON line of metrics. Cells are selected with W3BENCH_CELL:
//
//	engaged      transport-owned sockets, URO probed on, peer USO on
//	unexercised  transport-owned sockets, URO probed on, peer USO off
//	             (QUIC_GO_DISABLE_GSO=1: no segmented bursts to coalesce)
//	disabled     transport-owned sockets, QUIC_GO_DISABLE_GRO=1 (the
//	             W1/W2-identical receive path), peer USO on
//	unavailable  caller-supplied receive socket (never probed, offload off
//	             by the ownership rule), peer USO on
//
// Usage: go test -c -tags w3bench -o w3bench.test .
//
//	W3BENCH_CELL=engaged ./w3bench.test -test.run TestW3MeasurementCell -test.v
const w3benchRecordSize = 1071

func w3benchTLSConfigs(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"w3bench"},
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
	server = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"w3bench"}}
	client = &tls.Config{RootCAs: roots, NextProtos: []string{"w3bench"}, ServerName: "w3bench"}
	return server, client
}

// w3SocketConn counts socket reads at the ReadMsgUDP seam. Every call is one
// WSARecvMsg through the runtime poller, so reads are receive syscalls by
// construction.
type w3SocketConn struct {
	*net.UDPConn
	reads atomic.Int64
}

func (c *w3SocketConn) ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error) {
	n, oobn, flags, addr, err = c.UDPConn.ReadMsgUDP(b, oob)
	if err == nil {
		c.reads.Add(1)
	}
	return n, oobn, flags, addr, err
}

// w3CountingConn counts delivered datagrams at the ReadPacket seam and
// derives the per-read segment distribution from slab identity: consecutive
// views of one slab came from one coalesced read (the conn drains a read's
// pending views before issuing the next read), and a non-slab packet is
// exactly one read. ReadPacket runs on the transport's single reader
// goroutine; the run-length state is confined to it, and the counters are
// atomics so the test goroutine can read them after the transfer.
type w3CountingConn struct {
	*windowsConn
	socket             *w3SocketConn
	datagrams          atomic.Int64
	coalescedReads     atomic.Int64
	coalescedDatagrams atomic.Int64
	// segHist[i] counts reads that delivered i datagrams (last bucket
	// aggregates larger reads).
	segHist [64]atomic.Int64

	lastSlab *coalescedSlab
	runLen   int
}

func (c *w3CountingConn) flushRun() {
	if c.runLen == 0 {
		return
	}
	c.segHist[min(c.runLen, len(c.segHist)-1)].Add(1)
	if c.runLen > 1 {
		c.coalescedReads.Add(1)
		c.coalescedDatagrams.Add(int64(c.runLen))
	}
	c.runLen = 0
	c.lastSlab = nil
}

func (c *w3CountingConn) ReadPacket() (receivedPacket, error) {
	p, err := c.windowsConn.ReadPacket()
	if err != nil {
		return p, err
	}
	c.datagrams.Add(1)
	var slab *coalescedSlab
	if p.buffer != nil {
		slab = p.buffer.slab
	}
	if slab != nil && slab == c.lastSlab {
		c.runLen++
		return p, err
	}
	c.flushRun()
	if slab == nil {
		c.segHist[1].Add(1)
		return p, err
	}
	c.lastSlab = slab
	c.runLen = 1
	return p, err
}

// newW3CellConn builds one endpoint's rawConn. Every cell runs the same
// windowsConn datapath and binds loopback with identical socket buffers and
// DF flag; the only difference under measurement is the capability state the
// cell prescribes.
func newW3CellConn(t *testing.T, ownsSocket bool) (*w3CountingConn, *net.UDPAddr) {
	t.Helper()
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := udpConn.SetReadBuffer(4 << 20); err != nil {
		t.Fatal(err)
	}
	if err := udpConn.SetWriteBuffer(4 << 20); err != nil {
		t.Fatal(err)
	}
	dialAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: udpConn.LocalAddr().(*net.UDPAddr).Port}
	socket := &w3SocketConn{UDPConn: udpConn}
	conn, err := newConn(socket, true, ownsSocket)
	if err != nil {
		t.Fatal(err)
	}
	return &w3CountingConn{windowsConn: conn, socket: socket}, dialAddr
}

func TestW3MeasurementCell(t *testing.T) {
	cell := os.Getenv("W3BENCH_CELL")
	switch cell {
	case "engaged", "unavailable":
	case "unexercised":
		// The switches are read by the probes at conn construction; setting
		// them here keeps the workflow to a single cell knob.
		t.Setenv("QUIC_GO_DISABLE_GSO", "1")
	case "disabled":
		t.Setenv("QUIC_GO_DISABLE_GRO", "1")
	default:
		t.Fatalf("unknown W3BENCH_CELL %q (want engaged|unexercised|disabled|unavailable)", cell)
	}

	totalBytes := 512 << 20
	if v := os.Getenv("W3BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad W3BENCH_BYTES: %v", err)
		}
		totalBytes = n
	}
	// whole records only
	totalBytes = (totalBytes + w3benchRecordSize - 1) / w3benchRecordSize * w3benchRecordSize

	serverTLS, clientTLS := w3benchTLSConfigs(t)
	quicConf := &Config{
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
	}

	serverConn, serverAddr := newW3CellConn(t, true)
	clientConn, _ := newW3CellConn(t, cell != "unavailable")

	// The cell assertion: a silently misconfigured cell must not measure.
	wantGRO := cell == "engaged" || cell == "unexercised"
	if got := clientConn.capabilities().GRO; got != wantGRO {
		t.Fatalf("cell %q: client GRO capability = %v, want %v", cell, got, wantGRO)
	}
	wantServerGSO := cell != "unexercised"
	if got := serverConn.capabilities().GSO; got != wantServerGSO {
		t.Fatalf("cell %q: server GSO capability = %v, want %v", cell, got, wantServerGSO)
	}

	serverTr := &Transport{Conn: serverConn, createdConn: true, isSingleUse: true}
	defer serverTr.Close()
	ln, err := serverTr.Listen(serverTLS, quicConf)
	if err != nil {
		t.Fatal(err)
	}
	clientTr := &Transport{Conn: clientConn, createdConn: true, isSingleUse: true}
	defer clientTr.Close()

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
			record := make([]byte, w3benchRecordSize)
			for i := range record {
				record[i] = byte(i)
			}
			// A bulk sender outpaces the packetizer: records are generated
			// in 32-record batches so several packets are queued at once,
			// the way a bulk sender drives the send path, and the peer's
			// USO path forms real segment bursts for URO to coalesce.
			batch := make([]byte, 0, 32*w3benchRecordSize)
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
	conn, err := clientTr.Dial(ctx, serverAddr, clientTLS, quicConf)
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

	// Reading has stopped; account the final in-progress coalesced run.
	// ReadPacket is no longer called concurrently once the transfer and
	// close have completed.
	clientConn.flushRun()

	hist := map[string]int64{}
	for i := range clientConn.segHist {
		if n := clientConn.segHist[i].Load(); n > 0 {
			hist[strconv.Itoa(i)] = n
		}
	}

	reads := clientConn.socket.reads.Load()
	datagrams := clientConn.datagrams.Load()
	var readsPerDatagram float64
	if datagrams > 0 {
		readsPerDatagram = float64(reads) / float64(datagrams)
	}

	osv := windows.RtlGetVersion()
	out := map[string]any{
		"cell":       cell,
		"round":      os.Getenv("W3BENCH_ROUND"),
		"gro_cap":    clientConn.capabilities().GRO,
		"gso_cap":    serverConn.capabilities().GSO,
		"bytes":      totalBytes,
		"elapsed_ns": elapsed.Nanoseconds(),
		// decimal megabytes per second (bytes / 1e6 / s)
		"throughput_mb_per_s":        float64(totalBytes) / elapsed.Seconds() / 1e6,
		"client_socket_reads":        reads,
		"client_datagrams":           datagrams,
		"reads_per_datagram":         readsPerDatagram,
		"client_coalesced_reads":     clientConn.coalescedReads.Load(),
		"client_coalesced_datagrams": clientConn.coalescedDatagrams.Load(),
		"client_seg_hist":            hist,
		"total_alloc_bytes":          memAfter.TotalAlloc - memBefore.TotalAlloc,
		"mallocs":                    memAfter.Mallocs - memBefore.Mallocs,
		"num_gc":                     memAfter.NumGC - memBefore.NumGC,
		"heap_inuse_bytes":           memAfter.HeapInuse,
		"peak_working_set_bytes":     w3PeakWorkingSetBytes(),
		"os_build":                   fmt.Sprintf("%d.%d.%d", osv.MajorVersion, osv.MinorVersion, osv.BuildNumber),
		"num_cpu":                    runtime.NumCPU(),
		"gomaxprocs":                 runtime.GOMAXPROCS(0),
		"go_version":                 runtime.Version(),
		"arch":                       runtime.GOARCH,
	}
	line, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("W3BENCH %s\n", line)
}

// w3ProcessMemoryCounters is psapi's PROCESS_MEMORY_COUNTERS.
type w3ProcessMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

var w3ProcGetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func w3PeakWorkingSetBytes() int64 {
	var pmc w3ProcessMemoryCounters
	pmc.cb = uint32(unsafe.Sizeof(pmc))
	r1, _, _ := w3ProcGetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&pmc)),
		unsafe.Sizeof(pmc),
	)
	if r1 == 0 {
		return -1
	}
	return int64(pmc.peakWorkingSetSize)
}
