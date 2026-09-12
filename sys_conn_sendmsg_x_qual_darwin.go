//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

// Fail-closed qualification for the private sendmsg_x batch-send path, per
// docs/adr/2026-09-11-datapath-offload-plan.md: the batch capability is
// constructed once per process from a Darwin-kernel-major allowlist plus a
// production-shape loopback self-check, is disabled by the
// QUIC_GO_DISABLE_SENDMSG_X kill switch, latches off for the process after
// ENOSYS or any structurally invalid result, and is observable through
// engaged/fallback counters.

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

// sendmsgXDisableEnv is the kill switch, following the existing
// QUIC_GO_DISABLE_GSO convention.
const sendmsgXDisableEnv = "QUIC_GO_DISABLE_SENDMSG_X"

// qualifiedDarwinKernelMajors is the fail-closed allowlist, keyed by Darwin
// kernel major version — the value the runtime actually compares, read from
// the kernel release string (uname -r). Each entry records the macOS product
// version observed on the qualifying host, because the two namespaces differ
// (a host can report product 26.x with Darwin 25.x) and comparing the
// product number would silently misqualify. Entries are populated by the D1
// protocol's qualification runs (docs/audits/2026-09-12-d1-sendmsgx-protocol.md)
// and record the tested version floor, never a ceiling: an unlisted or newer
// major starts in the per-packet fallback path until a qualification run
// adds it.
var qualifiedDarwinKernelMajors = map[int]string{
	25: "macOS 26 (qualified on product 26.6.2, Darwin 25.6.0, arm64)",
}

// sendmsgXKernelMajor reports the running Darwin kernel major; tests inject
// a fake to exercise allowlist decisions for unlisted majors.
var sendmsgXKernelMajor = getMacOSVersion

// sendmsgXState is the process-wide batch-send capability: qualified is set
// once by qualify, and latched permanently disables the path after ENOSYS or
// a structurally invalid kernel result. The counters make the active path
// observable: batchSubmissions counts sendmsg_x syscalls, batchPackets the
// datagrams those syscalls accepted, and fallbackPackets the datagrams that
// consulted the capability and proceeded per-packet instead.
type sendmsgXState struct {
	qualifyOnce    sync.Once
	qualifyStarted atomic.Bool
	qualified      atomic.Bool
	latched        atomic.Bool

	batchSubmissions atomic.Uint64
	batchPackets     atomic.Uint64
	fallbackPackets  atomic.Uint64
}

var sendmsgX sendmsgXState

// sendmsgXEnsureQualified runs the qualification (kill switch, allowlist,
// self-check) to completion. Tests and the measurement harness call it for
// deterministic activation; the send path uses sendmsgXAvailable instead.
func sendmsgXEnsureQualified() {
	sendmsgX.qualifyStarted.Store(true)
	sendmsgX.qualifyOnce.Do(sendmsgXQualify)
}

// sendmsgXAvailable reports whether batched submission may be used. The
// first consultation starts the qualification in the background — the
// loopback self-check can take a while on a misbehaving kernel, and the
// send path must never block on it — and the path stays fail-closed until
// qualification completes successfully.
func sendmsgXAvailable() bool {
	if sendmsgX.latched.Load() {
		return false
	}
	if !sendmsgX.qualified.Load() {
		if !sendmsgX.qualifyStarted.Swap(true) {
			go sendmsgX.qualifyOnce.Do(sendmsgXQualify)
		}
		return false
	}
	return true
}

func sendmsgXQualify() {
	if disabled, err := strconv.ParseBool(os.Getenv(sendmsgXDisableEnv)); err == nil && disabled {
		utils.DefaultLogger.Debugf("sendmsg_x batch send disabled by %s.", sendmsgXDisableEnv)
		return
	}
	major, err := sendmsgXKernelMajor()
	if err != nil {
		utils.DefaultLogger.Debugf("sendmsg_x batch send disabled: reading Darwin kernel version: %s.", err)
		return
	}
	product, ok := qualifiedDarwinKernelMajors[major]
	if !ok {
		utils.DefaultLogger.Debugf("sendmsg_x batch send disabled: Darwin kernel major %d is not in the qualified set.", major)
		return
	}
	if err := sendmsgXSelfCheck(); err != nil {
		utils.DefaultLogger.Debugf("sendmsg_x batch send disabled: self-check failed on qualified kernel major %d (%s): %s.", major, product, err)
		return
	}
	utils.DefaultLogger.Debugf("sendmsg_x batch send enabled: Darwin kernel major %d (%s), self-check passed.", major, product)
	sendmsgX.qualified.Store(true)
}

