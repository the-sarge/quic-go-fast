//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

// Fail-closed qualification for the private recvmsg_x receive-batching path
// (Slice D2 of docs/adr/2026-09-11-darwin-batch-plan.md). The receive
// capability mirrors the D1 send capability in
// sys_conn_sendmsg_x_qual_darwin.go and shares its Darwin-kernel-major
// allowlist: constructed once per process from the allowlist plus a
// production-shape loopback self-check, disabled by the
// QUIC_GO_DISABLE_RECVMSG_X kill switch, latched off for the process after
// ENOSYS or any structurally invalid result, and observable through
// engaged/fallback counters.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/utils"
)

// recvmsgXDisableEnv is the kill switch, following the existing
// QUIC_GO_DISABLE_GSO convention.
const recvmsgXDisableEnv = "QUIC_GO_DISABLE_RECVMSG_X"

// recvmsgXKernelMajor reports the running Darwin kernel major; tests inject
// a fake to exercise allowlist decisions for unlisted majors. The qualified
// set itself is shared with D1 (qualifiedDarwinKernelMajors): both paths ride
// the same private msghdr_x ABI, and the receive self-check below is the
// receive-shape semantic authority on top of that shared allowlist.
var recvmsgXKernelMajor = getMacOSVersion

// recvmsgXState is the process-wide receive-batching capability: qualified
// is set once by qualify, and latched permanently disables the path after
// ENOSYS or a structurally invalid kernel result. The counters make the
// active path observable: batchReads counts delivering recvmsg_x syscalls,
// batchDatagrams the datagrams those syscalls delivered, and fallbackReads
// the capability consultations that declined, each followed by one
// single-datagram read on the preserved path.
type recvmsgXState struct {
	qualifyOnce    sync.Once
	qualifyStarted atomic.Bool
	qualified      atomic.Bool
	latched        atomic.Bool

	batchReads     atomic.Uint64
	batchDatagrams atomic.Uint64
	fallbackReads  atomic.Uint64
}

var recvmsgX recvmsgXState

// recvmsgXEnsureQualified runs the qualification (kill switch, allowlist,
// self-check) to completion. Tests and the measurement harness call it for
// deterministic activation; the read path uses recvmsgXAvailable instead.
func recvmsgXEnsureQualified() {
	recvmsgX.qualifyStarted.Store(true)
	recvmsgX.qualifyOnce.Do(recvmsgXQualify)
}

// recvmsgXAvailable reports whether batched receive may be used. The first
// consultation starts the qualification in the background — the loopback
// self-check can take a while on a misbehaving kernel, and the read path
// must never block on it — and the path stays fail-closed until
// qualification completes successfully.
func recvmsgXAvailable() bool {
	if recvmsgX.latched.Load() {
		return false
	}
	if !recvmsgX.qualified.Load() {
		if !recvmsgX.qualifyStarted.Swap(true) {
			go recvmsgX.qualifyOnce.Do(recvmsgXQualify)
		}
		return false
	}
	return true
}

func recvmsgXQualify() {
	if disabled, err := strconv.ParseBool(os.Getenv(recvmsgXDisableEnv)); err == nil && disabled {
		utils.DefaultLogger.Debugf("recvmsg_x receive batching disabled by %s.", recvmsgXDisableEnv)
		return
	}
	major, err := recvmsgXKernelMajor()
	if err != nil {
		utils.DefaultLogger.Debugf("recvmsg_x receive batching disabled: reading Darwin kernel version: %s.", err)
		return
	}
	product, ok := qualifiedDarwinKernelMajors[major]
	if !ok {
		utils.DefaultLogger.Debugf("recvmsg_x receive batching disabled: Darwin kernel major %d is not in the qualified set.", major)
		return
	}
	if err := recvmsgXSelfCheck(); err != nil {
		utils.DefaultLogger.Debugf("recvmsg_x receive batching disabled: self-check failed on qualified kernel major %d (%s): %s.", major, product, err)
		return
	}
	utils.DefaultLogger.Debugf("recvmsg_x receive batching enabled: Darwin kernel major %d (%s), self-check passed.", major, product)
	recvmsgX.qualified.Store(true)
}

// recvmsgXLatchOff permanently disables the receive-batching path for this
// process.
func recvmsgXLatchOff(reason string) {
	if !recvmsgX.latched.Swap(true) {
		utils.DefaultLogger.Debugf("recvmsg_x receive batching latched off: %s.", reason)
	}
}

