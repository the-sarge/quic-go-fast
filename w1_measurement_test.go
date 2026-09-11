//go:build windows && w1bench

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
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Measurement harness for the W1 noninferiority protocol
// (docs/audits/2026-09-11-w1-foundation-protocol.md). A verification aid for
// that finite investigation, excluded from ordinary builds and CI by the
// w1bench build tag; it is not a maintained product deliverable.
//
// One invocation runs one bulk transfer for one protocol cell and prints a
// single JSON line of metrics. Cells are selected with W1BENCH_CELL:
//
//	candidate  both endpoints on the W1 message-I/O conn (windowsConn)
//	baseline   both endpoints on the preserved plain-socket path (basicConn),
//	           the datapath the base commit runs on Windows
//
// Usage: go test -c -tags w1bench -o w1bench.test .
//	W1BENCH_CELL=candidate ./w1bench.test -test.run TestW1MeasurementCell -test.v

const w1benchRecordSize = 1071

func w1benchTLSConfigs(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"w1bench"},
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
	server = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"w1bench"}}
	client = &tls.Config{RootCAs: roots, NextProtos: []string{"w1bench"}, ServerName: "w1bench"}
	return server, client
}

// newW1CellConn builds one endpoint's rawConn for the given cell on a fresh
// loopback socket; the Transport uses it directly because both cell types
// satisfy rawConn. Both cells receive the same explicit socket buffers and
// the same DF flag, so the only difference under measurement is the datapath:
// windowsConn (ReadMsgUDP/WriteMsgUDP) against basicConn (ReadFrom/WriteTo),
// the exact construction the base commit's Windows newConn performed.
func newW1CellConn(t *testing.T, cell string) (net.PacketConn, *net.UDPAddr) {
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
	switch cell {
	case "candidate":
		conn, err := newConn(udpConn, true, true)
		if err != nil {
			t.Fatal(err)
		}
		return conn, udpConn.LocalAddr().(*net.UDPAddr)
	case "baseline":
		return &basicConn{PacketConn: udpConn, supportsDF: true}, udpConn.LocalAddr().(*net.UDPAddr)
	default:
		t.Fatalf("unknown W1BENCH_CELL %q (want candidate|baseline)", cell)
		return nil, nil
	}
}

func TestW1MeasurementCell(t *testing.T) {
	cell := os.Getenv("W1BENCH_CELL")
	totalBytes := 512 << 20
	if v := os.Getenv("W1BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad W1BENCH_BYTES: %v", err)
		}
		totalBytes = n
	}
	// whole records only
	totalBytes = (totalBytes + w1benchRecordSize - 1) / w1benchRecordSize * w1benchRecordSize

	serverTLS, clientTLS := w1benchTLSConfigs(t)
	quicConf := &Config{
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
	}

	serverConn, serverAddr := newW1CellConn(t, cell)
	clientConn, _ := newW1CellConn(t, cell)
	// The cell assertion: a silently misconfigured cell must not measure.
	switch cell {
	case "candidate":
		if _, ok := serverConn.(*windowsConn); !ok {
			t.Fatalf("candidate cell got %T", serverConn)
		}
	case "baseline":
		if _, ok := serverConn.(*basicConn); !ok {
			t.Fatalf("baseline cell got %T", serverConn)
		}
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
			// A bulk sender outpaces the packetizer: records are generated
			// in 32-record batches so several packets are queued at once,
			// the way a bulk sender drives the send path.
			batch := make([]byte, 0, 32*w1benchRecordSize)
			record := make([]byte, w1benchRecordSize)
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

	osv := windows.RtlGetVersion()
	out := map[string]any{
		"cell":                   cell,
		"round":                  os.Getenv("W1BENCH_ROUND"),
		"bytes":                  received,
		"elapsed_ns":             elapsed.Nanoseconds(),
		"throughput_mbps":        float64(received) / elapsed.Seconds() / 1e6,
		"total_alloc_bytes":      memAfter.TotalAlloc - memBefore.TotalAlloc,
		"mallocs":                memAfter.Mallocs - memBefore.Mallocs,
		"num_gc":                 memAfter.NumGC - memBefore.NumGC,
		"heap_inuse_bytes":       memAfter.HeapInuse,
		"peak_working_set_bytes": peakWorkingSetBytes(),
		"os_build":               fmt.Sprintf("%d.%d.%d", osv.MajorVersion, osv.MinorVersion, osv.BuildNumber),
		"num_cpu":                runtime.NumCPU(),
		"gomaxprocs":             runtime.GOMAXPROCS(0),
		"go_version":             runtime.Version(),
		"arch":                   runtime.GOARCH,
	}
	line, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("W1BENCH %s\n", line)
}

// processMemoryCounters is psapi's PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
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

var procK32GetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func peakWorkingSetBytes() int64 {
	var pmc processMemoryCounters
	pmc.cb = uint32(unsafe.Sizeof(pmc))
	r1, _, _ := procK32GetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&pmc)),
		unsafe.Sizeof(pmc),
	)
	if r1 == 0 {
		return -1
	}
	return int64(pmc.peakWorkingSetSize)
}
