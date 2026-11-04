//go:build picoquic && linux

package quic

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/multipathguard"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
)

const smokeALPN = "hq-interop"

// smokeTrace counts useful traffic per path from the qlog events this slice adds.
type smokeTrace struct {
	mu     sync.Mutex
	stream map[uint64]int64
	closes map[uint64]int
	live   chan struct{}
	once   sync.Once
}

func newSmokeTrace() *smokeTrace {
	return &smokeTrace{stream: make(map[uint64]int64), closes: make(map[uint64]int), live: make(chan struct{})}
}

func (s *smokeTrace) AddProducer() qlogwriter.Recorder { return s }
func (s *smokeTrace) SupportsSchemas(string) bool      { return true }
func (s *smokeTrace) Close() error                     { return nil }
func (s *smokeTrace) tracer(context.Context, bool, ConnectionID) qlogwriter.Trace {
	return s
}

func (s *smokeTrace) RecordEvent(event qlogwriter.Event) {
	switch e := event.(type) {
	case qlog.PathLifecycleUpdated:
		if e.Path.PathID == 1 && e.State == "live" {
			s.once.Do(func() { close(s.live) })
		}
	case qlog.PacketSent:
		if e.Path == nil {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, frame := range e.Frames {
			switch f := frame.Frame.(type) {
			case *qlog.StreamFrame:
				s.stream[e.Path.PathID] += f.Length
			case *qlog.ConnectionCloseFrame:
				s.closes[e.Path.PathID]++
			}
		}
	}
}

func (s *smokeTrace) report(t *testing.T) (map[uint64]int64, map[uint64]int) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Logf("sent STREAM bytes by path: %v; CONNECTION_CLOSE by path: %v", s.stream, s.closes)
	return s.stream, s.closes
}

// TestPicoquicMultipathSmoke is the first independent two-path run: useful STREAM
// data on both paths, a stream abort and a close, in both roles. It is example-level
// evidence and doesn't replace R17's matrix.
func TestPicoquicMultipathSmoke(t *testing.T) {
	require.Equal(t, protocol.Version1, protocol.SupportedVersions[0])
	binary := os.Getenv("PICOQUIC_BINARY")
	require.NotEmpty(t, binary, "PICOQUIC_BINARY must name the pinned peer")
	root := os.Getenv("PICOQUIC_LOG_DIR")
	require.NotEmpty(t, root)
	multipathguard.Active.Store(true)
	t.Cleanup(func() { multipathguard.Active.Store(false) })
	for _, role := range []string{"client", "server"} {
		t.Run(role, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			dir, err := os.MkdirTemp(root, "multipath-"+role+"-") //nolint:usetesting // Retain peer evidence after the test process exits.
			require.NoError(t, err)
			roots, leaf := writeSmokeCertificates(t, dir)
			trace := newSmokeTrace()
			conf := &Config{Versions: []Version{Version1}, MaxIdleTimeout: 10 * time.Second, Tracer: trace.tracer}
			require.NoError(t, conf.ConfigureMultipathV1(true, 2, "primary-backup"))
			// Fixed, non-random workload, large enough to outlast the path switch.
			payload := []byte(strings.Repeat("R14 two-path STREAM smoke\n", 1<<15))
			if role == "client" {
				smokeClient(t, ctx, binary, dir, payload, conf, &tls.Config{ServerName: "localhost", RootCAs: roots, NextProtos: []string{smokeALPN}}, trace)
			} else {
				smokeServer(t, ctx, binary, dir, payload, conf, &tls.Config{Certificates: []tls.Certificate{leaf}, NextProtos: []string{smokeALPN}}, trace)
			}
			stream, _ := trace.report(t)
			require.NotZero(t, stream[0], "useful data on path zero")
			require.NotZero(t, stream[1], "useful data on path one")
		})
	}
}

