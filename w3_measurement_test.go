//go:build windows && w3bench

package quic

import (
	"context"
	"encoding/json"
	"fmt"
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

// Receiver half of the W3 URO adoption harness, v2 cross-host mode
// (docs/audits/2026-09-11-w3-uro-protocol.md). A verification aid for that
// finite investigation, excluded from ordinary builds and CI by the w3bench
// build tag; it is not a maintained product deliverable.
//
// One invocation runs in the measured Windows guest, dials the Linux-host
// sender at W3BENCH_ADDR, reads one bulk transfer for one protocol cell, and
// prints a single JSON line of receiver-side metrics. Cells are selected
// with W3BENCH_CELL:
//
//	engaged      transport-owned socket, URO probed on, sender USO/GSO on
//	unexercised  transport-owned socket, URO probed on, sender GSO off
//	             (the sender honors the cell via QUIC_GO_DISABLE_GSO)
//	disabled     transport-owned socket, QUIC_GO_DISABLE_GRO=1 (the
//	             W1/W2-identical receive path), sender GSO on
//	unavailable  caller-supplied receive socket (never probed, offload off
//	             by the ownership rule), sender GSO on
//
// Usage: GOOS=windows go test -c -tags w3bench -o w3bench-client.exe .
//
//	W3BENCH_CELL=engaged W3BENCH_ADDR=host:port w3bench-client.exe -test.run TestW3MeasurementClient -test.v

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

// newW3CellConn builds the measured receive conn. Every cell runs the same
// windowsConn datapath with identical socket buffers and DF flag; the only
// difference under measurement is the capability state the cell prescribes.
func newW3CellConn(t *testing.T, ownsSocket bool) *w3CountingConn {
	t.Helper()
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	if err := udpConn.SetReadBuffer(4 << 20); err != nil {
		t.Fatal(err)
	}
	if err := udpConn.SetWriteBuffer(4 << 20); err != nil {
		t.Fatal(err)
	}
	socket := &w3SocketConn{UDPConn: udpConn}
	conn, err := newConn(socket, true, ownsSocket)
	if err != nil {
		t.Fatal(err)
	}
	return &w3CountingConn{windowsConn: conn, socket: socket}
}

func TestW3MeasurementClient(t *testing.T) {
	cell := os.Getenv("W3BENCH_CELL")
	switch cell {
	case "engaged", "unavailable", "unexercised":
		// unexercised is a sender-side knob (GSO off); the receiver keeps
		// URO on so the capability is available but has no bursts to merge.
	case "disabled":
		// The switch is read by the probe at conn construction; setting it
		// here keeps the orchestration to a single cell knob.
		t.Setenv("QUIC_GO_DISABLE_GRO", "1")
	default:
		t.Fatalf("unknown W3BENCH_CELL %q (want engaged|unexercised|disabled|unavailable)", cell)
	}
	addr := os.Getenv("W3BENCH_ADDR")
	if addr == "" {
		t.Skip("W3BENCH_ADDR not set")
	}
	totalBytes := 512 << 20
	if v := os.Getenv("W3BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad W3BENCH_BYTES: %v", err)
		}
		totalBytes = n
	}
	// whole records only, matching the sender
	totalBytes = (totalBytes + w3benchRecordSize - 1) / w3benchRecordSize * w3benchRecordSize

	clientConn := newW3CellConn(t, cell != "unavailable")
	// The cell assertion: a silently misconfigured cell must not measure.
	// The sender asserts and reports its own GSO state.
	wantGRO := cell == "engaged" || cell == "unexercised"
	if got := clientConn.capabilities().GRO; got != wantGRO {
		t.Fatalf("cell %q: client GRO capability = %v, want %v", cell, got, wantGRO)
	}

	serverAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	quicConf := &Config{
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
	}
	// Pin the sender's ephemeral certificate by the fingerprint it printed
	// at startup, preserving the v1 single-process harness's RootCAs pinning
	// across two processes.
	pin := os.Getenv("W3BENCH_PEER_CERTPIN")
	if pin == "" {
		t.Skip("W3BENCH_PEER_CERTPIN not set")
	}
	clientTLS := w3benchClientTLS(pin)

	clientTr := &Transport{Conn: clientConn, createdConn: true, isSingleUse: true}
	defer clientTr.Close()

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
	conn.CloseWithError(0, "done")

	// Reading has stopped; account the final in-progress coalesced run.
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
		"role":       "client",
		"cell":       cell,
		"round":      os.Getenv("W3BENCH_ROUND"),
		"gro_cap":    clientConn.capabilities().GRO,
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
