package quic

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"

	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlog"
)

// Setup evidence is immutable and owned by the endpoint, not a lease or transport.
// A zero size means unknown; it must never become a successful capacity claim.
type managedBufferResult struct {
	size int
	err  error
}

type managedBufferSetup struct {
	receive, send managedBufferResult
}

func inspectManagedBuffers(conn net.PacketConn, receiveErr, sendErr error) managedBufferSetup {
	b := managedBufferSetup{receive: managedBufferResult{err: receiveErr}, send: managedBufferResult{err: sendErr}}
	sc, ok := conn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	var raw syscall.RawConn
	var err error
	if ok {
		raw, err = sc.SyscallConn()
	}
	if err == nil && raw == nil {
		err = errors.New("socket buffer inspection unavailable")
	}
	if err != nil {
		b.receive.err = errors.Join(b.receive.err, err)
		b.send.err = errors.Join(b.send.err, err)
		return b
	}
	b.receive.size, err = inspectReadBuffer(raw)
	b.receive = checkedManagedBuffer(b.receive, err)
	b.send.size, err = inspectWriteBuffer(raw)
	b.send = checkedManagedBuffer(b.send, err)
	return b
}

func checkedManagedBuffer(r managedBufferResult, inspectErr error) managedBufferResult {
	if inspectErr != nil || r.size <= 0 {
		r.size = 0
		if inspectErr == nil {
			inspectErr = errors.New("socket buffer inspection unavailable")
		}
		r.err = errors.Join(r.err, inspectErr)
	} else if r.size < desiredBufferSize && r.err == nil {
		r.err = fmt.Errorf("socket buffer below target (wanted: %d, got: %d)", desiredBufferSize, r.size)
	}
	return r
}

// Only the existing sizing helpers own warning policy. Additional diagnostic
// inspection can establish unknown capacity without consuming that warning budget.
func managedBufferWarning(receiveErr, sendErr error) error {
	var errs []error
	if receiveErr != nil && !strings.Contains(receiveErr.Error(), "use of closed network connection") {
		errs = append(errs, fmt.Errorf("managed receive buffer: %w", receiveErr))
	}
	if sendErr != nil && !strings.Contains(sendErr.Error(), "use of closed network connection") {
		errs = append(errs, fmt.Errorf("managed send buffer: %w", sendErr))
	}
	return errors.Join(errs...)
}

func (r managedBufferResult) diagnostic(direction string) string {
	status := "configured"
	switch {
	case r.size == 0:
		status = "unknown"
	case r.size < desiredBufferSize:
		status = "insufficient"
	case r.err != nil:
		status = "error"
	}
	detail := ""
	if r.err != nil {
		detail = r.err.Error()
	}
	return fmt.Sprintf("%s_buffer_status=%s %s_buffer_bytes=%d %s_buffer_error=%q", direction, status, direction, r.size, direction, detail)
}

// Exact concrete views and explicit validated registrations are the only sources
// of managed evidence. In particular, promoted methods confer no provenance.
func (t *Transport) managedBuffers() *managedBufferSetup {
	if c := t.packetIO.external; c != nil {
		return c.managedBuffers
	}
	if c, ok := t.Conn.(*managedPacketConn); ok && c != nil {
		return &c.endpoint.buffers
	}
	return nil
}

func (t *Transport) traceManagedBuffers(b *managedBufferSetup, conn rawConn) {
	if b == nil {
		return
	}
	batch := t.packetIO.external != nil && t.packetIO.external.sendBatch != nil
	cap := conn.capabilities()
	receiveMode := "native"
	if _, ok := conn.(*basicConn); ok {
		receiveMode = "ordinary"
	}
	if c := t.packetIO.external; c != nil && c.managedReceiveCoalescing {
		receiveMode = "normalized"
		cap.GRO = true
	}
	message := fmt.Sprintf("provenance=managed_endpoint %s %s buffer_target_bytes=%d receive_mode=%s batch_callback_available=%t df=%t ecn=%t segmentation=%t coalescing=%t", b.receive.diagnostic("receive"), b.send.diagnostic("send"), desiredBufferSize, receiveMode, batch, cap.DF, cap.ECN, cap.GSO, cap.GRO)
	utils.DefaultLogger.Debugf("managed_packet_io: %s", message)
	if t.Tracer != nil {
		t.Tracer.RecordEvent(qlog.DebugEvent{EventName: "managed_packet_io", Message: message})
	}
}
