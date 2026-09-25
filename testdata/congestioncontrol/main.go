// This source is copied unchanged into isolated upstream and fork main modules.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/big"
	"os"
	"time"

	quic "github.com/quic-go/quic-go"
)

type (
	selection interface{ SetCongestionControlV1(string) error }
	identity  interface{ CongestionControlV1() string }
)

func selectSender(config *quic.Config, name string) error {
	setter, ok := any(config).(selection)
	if !ok {
		return fmt.Errorf("congestion control selection unsupported")
	}
	return setter.SetCongestionControlV1(name)
}

func run(mode string) error {
	conf := &quic.Config{}
	if mode == "upstream" {
		if err := selectSender(conf, "bbrv3"); err == nil {
			return fmt.Errorf("upstream silently accepted BBR")
		}
	} else if err := selectSender(conf, mode); err != nil {
		return err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, public, private)
	if err != nil {
		return err
	}
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}, NextProtos: []string{"congestion-control-fixture"}}
	listener, err := quic.ListenAddr("127.0.0.1:0", serverTLS, conf)
	if err != nil {
		return err
	}
	defer listener.Close()
	roots := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	roots.AddCert(parsed)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := quic.DialAddr(ctx, listener.Addr().String(), &tls.Config{RootCAs: roots, ServerName: "localhost", NextProtos: serverTLS.NextProtos}, conf)
	if err != nil {
		return err
	}
	defer client.CloseWithError(0, "")
	server, err := listener.Accept(ctx)
	if err != nil {
		return err
	}
	defer server.CloseWithError(0, "")
	for _, conn := range []*quic.Conn{client, server} {
		getter, present := any(conn).(identity)
		if mode == "upstream" {
			if present {
				return fmt.Errorf("unexpected upstream getter")
			}
		} else if !present || getter.CongestionControlV1() != mode {
			return fmt.Errorf("selected identity mismatch")
		}
	}
	fmt.Printf("%s: ordinary connection established; capability and identity assertions passed\n", mode)
	return nil
}

func main() {
	if len(os.Args) != 2 {
		panic("expected upstream, reno or bbrv3")
	}
	if err := run(os.Args[1]); err != nil {
		panic(err)
	}
}
