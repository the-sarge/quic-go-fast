package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	quic "github.com/quic-go/quic-go"
)

type Run struct {
	ID               string `json:"id"`
	Controller       string `json:"controller"`
	Workload         string `json:"workload"`
	StartUnixNS      int64  `json:"start_unix_ns"`
	WarmupMS         int64  `json:"warmup_ms"`
	MeasureMS        int64  `json:"measure_ms"`
	PayloadBytes     int    `json:"payload_bytes"`
	CompletionBytes  int64  `json:"completion_bytes,omitempty"`
	BulkPauseStartMS int64  `json:"bulk_pause_start_ms,omitempty"`
	BulkPauseEndMS   int64  `json:"bulk_pause_end_ms,omitempty"`
}

func (r Run) start() time.Time { return time.Unix(0, r.StartUnixNS) }
func (r Run) measuredStart() time.Time {
	return r.start().Add(time.Duration(r.WarmupMS) * time.Millisecond)
}
func (r Run) end() time.Time {
	return r.measuredStart().Add(time.Duration(r.MeasureMS) * time.Millisecond)
}
func (r Run) validate() error {
	if r.ID == "" || len(r.ID) > 128 || (r.Controller != "reno" && r.Controller != "bbrv3") || (r.Workload != "stream" && r.Workload != "datagram") {
		return fmt.Errorf("invalid run identity/controller/workload")
	}
	if r.WarmupMS < 0 || r.MeasureMS < 1 || r.WarmupMS+r.MeasureMS > 360000 || r.StartUnixNS <= 0 {
		return fmt.Errorf("invalid bounded time window")
	}
	if r.PayloadBytes <= headerBytes || r.PayloadBytes > 16384 || (r.Workload == "datagram" && r.PayloadBytes > 1200) {
		return fmt.Errorf("invalid payload size")
	}
	if r.CompletionBytes < 0 || r.CompletionBytes > 16<<20 || (r.CompletionBytes > 0 && (r.Workload != "stream" || r.WarmupMS != 0)) {
		return fmt.Errorf("invalid completion case")
	}
	if (r.BulkPauseStartMS != 0 || r.BulkPauseEndMS != 0) && (r.BulkPauseStartMS != 240000 || r.BulkPauseEndMS != 270000 || r.MeasureMS != 300000 || r.WarmupMS != 60000 || r.Workload != "stream" || r.CompletionBytes != 0) {
		return fmt.Errorf("only the accepted L6–L8 competitor bulk pause is supported")
	}
	return nil
}

type Control struct {
	Opportunities         [][2]int64 `json:"opportunity_times_ns"` // Scheduled and observed, for cadence checks.
	ExpectedOpportunities int        `json:"expected_opportunities"`
	MissedOpportunities   int        `json:"missed_opportunities"`
	Error                 string     `json:"error,omitempty"`
	Offered               int        `json:"offered"`
	Replies               int        `json:"replies"`
	Skipped               int        `json:"skipped"`
	Unresolved            int        `json:"unresolved"`
	LatencyNS             []int64    `json:"latency_ns"`
	// Each bound is receiver-clock minus sender-clock, with no symmetry claim.
	ClockBoundsNS [][2]int64 `json:"clock_offset_bounds_ns"`
}
type Result struct {
	BulkPauseObservedNS []int64  `json:"bulk_pause_observed_ns,omitempty"`
	Run                 Run      `json:"run"`
	Controller          string   `json:"controller"`
	Receiver            Delivery `json:"receiver"`
	Control             Control  `json:"control"`
	SentMessages        uint64   `json:"sent_messages"`
	SendCallNS          int64    `json:"send_call_ns"`
	CompletionNS        int64    `json:"completion_ns,omitempty"`
	CompletionVerified  bool     `json:"completion_verified"`
	Censored            bool     `json:"censored"`
	Errors              []string `json:"errors,omitempty"`
}