// sendmsgXLatchOff permanently disables the batch path for this process.
func sendmsgXLatchOff(reason string) {
	if !sendmsgX.latched.Swap(true) {
		utils.DefaultLogger.Debugf("sendmsg_x batch send latched off: %s.", reason)
	}
}

// sendmsgXSubmit performs one bounds-checked batched submission and returns
// how many leading payloads the kernel accepted. ENOSYS and structurally
// invalid results (an accepted count outside [0, len(payloads)], or an
// accepted count alongside an errno) latch the capability off for the
// process and report nothing accepted, so the worker's per-packet retry owns
// every datagram.
func sendmsgXSubmit(fd int, payloads [][]byte, name *byte, namelen uint32, oob []byte, msgs []msghdrX, iovs []syscall.Iovec) int {
	accepted, errno := sendmsgXBatchTo(fd, payloads, name, namelen, oob, msgs, iovs)
	sendmsgX.batchSubmissions.Add(1)
	if errno == syscall.ENOSYS {
		sendmsgXLatchOff("kernel returned ENOSYS")
		return 0
	}
	if errno != 0 {
		if accepted != 0 {
			sendmsgXLatchOff(fmt.Sprintf("structurally invalid result: accepted %d with errno %d", accepted, int(errno)))
		}
		return 0
	}
	if accepted < 0 || accepted > len(payloads) {
		sendmsgXLatchOff(fmt.Sprintf("structurally invalid result: accepted %d of %d offered", accepted, len(payloads)))
		return 0
	}
	sendmsgX.batchPackets.Add(uint64(accepted))
	return accepted
}

// sendmsgXCountersSnapshot returns (batch submissions, packets accepted via
// batches, packets that fell back to the per-packet path).
func sendmsgXCountersSnapshot() (submissions, batchPackets, fallbackPackets uint64) {
	return sendmsgX.batchSubmissions.Load(), sendmsgX.batchPackets.Load(), sendmsgX.fallbackPackets.Load()
}

// sendmsgXSelfCheck exercises the production call shape against the running
// kernel: unconnected sends from a dual-stack AF_INET6 socket carrying
// explicit destination sockaddrs in msg_name — a native IPv6 destination and
// a v4-mapped IPv4 destination — with the shared ECN control-message buffer
// built by the production encoders. These are the fields historical
// sendmsg_x restrictions actually broke. Delivered bytes, destinations, ECN
// marks, and accepted counts are verified semantically, so a future kernel
// returning plausible counts under an incompatible ABI is caught by content,
// not inferred from error codes.
func sendmsgXSelfCheck() error {
	sender, err := newSelfCheckSocket(unix.AF_INET6)
	if err != nil {
		return fmt.Errorf("sender socket: %w", err)
	}
	defer unix.Close(sender)
	if err := unix.SetsockoptInt(sender, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 0); err != nil {
		return fmt.Errorf("clearing IPV6_V6ONLY: %w", err)
	}

	recv6, port6, err := newSelfCheckReceiver(unix.AF_INET6)
	if err != nil {
		return fmt.Errorf("IPv6 receiver: %w", err)
	}
	defer unix.Close(recv6)
	recv4, port4, err := newSelfCheckReceiver(unix.AF_INET)
	if err != nil {
		return fmt.Errorf("IPv4 receiver: %w", err)
	}
	defer unix.Close(recv4)

	msgs := make([]msghdrX, 2)
	iovs := make([]syscall.Iovec, 2)

	// Native IPv6 destination with the production IPv6 ECN control message.
	payloads6 := [][]byte{
		[]byte("quic-go-fast sendmsg_x self-check 6a"),
		[]byte("quic-go-fast sendmsg_x self-check 6b"),
	}
	dest6 := newSendmsgXDest(&net.UDPAddr{IP: net.IPv6loopback, Port: port6}, unix.AF_INET6)
	oob6 := appendIPv6ECNMsg(nil, protocol.ECT0)
	accepted, errno := sendmsgXBatchTo(sender, payloads6, dest6.name(), dest6.namelen, oob6, msgs, iovs)
	if errno != 0 {
		return fmt.Errorf("IPv6 batch: errno %d", int(errno))
	}
	if accepted != len(payloads6) {
		return fmt.Errorf("IPv6 batch: accepted %d of %d", accepted, len(payloads6))
	}
	if err := verifySelfCheckDelivery(recv6, payloads6, unix.IPPROTO_IPV6, unix.IPV6_TCLASS); err != nil {
		return fmt.Errorf("IPv6 delivery: %w", err)
	}

	// V4-mapped IPv4 destination on the same dual-stack socket with the
	// production IPv4 ECN control message.
	payloads4 := [][]byte{
		[]byte("quic-go-fast sendmsg_x self-check 4a"),
		[]byte("quic-go-fast sendmsg_x self-check 4b"),
	}
	dest4 := newSendmsgXDest(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port4}, unix.AF_INET6)
	oob4 := appendIPv4ECNMsg(nil, protocol.ECT0)
	accepted, errno = sendmsgXBatchTo(sender, payloads4, dest4.name(), dest4.namelen, oob4, msgs, iovs)
	if errno != 0 {
		return fmt.Errorf("v4-mapped batch: errno %d", int(errno))
	}
	if accepted != len(payloads4) {
		return fmt.Errorf("v4-mapped batch: accepted %d of %d", accepted, len(payloads4))
	}
	if err := verifySelfCheckDelivery(recv4, payloads4, unix.IPPROTO_IP, msgTypeIPTOS); err != nil {
		return fmt.Errorf("v4-mapped delivery: %w", err)
	}
	return nil
}