func smokeSetUse(conn *Conn, id protocol.PathID, backup bool) error {
	return conn.runPathCommand(func(r *pathCreditRegistry, now monotime.Time) error { return r.setLocalUsePrivate(id, backup, now) })
}

func writeSmokeCertificates(t *testing.T, dir string) (*x509.CertPool, tls.Certificate) {
	t.Helper()
	// integrationtests/tools imports this package, so this file builds its own chain.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "R14 independent peer smoke CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
	require.NoError(t, err)
	ca, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, leafKey.Public(), caKey)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(leafDER)
	require.NoError(t, err)
	key, err := x509.MarshalPKCS8PrivateKey(leafKey)
	require.NoError(t, err)
	for name, data := range map[string][]byte{
		"cert.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw}),
		"ca.pem":   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}),
		"key.pem":  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return roots, tls.Certificate{Certificate: [][]byte{leaf.Raw}, PrivateKey: leafKey}
}

func smokeTransport(t *testing.T, ip net.IP) *Transport {
	t.Helper()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
	require.NoError(t, err)
	tr := &Transport{Conn: socket, ConnectionIDLength: 8}
	require.NoError(t, tr.configureNativeIO(100*time.Millisecond))
	t.Cleanup(func() {
		tr.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, tr.waitNativeIOStopped(ctx))
		socket.Close()
	})
	return tr
}

func smokeRequest(t *testing.T, ctx context.Context, conn *Conn, path string) *Stream {
	t.Helper()
	stream, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)
	_, err = io.WriteString(stream, "GET "+path+"\r\n")
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	return stream
}

// smokeClient drives picoquic's server. Our client's requests are the useful data it
// sends, one on each path, after a transfer proves the connection works.
func smokeClient(t *testing.T, ctx context.Context, binary, dir string, payload []byte, conf *Config, tlsConf *tls.Config, trace *smokeTrace) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "r14-smoke.bin"), payload, 0o600))
	reserved, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	addr := reserved.LocalAddr().(*net.UDPAddr)
	require.NoError(t, reserved.Close())
	peer := startSmokePeer(t, ctx, binary, dir, "-p", strconv.Itoa(addr.Port), "-c", filepath.Join(dir, "cert.pem"), "-k", filepath.Join(dir, "key.pem"), "-w", dir, "-a", smokeALPN, "-M", "-1")
	select {
	case <-peer.ready:
	case <-peer.done:
		t.Fatal("peer exited before listening; see peer.log")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	primary, alternate := smokeTransport(t, net.IPv4(127, 0, 0, 1)), smokeTransport(t, net.IPv4(127, 0, 0, 2))
	require.NoError(t, alternate.init(false), "prepareTarget does this inside OpenPathV1")
	conn, err := primary.Dial(ctx, addr, tlsConf, conf)
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")
	deadline, _ := ctx.Deadline()

	// picoquic needs a completed exchange before a path challenge.
	first := smokeRequest(t, ctx, conn, "/r14-smoke.bin")
	require.NoError(t, first.SetDeadline(deadline))
	got, err := io.ReadAll(first)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	_, err = conn.openPathPrivate(ctx, alternate, addr)
	require.NoError(t, err)
	require.NoError(t, smokeSetUse(conn, 1, false))
	require.NoError(t, smokeSetUse(conn, 0, true))
	second := smokeRequest(t, ctx, conn, "/r14-smoke.bin")
	require.NoError(t, second.SetDeadline(deadline))
	got, err = io.ReadAll(second)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	// Abort a transfer in progress, then show the connection still carries the next one.
	abort := smokeRequest(t, ctx, conn, "/r14-smoke.bin")
	require.NoError(t, abort.SetDeadline(deadline))
	_, err = io.ReadFull(abort, make([]byte, 1))
	require.NoError(t, err)
	abort.CancelRead(0)
	after := smokeRequest(t, ctx, conn, "/r14-smoke.bin")
	require.NoError(t, after.SetDeadline(deadline))
	got, err = io.ReadAll(after)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	require.NoError(t, conn.CloseWithError(0, "smoke complete"))
	_, closes := trace.report(t)
	require.NotZero(t, closes[0]+closes[1], "the close left through a path")
	peer.wait(t)
}