func newTransport(address string, managed bool) (*quic.Transport, func(), error) {
	addr, err := net.ResolveUDPAddr("udp4", address)
	if err != nil {
		return nil, nil, err
	}
	tr := &quic.Transport{}
	if !managed {
		s, e := net.ListenUDP("udp4", addr)
		if e != nil {
			return nil, nil, e
		}
		tr.Conn = s
		return tr, func() { tr.Close(); s.Close() }, nil
	}
	parent, acquire, err := tr.NewManagedPacketEndpointV1("udp4", addr)
	if err != nil {
		return nil, nil, err
	}
	lease, err := acquire()
	if err != nil {
		parent.Close()
		return nil, nil, err
	}
	tr.Conn = lease
	if err = tr.ConfigureManagedPacketIOV1(lease, lease, nil); err != nil {
		lease.Close()
		parent.Close()
		return nil, nil, err
	}
	return tr, func() { tr.Close(); lease.Close(); parent.Close() }, nil
}
func quicConfig(r Run, tr *trace) *quic.Config {
	c := &quic.Config{EnableDatagrams: true, InitialPacketSize: 1400, DisablePathMTUDiscovery: true,
		InitialStreamReceiveWindow: 64 << 20, MaxStreamReceiveWindow: 64 << 20, InitialConnectionReceiveWindow: 128 << 20, MaxConnectionReceiveWindow: 128 << 20,
		MaxIdleTimeout: 3 * time.Minute, HandshakeIdleTimeout: 5 * time.Second, MaxIncomingStreams: 4, MaxIncomingUniStreams: 4}
	_ = c.SetCongestionControlV1(r.Controller)
	c.Tracer = tr.factory
	return c
}
func createCertificate(dir string) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	t := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "q1-campaign"}, DNSNames: []string{"q1-campaign"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	secret, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	certPath, keyPath := filepath.Join(dir, "campaign.pem"), filepath.Join(dir, "campaign-key.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return "", "", err
	}
	err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: secret}), 0600)
	return certPath, keyPath, err
}
func loadTLS(certPath, keyPath string, server bool) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("invalid campaign certificate")
	}
	c := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: roots, ClientCAs: roots, ServerName: "q1-campaign", NextProtos: []string{"q1-bbr-campaign-v1"}}
	if server {
		c.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return c, nil
}
func writeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("record too large")
	}
	h := make([]byte, 4)
	binary.BigEndian.PutUint32(h, uint32(len(b)))
	if err = writeAll(w, h); err != nil {
		return err
	}
	return writeAll(w, b)
}
func readJSON(r io.Reader, v any) error {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(h[:])
	if n > 1<<20 {
		return fmt.Errorf("record too large")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, e := w.Write(p)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrNoProgress
		}
		p = p[n:]
	}
	return nil
}
func waitUntil(ctx context.Context, t time.Time) error {
	for time.Now().Before(t) {
		timer := time.NewTimer(time.Until(t))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func receiveSession(ctx context.Context, conn *quic.Conn, cfg Run) (Result, error) {
	result := Result{Run: cfg, Controller: conn.CongestionControlV1()}
	ctrl, err := conn.AcceptStream(ctx)
	if err != nil {
		return result, err
	}
	ctrl.SetDeadline(cfg.end().Add(3 * time.Second))
	var remote Run
	if err = readJSON(ctrl, &remote); err != nil {
		return result, err
	}
	if !reflect.DeepEqual(remote, cfg) {
		return result, fmt.Errorf("run config mismatch")
	}
	if err = writeAll(ctrl, []byte{1}); err != nil {
		return result, err
	}
	controlDone := make(chan error, 1)
	go func() {
		var p [32]byte
		for {
			_, e := io.ReadFull(ctrl, p[:])
			if e != nil {
				controlDone <- e
				return
			}
			received := time.Now().UnixNano()
			binary.BigEndian.PutUint64(p[16:24], uint64(received))
			binary.BigEndian.PutUint64(p[24:32], uint64(time.Now().UnixNano()))
			if e = writeAll(ctrl, p[:]); e != nil {
				controlDone <- e
				return
			}
		}
	}()
	counter := NewCounter(cfg.measuredStart(), time.Duration(cfg.MeasureMS)*time.Millisecond)
	end := cfg.end().Add(1500 * time.Millisecond)
	receiveCtx, cancel := context.WithDeadline(ctx, end)
	defer cancel()
	var consumed int64
	var admitted time.Time
	var expectedSequence uint64
	var receiveErr error
	if cfg.Workload == "stream" {
		stream, e := conn.AcceptUniStream(receiveCtx)
		receiveErr = e
		if e == nil {
			admitted = time.Now()
			if cfg.CompletionBytes > 0 {
				end = admitted.Add(60 * time.Second)
				counter = NewCounter(admitted, 60*time.Second)
			}
			stream.SetReadDeadline(end)
			p := make([]byte, cfg.PayloadBytes)
			for {
				_, e = io.ReadFull(stream, p[:headerBytes])
				if e == io.EOF {
					if cfg.CompletionBytes > 0 && consumed == cfg.CompletionBytes {
						result.CompletionVerified = true
						result.CompletionNS = time.Since(admitted).Nanoseconds()
					} else if cfg.CompletionBytes > 0 {
						receiveErr = fmt.Errorf("premature completion EOF: %d of %d bytes", consumed, cfg.CompletionBytes)
					}
					break
				}
				if e != nil {
					receiveErr = e
					break
				}
				if binary.BigEndian.Uint64(p[4:12]) != expectedSequence {
					receiveErr = fmt.Errorf("non-contiguous stream identity")
					break
				}
				expectedSequence++
				n := int(binary.BigEndian.Uint32(p[12:16]))
				if n < 1 || n > cfg.PayloadBytes-headerBytes {
					receiveErr = fmt.Errorf("invalid stream frame length")
					break
				}
				if _, e = io.ReadFull(stream, p[headerBytes:headerBytes+n]); e != nil {
					receiveErr = e
					break
				}
				if e = counter.Record(p[:headerBytes+n], time.Now()); e != nil {
					receiveErr = e
					break
				}
				consumed += int64(n)
				if cfg.CompletionBytes > 0 && consumed > cfg.CompletionBytes {
					receiveErr = fmt.Errorf("completion payload exceeded target")
					break
				}
			}
		}
	} else {
		for {
			p, e := conn.ReceiveDatagram(receiveCtx)
			if e != nil {
				if !errors.Is(e, context.DeadlineExceeded) {
					receiveErr = e
				}
				break
			}
			if e = counter.Record(p, time.Now()); e != nil {
				receiveErr = e
				break
			}
		}
	}
	result.Receiver = counter.Snapshot()
	if cfg.CompletionBytes > 0 && !result.CompletionVerified {
		var timeout net.Error
		result.Censored = errors.Is(receiveErr, context.DeadlineExceeded) || (errors.As(receiveErr, &timeout) && timeout.Timeout())
	}
	if receiveErr != nil {
		var timeout net.Error
		if !errors.As(receiveErr, &timeout) || !timeout.Timeout() {
			result.Errors = append(result.Errors, receiveErr.Error())
		}
	}
	// Sender closes its control write side after the observation window.
	if e := <-controlDone; e != nil && !errors.Is(e, io.EOF) {
		var timeout net.Error
		if !errors.As(e, &timeout) || !timeout.Timeout() {
			result.Errors = append(result.Errors, e.Error())
		}
	}
	report, e := conn.OpenStreamSync(ctx)
	if e != nil {
		return result, e
	}
	report.SetDeadline(time.Now().Add(5 * time.Second))
	if e = writeJSON(report, result); e != nil {
		return result, e
	}
	var ack [1]byte
	if _, e = io.ReadFull(report, ack[:]); e != nil {
		return result, e
	}
	if ack[0] != 1 {
		return result, fmt.Errorf("invalid report acknowledgment")
	}
	report.Close()
	// The receiver owns successful shutdown after consuming the sender acknowledgment.
	conn.CloseWithError(0, "receipt accepted")
	return result, nil
}
func sendSession(ctx context.Context, conn *quic.Conn, cfg Run) (Result, error) {
	result := Result{Run: cfg, Controller: conn.CongestionControlV1()}
	if cfg.Workload == "datagram" && !conn.ConnectionState().SupportsDatagrams.Remote {
		return result, fmt.Errorf("peer did not negotiate DATAGRAM")
	}
	ctrl, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return result, err
	}
	ctrl.SetDeadline(cfg.end().Add(3 * time.Second))
	if err = writeJSON(ctrl, cfg); err != nil {
		return result, err
	}
	var ack [1]byte
	if _, err = io.ReadFull(ctrl, ack[:]); err != nil {
		return result, err
	}
	if time.Now().After(cfg.start()) {
		return result, fmt.Errorf("start deadline missed during setup")
	}
	if err = waitUntil(ctx, cfg.start()); err != nil {
		return result, err
	}
	bulkCtx, cancel := context.WithDeadline(ctx, cfg.end())
	defer cancel()
	controlDone := make(chan Control, 1)
	go func() { controlDone <- observeControl(bulkCtx, ctrl, cfg) }()
	var stream *quic.SendStream
	if cfg.Workload == "stream" {
		stream, err = conn.OpenUniStreamSync(ctx)
		if err != nil {
			return result, err
		}
		stream.SetWriteDeadline(cfg.end().Add(time.Second))
	}
	p := make([]byte, cfg.PayloadBytes)
	var useful int64
	for seq := uint64(0); seq < maxSequence && time.Now().Before(cfg.end()); seq++ {
		if err = waitBulkDemand(bulkCtx, cfg, &result); err != nil {
			break
		}
		if cfg.CompletionBytes > 0 && useful >= cfg.CompletionBytes {
			break
		}
		payload := p
		if cfg.CompletionBytes > 0 {
			payload = p[:headerBytes+min(int64(len(p)-headerBytes), cfg.CompletionBytes-useful)]
		}
		encodePayload(payload, seq)
		before := time.Now()
		if stream != nil {
			err = writeAll(stream, payload)
		} else {
			err = conn.SendDatagram(payload)
		}
		result.SendCallNS += time.Since(before).Nanoseconds()
		if err != nil {
			break
		}
		result.SentMessages++
		useful += int64(len(payload) - headerBytes)
	}
	// Preserve the failure before waiting for the control observer: that wait
	// can run through the window and must not turn an early failure into a
	// seemingly normal duration stop.
	if err != nil && !(os.IsTimeout(err) && !time.Now().Before(cfg.end())) {
		result.Errors = append(result.Errors, err.Error())
	}
	if stream != nil {
		stream.Close()
	}
	if cfg.CompletionBytes > 0 {
		cancel()
	}
	result.Control = <-controlDone
	if result.Control.Error != "" {
		result.Errors = append(result.Errors, result.Control.Error)
	}
	ctrl.Close()
	if err != nil && !time.Now().After(cfg.end()) {
		return result, err
	}
	report, e := conn.AcceptStream(ctx)
	if e != nil {
		return result, e
	}
	report.SetDeadline(time.Now().Add(5 * time.Second))
	var receiver Result
	if e = readJSON(report, &receiver); e != nil {
		return result, e
	}
	if e = writeAll(report, []byte{1}); e != nil {
		return result, e
	}
	report.Close()
	// A successful Write is only local admission. Wait for receiver-owned close
	// to prove the final acknowledgment reached its consumer before teardown.
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case <-conn.Context().Done():
		var app *quic.ApplicationError
		if !errors.As(context.Cause(conn.Context()), &app) || !app.Remote || app.ErrorCode != 0 || app.ErrorMessage != "receipt accepted" {
			return result, context.Cause(conn.Context())
		}
	}
	result.Receiver = receiver.Receiver
	result.Errors = append(result.Errors, receiver.Errors...)
	result.CompletionNS = receiver.CompletionNS
	result.CompletionVerified = receiver.CompletionVerified
	result.Censored = receiver.Censored
	return result, nil
}