func newSelfCheckSocket(family int) (int, error) {
	fd, err := unix.Socket(family, unix.SOCK_DGRAM, 0)
	if err != nil {
		return -1, err
	}
	unix.CloseOnExec(fd)
	return fd, nil
}

// newSelfCheckReceiver binds a loopback UDP socket of the given family with
// ECN receipt enabled and a receive timeout, returning its port.
func newSelfCheckReceiver(family int) (int, int, error) {
	fd, err := newSelfCheckSocket(family)
	if err != nil {
		return -1, 0, err
	}
	fail := func(err error) (int, int, error) {
		unix.Close(fd)
		return -1, 0, err
	}
	timeout := unix.NsecToTimeval((2 * time.Second).Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &timeout); err != nil {
		return fail(fmt.Errorf("SO_RCVTIMEO: %w", err))
	}
	var sa unix.Sockaddr
	switch family {
	case unix.AF_INET6:
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_RECVTCLASS, 1); err != nil {
			return fail(fmt.Errorf("IPV6_RECVTCLASS: %w", err))
		}
		sa = &unix.SockaddrInet6{Addr: [16]byte{15: 1}}
	case unix.AF_INET:
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_RECVTOS, 1); err != nil {
			return fail(fmt.Errorf("IP_RECVTOS: %w", err))
		}
		sa = &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}
	}
	if err := unix.Bind(fd, sa); err != nil {
		return fail(fmt.Errorf("bind: %w", err))
	}
	bound, err := unix.Getsockname(fd)
	if err != nil {
		return fail(fmt.Errorf("getsockname: %w", err))
	}
	switch sa := bound.(type) {
	case *unix.SockaddrInet6:
		return fd, sa.Port, nil
	case *unix.SockaddrInet4:
		return fd, sa.Port, nil
	}
	return fail(errors.New("unexpected bound address family"))
}

// verifySelfCheckDelivery reads len(want) datagrams from fd and verifies the
// payload set matches exactly (arrival at this socket is the destination
// check) and that every datagram carries an ECT(0) ECN mark in the expected
// control-message level/type.
func verifySelfCheckDelivery(fd int, want [][]byte, cmsgLevel, cmsgType int32) error {
	outstanding := make(map[string]bool, len(want))
	for _, p := range want {
		outstanding[string(p)] = true
	}
	buf := make([]byte, 2048)
	oob := make([]byte, 512)
	for range want {
		n, oobn, _, _, err := unix.Recvmsg(fd, buf, oob, 0)
		if err != nil {
			return fmt.Errorf("recvmsg: %w", err)
		}
		payload := string(buf[:n])
		if !outstanding[payload] {
			return fmt.Errorf("unexpected payload %q", payload)
		}
		delete(outstanding, payload)
		ecnBits, err := selfCheckECNBits(oob[:oobn], cmsgLevel, cmsgType)
		if err != nil {
			return err
		}
		if ecnBits != protocol.ECT0.ToHeaderBits() {
			return fmt.Errorf("ECN bits %#x, want ECT(0)", ecnBits)
		}
	}
	return nil
}

func selfCheckECNBits(oob []byte, level, typ int32) (uint8, error) {
	for len(oob) > 0 {
		hdr, body, remainder, err := unix.ParseOneSocketControlMessage(oob)
		if err != nil {
			return 0, fmt.Errorf("parsing control messages: %w", err)
		}
		if hdr.Level == level && hdr.Type == typ && len(body) >= 1 {
			return body[0] & ecnMask, nil
		}
		oob = remainder
	}
	return 0, fmt.Errorf("no control message with level %d type %d", level, typ)
}