// smokeServer drives picoquic's client, which opens path 1 itself. We reset its first
// request, then send half of the next response on each path.
func smokeServer(t *testing.T, ctx context.Context, binary, dir string, payload []byte, conf *Config, tlsConf *tls.Config, trace *smokeTrace) {
	t.Helper()
	tr := smokeTransport(t, net.IPv4(127, 0, 0, 1))
	listener, err := tr.Listen(tlsConf, conf)
	require.NoError(t, err)
	defer listener.Close()
	port := tr.Conn.LocalAddr().(*net.UDPAddr).Port
	peer := startSmokePeer(t, ctx, binary, dir, "-n", "localhost", "-a", smokeALPN, "-t", filepath.Join(dir, "ca.pem"), "-o", dir, "-M", "-A", "127.0.0.2/0", "-v", "00000001", "127.0.0.1", strconv.Itoa(port), "/r14-abort.bin;/r14-smoke.bin")
	conn, err := listener.Accept(ctx)
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")
	deadline, _ := ctx.Deadline()

	// The peer starts its next request when this reset ends the first.
	abort, err := conn.AcceptStream(ctx)
	require.NoError(t, err)
	require.NoError(t, abort.SetDeadline(deadline))
	request, err := io.ReadAll(abort)
	require.NoError(t, err)
	require.Equal(t, "GET /r14-abort.bin", strings.TrimSpace(string(request)))
	abort.CancelWrite(0)

	stream, err := conn.AcceptStream(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.SetDeadline(deadline))
	request, err = io.ReadAll(stream)
	require.NoError(t, err)
	require.Equal(t, "GET /r14-smoke.bin", strings.TrimSpace(string(request)))
	half := len(payload) / 2
	_, err = stream.Write(payload[:half])
	require.NoError(t, err)
	select {
	case <-trace.live:
	case <-ctx.Done():
		t.Fatal("the peer's second path never validated")
	}
	require.NoError(t, smokeSetUse(conn, 1, false))
	require.NoError(t, smokeSetUse(conn, 0, true))
	_, err = stream.Write(payload[half:])
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	peer.wait(t)
	log, err := os.ReadFile(filepath.Join(dir, "peer.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "reset after", "the peer saw the abort")
	got, err := os.ReadFile(filepath.Join(dir, "r14-smoke.bin"))
	require.NoError(t, err)
	require.Equal(t, payload, got)
	select {
	case <-conn.Context().Done():
	case <-ctx.Done():
		t.Fatal("the peer's close never arrived")
	}
}

type smokePeer struct {
	ready chan struct{}
	done  chan struct{}
	err   error // published by closing done
}

func startSmokePeer(t *testing.T, ctx context.Context, binary, dir string, args ...string) *smokePeer {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "stdbuf", append([]string{"-oL", "-eL", binary}, args...)...)
	cmd.Dir = dir
	output, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = cmd.Stdout
	log, err := os.Create(filepath.Join(dir, "peer.log"))
	require.NoError(t, err)
	peer := &smokePeer{ready: make(chan struct{}), done: make(chan struct{})}
	require.NoError(t, cmd.Start())
	go func() {
		scanner := bufio.NewScanner(output)
		ready := false
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(log, line)
			if !ready && strings.Contains(line, "Waiting for packets") {
				close(peer.ready)
				ready = true
			}
		}
		peer.err = cmd.Wait()
		if peer.err == nil {
			peer.err = scanner.Err()
		}
		log.Close()
		close(peer.done)
	}()
	t.Cleanup(func() {
		cancel()
		<-peer.done
	})
	return peer
}

func (p *smokePeer) wait(t *testing.T) {
	t.Helper()
	<-p.done
	require.NoError(t, p.err, "see peer.log")
}