// recvmsgXCountersSnapshot returns (delivering batch reads, datagrams
// delivered via batch reads, declined capability consultations each followed
// by one single-datagram read).
func recvmsgXCountersSnapshot() (batchReads, batchDatagrams, fallbackReads uint64) {
	return recvmsgX.batchReads.Load(), recvmsgX.batchDatagrams.Load(), recvmsgX.fallbackReads.Load()
}

// recvmsgXSelfCheck exercises the production receive shape against the
// running kernel: a dual-stack AF_INET6 loopback receiver with the
// production control-message options enabled (IPV6_RECVTCLASS, IP_RECVTOS,
// and both packet-info options, exactly the set newConn requests), read via
// recvmsg_x with per-message name, iovec, and control buffers. Two senders —
// native IPv6 and IPv4 through the dual-stack socket — send datagrams
// carrying DIFFERENT ECN marks, so per-message ancillary attribution is
// verified by content: a kernel that shared or shifted control data across
// the batch would surface the wrong mark on the wrong payload. Payloads,
// per-message byte counts, source address families, and source ports are
// verified semantically, so a future kernel returning plausible counts under
// an incompatible ABI is caught by content, not inferred from error codes.
func recvmsgXSelfCheck() error {
	recv, port, err := newRecvSelfCheckReceiver()
	if err != nil {
		return err
	}
	defer unix.Close(recv)

	send6, err := newSelfCheckSocket(unix.AF_INET6)
	if err != nil {
		return fmt.Errorf("IPv6 sender socket: %w", err)
	}
	defer unix.Close(send6)
	if err := unix.SetsockoptInt(send6, unix.IPPROTO_IPV6, unix.IPV6_TCLASS, 0x2 /* ECT(0) */); err != nil {
		return fmt.Errorf("IPV6_TCLASS: %w", err)
	}
	send4, err := newSelfCheckSocket(unix.AF_INET)
	if err != nil {
		return fmt.Errorf("IPv4 sender socket: %w", err)
	}
	defer unix.Close(send4)
	if err := unix.SetsockoptInt(send4, unix.IPPROTO_IP, unix.IP_TOS, 0x1 /* ECT(1) */); err != nil {
		return fmt.Errorf("IP_TOS: %w", err)
	}

	// Payload → the ECN mark its message's OWN control data must carry.
	wantECN := map[string]uint8{
		"quic-go-fast recvmsg_x self-check 6a": 0x2,
		"quic-go-fast recvmsg_x self-check 6b": 0x2,
		"quic-go-fast recvmsg_x self-check 4a": 0x1,
		"quic-go-fast recvmsg_x self-check 4b": 0x1,
	}
	dst6 := &unix.SockaddrInet6{Port: port, Addr: [16]byte{15: 1}}
	dst4 := &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}}
	for payload, ecn := range wantECN {
		var sendErr error
		if ecn == 0x2 {
			sendErr = unix.Sendto(send6, []byte(payload), 0, dst6)
		} else {
			sendErr = unix.Sendto(send4, []byte(payload), 0, dst4)
		}
		if sendErr != nil {
			return fmt.Errorf("sending %q: %w", payload, sendErr)
		}
	}

	rc := newRecvmsgXScratch(len(wantECN))
	bufs := make([][]byte, len(wantECN))
	oobs := make([][]byte, len(wantECN))
	for i := range bufs {
		bufs[i] = make([]byte, 2048)
		oobs[i] = make([]byte, oobBufferSize)
	}
	for len(wantECN) > 0 {
		rc.prepare(bufs, oobs)
		n, errno := rawRecvmsgX(recv, rc.msgs[:len(bufs)], 0)
		if errno != 0 {
			return fmt.Errorf("recvmsg_x: errno %d", int(errno))
		}
		if n <= 0 || n > len(bufs) {
			return fmt.Errorf("recvmsg_x: structurally invalid message count %d of %d", n, len(bufs))
		}
		for i := range n {
			datalen := int(rc.msgs[i].Datalen)
			if datalen < 0 || datalen > len(bufs[i]) {
				return fmt.Errorf("message %d: structurally invalid datalen %d", i, datalen)
			}
			payload := string(bufs[i][:datalen])
			want, ok := wantECN[payload]
			if !ok {
				return fmt.Errorf("unexpected payload %q", payload)
			}
			delete(wantECN, payload)
			ctrlLen := int(rc.msgs[i].Controllen)
			if ctrlLen < 0 || ctrlLen > len(oobs[i]) {
				return fmt.Errorf("message %d: structurally invalid controllen %d", i, ctrlLen)
			}
			// The message's OWN control data must carry its sender's mark.
			// Which level darwin surfaces it at is the platform's choice —
			// a dual-stack socket typically only activates IPV6_RECVTCLASS
			// and maps a v4 sender's TOS into it — and the production
			// control-message walk parses both levels, so the self-check
			// accepts either and pins the value.
			got, err := selfCheckECNBits(oobs[i][:ctrlLen], unix.IPPROTO_IPV6, unix.IPV6_TCLASS)
			if err != nil {
				got, err = selfCheckECNBits(oobs[i][:ctrlLen], unix.IPPROTO_IP, msgTypeIPTOS)
			}
			if err != nil {
				return fmt.Errorf("message %d (%q): %w", i, payload, err)
			}
			if got != want {
				return fmt.Errorf("message %d (%q): ECN bits %#x, want %#x", i, payload, got, want)
			}
			if err := recvSelfCheckSourceMatches(rc.names[i][:], want, send6, send4); err != nil {
				return fmt.Errorf("message %d (%q): %w", i, payload, err)
			}
		}
	}
	return nil
}

