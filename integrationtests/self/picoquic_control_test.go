//go:build picoquic

package self_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/integrationtests/tools"
	"github.com/quic-go/quic-go/interop/http09"

	"github.com/stretchr/testify/require"
)

// This control deliberately uses ordinary public constructors and never enables
// picoquic's -M option. Multipath cases are added by their owning slices.
func TestPicoquicOrdinaryControl(t *testing.T) {
	require.Equal(t, "linux", runtime.GOOS, "required peer control cannot skip prerequisites")
	require.Equal(t, "amd64", runtime.GOARCH)
	require.Equal(t, quic.Version1, version)
	binary := os.Getenv("PICOQUIC_BINARY")
	require.NotEmpty(t, binary, "PICOQUIC_BINARY must name the pinned peer")
	_, err := exec.LookPath(binary)
	require.NoError(t, err)
	root := os.Getenv("PICOQUIC_LOG_DIR")
	require.NotEmpty(t, root)
	// Fixed, non-random workload. Compare the entire payload in both roles.
	payload := []byte(strings.Repeat("R1 ordinary QUIC v1 control\n", 4096))
	for _, role := range []string{"client", "server"} {
		t.Run(role, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			dir, err := os.MkdirTemp(root, role+"-") //nolint:usetesting // Retain peer evidence after the test process exits.
			require.NoError(t, err)
			ca, caKey, err := tools.GenerateCA()
			require.NoError(t, err)
			// OpenSSL treats an empty issuer/subject leaf as self-issued. Give
			// this independent-peer CA a distinct name before issuing its leaf.
			ca.Subject = pkix.Name{CommonName: "R1 independent peer control CA"}
			caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, ca.PublicKey, caKey)
			require.NoError(t, err)
			ca, err = x509.ParseCertificate(caDER)
			require.NoError(t, err)
			leaf, leafKey, err := tools.GenerateLeafCert(ca, caKey)
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
			conf := &quic.Config{Versions: []quic.Version{quic.Version1}, MaxIdleTimeout: 5 * time.Second}
			roots := x509.NewCertPool()
			roots.AddCert(ca)
			if role == "client" {
				testOrdinaryPicoquicServer(t, ctx, binary, dir, payload, conf, &tls.Config{ServerName: "localhost", RootCAs: roots, NextProtos: []string{http09.NextProto}})
			} else {
				testOrdinaryPicoquicClient(t, ctx, binary, dir, payload, conf, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leaf.Raw}, PrivateKey: leafKey}}, NextProtos: []string{http09.NextProto}})
			}
		})
	}
}

func testOrdinaryPicoquicServer(t *testing.T, ctx context.Context, binary, dir string, payload []byte, conf *quic.Config, tlsConf *tls.Config) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "r1-control.bin"), payload, 0o600))
	// Reserve an available port, then hand it to the independent process. A bind
	// race fails this single attempt rather than silently trying another case.
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	addr := socket.LocalAddr().String()
	port := socket.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, socket.Close())
	peer := startOrdinaryPeer(t, ctx, binary, dir, "-p", strconv.Itoa(port), "-c", filepath.Join(dir, "cert.pem"), "-k", filepath.Join(dir, "key.pem"), "-w", dir, "-a", http09.NextProto, "-1")
	select {
	case <-peer.ready:
	case <-peer.done:
		t.Fatal("peer exited before listening; see peer.log")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	conn, err := quic.DialAddr(ctx, addr, tlsConf, conf)
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")
	require.Equal(t, http09.NextProto, conn.ConnectionState().TLS.NegotiatedProtocol)
	stream, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)
	deadline, _ := ctx.Deadline()
	require.NoError(t, stream.SetDeadline(deadline))
	_, err = io.WriteString(stream, "GET /r1-control.bin\r\n")
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	got, err := io.ReadAll(stream)
	require.NoError(t, err)
	require.Equal(t, payload, got)
	require.NoError(t, conn.CloseWithError(0, "control complete"))
	peer.wait(t)
}

func testOrdinaryPicoquicClient(t *testing.T, ctx context.Context, binary, dir string, payload []byte, conf *quic.Config, tlsConf *tls.Config) {
	t.Helper()
	listener, err := quic.ListenAddr("127.0.0.1:0", tlsConf, conf)
	require.NoError(t, err)
	defer listener.Close()
	peer := startOrdinaryPeer(t, ctx, binary, dir, "-n", "localhost", "-a", http09.NextProto, "-t", filepath.Join(dir, "ca.pem"), "-o", dir, "-v", "00000001", "127.0.0.1", strconv.Itoa(listener.Addr().(*net.UDPAddr).Port), "/r1-control.bin")
	conn, err := listener.Accept(ctx)
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")
	require.Equal(t, http09.NextProto, conn.ConnectionState().TLS.NegotiatedProtocol)
	stream, err := conn.AcceptStream(ctx)
	require.NoError(t, err)
	deadline, _ := ctx.Deadline()
	require.NoError(t, stream.SetDeadline(deadline))
	request, err := io.ReadAll(stream)
	require.NoError(t, err)
	require.Equal(t, "GET /r1-control.bin", strings.TrimSpace(string(request)))
	_, err = stream.Write(payload)
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	peer.wait(t)
	got, err := os.ReadFile(filepath.Join(dir, "r1-control.bin"))
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

type ordinaryPeer struct {
	ready chan struct{}
	done  chan struct{}
	err   error // published by closing done
}

func startOrdinaryPeer(t *testing.T, ctx context.Context, binary, dir string, args ...string) *ordinaryPeer {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "stdbuf", append([]string{"-oL", "-eL", binary}, args...)...)
	cmd.Dir = dir
	output, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = cmd.Stdout
	log, err := os.Create(filepath.Join(dir, "peer.log"))
	require.NoError(t, err)
	peer := &ordinaryPeer{ready: make(chan struct{}), done: make(chan struct{})}
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

func (p *ordinaryPeer) wait(t *testing.T) {
	t.Helper()
	<-p.done
	require.NoError(t, p.err, "see peer.log")
}
