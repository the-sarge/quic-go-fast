//go:build windows && w2bench

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

	"github.com/quic-go/quic-go/internal/protocol"
)

// Measurement harness for the W2 USO adoption protocol
// (docs/audits/2026-09-11-w2-uso-protocol.md). A verification aid for that
// finite investigation, excluded from ordinary builds and CI by the w2bench
// build tag; it is not a maintained product deliverable.
//
// One invocation runs one transfer for one protocol cell and prints a single
// JSON line of metrics. Cells are selected with W2BENCH_CELL:
//
//	engaged      transport-owned sockets, USO probed on, bulk workload
//	unexercised  transport-owned sockets, USO probed on, paced workload
//	             (application-limited, so submissions do not batch)
//	disabled     transport-owned sockets, QUIC_GO_DISABLE_GSO=1 (the
//	             W1-identical send path), bulk workload
//	unavailable  caller-supplied sockets (never probed, offload off by the
//	             ownership rule), bulk workload
//
// Usage: go test -c -tags w2bench -o w2bench.test .
//	W2BENCH_CELL=engaged ./w2bench.test -test.run TestW2MeasurementCell -test.v

const w2benchRecordSize = 1071

func w2benchTLSConfigs(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"w2bench"},
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
	server = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"w2bench"}}
	client = &tls.Config{RootCAs: roots, NextProtos: []string{"w2bench"}, ServerName: "w2bench"}
	return server, client
}

// w2CountingConn counts send submissions and the packets they carry at the
// WritePacket seam. Every WritePacket issues exactly one WSASendMsg through
// the runtime poller, so submissions are send syscalls by construction; a
// segmented submission carries ceil(n/gsoSize) packets.
type w2CountingConn struct {
	*windowsConn
	submissions    atomic.Int64
	gsoSubmissions atomic.Int64
	packets        atomic.Int64
	packetsInMulti atomic.Int64
	// batchHist[i] counts submissions that carried i packets (last bucket
	// aggregates larger batches).
	batchHist [64]atomic.Int64
}

func (c *w2CountingConn) WritePacket(b []byte, addr net.Addr, oob []byte, gsoSize uint16, ecn protocol.ECN) (int, error) {
	n, err := c.windowsConn.WritePacket(b, addr, oob, gsoSize, ecn)
	if err != nil {
		return n, err
	}
	pkts := 1
	if gsoSize > 0 {
		c.gsoSubmissions.Add(1)
		pkts = (n + int(gsoSize) - 1) / int(gsoSize)
	}
	c.submissions.Add(1)
	c.packets.Add(int64(pkts))
	if pkts > 1 {
		c.packetsInMulti.Add(int64(pkts))
	}
	c.batchHist[min(pkts, len(c.batchHist)-1)].Add(1)
	return n, err
}

// newW2CellConn builds one endpoint's rawConn for the given cell. Every cell
// runs the same windowsConn datapath and binds loopback with identical
// socket buffers and DF flag; the only difference under measurement is the
// USO capability state the cell prescribes.
func newW2CellConn(t *testing.T, cell string) (*w2CountingConn, *net.UDPAddr) {
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
	ownsSocket := cell != "unavailable"
	conn, err := newConn(udpConn, true, ownsSocket)
	if err != nil {
		t.Fatal(err)
	}
	// The cell assertion: a silently misconfigured cell must not measure.
	wantGSO := cell == "engaged" || cell == "unexercised"
	if got := conn.capabilities().GSO; got != wantGSO {
		t.Fatalf("cell %q: GSO capability = %v, want %v", cell, got, wantGSO)
	}
	return &w2CountingConn{windowsConn: conn}, dialAddr
}