// newRecvSelfCheckReceiver binds a dual-stack loopback UDP socket with the
// production receive options enabled (the set newConn requests on a
// wildcard-bound socket) and a receive timeout, returning its port.
func newRecvSelfCheckReceiver() (int, int, error) {
	fd, err := newSelfCheckSocket(unix.AF_INET6)
	if err != nil {
		return -1, 0, err
	}
	fail := func(err error) (int, int, error) {
		unix.Close(fd)
		return -1, 0, err
	}
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 0); err != nil {
		return fail(fmt.Errorf("clearing IPV6_V6ONLY: %w", err))
	}
	// Request the production option set the way newConn does: both IP
	// versions are attempted and at least one of each pair must succeed
	// (darwin rejects the IPPROTO_IP options on an AF_INET6 socket).
	errECNIPv4 := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_RECVTOS, 1)
	errECNIPv6 := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_RECVTCLASS, 1)
	if errECNIPv4 != nil && errECNIPv6 != nil {
		return fail(fmt.Errorf("activating ECN receipt failed for both IP versions: %w / %w", errECNIPv4, errECNIPv6))
	}
	errPIIPv4 := unix.SetsockoptInt(fd, unix.IPPROTO_IP, ipv4PKTINFO, 1)
	errPIIPv6 := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_RECVPKTINFO, 1)
	if errPIIPv4 != nil && errPIIPv6 != nil {
		return fail(fmt.Errorf("activating packet info failed for both IP versions: %w / %w", errPIIPv4, errPIIPv6))
	}
	timeout := unix.NsecToTimeval((2 * time.Second).Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &timeout); err != nil {
		return fail(fmt.Errorf("SO_RCVTIMEO: %w", err))
	}
	// Bind the dual-stack wildcard, the production socket shape
	// (net.ListenUDP("udp", nil)): a [::1]-only binding could never receive
	// the v4-mapped datagrams whose attribution this check qualifies.
	if err := unix.Bind(fd, &unix.SockaddrInet6{}); err != nil {
		return fail(fmt.Errorf("bind: %w", err))
	}
	bound, err := unix.Getsockname(fd)
	if err != nil {
		return fail(fmt.Errorf("getsockname: %w", err))
	}
	sa, ok := bound.(*unix.SockaddrInet6)
	if !ok {
		return fail(errors.New("unexpected bound address family"))
	}
	return fd, sa.Port, nil
}

// recvSelfCheckSourceMatches verifies a received message's msg_name against
// the sender socket it must have come from: family (native IPv6 vs v4-mapped
// through the dual-stack receiver) and source port.
func recvSelfCheckSourceMatches(name []byte, ecn uint8, send6, send4 int) error {
	sa := (*syscall.RawSockaddrInet6)(unsafe.Pointer(&name[0]))
	if sa.Family != syscall.AF_INET6 {
		return fmt.Errorf("source family %d, want AF_INET6", sa.Family)
	}
	senderFD := send6
	if ecn == 0x1 {
		senderFD = send4
		if sa.Addr[10] != 0xff || sa.Addr[11] != 0xff {
			return errors.New("IPv4 sender did not surface as a v4-mapped source")
		}
	}
	bound, err := unix.Getsockname(senderFD)
	if err != nil {
		return fmt.Errorf("sender getsockname: %w", err)
	}
	var wantPort int
	switch sa := bound.(type) {
	case *unix.SockaddrInet6:
		wantPort = sa.Port
	case *unix.SockaddrInet4:
		wantPort = sa.Port
	default:
		return errors.New("unexpected sender address family")
	}
	gotPort := int(sa.Port>>8)&0xff | int(sa.Port&0xff)<<8
	if gotPort != wantPort {
		return fmt.Errorf("source port %d, want %d", gotPort, wantPort)
	}
	return nil
}