type controlStream interface {
	io.ReadWriter
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

func observeControl(ctx context.Context, s controlStream, cfg Run) (c Control) {
	type response struct {
		latency  int64
		bounds   [2]int64
		err      error
		measured bool
	}
	replies := make(chan response, 1)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	pending, pendingMeasured := false, false
	seq := uint64(0)
	var wg sync.WaitGroup
	defer func() {
		s.SetReadDeadline(time.Now())
		s.SetWriteDeadline(time.Now())
		wg.Wait()
		if cfg.CompletionBytes == 0 {
			c.ExpectedOpportunities = max(0, int((cfg.WarmupMS+cfg.MeasureMS-1)/1000-(cfg.WarmupMS+999)/1000+1))
			c.MissedOpportunities = max(0, c.ExpectedOpportunities-len(c.Opportunities))
		}
	}()
	offer := func(due time.Time) {
		now := time.Now()
		measured := !now.Before(cfg.measuredStart()) && now.Before(cfg.end())
		if measured {
			c.Opportunities = append(c.Opportunities, [2]int64{due.UnixNano(), now.UnixNano()})
		}
		if pending {
			if measured {
				c.Skipped++
			}
			return
		}
		pending = true
		pendingMeasured = measured
		seq++
		if measured {
			c.Offered++
		}
		id := seq
		wg.Add(1)
		go func() {
			defer wg.Done()
			var p [32]byte
			sent := time.Now().UnixNano()
			binary.BigEndian.PutUint64(p[:8], id)
			binary.BigEndian.PutUint64(p[8:16], uint64(sent))
			e := writeAll(s, p[:])
			if e == nil {
				_, e = io.ReadFull(s, p[:])
			}
			received := time.Now().UnixNano()
			if e == nil && binary.BigEndian.Uint64(p[:8]) != id {
				e = fmt.Errorf("control sequence mismatch")
			}
			replies <- response{received - sent, [2]int64{int64(binary.BigEndian.Uint64(p[24:32])) - received, int64(binary.BigEndian.Uint64(p[16:24])) - sent}, e, measured}
		}()
	}
	offer(cfg.start())
	for {
		select {
		case <-ctx.Done():
			if pending && pendingMeasured {
				c.Unresolved++
			}
			return c
		case due := <-ticker.C:
			offer(due)
		case r := <-replies:
			pending = false
			if r.err != nil {
				c.Error = r.err.Error()
				if r.measured {
					c.Unresolved++
				}
				return c
			}
			c.ClockBoundsNS = append(c.ClockBoundsNS, r.bounds)
			if r.measured {
				c.Replies++
				c.LatencyNS = append(c.LatencyNS, r.latency)
			}
		}
	}
}

// The competitor removes bulk demand while preserving its connection/controller
// state. The low-rate control probe remains observable during the absence.
// The fixed receiver window still includes all 30 seconds of absent bulk demand.
func waitBulkDemand(ctx context.Context, cfg Run, r *Result) error {
	if cfg.BulkPauseEndMS == 0 || len(r.BulkPauseObservedNS) > 0 {
		return nil
	}
	start := cfg.measuredStart().Add(time.Duration(cfg.BulkPauseStartMS) * time.Millisecond)
	end := cfg.measuredStart().Add(time.Duration(cfg.BulkPauseEndMS) * time.Millisecond)
	now := time.Now()
	if now.Before(start) {
		return nil
	}
	if !now.Before(end) {
		return fmt.Errorf("competitor bulk pause was missed")
	}
	r.BulkPauseObservedNS = append(r.BulkPauseObservedNS, now.UnixNano())
	e := waitUntil(ctx, end)
	r.BulkPauseObservedNS = append(r.BulkPauseObservedNS, time.Now().UnixNano())
	return e
}