func TestW2MeasurementCell(t *testing.T) {
	cell := os.Getenv("W2BENCH_CELL")
	switch cell {
	case "engaged", "unexercised", "unavailable":
	case "disabled":
		// The kill switch is read by the probe at conn construction; setting
		// it here keeps the workflow to a single cell knob.
		t.Setenv("QUIC_GO_DISABLE_GSO", "1")
	default:
		t.Fatalf("unknown W2BENCH_CELL %q (want engaged|unexercised|disabled|unavailable)", cell)
	}

	totalBytes := 512 << 20
	if v := os.Getenv("W2BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad W2BENCH_BYTES: %v", err)
		}
		totalBytes = n
	}
	pacedRecords := 2000
	if v := os.Getenv("W2BENCH_RECORDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad W2BENCH_RECORDS: %v", err)
		}
		pacedRecords = n
	}
	paced := cell == "unexercised"
	if paced {
		totalBytes = pacedRecords * w2benchRecordSize
	} else {
		// whole records only
		totalBytes = (totalBytes + w2benchRecordSize - 1) / w2benchRecordSize * w2benchRecordSize
	}

	serverTLS, clientTLS := w2benchTLSConfigs(t)
	quicConf := &Config{
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
	}

	serverConn, serverAddr := newW2CellConn(t, cell)
	clientConn, _ := newW2CellConn(t, cell)

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
			record := make([]byte, w2benchRecordSize)
			for i := range record {
				record[i] = byte(i)
			}
			if paced {
				// An application-limited sender: one record per tick, so
				// the packetizer never has a batch to segment and USO stays
				// available but unexercised.
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for sent := 0; sent < totalBytes; sent += len(record) {
					<-ticker.C
					if _, err := str.Write(record); err != nil {
						return err
					}
				}
				return str.Close()
			}
			// A bulk sender outpaces the packetizer: records are generated
			// in 32-record batches so several packets are queued at once,
			// the way a bulk sender drives the send path.
			batch := make([]byte, 0, 32*w2benchRecordSize)
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

	hist := map[string]int64{}
	for i := range serverConn.batchHist {
		if n := serverConn.batchHist[i].Load(); n > 0 {
			hist[strconv.Itoa(i)] = n
		}
	}

	osv := windows.RtlGetVersion()
	out := map[string]any{
		"cell":       cell,
		"round":      os.Getenv("W2BENCH_ROUND"),
		"gso_cap":    serverConn.capabilities().GSO,
		"paced":      paced,
		"bytes":      totalBytes,
		"elapsed_ns": elapsed.Nanoseconds(),
		// decimal megabytes per second (bytes / 1e6 / s)
		"throughput_mb_per_s":     float64(totalBytes) / elapsed.Seconds() / 1e6,
		"server_submissions":      serverConn.submissions.Load(),
		"server_gso_submissions":  serverConn.gsoSubmissions.Load(),
		"server_packets":          serverConn.packets.Load(),
		"server_packets_in_multi": serverConn.packetsInMulti.Load(),
		"server_batch_hist":       hist,
		"client_submissions":      clientConn.submissions.Load(),
		"client_packets":          clientConn.packets.Load(),
		"total_alloc_bytes":       memAfter.TotalAlloc - memBefore.TotalAlloc,
		"mallocs":                 memAfter.Mallocs - memBefore.Mallocs,
		"num_gc":                  memAfter.NumGC - memBefore.NumGC,
		"heap_inuse_bytes":        memAfter.HeapInuse,
		"peak_working_set_bytes":  w2PeakWorkingSetBytes(),
		"os_build":                fmt.Sprintf("%d.%d.%d", osv.MajorVersion, osv.MinorVersion, osv.BuildNumber),
		"num_cpu":                 runtime.NumCPU(),
		"gomaxprocs":              runtime.GOMAXPROCS(0),
		"go_version":              runtime.Version(),
		"arch":                    runtime.GOARCH,
	}
	line, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("W2BENCH %s\n", line)
}

// w2ProcessMemoryCounters is psapi's PROCESS_MEMORY_COUNTERS.
type w2ProcessMemoryCounters struct {
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

var w2ProcGetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func w2PeakWorkingSetBytes() int64 {
	var pmc w2ProcessMemoryCounters
	pmc.cb = uint32(unsafe.Sizeof(pmc))
	r1, _, _ := w2ProcGetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&pmc)),
		unsafe.Sizeof(pmc),
	)
	if r1 == 0 {
		return -1
	}
	return int64(pmc.peakWorkingSetSize)
}
